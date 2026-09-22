//go:build live

package skyhub

import (
	"context"
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
