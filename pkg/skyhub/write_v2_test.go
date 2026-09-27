package skyhub_test

import (
	"context"
	"errors"
	"net/http"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/fredericgermain/skyhub/pkg/skyhub"
)

func TestChangeAdminPassword(t *testing.T) {
	h, c := newFake(t)
	h.Handle("sky_password.scgi", func(w http.ResponseWriter, r *http.Request, v url.Values) bool {
		if v.Get("inOrgPassword") == h.Pass && v.Get("inPassword") == v.Get("inConfirmPasswd") {
			h.Pass = v.Get("inPassword") // the hub now wants the new password
		}
		return false
	})
	if err := c.ChangeAdminPassword(context.Background(), "N3wPassword!"); err != nil {
		t.Fatal(err)
	}
	expect(t, lastPost(t, h, "sky_password.scgi"), map[string]string{
		"inOrgPassword": "secret12", "inPassword": "N3wPassword!", "inConfirmPasswd": "N3wPassword!",
		"inUserName": "admin", "todo": "save_passwd", "sessionIdleTimeout": "5",
	})
	if !c.HasPassword("N3wPassword!") {
		t.Fatal("client did not switch to the new password")
	}
	// Later calls in the same process keep working.
	if _, err := c.SystemStats(context.Background()); err != nil {
		t.Fatalf("after change: %v", err)
	}
	// Same password again: no POST.
	n := len(h.Posts())
	if err := c.ChangeAdminPassword(context.Background(), "N3wPassword!"); err != nil || len(h.Posts()) != n {
		t.Fatalf("no-op change posted or failed: %v", err)
	}
	if err := c.ChangeAdminPassword(context.Background(), "has space\x01"); err == nil {
		t.Error("expected charset error")
	}
}

func TestChangeAdminPasswordRejected(t *testing.T) {
	h, c := newFake(t)
	// Handler leaves the password unchanged: the client must revert.
	if err := c.ChangeAdminPassword(context.Background(), "Other12345"); err == nil {
		t.Fatal("expected an error when the hub keeps the old password")
	}
	if !c.HasPassword("secret12") {
		t.Fatal("client did not revert to the old password")
	}
	if _, err := c.SystemStats(context.Background()); err != nil {
		t.Fatalf("client unusable after rejected change: %v", err)
	}
	_ = h
}

func TestSetWireless24Body(t *testing.T) {
	h, c := newFake(t)
	err := c.SetWireless(context.Background(), skyhub.WirelessSettings{Band: "2.4", Enabled: true, SSID: "Home Net", PSK: "correct horse battery", Channel: 6, Bandwidth: "20/40"})
	if err != nil {
		t.Fatal(err)
	}
	v := lastPost(t, h, "sky_wireless_settings.cgi")
	expect(t, v, map[string]string{
		"wlSsid": "Home Net", "wlWpaPsk": "correct horse battery", "wlBand": "2",
		"wlEnbl": "1", "wifi_enabled": "0", "wlHide": "0", "wifi_hide": "0",
		"secType": "2", "wlAuthMode": "psk2", "wlWpa": "aes", "wlSecMode": "5", "wlPreauth": "1", "wlWep": "disabled",
		"wlChannel": "6", "wlBandWidth": "2", "wlNBwCap": "2", "wlBntWth": "0", "wlSyncNvram": "1",
		"wlWscMode": "enabled", "wlSyncSettings": "1",
	})
	absent(t, v, "Region", "wlTxMode", "wlEncrtype", "wpspin") // disabled inputs
}

func TestSetWireless5BodyAndValidation(t *testing.T) {
	h, c := newFake(t)
	err := c.SetWireless(context.Background(), skyhub.WirelessSettings{Band: "5", Enabled: true, SSID: "Home Net", Hidden: true, Channel: 36, Bandwidth: "80"})
	if err != nil {
		t.Fatal(err)
	}
	v := lastPost(t, h, "sky_wireless_settings.cgi")
	expect(t, v, map[string]string{"wlBand": "1", "wlChannel": "36", "wlNBwCap": "7", "wlHide": "1", "wlWpaPsk": "REDACTED"})
	absent(t, v, "wifi_hide")
	for _, bad := range []skyhub.WirelessSettings{
		{Band: "5", SSID: "x", Channel: 44, Bandwidth: "80"},
		{Band: "2.4", SSID: "x", Channel: 14, Bandwidth: "20"},
		{Band: "2.4", SSID: "x", Channel: 1, Bandwidth: "40"},
		{Band: "2.4", SSID: "x", Channel: 1, Bandwidth: "20", PSK: "short"},
		{Band: "6", SSID: "x"},
	} {
		if err := c.SetWireless(context.Background(), bad); err == nil {
			t.Errorf("expected error for %+v", bad)
		}
	}
}

func TestBackup(t *testing.T) {
	h, c := newFake(t)
	h.SetFixture("sky_router_settings.conf", []byte("<?xml version=\"1.0\"?><DslCpeConfig>…</DslCpeConfig>"))
	b, err := c.Backup(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(b), "DslCpeConfig") {
		t.Fatalf("backup = %q", b)
	}
}

func TestSetLANConfigRestart(t *testing.T) {
	h, c := newFake(t)
	h.Handle("sky_lan_ip_setup.sky", func(w http.ResponseWriter, r *http.Request, v url.Values) bool {
		// The hub restarts: the connection just drops.
		if hj, ok := w.(http.Hijacker); ok {
			conn, _, _ := hj.Hijack()
			conn.Close()
		}
		return true
	})
	err := c.SetLANConfig(context.Background(), skyhub.LANConfig{IP: skyhub.Addr{mustAddr("192.168.51.1")}, Netmask: skyhub.Addr{mustAddr("255.255.255.0")}, DHCPEnabled: true, PoolStart: skyhub.Addr{mustAddr("192.168.51.2")}, PoolEnd: skyhub.Addr{mustAddr("192.168.51.254")}})
	if !errors.Is(err, skyhub.ErrHubRestarting) {
		t.Fatalf("err = %v, want ErrHubRestarting", err)
	}
	// Pool-only change: no restart expected, a dropped connection is an error.
	err = c.SetLANConfig(context.Background(), skyhub.LANConfig{IP: skyhub.Addr{mustAddr("192.168.50.1")}, Netmask: skyhub.Addr{mustAddr("255.255.255.0")}, DHCPEnabled: true, PoolStart: skyhub.Addr{mustAddr("192.168.50.5")}, PoolEnd: skyhub.Addr{mustAddr("192.168.50.254")}})
	if err == nil || errors.Is(err, skyhub.ErrHubRestarting) {
		t.Fatalf("pool-only change with dropped connection: err = %v", err)
	}
}

func TestSetEthernetWaitsForReboot(t *testing.T) {
	h := newFakeHubOnly(t)
	c, _ := skyhub.New(h.URL(), "admin", "secret12", skyhub.WithPostDelay(0), skyhub.WithTimeout(5*time.Second), skyhub.WithRebootPause(10*time.Millisecond))
	h.Handle("sky_eth_setup.sky", func(w http.ResponseWriter, r *http.Request, v url.Values) bool {
		if hj, ok := w.(http.Hijacker); ok {
			conn, _, _ := hj.Hijack()
			conn.Close()
		}
		return true
	})
	start := time.Now()
	if err := c.SetEthernet(context.Background(), skyhub.EthernetConfig{Type: "Fast", EEE: true}); err != nil {
		t.Fatal(err)
	}
	if time.Since(start) > 30*time.Second {
		t.Error("waited too long")
	}
}

func TestAuthRetryAfterNonceRace(t *testing.T) {
	h, c := newFake(t)
	if _, err := c.SystemStats(context.Background()); err != nil {
		t.Fatal(err)
	}
	h.FailAuthCount = 2 // the transport's own retry and the first client retry both fail
	if _, err := c.SystemStats(context.Background()); err != nil {
		t.Fatalf("not retried: %v", err)
	}
	// A client that never authenticated does not retry: wrong credentials fail fast.
	bad, _ := skyhub.New(h.URL(), "admin", "wrong")
	start := time.Now()
	if _, err := bad.SystemStats(context.Background()); !errors.Is(err, skyhub.ErrAuth) {
		t.Fatalf("err = %v", err)
	}
	if time.Since(start) > 400*time.Millisecond {
		t.Error("bad credentials were retried")
	}
}

func TestWirelessReaderPSKSetAndBandwidth(t *testing.T) {
	r, err := skyhub.ParseWirelessRadio(fixture(t, skyhub.WirelessPage5))
	if err != nil {
		t.Fatal(err)
	}
	if r.Band != "5" || r.Bandwidth != "80" || r.Channel != 36 || !r.PSKSet {
		t.Errorf("5 GHz = %+v", r)
	}
}

func TestSetWirelessRidesThroughRadioRestart(t *testing.T) {
	h, c := newFake(t)
	const gap = 300 * time.Millisecond
	posted := make(chan struct{})
	h.Handle("sky_wireless_settings.cgi", func(w http.ResponseWriter, r *http.Request, v url.Values) bool {
		// The radio restarts: this client's WiFi is gone for a while.
		h.GoOffline(gap)
		if hj, ok := w.(http.Hijacker); ok {
			conn, _, _ := hj.Hijack()
			conn.Close()
		}
		close(posted)
		return true
	})
	start := time.Now()
	wifiDone := make(chan time.Time, 1)
	go func() {
		err := c.SetWireless(context.Background(), skyhub.WirelessSettings{Band: "2.4", Enabled: true, SSID: "Home Net", PSK: "correct horse battery", Channel: 6, Bandwidth: "20/40"})
		if err != nil {
			t.Error(err)
		}
		wifiDone <- time.Now()
	}()
	<-posted
	// Another resource applying in parallel queues behind the outage
	// instead of failing in it.
	if _, err := c.Get(context.Background(), "sky_system.html"); err != nil {
		t.Fatalf("concurrent read failed during the WiFi gap: %v", err)
	}
	getDone := time.Now()
	done := <-wifiDone
	if getDone.Before(done) {
		t.Error("concurrent read ran before SetWireless finished waiting")
	}
	if done.Sub(start) < gap {
		t.Errorf("SetWireless returned after %s, before the hub was back", done.Sub(start))
	}
}

func TestSetWirelessHostPageFailureIsNotSubmitted(t *testing.T) {
	// Nothing listens: fetching the host page fails before any POST, which
	// must be an error straight away, not a wait for a radio restart.
	c, _ := skyhub.New("http://127.0.0.1:1/", "admin", "secret12", skyhub.WithTimeout(time.Second))
	start := time.Now()
	err := c.SetWireless(context.Background(), skyhub.WirelessSettings{Band: "5", Enabled: true, SSID: "x", Channel: 36, Bandwidth: "80"})
	if err == nil {
		t.Fatal("no error")
	}
	if time.Since(start) > 5*time.Second {
		t.Errorf("took %s", time.Since(start))
	}
}
