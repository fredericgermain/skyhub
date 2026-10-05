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

// resetRecorder takes the fake hub offline after it answers the factory
// reset GET (sky_restoreinfo.scgi), as the real hub erases and restarts, and
// keeps the query it was sent.
type resetRecorder struct {
	hub   *skyhubtest.FakeHub
	mu    sync.Mutex
	query url.Values
}

func (r *resetRecorder) RoundTrip(req *http.Request) (*http.Response, error) {
	resp, err := http.DefaultTransport.RoundTrip(req)
	if err == nil && req.Method == http.MethodGet && req.URL.Path == "/sky_restoreinfo.scgi" && resp.StatusCode == http.StatusOK {
		r.mu.Lock()
		r.query = req.URL.Query()
		r.mu.Unlock()
		r.hub.GoOffline(time.Second)
	}
	return resp, err
}

func factoryHub(t *testing.T) (*skyhubtest.FakeHub, *skyhub.Client, *resetRecorder) {
	t.Helper()
	t.Setenv(skyhub.AllowDestructiveEnv, "1")
	h := newFakeHubOnly(t)
	h.SetFixture("sky_restoreinfo.scgi", []byte("<html><head><title>Sky Hub &gt; Restore</title></head></html>"))
	rec := &resetRecorder{hub: h}
	c, err := skyhub.New(h.URL(), "admin", "secret12", skyhub.WithPostDelay(0), skyhub.WithTimeout(5*time.Second),
		skyhub.WithRebootPause(10*time.Millisecond), skyhub.WithTransport(rec))
	if err != nil {
		t.Fatal(err)
	}
	return h, c, rec
}

func TestFactoryReset(t *testing.T) {
	h, c, rec := factoryHub(t)
	if err := c.FactoryReset(context.Background()); err != nil {
		t.Fatal(err)
	}
	rec.mu.Lock()
	q := rec.query
	rec.mu.Unlock()
	if q.Get("todo") != "defaultsettings" || q.Get("sessionKey") == "" || q.Get("sessionKey") == skyhubtest.SessionKeyPlaceholder {
		t.Fatalf("reset query = %v, want todo=defaultsettings with the live sessionKey", q)
	}
	if n := len(h.Posts()); n != 0 {
		t.Errorf("%d POSTs sent; the reset is a GET form", n)
	}
}

func TestFactoryResetNoForm(t *testing.T) {
	h, c, _ := factoryHub(t)
	h.SetFixture("sky_backup_settings-erase.html", []byte("<html><body>nothing to submit</body></html>"))
	var pe *skyhub.ParseError
	if err := c.FactoryReset(context.Background()); !errors.As(err, &pe) {
		t.Fatalf("err = %v, want ParseError", err)
	}
}

func TestFactoryResetGuarded(t *testing.T) {
	h := newFakeHubOnly(t)
	c, err := skyhub.New(h.URL(), "admin", "secret12")
	if err != nil {
		t.Fatal(err)
	}
	t.Setenv(skyhub.AllowDestructiveEnv, "")
	if err := c.FactoryReset(context.Background()); !errors.Is(err, skyhub.ErrDestructive) {
		t.Fatalf("err = %v, want ErrDestructive", err)
	}
	if n := h.Challenges(); n != 0 {
		t.Errorf("%d requests reached the hub", n)
	}
}

func TestPostFormGuardsReboot(t *testing.T) {
	h, c := newFake(t)
	reboot := func(_ *skyhub.Page, f *skyhub.Form) error {
		f.Set("todo", "reboot")
		return nil
	}
	t.Setenv(skyhub.AllowDestructiveEnv, "")
	if _, err := c.PostForm(context.Background(), "sky_lanaddmac.sky", "sky_lan_ip_setup.html", "name=frmLan2", reboot); !errors.Is(err, skyhub.ErrDestructive) {
		t.Fatalf("err = %v, want ErrDestructive", err)
	}
	if len(h.Posts()) != 0 {
		t.Fatal("a POST was sent")
	}
	t.Setenv(skyhub.AllowDestructiveEnv, "1")
	if _, err := c.PostForm(context.Background(), "sky_lanaddmac.sky", "sky_lan_ip_setup.html", "name=frmLan2", reboot); err != nil {
		t.Fatal(err)
	}
	if len(h.Posts()) != 1 {
		t.Fatal("allowed POST not sent")
	}
}
