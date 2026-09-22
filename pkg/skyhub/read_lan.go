package skyhub

import (
	"context"
	"regexp"
	"strings"
)

var staticIPListRE = regexp.MustCompile(`\bstaticIPList\s*=\s*'((?:[^'\\]|\\.)*)'`)

// lanPage bundles the two readers that share sky_lan_ip_setup.html.
type lanPage struct {
	Config       *LANConfig
	Reservations []DHCPReservation
}

// LANConfig reads sky_lan_ip_setup.html.
func (c *Client) LANConfig(ctx context.Context) (*LANConfig, error) {
	p, err := c.Get(ctx, "sky_lan_ip_setup.html")
	if err != nil {
		return nil, err
	}
	lp, err := ParseLANPage(p)
	if err != nil {
		return nil, err
	}
	return lp.Config, nil
}

// DHCPReservations reads the reserved-address table.
func (c *Client) DHCPReservations(ctx context.Context) ([]DHCPReservation, error) {
	p, err := c.Get(ctx, "sky_lan_ip_setup.html")
	if err != nil {
		return nil, err
	}
	lp, err := ParseLANPage(p)
	if err != nil {
		return nil, err
	}
	return lp.Reservations, nil
}

// ParseLANConfigPage parses only the LAN configuration.
func ParseLANConfigPage(p *Page) (*LANConfig, error) {
	lp, err := ParseLANPage(p)
	if err != nil {
		return nil, err
	}
	return lp.Config, nil
}

// ParseDHCPReservations parses only the reservation table.
func ParseDHCPReservations(p *Page) ([]DHCPReservation, error) {
	lp, err := ParseLANPage(p)
	if err != nil {
		return nil, err
	}
	return lp.Reservations, nil
}

// ParseLANPage parses config and reservations from sky_lan_ip_setup.html.
// State comes from JS variables; the form's value= attributes are template
// defaults (192.168.0.x) and must not be trusted.
func ParseLANPage(p *Page) (*lanPage, error) {
	ip, err := p.MustJSVar("Ipaddr")
	if err != nil {
		return nil, err
	}
	cfg := &LANConfig{IP: Addr{parseAddr(ip)}}
	v, _ := p.JSVar("dhcpStart")
	cfg.PoolStart = Addr{parseAddr(v)}
	v, _ = p.JSVar("dhcpEnd")
	cfg.PoolEnd = Addr{parseAddr(v)}
	v, _ = p.JSVar("dhcpLease")
	cfg.LeaseHours = parseIntDef(v, 0)
	v, _ = p.JSVar("dhcpEnbl")
	cfg.DHCPEnabled = isTruthy(v)

	// Netmask has no JS var; the c4_ mirror hidden input carries the real
	// value (unlike the visible octet inputs).
	if f, err := p.Form("name=frmLan"); err == nil {
		cfg.Netmask = Addr{parseAddr(f.Get("c4_sysLANSubnetMask"))}
	}
	// IPv6 block: the hidden enbl* inputs mirror the state.
	if f, err := p.Form("id=form_frm_lan"); err == nil {
		cfg.IPv6.RadvdEnabled = isTruthy(f.Get("enblRadvd"))
		cfg.IPv6.DHCP6Enabled = isTruthy(f.Get("enblDhcp6s"))
		cfg.IPv6.MLDQuerier = isTruthy(f.Get("enblMldQuerier"))
		cfg.IPv6.Enabled = f.Get("i_enbleLANIPV6") == "1"
		cfg.IPv6.ULAEnabled = f.Get("i_enblRadvdUla") == "1"
		cfg.IPv6.ULARandom = f.Get("i_ipv6UlaRandom") == "1"
		cfg.IPv6.ULAPrefix = Prefix{parsePrefix(f.Get("i_ipv6UlaPrefix"))}
	}

	lp := &lanPage{Config: cfg}
	// Reservations: attach_dev = 'MAC<br>name<br>IP<br>idx<lf>...' carries the
	// delete index; staticIPList = 'MAC<br>IP<br>name<lf>...' is the same data.
	if m := attachDevRE.FindSubmatch(p.Body); m != nil {
		for _, rec := range splitLF(jsUnescape(string(m[1]))) {
			f := splitBR(rec)
			if len(f) < 4 {
				continue
			}
			r := DHCPReservation{
				MAC:   MAC{parseMAC(f[0])},
				Name:  f[1],
				IP:    Addr{parseAddr(f[2])},
				Index: parseIntDef(f[3], 0),
			}
			if r.MAC.HardwareAddr != nil {
				lp.Reservations = append(lp.Reservations, r)
			}
		}
	} else if m := staticIPListRE.FindSubmatch(p.Body); m != nil {
		for i, rec := range splitLF(jsUnescape(string(m[1]))) {
			f := splitBR(rec)
			if len(f) < 3 {
				continue
			}
			lp.Reservations = append(lp.Reservations, DHCPReservation{
				Index: i + 1, MAC: MAC{parseMAC(f[0])}, IP: Addr{parseAddr(f[1])}, Name: strings.TrimSpace(f[2]),
			})
		}
	}
	return lp, nil
}
