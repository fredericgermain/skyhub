package skyhub_test

import (
	"net/netip"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"testing"

	"github.com/fredericgermain/skyhub/pkg/skyhub"
	"github.com/fredericgermain/skyhub/pkg/skyhubtest"
)

func fixture(t *testing.T, page string) *skyhub.Page {
	t.Helper()
	b, err := os.ReadFile(filepath.Join(fixtureDir, skyhubtest.FixtureName(page)))
	if err != nil {
		t.Fatal(err)
	}
	return skyhub.NewPage(page, b)
}

func mustAddr(s string) netip.Addr { return netip.MustParseAddr(s) }

func TestSessionKeyDeliveryModes(t *testing.T) {
	for _, page := range []string{
		"sky_lan_ip_setup.html",        // var sessionId, hidden input ""
		"sky_firewall_rules-in.html",   // var sessionID, hidden input "0"
		"sky_services.html",            // var sessionKey, hidden input ""
		"sky_wireless_settings.html",   // var sessionKey, hidden "uninitialised"
		"sky_lan_ip_setup_addmac.html", // hidden input only
		"sky_alg.html",
		"sky_upnp.html",
		"sky_wan_setup.html",
		"sky_firewall_rules.html",
	} {
		k, err := fixture(t, page).SessionKey()
		if err != nil {
			t.Errorf("%s: %v", page, err)
			continue
		}
		if k != skyhubtest.SessionKeyPlaceholder {
			t.Errorf("%s: key = %q", page, k)
		}
	}
	if _, err := fixture(t, "sky_system.html").SessionKey(); err == nil {
		t.Error("sky_system.html should have no sessionKey")
	}
}

func TestParseSystemStats(t *testing.T) {
	s, err := skyhub.ParseSystemStats(fixture(t, "sky_system.html"))
	if err != nil {
		t.Fatal(err)
	}
	if s.Uptime == 0 {
		t.Error("uptime = 0")
	}
	if len(s.Ports) != 4 {
		t.Fatalf("ports = %d", len(s.Ports))
	}
	for i, k := range []string{"wan", "lan", "wlan24", "wlan5"} {
		if s.Ports[i].Key != k {
			t.Errorf("port %d key = %q, want %q", i, s.Ports[i].Key, k)
		}
	}
	if s.Port("wan").TxPkts == 0 || s.Port("wan").Uptime == 0 {
		t.Error("wan row not parsed")
	}
	if s.DSL == nil {
		t.Fatal("dsl nil")
	}
	if s.DSL.DownKbps < 1000 || s.DSL.UpKbps < 1000 || s.DSL.NoiseMarginDownDB == 0 || len(s.DSL.AttenuationDownDB) != 3 {
		t.Errorf("dsl = %+v", s.DSL)
	}
}

func TestParseSystemStatsNoDSL(t *testing.T) {
	b := []byte(`<html><span id="router-stati-uptime-value">1:02:03</span>
<table id="router-statistics-top-table"><tr class="header"><td>Port</td></tr>
<tr><td>WAN</td><td>Down</td><td>1</td><td>2</td><td>0</td><td>0</td><td>0</td><td>00:00:00</td></tr></table>
<script>document.write('<tr><td >Connection Speed </td><td>N/A</td><td>N/A</td></tr>');</script></html>`)
	s, err := skyhub.ParseSystemStats(skyhub.NewPage("x", b))
	if err != nil {
		t.Fatal(err)
	}
	if s.DSL != nil {
		t.Error("expected nil DSL")
	}
	if s.Uptime.Std().Seconds() != 3723 {
		t.Errorf("uptime = %v", s.Uptime.Std())
	}
}

func TestParseWANStatus(t *testing.T) {
	w, err := skyhub.ParseWANStatus(fixture(t, "sky_st_poe.html"))
	if err != nil {
		t.Fatal(err)
	}
	if !w.Up || w.Protocol != "ipoe" || w.Interface != "ptm0.1" {
		t.Errorf("wan = %+v", w)
	}
	if !w.IPv4.IsValid() || !w.Gateway.IsValid() || w.Netmask.String() != "255.255.252.0" {
		t.Errorf("addressing = %+v", w)
	}
	if len(w.DNS) != 2 || w.MAC.HardwareAddr == nil || w.Uptime == 0 {
		t.Errorf("dns/mac/uptime = %+v", w)
	}
	if !w.IPv6.IsValid() || w.IPv6.Bits() != 64 || !w.DelegatedPrefix.IsValid() || w.DelegatedPrefix.Bits() != 56 {
		t.Errorf("ipv6 = %v delegated = %v", w.IPv6, w.DelegatedPrefix)
	}
	if w.ModemState != "Connected" || w.RouterMode != "AUTO" {
		t.Errorf("modem/mode = %q %q", w.ModemState, w.RouterMode)
	}
}

func TestParseRouterInfo(t *testing.T) {
	ri, err := skyhub.ParseRouterInfo(fixture(t, "sky_router_status.html"))
	if err != nil {
		t.Fatal(err)
	}
	if ri.Firmware != "7.04.0208.R" || ri.DSLFirmware == "" || ri.TrafficMode != "PTM" {
		t.Errorf("info = %+v", ri)
	}
	if ri.LineRateDownKbps == 0 || ri.LineRateUpKbps == 0 {
		t.Errorf("line rates = %d/%d", ri.LineRateDownKbps, ri.LineRateUpKbps)
	}
	if !ri.WAN.IPv4.IsValid() {
		t.Error("wan not parsed from router status")
	}
}

func TestParseAttachedDevices(t *testing.T) {
	devs, err := skyhub.ParseAttachedDevices(fixture(t, "sky_attached_devices.html"))
	if err != nil {
		t.Fatal(err)
	}
	if len(devs) < 5 {
		t.Fatalf("devices = %d", len(devs))
	}
	var named, v6 int
	for _, d := range devs {
		if d.MAC.HardwareAddr == nil || !d.IPv4.IsValid() {
			t.Errorf("bad device %+v", d)
		}
		if d.DHCPName != "" {
			named++
		}
		if d.IPv6.IsValid() {
			v6++
		}
	}
	if named == 0 || v6 == 0 {
		t.Errorf("named=%d v6=%d", named, v6)
	}
}

func TestParseWirelessRadio(t *testing.T) {
	r, err := skyhub.ParseWirelessRadio(fixture(t, "sky_wireless_settings.html"))
	if err != nil {
		t.Fatal(err)
	}
	if r.SSID != "TestSSID" {
		t.Errorf("ssid = %q (sanitised fixture expected TestSSID)", r.SSID)
	}
	if !r.Enabled || r.AuthMode != "psk2" || r.Cipher != "aes" || !r.WPSEnabled {
		t.Errorf("radio = %+v", r)
	}
	if r.Band != "2.4" && r.Band != "5" {
		t.Errorf("band = %q", r.Band)
	}
}

func TestParseSyslog(t *testing.T) {
	b, err := os.ReadFile(filepath.Join(fixtureDir, "sky_sys.log"))
	if err != nil {
		t.Fatal(err)
	}
	e := skyhub.ParseSyslog(b)
	if len(e) < 10 {
		t.Fatalf("entries = %d", len(e))
	}
	if e[0].Time.IsZero() || e[0].Facility == "" || e[0].Message == "" {
		t.Errorf("first = %+v", e[0])
	}
}

func TestParseLANPage(t *testing.T) {
	cfg, err := skyhub.ParseLANConfigPage(fixture(t, "sky_lan_ip_setup.html"))
	if err != nil {
		t.Fatal(err)
	}
	if cfg.IP.String() != "192.168.50.1" || cfg.Netmask.String() != "255.255.255.0" {
		t.Errorf("lan = %+v", cfg)
	}
	if !cfg.DHCPEnabled || cfg.PoolStart.String() != "192.168.50.2" || cfg.PoolEnd.String() != "192.168.50.254" || cfg.LeaseHours != 24 {
		t.Errorf("dhcp = %+v", cfg)
	}
	res, err := skyhub.ParseDHCPReservations(fixture(t, "sky_lan_ip_setup.html"))
	if err != nil {
		t.Fatal(err)
	}
	if len(res) != 2 {
		t.Fatalf("reservations = %d", len(res))
	}
	if res[0].Index != 1 || res[1].Index != 2 || res[0].IP.String() != "192.168.50.10" || res[0].Name != "nanobox" {
		t.Errorf("reservations = %+v", res)
	}
	if res[0].MAC.String() == "" {
		t.Error("mac empty")
	}
}

func TestParseFirewallConfig(t *testing.T) {
	fw, err := skyhub.ParseFirewallConfig(fixture(t, "sky_firewall_rules.html"))
	if err != nil {
		t.Fatal(err)
	}
	if !fw.Globals.IPv6Firewall || !fw.Globals.IPSecPassthrough || !fw.Globals.AllowInboundICMPv6Echo {
		t.Errorf("globals = %+v", fw.Globals)
	}
	if len(fw.Inbound) != 6 || len(fw.Outbound) != 0 {
		t.Fatalf("in=%d out=%d", len(fw.Inbound), len(fw.Outbound))
	}
	want := []string{"SvcA", "VPN IPSEC", "SvcB", "SvcC", "Any(All)", "SvcD"}
	for i, r := range fw.Inbound {
		if r.Service != want[i] || r.Position != i+1 || r.IPVersion != 4 || r.Direction != skyhub.Inbound {
			t.Errorf("rule %d = %+v", i, r)
		}
		// Fixture bitmask in_enable=000111 (rows are rendered checked regardless).
		if r.Enabled != (i >= 3) {
			t.Errorf("rule %d enabled = %v (bitmask 000111)", i, r.Enabled)
		}
		if r.Action != skyhub.ActionAllowAlways {
			t.Errorf("rule %d action = %q", i, r.Action)
		}
	}
	if fw.Inbound[4].Logging != skyhub.LogAlways || fw.Inbound[0].Logging != skyhub.LogNever {
		t.Errorf("logging = %q %q", fw.Inbound[4].Logging, fw.Inbound[0].Logging)
	}
	if fw.Find(skyhub.Inbound, 4, "SvcD") == nil || fw.Find(skyhub.Outbound, 0, "SvcD") != nil {
		t.Error("Find")
	}
}

func TestParseServices(t *testing.T) {
	s, err := skyhub.ParseServices(fixture(t, "sky_services.html"))
	if err != nil {
		t.Fatal(err)
	}
	if len(s) != 4 {
		t.Fatalf("services = %d", len(s))
	}
	want := []skyhub.Service{
		{1, "SvcA", "tcp_udp", 7001, 7001},
		{2, "SvcB", "udp", 7002, 7002},
		{3, "SvcC", "tcp", 7003, 7003},
		{4, "SvcD", "udp", 7100, 7110},
	}
	for i := range want {
		if s[i] != want[i] {
			t.Errorf("service %d = %+v, want %+v", i, s[i], want[i])
		}
	}
}

func TestParseMiscConfig(t *testing.T) {
	w, err := skyhub.ParseWANConfig(fixture(t, "sky_wan_setup.html"))
	if err != nil {
		t.Fatal(err)
	}
	if w.RouterMode != "AUTO" || w.MTU != 1500 || !w.DMZEnabled || w.DMZIP.String() != "192.168.50.200" || !w.RespondToPing || !w.RespondToPing6 {
		t.Errorf("wan = %+v", w)
	}
	u, err := skyhub.ParseUPnPConfig(fixture(t, "sky_upnp.html"))
	if err != nil {
		t.Fatal(err)
	}
	if !u.Enabled || u.AdvertiseInterval != 30 || u.AdvertiseTTL != 4 {
		t.Errorf("upnp = %+v", u)
	}
	a, err := skyhub.ParseALGConfig(fixture(t, "sky_alg.html"))
	if err != nil {
		t.Fatal(err)
	}
	if !a.SIP || !a.H323 {
		t.Errorf("alg = %+v", a)
	}
	e, err := skyhub.ParseEthernetConfig(fixture(t, "sky_eth_setup.html"))
	if err != nil {
		t.Fatal(err)
	}
	if e.Type != "Gigabit" || e.EEE {
		t.Errorf("eth = %+v", e)
	}
}

func TestParseErrorOnForeignPage(t *testing.T) {
	p := skyhub.NewPage("x", []byte("<html><body>nothing here</body></html>"))
	if _, err := skyhub.ParseWANStatus(p); err == nil {
		t.Error("wan: expected ParseError")
	}
	if _, err := skyhub.ParseSystemStats(p); err == nil {
		t.Error("stats: expected ParseError")
	}
	if _, err := skyhub.ParseFirewallConfig(p); err == nil {
		t.Error("firewall: expected ParseError")
	}
}

var (
	ipv4Tok = regexp.MustCompile(`\b(?:\d{1,3}\.){3}\d{1,3}\b`)
	ipv6Tok = regexp.MustCompile(`(?i)[0-9a-f]{0,4}(?::[0-9a-f]{0,4}){2,7}\b`)
	macTok  = regexp.MustCompile(`(?i)\b[0-9a-f]{2}(?::[0-9a-f]{2}){5}\b`)
	docV4   = []netip.Prefix{
		netip.MustParsePrefix("192.0.2.0/24"), netip.MustParsePrefix("198.51.100.0/24"), netip.MustParsePrefix("203.0.113.0/24"),
	}
	docV6 = netip.MustParsePrefix("2001:db8::/32")
	ulaV6 = netip.MustParsePrefix("fd12:3456:789a::/48") // the fixtures' placeholder ULA
)

// TestFixturesSanitised rejects anything in the captured pages that could be
// a real network's: public IPv4 outside the documentation ranges, global or
// ULA IPv6 outside the placeholder prefixes, and MACs other than the
// locally administered 02:00:00:... placeholders.
func TestFixturesSanitised(t *testing.T) {
	entries, err := os.ReadDir(fixtureDir)
	if err != nil {
		t.Fatal(err)
	}
	for _, e := range entries {
		b, err := os.ReadFile(filepath.Join(fixtureDir, e.Name()))
		if err != nil {
			t.Fatal(err)
		}
		s := string(b)
		for _, tok := range ipv4Tok.FindAllString(s, -1) {
			a, err := netip.ParseAddr(tok)
			if err != nil || a.IsPrivate() || a.IsLoopback() || a.IsUnspecified() || a.IsMulticast() || a.IsLinkLocalUnicast() || a.As4()[0] == 255 {
				continue
			}
			if !slices.ContainsFunc(docV4, func(p netip.Prefix) bool { return p.Contains(a) }) {
				t.Errorf("%s: public IPv4 %s", e.Name(), tok)
			}
		}
		for _, tok := range ipv6Tok.FindAllString(s, -1) {
			a, err := netip.ParseAddr(tok)
			if err != nil || !a.Is6() || a.IsLinkLocalUnicast() || a.IsLoopback() || a.IsUnspecified() || a.IsMulticast() {
				continue
			}
			if a.IsPrivate() && !ulaV6.Contains(a) || a.IsGlobalUnicast() && !a.IsPrivate() && !docV6.Contains(a) {
				t.Errorf("%s: real-looking IPv6 %s", e.Name(), tok)
			}
		}
		for _, tok := range macTok.FindAllString(s, -1) {
			m := strings.ToLower(tok)
			if !strings.HasPrefix(m, "02:00:00:") && m != "ff:ff:ff:ff:ff:ff" && m != "00:00:00:00:00:00" {
				t.Errorf("%s: real-looking MAC %s", e.Name(), tok)
			}
		}
	}
}
