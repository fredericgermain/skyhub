//go:build live

package skyhub

import (
	"context"
	"net"
	"net/netip"
	"os"
	"testing"
	"time"
)

func liveClient(t *testing.T) *Client {
	t.Helper()
	if os.Getenv("SKYHUB_LIVE") == "" {
		t.Skip("SKYHUB_LIVE not set")
	}
	c, err := NewFromEnv(WithTimeout(40 * time.Second))
	if err != nil {
		t.Fatal(err)
	}
	return c
}

func TestLiveGet(t *testing.T) {
	c := liveClient(t)
	ctx := context.Background()
	for _, page := range []string{"sky_system.html", "sky_lan_ip_setup.html", "sky_firewall_rules-in.html", "sky_services.html"} {
		p, err := c.Get(ctx, page)
		if err != nil {
			t.Fatalf("%s: %v", page, err)
		}
		key, err := p.SessionKey()
		if err != nil && page != "sky_system.html" {
			t.Errorf("%s: %v", page, err)
		}
		t.Logf("%s: %d bytes, sessionKey=%s", page, len(p.Body), key)
	}
	if _, err := c.Get(ctx, "sky_does_not_exist.html"); err == nil {
		t.Error("expected error for missing page")
	} else {
		t.Logf("missing page: %v", err)
	}
}

func TestLiveBadPassword(t *testing.T) {
	c := liveClient(t)
	bad, _ := New(c.BaseURL(), "admin", "wrong-password")
	_, err := bad.Get(context.Background(), "sky_system.html")
	if err != ErrAuth {
		t.Fatalf("expected ErrAuth, got %v", err)
	}
}

// TestLiveWriteCycle adds and removes throwaway entries on the real hub:
// reservation 02:00:00:00:00:01, service tfTest (TCP 65001) and an inbound
// firewall rule on it. Everything is cleaned up, including on failure.
func TestLiveWriteCycle(t *testing.T) {
	c := liveClient(t)
	ctx := context.Background()
	mac, _ := net.ParseMAC("02:00:00:00:00:01")

	t.Cleanup(func() {
		_ = c.RemoveFirewallRule(ctx, Inbound, 4, "tfTest")
		_ = c.RemoveService(ctx, "tfTest")
		_ = c.RemoveDHCPReservation(ctx, mac)
	})

	// --- DHCP reservation ---
	if err := c.AddDHCPReservation(ctx, DHCPReservation{MAC: MAC{mac}, IP: Addr{netip.MustParseAddr("192.168.50.250")}, Name: "tftest"}); err != nil {
		t.Fatalf("add reservation: %v", err)
	}
	res, err := c.DHCPReservations(ctx)
	if err != nil {
		t.Fatal(err)
	}
	var got *DHCPReservation
	for i := range res {
		if res[i].MAC.String() == mac.String() {
			got = &res[i]
		}
	}
	if got == nil {
		t.Fatalf("reservation not found after add: %+v", res)
	}
	t.Logf("reservation added: %+v", *got)
	if got.IP.String() != "192.168.50.250" || got.Name != "tftest" {
		t.Errorf("reservation mismatch: %+v", *got)
	}
	if err := c.RemoveDHCPReservation(ctx, mac); err != nil {
		t.Fatalf("remove reservation: %v", err)
	}
	res, _ = c.DHCPReservations(ctx)
	for _, r := range res {
		if r.MAC.String() == mac.String() {
			t.Fatalf("reservation still present after remove")
		}
	}
	t.Log("reservation removed")

	// --- service ---
	if err := c.AddService(ctx, Service{Name: "tfTest", Protocol: "tcp", StartPort: 65001, EndPort: 65001}); err != nil {
		t.Fatalf("add service: %v", err)
	}
	svcs, err := c.Services(ctx)
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, s := range svcs {
		if s.Name == "tfTest" {
			found = true
			if s.Protocol != "tcp" || s.StartPort != 65001 || s.EndPort != 65001 {
				t.Errorf("service mismatch: %+v", s)
			}
		}
	}
	if !found {
		t.Fatalf("service not found after add: %+v", svcs)
	}
	t.Log("service added")

	// --- firewall rule ---
	rule := FirewallRule{Direction: Inbound, Service: "tfTest", Action: ActionAllowAlways, Logging: LogNever, LANStart: "192.168.50.250", WANType: IPTypeAny}
	if err := c.AddFirewallRule(ctx, rule); err != nil {
		t.Fatalf("add firewall rule: %v", err)
	}
	fw, err := c.FirewallConfig(ctx)
	if err != nil {
		t.Fatal(err)
	}
	r := fw.Find(Inbound, 4, "tfTest")
	if r == nil {
		t.Fatalf("rule not found after add: %+v", fw.Inbound)
	}
	t.Logf("rule added: %+v", *r)
	if !r.Enabled || r.Action != ActionAllowAlways {
		t.Errorf("rule mismatch: %+v", *r)
	}
	nRules := len(fw.Inbound)
	before := map[string]bool{}
	for _, o := range fw.Inbound {
		before[o.Service] = o.Enabled
	}

	// disable it
	if err := c.SetFirewallRuleSet(ctx, Inbound, nil, map[string]bool{"tfTest": false}); err != nil {
		t.Fatalf("disable rule: %v", err)
	}
	fw, _ = c.FirewallConfig(ctx)
	if r = fw.Find(Inbound, 4, "tfTest"); r == nil || r.Enabled {
		t.Fatalf("rule not disabled: %+v", r)
	}
	t.Log("rule disabled")
	for _, o := range fw.Inbound {
		if o.Service != "tfTest" && o.Enabled != before[o.Service] {
			t.Errorf("other rule %q changed enabled %v -> %v", o.Service, before[o.Service], o.Enabled)
		}
	}

	// move it to the top
	var order []string
	order = append(order, "tfTest")
	for _, o := range fw.Inbound {
		if o.Service != "tfTest" {
			order = append(order, o.Service)
		}
	}
	if err := c.SetFirewallRuleSet(ctx, Inbound, order, nil); err != nil {
		t.Fatalf("reorder: %v", err)
	}
	fw, _ = c.FirewallConfig(ctx)
	if len(fw.Inbound) != nRules || fw.Inbound[0].Service != "tfTest" {
		t.Errorf("reorder failed: %v", names(fw.Inbound))
	} else {
		t.Logf("rule moved to top: %v", names(fw.Inbound))
	}
	// move it back to the bottom
	order = append(order[1:], "tfTest")
	if err := c.SetFirewallRuleSet(ctx, Inbound, order, nil); err != nil {
		t.Fatalf("reorder back: %v", err)
	}
	fw, _ = c.FirewallConfig(ctx)
	if fw.Inbound[len(fw.Inbound)-1].Service != "tfTest" {
		t.Errorf("reorder back failed: %v", names(fw.Inbound))
	}

	// delete it
	if err := c.RemoveFirewallRule(ctx, Inbound, 4, "tfTest"); err != nil {
		t.Fatalf("remove rule: %v", err)
	}
	fw, _ = c.FirewallConfig(ctx)
	if fw.Find(Inbound, 4, "tfTest") != nil || len(fw.Inbound) != nRules-1 {
		t.Fatalf("rule still present after remove: %v", names(fw.Inbound))
	}
	t.Log("rule removed")

	// --- service removal ---
	if err := c.RemoveService(ctx, "tfTest"); err != nil {
		t.Fatalf("remove service: %v", err)
	}
	svcs, _ = c.Services(ctx)
	for _, s := range svcs {
		if s.Name == "tfTest" {
			t.Fatal("service still present after remove")
		}
	}
	t.Log("service removed")
}

func names(rules []FirewallRule) []string {
	var out []string
	for _, r := range rules {
		out = append(out, r.Service)
	}
	return out
}

// TestLiveResaveIdempotent re-posts the current ALG and UPnP settings.
func TestLiveResaveIdempotent(t *testing.T) {
	c := liveClient(t)
	ctx := context.Background()
	alg, err := c.ALGConfig(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if err := c.SetALG(ctx, *alg); err != nil {
		t.Fatalf("alg: %v", err)
	}
	after, _ := c.ALGConfig(ctx)
	if *after != *alg {
		t.Errorf("alg changed: %+v -> %+v", *alg, *after)
	}
	up, err := c.UPnPConfig(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if err := c.SetUPnP(ctx, *up); err != nil {
		t.Fatalf("upnp: %v", err)
	}
	after2, _ := c.UPnPConfig(ctx)
	if after2.Enabled != up.Enabled || after2.AdvertiseInterval != up.AdvertiseInterval || after2.AdvertiseTTL != up.AdvertiseTTL {
		t.Errorf("upnp changed: %+v -> %+v", *up, *after2)
	}
}
