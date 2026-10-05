package skyhub_test

import (
	"context"
	"errors"
	"net/http"
	"net/url"
	"sync"
	"testing"
	"time"

	"github.com/fredericgermain/skyhub/pkg/skyhub"
	"github.com/fredericgermain/skyhub/pkg/skyhubtest"
)

// rebootHub is a fake hub that answers handler like the real one answers a
// save needing a restart (a meta refresh to sky_rebootinfo.html, with a
// matching Location header), and only restarts (goes offline for gap) once
// that page is loaded. It returns a client and how many times the page was
// loaded.
func rebootHub(t *testing.T, handler string, gap time.Duration) (*skyhub.Client, func() int) {
	t.Helper()
	h := newFakeHubOnly(t)
	h.SetFixture("sky_rebootinfo.html", []byte("<html><head><title>Sky Hub &gt; Reboot</title></head><body></body></html>"))
	h.Handle(handler, func(w http.ResponseWriter, r *http.Request, v url.Values) bool {
		w.Header().Set("Location", "sky_rebootinfo.html")
		_, _ = w.Write([]byte("<html><head><meta http-equiv='refresh' content='0; URL=sky_rebootinfo.html' /></head></html>"))
		return true
	})
	rec := &rebootRecorder{hub: h, gap: gap}
	c, err := skyhub.New(h.URL(), "admin", "secret12", skyhub.WithPostDelay(0), skyhub.WithTimeout(5*time.Second),
		skyhub.WithRebootPause(10*time.Millisecond), skyhub.WithTransport(rec))
	if err != nil {
		t.Fatal(err)
	}
	return c, rec.count
}

// rebootRecorder counts authenticated loads of the reboot page and takes
// the fake hub offline after each, as the real hub restarts then.
type rebootRecorder struct {
	hub   *skyhubtest.FakeHub
	gap   time.Duration
	mu    sync.Mutex
	loads int
}

func (r *rebootRecorder) RoundTrip(req *http.Request) (*http.Response, error) {
	resp, err := http.DefaultTransport.RoundTrip(req)
	if err == nil && req.Method == http.MethodGet && req.URL.Path == "/sky_rebootinfo.html" && resp.StatusCode == http.StatusOK {
		r.mu.Lock()
		r.loads++
		r.mu.Unlock()
		r.hub.GoOffline(r.gap)
	}
	return resp, err
}

func (r *rebootRecorder) count() int {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.loads
}

func TestSetLANConfigLoadsRebootPage(t *testing.T) {
	c, loads := rebootHub(t, "sky_lan_ip_setup.sky", 200*time.Millisecond)
	err := c.SetLANConfig(context.Background(), skyhub.LANConfig{IP: skyhub.Addr{mustAddr("192.168.51.1")}, Netmask: skyhub.Addr{mustAddr("255.255.255.0")}, DHCPEnabled: true, PoolStart: skyhub.Addr{mustAddr("192.168.51.2")}, PoolEnd: skyhub.Addr{mustAddr("192.168.51.254")}})
	if !errors.Is(err, skyhub.ErrHubRestarting) {
		t.Fatalf("err = %v, want ErrHubRestarting", err)
	}
	if n := loads(); n != 1 {
		t.Fatalf("reboot page loaded %d times, want 1: the change stays staged without it", n)
	}
}

func TestSetLANConfigPoolOnlyNoRebootPage(t *testing.T) {
	c, loads := rebootHub(t, "sky_lan_ip_setup.sky", 200*time.Millisecond)
	err := c.SetLANConfig(context.Background(), skyhub.LANConfig{IP: skyhub.Addr{mustAddr("192.168.50.1")}, Netmask: skyhub.Addr{mustAddr("255.255.255.0")}, DHCPEnabled: true, PoolStart: skyhub.Addr{mustAddr("192.168.50.5")}, PoolEnd: skyhub.Addr{mustAddr("192.168.50.254")}})
	if err != nil {
		t.Fatal(err)
	}
	if n := loads(); n != 0 {
		t.Fatalf("reboot page loaded %d times for a pool-only change", n)
	}
}

func TestSetEthernetLoadsRebootPage(t *testing.T) {
	c, loads := rebootHub(t, "sky_eth_setup.sky", 200*time.Millisecond)
	start := time.Now()
	if err := c.SetEthernet(context.Background(), skyhub.EthernetConfig{Type: "Fast", EEE: true}); err != nil {
		t.Fatal(err)
	}
	if n := loads(); n != 1 {
		t.Fatalf("reboot page loaded %d times, want 1: the change stays staged without it", n)
	}
	// It waited for the restart, not just for an answer from the hub that
	// had not gone down yet.
	if d := time.Since(start); d < 200*time.Millisecond {
		t.Errorf("returned after %s, before the hub was back", d)
	}
}
