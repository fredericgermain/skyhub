package skyhub

import (
	"encoding/json"
	"net"
	"net/netip"
	"time"
)

// Duration marshals as whole seconds in JSON.
type Duration time.Duration

func (d Duration) MarshalJSON() ([]byte, error) {
	return json.Marshal(int64(time.Duration(d) / time.Second))
}

func (d *Duration) UnmarshalJSON(b []byte) error {
	var s int64
	if err := json.Unmarshal(b, &s); err != nil {
		return err
	}
	*d = Duration(time.Duration(s) * time.Second)
	return nil
}

// Std returns the time.Duration.
func (d Duration) Std() time.Duration { return time.Duration(d) }

// Addr is a netip.Addr that marshals as "" when unset.
type Addr struct{ netip.Addr }

func (a Addr) MarshalJSON() ([]byte, error) { return json.Marshal(addrString(a.Addr)) }

// String returns the address, or "" when unset.
func (a Addr) String() string { return addrString(a.Addr) }
func (a *Addr) UnmarshalJSON(b []byte) error {
	var s string
	if err := json.Unmarshal(b, &s); err != nil {
		return err
	}
	a.Addr = parseAddr(s)
	return nil
}

// Prefix is a netip.Prefix that marshals as "" when unset.
type Prefix struct{ netip.Prefix }

func (p Prefix) MarshalJSON() ([]byte, error) { return json.Marshal(prefixString(p.Prefix)) }

// String returns the prefix, or "" when unset.
func (p Prefix) String() string { return prefixString(p.Prefix) }
func (p *Prefix) UnmarshalJSON(b []byte) error {
	var s string
	if err := json.Unmarshal(b, &s); err != nil {
		return err
	}
	p.Prefix = parsePrefix(s)
	return nil
}

// MAC is a net.HardwareAddr that marshals as a colon string.
type MAC struct{ net.HardwareAddr }

func (m MAC) MarshalJSON() ([]byte, error) { return json.Marshal(macString(m.HardwareAddr)) }
func (m *MAC) UnmarshalJSON(b []byte) error {
	var s string
	if err := json.Unmarshal(b, &s); err != nil {
		return err
	}
	m.HardwareAddr = parseMAC(s)
	return nil
}

// String returns the colon form, or "".
func (m MAC) String() string { return macString(m.HardwareAddr) }

// ---- statistics ----

// PortStats is one row of the statistics table.
type PortStats struct {
	Key        string   `json:"key"` // wan | lan | wlan24 | wlan5
	Name       string   `json:"name"`
	Status     string   `json:"status"`
	TxPkts     uint64   `json:"tx_pkts"`
	RxPkts     uint64   `json:"rx_pkts"`
	Collisions uint64   `json:"collisions"`
	TxBps      uint64   `json:"tx_bps"`
	RxBps      uint64   `json:"rx_bps"`
	Uptime     Duration `json:"uptime_s"`
}

// DSLStats is the broadband link block of the statistics page.
type DSLStats struct {
	DownKbps          int       `json:"down_kbps"`
	UpKbps            int       `json:"up_kbps"`
	AttenuationDownDB []float64 `json:"attenuation_down_db"`
	AttenuationUpDB   []float64 `json:"attenuation_up_db"`
	NoiseMarginDownDB float64   `json:"noise_margin_down_db"`
	NoiseMarginUpDB   float64   `json:"noise_margin_up_db"`
}

// SystemStats is sky_system.html.
type SystemStats struct {
	Uptime Duration    `json:"uptime_s"`
	Ports  []PortStats `json:"ports"`
	DSL    *DSLStats   `json:"dsl,omitempty"`
}

// Port returns the row with the given key or nil.
func (s *SystemStats) Port(key string) *PortStats {
	for i := range s.Ports {
		if s.Ports[i].Key == key {
			return &s.Ports[i]
		}
	}
	return nil
}

// ---- WAN ----

// WANStatus is decoded from the wanStatus / wanDslLinkConfig blob.
type WANStatus struct {
	Up              bool     `json:"up"`
	Protocol        string   `json:"protocol"`
	VLAN            string   `json:"vlan"`
	Interface       string   `json:"interface"`
	IPv4            Addr     `json:"ipv4"`
	Netmask         Addr     `json:"netmask"`
	Gateway         Addr     `json:"gateway"`
	MAC             MAC      `json:"mac"`
	DNS             []Addr   `json:"dns"`
	Uptime          Duration `json:"uptime_s"`
	IPv6            Prefix   `json:"ipv6"`
	IPv6Gateway     Addr     `json:"ipv6_gateway"`
	DelegatedPrefix Prefix   `json:"delegated_prefix"`
	WANLinkLocal    Addr     `json:"wan_link_local"`
	ModemState      string   `json:"modem_state"`
	RouterMode      string   `json:"router_mode"`
	Raw             []string `json:"-"`
}

// RouterInfo is the static part of sky_router_status.html.
type RouterInfo struct {
	Firmware         string    `json:"firmware"`
	DSLFirmware      string    `json:"dsl_firmware"`
	TrafficMode      string    `json:"traffic_mode"`
	WANInterfaceInfo string    `json:"wan_interface_info"`
	LineRateDownKbps int       `json:"line_rate_down_kbps"`
	LineRateUpKbps   int       `json:"line_rate_up_kbps"`
	WAN              WANStatus `json:"wan"`
}

// ---- devices ----

// AttachedDevice is one entry of the attached devices list. Devices without
// an IPv4 lease still appear (from the ARP/bridge table) with Hostname
// "UNKNOWN" and an IPv4 but no DHCPName.
type AttachedDevice struct {
	MAC      MAC    `json:"mac"`
	Hostname string `json:"hostname"`
	IPv4     Addr   `json:"ipv4"`
	DHCPName string `json:"dhcp_name"`
	IPv6     Addr   `json:"ipv6"`
}

// ---- wireless ----

// WirelessRadio is the state of one band.
type WirelessRadio struct {
	Band         string `json:"band"` // "2.4" | "5"
	Enabled      bool   `json:"enabled"`
	SSID         string `json:"ssid"`
	Hidden       bool   `json:"hidden"`
	Isolation    bool   `json:"isolation"`
	Channel      int    `json:"channel"` // 0 = auto
	Bandwidth    string `json:"bandwidth"`
	SyncSettings bool   `json:"sync_settings"`
	AuthMode     string `json:"auth_mode"`
	Cipher       string `json:"cipher"`
	WPSEnabled   bool   `json:"wps_enabled"`
}

// ---- syslog ----

// SyslogEntry is one line of sky_sys.log.
type SyslogEntry struct {
	Time     time.Time `json:"time"`
	Facility string    `json:"facility"`
	Message  string    `json:"message"`
}

// ---- LAN ----

// LANIPv6 is the IPv6 LAN block.
type LANIPv6 struct {
	Enabled      bool   `json:"enabled"`
	RadvdEnabled bool   `json:"radvd_enabled"`
	DHCP6Enabled bool   `json:"dhcp6_enabled"`
	MLDQuerier   bool   `json:"mld_querier"`
	ULAEnabled   bool   `json:"ula_enabled"`
	ULARandom    bool   `json:"ula_random"`
	ULAPrefix    Prefix `json:"ula_prefix"`
}

// LANConfig is sky_lan_ip_setup.html.
type LANConfig struct {
	IP          Addr    `json:"ip"`
	Netmask     Addr    `json:"netmask"`
	DHCPEnabled bool    `json:"dhcp_enabled"`
	PoolStart   Addr    `json:"pool_start"`
	PoolEnd     Addr    `json:"pool_end"`
	LeaseHours  int     `json:"lease_hours"`
	IPv6        LANIPv6 `json:"ipv6"`
}

// DHCPReservation is one reserved address.
type DHCPReservation struct {
	Index int    `json:"index"` // 1-based, as used by the delete form
	MAC   MAC    `json:"mac"`
	IP    Addr   `json:"ip"`
	Name  string `json:"name"`
}

// ---- firewall ----

// Direction of a firewall rule.
type Direction string

const (
	Inbound  Direction = "in"
	Outbound Direction = "out"
)

// Firewall rule actions and logging modes (hub vocabulary).
const (
	ActionBlockAlways   = "block_always"
	ActionBlockSchedule = "block_schedule"
	ActionAllowAlways   = "allow_always"
	ActionAllowSchedule = "allow_schedule"

	LogNever    = "never"
	LogAlways   = "always"
	LogMatch    = "match"
	LogNotMatch = "not_match"

	IPTypeAny    = "any"
	IPTypeSingle = "single"
	IPTypeRange  = "range"
)

// FirewallRule is one row of the inbound or outbound table.
type FirewallRule struct {
	Direction  Direction `json:"direction"`
	IPVersion  int       `json:"ip_version"`
	Position   int       `json:"position"` // 1-based
	Enabled    bool      `json:"enabled"`
	Service    string    `json:"service"`
	Action     string    `json:"action"`
	LANUsers   string    `json:"lan_users"`   // raw cell, e.g. "192.168.50.200 (7001:7001)" or "Any"
	WANServers string    `json:"wan_servers"` // raw cell
	Logging    string    `json:"logging"`

	// Fields only known when writing (the list page does not show them).
	LANType    string `json:"lan_type,omitempty"`
	LANStart   string `json:"lan_start,omitempty"`
	LANEnd     string `json:"lan_end,omitempty"`
	WANType    string `json:"wan_type,omitempty"`
	WANStart   string `json:"wan_start,omitempty"`
	WANEnd     string `json:"wan_end,omitempty"`
	LANIPv6    string `json:"lan_ipv6,omitempty"`
	WANIPv6    string `json:"wan_ipv6,omitempty"`
	WANIPv6End string `json:"wan_ipv6_end,omitempty"`
}

// FirewallGlobals are the master toggles.
type FirewallGlobals struct {
	IPv6Firewall           bool `json:"ipv6_firewall"`
	IPSecPassthrough       bool `json:"ipsec_passthrough"`
	AllowInboundICMPv6Echo bool `json:"allow_inbound_icmpv6_echo"`
}

// FirewallConfig is sky_firewall_rules.html.
type FirewallConfig struct {
	Globals  FirewallGlobals `json:"globals"`
	Inbound  []FirewallRule  `json:"inbound"`
	Outbound []FirewallRule  `json:"outbound"`
}

// Rules returns the list for a direction.
func (f *FirewallConfig) Rules(d Direction) []FirewallRule {
	if d == Outbound {
		return f.Outbound
	}
	return f.Inbound
}

// Find returns the rule with this service name in a direction.
func (f *FirewallConfig) Find(d Direction, ipVersion int, service string) *FirewallRule {
	rules := f.Rules(d)
	for i := range rules {
		if rules[i].Service == service && (ipVersion == 0 || rules[i].IPVersion == ipVersion) {
			return &rules[i]
		}
	}
	return nil
}

// ---- services ----

// Service is a custom port service definition.
type Service struct {
	Index     int    `json:"index"`
	Name      string `json:"name"`
	Protocol  string `json:"protocol"` // tcp | udp | tcp_udp
	StartPort int    `json:"start_port"`
	EndPort   int    `json:"end_port"`
}

// ---- misc config ----

// WANConfig is sky_wan_setup.html.
type WANConfig struct {
	RouterMode     string `json:"router_mode"` // ADSL | WANOE | AUTO
	MTU            int    `json:"mtu"`
	DMZEnabled     bool   `json:"dmz_enabled"`
	DMZIP          Addr   `json:"dmz_ip"`
	DMZIPv6        Addr   `json:"dmz_ipv6"`
	RespondToPing  bool   `json:"respond_to_ping"`
	RespondToPing6 bool   `json:"respond_to_ping6"`
}

// UPnPConfig is sky_upnp.html.
type UPnPConfig struct {
	Enabled           bool   `json:"enabled"`
	AdvertiseInterval int    `json:"advertise_interval"`
	AdvertiseTTL      int    `json:"advertise_ttl"`
	PortMapTable      string `json:"port_map_table,omitempty"`
}

// ALGConfig is sky_alg.html.
type ALGConfig struct {
	SIP  bool `json:"sip"`
	H323 bool `json:"h323"`
}

// EthernetConfig is sky_eth_setup.html.
type EthernetConfig struct {
	Type string `json:"type"` // Gigabit | Fast
	EEE  bool   `json:"eee"`
}

// WirelessOnOff is sky_wireless_onoff.html.
type WirelessOnOff struct {
	Enabled bool   `json:"enabled"`
	Model   string `json:"model"`
}
