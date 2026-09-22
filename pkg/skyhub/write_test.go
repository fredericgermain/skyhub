package skyhub_test

import (
	"context"
	"errors"
	"net"
	"net/url"
	"testing"

	"github.com/fredericgermain/skyhub/pkg/skyhub"
	"github.com/fredericgermain/skyhub/pkg/skyhubtest"
)

func lastPost(t *testing.T, h *skyhubtest.FakeHub, handler string) url.Values {
	t.Helper()
	posts := h.Posts()
	if len(posts) == 0 {
		t.Fatal("no POST recorded")
	}
	p := posts[len(posts)-1]
	if p.Handler != handler {
		t.Fatalf("posted to %s, want %s", p.Handler, handler)
	}
	return p.Values
}

func expect(t *testing.T, v url.Values, want map[string]string) {
	t.Helper()
	for k, w := range want {
		if got := v.Get(k); got != w {
			t.Errorf("%s = %q, want %q", k, got, w)
		}
	}
	if v.Get("todo") == "reboot" || v.Get("todo") == "factory" {
		t.Fatalf("destructive todo=%s", v.Get("todo"))
	}
}

func absent(t *testing.T, v url.Values, keys ...string) {
	t.Helper()
	for _, k := range keys {
		if _, ok := v[k]; ok {
			t.Errorf("%s should not be submitted (got %q)", k, v.Get(k))
		}
	}
}

func TestAddDHCPReservationBody(t *testing.T) {
	h, c := newFake(t)
	mac, _ := net.ParseMAC("02:00:00:00:00:01")
	err := c.AddDHCPReservation(context.Background(), skyhub.DHCPReservation{MAC: skyhub.MAC{mac}, IP: skyhub.Addr{mustAddr("192.168.50.250")}, Name: "tftest"})
	if err != nil {
		t.Fatal(err)
	}
	// Exact body observed from the browser (Playwright capture, 2026-09-23).
	expect(t, lastPost(t, h, "sky_lanaddmac.sky"), map[string]string{
		"new_lan_ip_addr1": "192", "new_lan_ip_addr2": "168", "new_lan_ip_addr3": "50", "new_lan_ip_addr4": "250",
		"new_lan_ip_num": "{new_mac_num}", "new_lan_mac": "020000000001", "new_lan_devname": "tftest",
		"mac": "addnewmac", "static_ip": "192.168.50.250", "action": "add", "todo": "save",
		"this_file": "sky_lan_ip_setup_addmac.html", "next_file": "sky_lan_ip_setup.html",
	})
}

func TestRemoveDHCPReservationBody(t *testing.T) {
	h, c := newFake(t)
	mac, _ := net.ParseMAC("02:00:00:00:00:02") // second sanitised reservation
	if err := c.RemoveDHCPReservation(context.Background(), mac); err != nil {
		t.Fatal(err)
	}
	expect(t, lastPost(t, h, "sky_lanaddmac.sky"), map[string]string{
		"index": "2", "mac": "", "action": "remove", "todo": "", "next_file": "sky_lan_ip_setup.html",
	})
	unknown, _ := net.ParseMAC("02:00:00:00:00:99")
	if err := c.RemoveDHCPReservation(context.Background(), unknown); !errors.Is(err, skyhub.ErrNotFound) {
		t.Fatalf("err = %v, want ErrNotFound", err)
	}
}

func TestAddServiceBody(t *testing.T) {
	h, c := newFake(t)
	err := c.AddService(context.Background(), skyhub.Service{Name: "tfTest", Protocol: "tcp", StartPort: 65001})
	if err != nil {
		t.Fatal(err)
	}
	// Exact body observed from the browser (dry-run capture, 2026-09-23).
	expect(t, lastPost(t, h, "sky_service_custom-add.sky"), map[string]string{
		"service_name": "tfTest", "svc_type": "TCP", "serv_sport": "65001", "serv_endport": "65001", "todo": "add",
		"this_file": "sky_services_custom-add.html", "next_file": "sky_services.html", "error_file": "sky_services_custom-add_error.html",
	})
	if err := c.AddService(context.Background(), skyhub.Service{Name: "bad name!", Protocol: "tcp", StartPort: 1}); err == nil {
		t.Error("expected name validation error")
	}
}

func TestRemoveServiceBody(t *testing.T) {
	h, c := newFake(t)
	if err := c.RemoveService(context.Background(), "SvcD"); err != nil {
		t.Fatal(err)
	}
	expect(t, lastPost(t, h, "sky_service_custom-add.sky"), map[string]string{"ruleSelect": "SvcD", "todo": "delete", "serviceCnt": "4"})
	if err := c.RemoveService(context.Background(), "nope"); !errors.Is(err, skyhub.ErrNotFound) {
		t.Fatalf("err = %v", err)
	}
}

func TestAddFirewallRuleInboundBody(t *testing.T) {
	h, c := newFake(t)
	err := c.AddFirewallRule(context.Background(), skyhub.FirewallRule{
		Direction: skyhub.Inbound, Service: "SSH", Action: skyhub.ActionAllowAlways, Logging: skyhub.LogAlways,
		LANStart: "192.168.50.250", WANType: skyhub.IPTypeRange, WANStart: "203.0.113.1", WANEnd: "203.0.113.9",
	})
	if err != nil {
		t.Fatal(err)
	}
	v := lastPost(t, h, "sky_firewall_rules.cmd")
	expect(t, v, map[string]string{
		"service_list": "SSH", "h_service_list": "SSH", "fw_action": "2", "h_fw_action": "2", "fw_logging": "1", "h_fw_logging": "1",
		"lan_start_ip1": "192", "lan_start_ip4": "250", "c4_lan_start_ip": "192.168.50.250",
		"fw_waniptype": "rangeip", "h_fw_waniptype": "rangeip",
		"wan_start_ip1": "203", "c4_wan_start_ip": "203.0.113.1", "wan_finish_ip4": "9", "c4_wan_finish_ip": "203.0.113.9",
		"fwall_action": "add", "fw_direction": "1", "fw_ip_version": "4", "edit": "0", "todo": "save",
		"this_file": "sky_firewall_rules-in.html", "next_file": "sky_firewall_rules.html",
	})
	// Any-WAN rule: the disabled WAN octet inputs must not be submitted.
	if err := c.AddFirewallRule(context.Background(), skyhub.FirewallRule{Direction: skyhub.Inbound, Service: "HTTP", Action: skyhub.ActionBlockAlways, LANStart: "192.168.50.7"}); err != nil {
		t.Fatal(err)
	}
	v = lastPost(t, h, "sky_firewall_rules.cmd")
	expect(t, v, map[string]string{"fw_waniptype": "anyip", "fw_action": "0", "fw_logging": "0"})
	absent(t, v, "wan_start_ip1", "wan_finish_ip1")
	// Unknown service is rejected before posting.
	n := len(h.Posts())
	if err := c.AddFirewallRule(context.Background(), skyhub.FirewallRule{Direction: skyhub.Inbound, Service: "nope", Action: skyhub.ActionAllowAlways, LANStart: "10.0.0.1"}); !errors.Is(err, skyhub.ErrNotFound) {
		t.Fatalf("err = %v", err)
	}
	if len(h.Posts()) != n {
		t.Error("posted despite unknown service")
	}
}

func TestAddFirewallRuleOutboundAndIPv6Body(t *testing.T) {
	h, c := newFake(t)
	err := c.AddFirewallRule(context.Background(), skyhub.FirewallRule{
		Direction: skyhub.Outbound, Service: "SSH", Action: skyhub.ActionBlockAlways,
		LANType: skyhub.IPTypeRange, LANStart: "192.168.50.250", LANEnd: "192.168.50.251",
	})
	if err != nil {
		t.Fatal(err)
	}
	v := lastPost(t, h, "sky_firewall_rules.cmd")
	expect(t, v, map[string]string{
		"fw_direction": "0", "fw_ip_version": "4", "fw_laniptype": "rangeip", "h_fw_laniptype": "rangeip",
		"c4_lan_start_ip": "192.168.50.250", "c4_lan_finish_ip": "192.168.50.251", "lan_finish_ip4": "251",
		"this_file": "sky_firewall_rules-out.html",
	})
	err = c.AddFirewallRule(context.Background(), skyhub.FirewallRule{
		Direction: skyhub.Inbound, IPVersion: 6, Service: "SSH", Action: skyhub.ActionAllowAlways,
		LANIPv6: "2001:db8:1:2|::10", WANType: skyhub.IPTypeSingle, WANIPv6: "2001:db8::5",
	})
	if err != nil {
		t.Fatal(err)
	}
	v = lastPost(t, h, "sky_firewall_rules.cmd")
	expect(t, v, map[string]string{
		"fw_ip_version": "6", "lan_start_ip6_prefix": "2001:db8:1:2", "lan_start_ip6": "::10",
		"lan_ip6_start_comb": "2001:db8:1:2::10", "fw_waniptype": "singleip", "wan_start_ip6": "2001:db8::5",
	})
}

func TestRemoveFirewallRuleBody(t *testing.T) {
	h, c := newFake(t)
	if err := c.RemoveFirewallRule(context.Background(), skyhub.Inbound, 4, "SvcC"); err != nil {
		t.Fatal(err)
	}
	v := lastPost(t, h, "sky_firewall_rules.cmd")
	expect(t, v, map[string]string{
		"in_sel": "SvcC", "h_in_sel": "SvcC", "ip_version": "4", "selected_index_edit_delete": "3",
		"fwall_action": "remove", "todo": "delete", "next_file": "sky_firewall_rules.html",
		"inboundRuleCnt": "6", "in_enable": "000111", "ipv6firewallenable": "1",
	})
	if err := c.RemoveFirewallRule(context.Background(), skyhub.Outbound, 0, "SvcC"); !errors.Is(err, skyhub.ErrNotFound) {
		t.Fatalf("err = %v", err)
	}
}

func TestSetFirewallRuleSetBody(t *testing.T) {
	h, c := newFake(t)
	// Move SvcD (pos 6) to the top and disable Any(All).
	order := []string{"SvcD", "SvcA", "VPN IPSEC", "SvcB", "SvcC", "Any(All)"}
	if err := c.SetFirewallRuleSet(context.Background(), skyhub.Inbound, order, map[string]bool{"Any(All)": false}); err != nil {
		t.Fatal(err)
	}
	v := lastPost(t, h, "sky_firewall_rules.cmd")
	expect(t, v, map[string]string{
		"fwall_action": "enable_disable", "todo": "save",
		"in_enable": "100010", "in_fwrule_seq": "6-1-2-3-4-5", "in_fwrule_ipversion_seq": "4-4-4-4-4-4",
		"out_enable": "", "out_fwrule_seq": "", "inboundRuleCnt": "6", "outboundRuleCnt": "0",
	})
	absent(t, v, "in_enable_1", "in_enable_6")
	// Unchanged order: seq stays empty.
	if err := c.SetFirewallRuleSet(context.Background(), skyhub.Inbound, nil, map[string]bool{"SvcD": false}); err != nil {
		t.Fatal(err)
	}
	v = lastPost(t, h, "sky_firewall_rules.cmd")
	expect(t, v, map[string]string{"in_enable": "000110", "in_fwrule_seq": ""})
	if err := c.SetFirewallRuleSet(context.Background(), skyhub.Inbound, []string{"SvcD"}, nil); err == nil {
		t.Error("expected permutation error")
	}
}

func TestSetALGUPnPBodies(t *testing.T) {
	h, c := newFake(t)
	if err := c.SetALG(context.Background(), skyhub.ALGConfig{SIP: false, H323: true}); err != nil {
		t.Fatal(err)
	}
	v := lastPost(t, h, "sky_alg.cgi")
	expect(t, v, map[string]string{"sipalgenable": "0", "h323algenable": "1", "enable_h323_alg": "on", "todo": "save"})
	absent(t, v, "enable_sip_alg")

	if err := c.SetUPnP(context.Background(), skyhub.UPnPConfig{Enabled: false, AdvertiseInterval: 30, AdvertiseTTL: 4}); err != nil {
		t.Fatal(err)
	}
	v = lastPost(t, h, "sky_upnp.cgi")
	expect(t, v, map[string]string{"h_enblUpnp": "disable", "upnpAdvTime": "30", "upnpAdvTTL": "4", "todo": "apply"})
	absent(t, v, "enblUpnp")
}

func TestSetWANConfigBody(t *testing.T) {
	h, c := newFake(t)
	err := c.SetWANConfig(context.Background(), skyhub.WANConfig{RouterMode: "AUTO", MTU: 1500, DMZEnabled: true, DMZIP: skyhub.Addr{mustAddr("192.168.50.200")}, RespondToPing: true, RespondToPing6: false})
	if err != nil {
		t.Fatal(err)
	}
	v := lastPost(t, h, "sky_wansetup.sky")
	expect(t, v, map[string]string{
		"router_mode": "AUTO", "h_rmode": "AUTO", "mtu_size": "1500", "dmz_enable": "dmz_enable",
		"dmzip4": "200", "address": "192.168.50.200", "rspToPing": "1", "h_rspToPing": "enable", "h_rspToPing6": "disable",
		"wanport": "0", "h_wanport": "0", "todo": "save",
	})
	absent(t, v, "rspToPing6")
}

func TestSetLANEthernetWirelessBodies(t *testing.T) {
	h, c := newFake(t)
	err := c.SetLANConfig(context.Background(), skyhub.LANConfig{IP: skyhub.Addr{mustAddr("192.168.50.1")}, Netmask: skyhub.Addr{mustAddr("255.255.255.0")}, DHCPEnabled: true, PoolStart: skyhub.Addr{mustAddr("192.168.50.2")}, PoolEnd: skyhub.Addr{mustAddr("192.168.50.254")}})
	if err != nil {
		t.Fatal(err)
	}
	expect(t, lastPost(t, h, "sky_lan_ip_setup.sky"), map[string]string{
		"c4_sysLANIPAddr": "192.168.50.1", "c4_sysLANSubnetMask": "255.255.255.0", "c4_sysPoolStartingAddr": "192.168.50.2",
		"c4_sysPoolFinishAddr": "192.168.50.254", "h_dhcp_server": "enable", "dhcp_server": "on", "todo": "save",
	})

	n := len(h.Posts())
	if err := c.SetEthernet(context.Background(), skyhub.EthernetConfig{Type: "Gigabit", EEE: false}); err != nil {
		t.Fatal(err)
	}
	if len(h.Posts()) != n {
		t.Error("unchanged ethernet config should not post")
	}
	if err := c.SetEthernet(context.Background(), skyhub.EthernetConfig{Type: "Fast", EEE: true}); err != nil {
		t.Fatal(err)
	}
	expect(t, lastPost(t, h, "sky_eth_setup.sky"), map[string]string{"ethernet_type": "Fast", "ethernet_eee": "enabled"})

	if err := c.SetWirelessEnabled(context.Background(), false); err != nil {
		t.Fatal(err)
	}
	v := lastPost(t, h, "sky_wireless_update.cmd")
	expect(t, v, map[string]string{"h_enable_ap": "0", "todo": "save"})
	absent(t, v, "enable_ap")
}
