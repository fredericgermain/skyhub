package main

import (
	"context"
	"encoding/json"
	"fmt"
	"net/netip"
	"strings"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/fredericgermain/skyhub/pkg/skyhub"
)

// toolNames lists every registered tool; tools_test.go checks each is
// read-only.
var toolNames []string

// newServer builds the MCP server over one hub client. Every tool is
// read-only; there is deliberately no way to change hub configuration.
func newServer(c *skyhub.Client, version string) *mcp.Server {
	s := mcp.NewServer(&mcp.Implementation{Name: "skyhub", Version: version}, &mcp.ServerOptions{
		Instructions: "Read-only access to a Sky Hub home router: link statistics, WAN status, " +
			"connected devices, WiFi, LAN/DHCP, firewall rules, port services, syslog and " +
			"ping/DNS diagnostics run from the router itself. Configuration changes are not " +
			"available here (use the Terraform provider).",
	})
	toolNames = nil

	add(s, "skyhub_system_stats",
		"Router uptime, per-port packet/byte-rate counters (WAN, LAN, WLAN 2.4 GHz, WLAN 5 GHz) and DSL line stats (sync speed, attenuation, noise margin). Use for link health and throughput questions.",
		func(ctx context.Context, _ empty) (any, error) { return c.SystemStats(ctx) })

	add(s, "skyhub_wan_status",
		"WAN connection state: public IPv4/IPv6, gateway, DNS servers, MAC, connection uptime, delegated IPv6 prefix, modem state, plus router firmware version.",
		func(ctx context.Context, _ empty) (any, error) {
			w, err := c.WANStatus(ctx)
			if err != nil {
				return nil, err
			}
			out := wanStatusOut{WANStatus: *w}
			if ri, err := c.RouterInfo(ctx); err == nil {
				out.Firmware = ri.Firmware
				out.DSLFirmware = ri.DSLFirmware
			}
			return out, nil
		})

	add(s, "skyhub_attached_devices",
		"Devices currently known to the router (MAC, hostname, IPv4, DHCP name, IPv6). Use to find what is on the LAN or to look up a device's address.",
		func(ctx context.Context, _ empty) (any, error) {
			d, err := c.AttachedDevices(ctx)
			return listOut[skyhub.AttachedDevice]{"devices", d}, err
		})

	add(s, "skyhub_wireless",
		"WiFi radio settings for both bands: enabled, SSID, hidden, channel, bandwidth, auth mode, WPS. The passphrase is never returned.",
		func(ctx context.Context, _ empty) (any, error) {
			r, err := c.WirelessRadios(ctx)
			return listOut[skyhub.WirelessRadio]{"radios", r}, err
		})

	add(s, "skyhub_syslog",
		"Router syslog entries (DSL rate changes, DHCP, firewall, WAN events). Returns the most recent `tail` entries (default 200), optionally filtered by a case-insensitive substring.",
		func(ctx context.Context, in syslogIn) (any, error) {
			e, err := c.Syslog(ctx)
			if err != nil {
				return nil, err
			}
			if in.Grep != "" {
				g := strings.ToLower(in.Grep)
				var f []skyhub.SyslogEntry
				for _, x := range e {
					if strings.Contains(strings.ToLower(x.Message), g) || strings.Contains(strings.ToLower(x.Facility), g) {
						f = append(f, x)
					}
				}
				e = f
			}
			tail := in.Tail
			if tail <= 0 {
				tail = 200
			}
			if len(e) > tail {
				e = e[len(e)-tail:]
			}
			return listOut[skyhub.SyslogEntry]{"entries", e}, nil
		})

	add(s, "skyhub_lan_config",
		"LAN configuration: router LAN IP, netmask, DHCP server enabled, DHCP pool range, lease time and the IPv6 LAN settings.",
		func(ctx context.Context, _ empty) (any, error) { return c.LANConfig(ctx) })

	add(s, "skyhub_dhcp_reservations",
		"DHCP address reservations (MAC to fixed IPv4 with a name) configured on the router.",
		func(ctx context.Context, _ empty) (any, error) {
			r, err := c.DHCPReservations(ctx)
			return listOut[skyhub.DHCPReservation]{"reservations", r}, err
		})

	add(s, "skyhub_firewall_rules",
		"Firewall configuration: master toggles (IPv6 firewall, IPsec passthrough, ICMPv6 echo) and the ordered inbound and outbound rule lists (service, action, LAN/WAN scope, logging, enabled). Optionally restrict to direction 'in' or 'out'.",
		func(ctx context.Context, in firewallIn) (any, error) {
			fw, err := c.FirewallConfig(ctx)
			if err != nil {
				return nil, err
			}
			switch in.Direction {
			case "in":
				fw.Outbound = nil
			case "out":
				fw.Inbound = nil
			case "":
			default:
				return nil, fmt.Errorf("direction must be \"in\" or \"out\"")
			}
			return fw, nil
		})

	add(s, "skyhub_services",
		"Custom port service definitions (name, protocol, port range) that firewall rules reference. Set include_builtin to also list the router's predefined service names.",
		func(ctx context.Context, in servicesIn) (any, error) {
			svc, err := c.Services(ctx)
			if err != nil {
				return nil, err
			}
			out := servicesOut{Services: svc}
			if in.IncludeBuiltin {
				if b, err := c.BuiltinServices(ctx); err == nil {
					out.Builtin = b
				} else {
					return nil, err
				}
			}
			return out, nil
		})

	add(s, "skyhub_wan_config",
		"WAN setup: router mode (ADSL/WANOE/AUTO), MTU, DMZ host, and whether the router answers pings on the WAN side.",
		func(ctx context.Context, _ empty) (any, error) { return c.WANConfig(ctx) })

	add(s, "skyhub_upnp",
		"UPnP settings (enabled, advertisement interval and TTL).",
		func(ctx context.Context, _ empty) (any, error) { return c.UPnPConfig(ctx) })

	add(s, "skyhub_alg",
		"Application Layer Gateway toggles for SIP and H.323.",
		func(ctx context.Context, _ empty) (any, error) { return c.ALGConfig(ctx) })

	add(s, "skyhub_ethernet",
		"Ethernet port settings (Gigabit or Fast, Energy Efficient Ethernet).",
		func(ctx context.Context, _ empty) (any, error) { return c.EthernetConfig(ctx) })

	add(s, "skyhub_ping",
		"Ping an IPv4 or IPv6 address from the router itself (4 packets, takes a few seconds). Returns raw output plus sent/received counts and round-trip times. Use to test reachability from the router's side.",
		func(ctx context.Context, in pingIn) (any, error) {
			a, err := netip.ParseAddr(strings.TrimSpace(in.Target))
			if err != nil {
				return nil, fmt.Errorf("target must be an IP address: %w", err)
			}
			return c.Ping(ctx, a)
		})

	add(s, "skyhub_dns_lookup",
		"Resolve a hostname using the router's own DNS resolver (IPv4 by default, IPv6 with ipv6=true). Also returns the upstream DNS servers the router uses.",
		func(ctx context.Context, in dnsIn) (any, error) {
			return c.DNSLookup(ctx, in.Name, in.IPv6)
		})

	return s
}

type empty struct{}

type syslogIn struct {
	Tail int    `json:"tail,omitempty" jsonschema:"number of most recent entries to return (default 200)"`
	Grep string `json:"grep,omitempty" jsonschema:"case-insensitive substring filter on message or facility"`
}

type firewallIn struct {
	Direction string `json:"direction,omitempty" jsonschema:"restrict to 'in' (inbound) or 'out' (outbound) rules"`
}

type servicesIn struct {
	IncludeBuiltin bool `json:"include_builtin,omitempty" jsonschema:"also return the router's predefined service names"`
}

type pingIn struct {
	Target string `json:"target" jsonschema:"IPv4 or IPv6 address to ping"`
}

type dnsIn struct {
	Name string `json:"name" jsonschema:"hostname to resolve"`
	IPv6 bool   `json:"ipv6,omitempty" jsonschema:"look up AAAA records instead of A"`
}

type wanStatusOut struct {
	skyhub.WANStatus
	Firmware    string `json:"firmware"`
	DSLFirmware string `json:"dsl_firmware"`
}

type servicesOut struct {
	Services []skyhub.Service `json:"services"`
	Builtin  []string         `json:"builtin,omitempty"`
}

// listOut wraps a slice under a named key so the output is an object.
type listOut[T any] struct {
	key   string
	items []T
}

func (l listOut[T]) MarshalJSON() ([]byte, error) {
	items := l.items
	if items == nil {
		items = []T{}
	}
	return json.Marshal(map[string]any{l.key: items})
}

// add registers a read-only tool. The handler's value is returned both as
// structured content and as pretty JSON text; errors become tool errors.
func add[In any](s *mcp.Server, name, desc string, fn func(context.Context, In) (any, error)) {
	toolNames = append(toolNames, name)
	ro, open := true, true
	mcp.AddTool(s, &mcp.Tool{
		Name:        name,
		Description: desc,
		Annotations: &mcp.ToolAnnotations{ReadOnlyHint: ro, IdempotentHint: true, OpenWorldHint: &open},
	}, func(ctx context.Context, _ *mcp.CallToolRequest, in In) (*mcp.CallToolResult, any, error) {
		v, err := fn(ctx, in)
		if err != nil {
			return nil, nil, err
		}
		b, err := json.MarshalIndent(v, "", "  ")
		if err != nil {
			return nil, nil, err
		}
		var structured any
		if err := json.Unmarshal(b, &structured); err != nil {
			return nil, nil, err
		}
		return &mcp.CallToolResult{
			Content:           []mcp.Content{&mcp.TextContent{Text: string(b)}},
			StructuredContent: structured,
		}, nil, nil
	})
}
