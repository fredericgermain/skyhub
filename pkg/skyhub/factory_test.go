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
	"github.com/fredericgermain/skyhub/pkg/skyhubtest"
)

// offlineOnGet takes the fake hub offline for gap after it answers a GET of
// path, like a hub that acts on a page load.
type offlineOnGet struct {
	hub  *skyhubtest.FakeHub
	path string
	gap  time.Duration
}

func (o offlineOnGet) RoundTrip(req *http.Request) (*http.Response, error) {
	resp, err := http.DefaultTransport.RoundTrip(req)
	if err == nil && o.path != "" && req.Method == http.MethodGet && req.URL.Path == "/"+o.path && resp.StatusCode == http.StatusOK {
		o.hub.GoOffline(o.gap)
	}
	return resp, err
}

func factoryHub(t *testing.T, erasePage, offlinePath string) (*skyhubtest.FakeHub, *skyhub.Client) {
	t.Helper()
	t.Setenv(skyhub.AllowDestructiveEnv, "1")
	h := newFakeHubOnly(t)
	h.SetFixture("sky_backup_settings-erase.html", []byte(erasePage))
	c, err := skyhub.New(h.URL(), "admin", "secret12", skyhub.WithPostDelay(0), skyhub.WithTimeout(5*time.Second),
		skyhub.WithRebootPause(10*time.Millisecond), skyhub.WithTransport(offlineOnGet{hub: h, path: offlinePath, gap: time.Second}))
	if err != nil {
		t.Fatal(err)
	}
	return h, c
}

func TestFactoryResetOnPageLoad(t *testing.T) {
	h, c := factoryHub(t, "<html><head><title>Sky Hub &gt; Erasing</title></head></html>", "sky_backup_settings-erase.html")
	if err := c.FactoryReset(context.Background()); err != nil {
		t.Fatal(err)
	}
	if n := len(h.Posts()); n != 0 {
		t.Errorf("%d POSTs sent; loading the page was enough", n)
	}
}

func TestFactoryResetPostsConfirmForm(t *testing.T) {
	page := `<html><head><script>var sessionId = '` + skyhubtest.SessionKeyPlaceholder + `';</script></head><body>
<form name="erase" method="post" action="">
<input name="todo" value="factory" type="hidden"/>
<input name="sessionKey" value="" type="hidden"/>
<input name="this_file" value="sky_backup_settings-erase.html" type="hidden"/>
<input name="next_file" value="sky_rebootinfo.html" type="hidden"/>
</form></body></html>`
	h, c := factoryHub(t, page, "")
	h.Handle("sky_backup_settings-erase.html", func(w http.ResponseWriter, r *http.Request, v url.Values) bool {
		h.GoOffline(time.Second) // the hub erases and restarts
		return false
	})
	if err := c.FactoryReset(context.Background()); err != nil {
		t.Fatal(err)
	}
	posts := h.Posts()
	if len(posts) != 1 || posts[0].Handler != "sky_backup_settings-erase.html" || posts[0].Values.Get("todo") != "factory" {
		t.Fatalf("posts = %+v, want one todo=factory to the erase page", posts)
	}
}

func TestFactoryResetFailsWhenHubStaysUp(t *testing.T) {
	_, c := factoryHub(t, "<html><body>nothing to submit</body></html>", "")
	err := c.FactoryReset(context.Background())
	if err == nil || !strings.Contains(err.Error(), "no todo=factory form") {
		t.Fatalf("err = %v", err)
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
