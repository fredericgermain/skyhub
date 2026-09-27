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

const fixtureDir = "testdata/7.04.0208.R"

func newFake(t *testing.T) (*skyhubtest.FakeHub, *skyhub.Client) {
	t.Helper()
	h := skyhubtest.NewFakeHub(t, fixtureDir, "admin", "secret12")
	c, err := skyhub.New(h.URL(), "admin", "secret12", skyhub.WithPostDelay(0), skyhub.WithTimeout(5*time.Second), skyhub.WithRebootPause(10*time.Millisecond))
	if err != nil {
		t.Fatal(err)
	}
	return h, c
}

func TestDigestPreemptive(t *testing.T) {
	h, c := newFake(t)
	ctx := context.Background()
	for i := 0; i < 3; i++ {
		if _, err := c.Get(ctx, "sky_system.html"); err != nil {
			t.Fatalf("get %d: %v", i, err)
		}
	}
	if got := h.Challenges(); got != 1 {
		t.Errorf("challenges = %d, want 1 (pre-emptive auth after the first)", got)
	}
}

func TestDigestBadPassword(t *testing.T) {
	h, _ := newFake(t)
	c, _ := skyhub.New(h.URL(), "admin", "wrong")
	_, err := c.Get(context.Background(), "sky_system.html")
	if !errors.Is(err, skyhub.ErrAuth) {
		t.Fatalf("err = %v, want ErrAuth", err)
	}
}

func TestDigestStaleNonceRecovery(t *testing.T) {
	h, c := newFake(t)
	h.RotateNonceEvery = 2
	ctx := context.Background()
	for i := 0; i < 6; i++ {
		if _, err := c.Get(ctx, "sky_system.html"); err != nil {
			t.Fatalf("get %d: %v", i, err)
		}
	}
}

func TestGetNotFound(t *testing.T) {
	_, c := newFake(t)
	_, err := c.Get(context.Background(), "nope.html")
	var he *skyhub.HTTPError
	if !errors.As(err, &he) || he.Status != http.StatusNotFound {
		t.Fatalf("err = %v, want HTTPError 404", err)
	}
}

func TestGetWithQuery(t *testing.T) {
	h, c := newFake(t)
	h.SetFixture("sky_wireless_band.cgi?band=5GHz", []byte("<html>var sky_wlBand = '1';</html>"))
	p, err := c.Get(context.Background(), "sky_wireless_band.cgi?band=5GHz")
	if err != nil {
		t.Fatal(err)
	}
	if v, _ := p.JSVar("sky_wlBand"); v != "1" {
		t.Fatalf("band = %q", v)
	}
}

func TestPostFormUsesFreshKey(t *testing.T) {
	h, c := newFake(t)
	var keyAtBuild string
	res, err := c.PostForm(context.Background(), "sky_lanaddmac.sky", "sky_lan_ip_setup.html", "name=frmLan2", func(host *skyhub.Page, f *skyhub.Form) error {
		keyAtBuild = h.CurrentKey()
		f.Set("index", "2")
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	posts := h.Posts()
	if len(posts) != 1 {
		t.Fatalf("posts = %d", len(posts))
	}
	v := posts[0].Values
	if v.Get("sessionKey") != keyAtBuild || keyAtBuild == "" {
		t.Errorf("sessionKey %q != key issued by host GET %q", v.Get("sessionKey"), keyAtBuild)
	}
	for k, want := range map[string]string{"index": "2", "action": "remove", "todo": "", "next_file": "sky_lan_ip_setup.html"} {
		if v.Get(k) != want {
			t.Errorf("%s = %q, want %q", k, v.Get(k), want)
		}
	}
	if res.Status != http.StatusFound {
		t.Errorf("status = %d", res.Status)
	}
}

func TestPostFormRetriesOnStaleKey(t *testing.T) {
	h, c := newFake(t)
	h.RejectPostsOnce = true
	_, err := c.PostForm(context.Background(), "sky_alg.cgi", "sky_alg.html", "name=formname", nil)
	if err != nil {
		t.Fatal(err)
	}
	posts := h.Posts()
	if len(posts) < 2 {
		t.Fatalf("posts = %d, want at least 2 (rejected, then retried with a fresh key)", len(posts))
	}
	first, last := posts[0].Values.Get("sessionKey"), posts[len(posts)-1].Values.Get("sessionKey")
	if first == last {
		t.Error("retry reused the same sessionKey")
	}
	if last != "" && last == h.CurrentKey() {
		t.Error("key not consumed by successful POST")
	}
}

func TestPostFormErrorPage(t *testing.T) {
	h, c := newFake(t)
	h.Handle("sky_service_custom-add.sky", func(w http.ResponseWriter, r *http.Request, v url.Values) bool {
		http.Redirect(w, r, "/"+v.Get("error_file"), http.StatusFound)
		return true
	})
	_, err := c.PostForm(context.Background(), "", "sky_services_custom-add.html", "action=sky_service_custom-add.sky", func(_ *skyhub.Page, f *skyhub.Form) error {
		f.Set("service_name", "x")
		return nil
	})
	var he *skyhub.HubError
	if !errors.As(err, &he) {
		t.Fatalf("err = %v, want HubError", err)
	}
	if he.ErrorPage != "/sky_services_custom-add_error.html" {
		t.Errorf("error page = %q", he.ErrorPage)
	}
}

func TestPostFormRefusesReboot(t *testing.T) {
	h, c := newFake(t)
	_, err := c.PostForm(context.Background(), "sky_lanaddmac.sky", "sky_lan_ip_setup.html", "name=frmLan2", func(_ *skyhub.Page, f *skyhub.Form) error {
		f.Set("todo", "reboot")
		return nil
	})
	if err == nil {
		t.Fatal("expected refusal")
	}
	if len(h.Posts()) != 0 {
		t.Fatal("a POST was sent")
	}
}

func TestDataToHidden(t *testing.T) {
	h, c := newFake(t)
	_, err := c.PostForm(context.Background(), "sky_lan_ip_setup.sky", "sky_lan_ip_setup.html", "name=frmLan", func(_ *skyhub.Page, f *skyhub.Form) error {
		f.SetIPv4("sysLANIPAddr", mustAddr("10.1.2.3"))
		f.SetCheckbox("dhcp_server", false)
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	v := h.Posts()[0].Values
	if v.Get("c4_sysLANIPAddr") != "10.1.2.3" {
		t.Errorf("c4_sysLANIPAddr = %q", v.Get("c4_sysLANIPAddr"))
	}
	if v.Get("h_dhcp_server") != "disable" {
		t.Errorf("h_dhcp_server = %q", v.Get("h_dhcp_server"))
	}
	if _, present := v["dhcp_server"]; present {
		t.Error("unchecked checkbox was submitted")
	}
	// Untouched mirrors keep the hub's own values.
	if v.Get("c4_sysLANSubnetMask") != "255.255.255.0" {
		t.Errorf("c4_sysLANSubnetMask = %q", v.Get("c4_sysLANSubnetMask"))
	}
}

func TestSerialised(t *testing.T) {
	_, c := newFake(t)
	var wg sync.WaitGroup
	errs := make(chan error, 10)
	for i := 0; i < 10; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if _, err := c.Get(context.Background(), "sky_alg.html"); err != nil {
				errs <- err
			}
		}()
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		t.Error(err)
	}
}

func newFakeHubOnly(t *testing.T) *skyhubtest.FakeHub {
	t.Helper()
	return skyhubtest.NewFakeHub(t, fixtureDir, "admin", "secret12")
}
