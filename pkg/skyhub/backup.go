package skyhub

import (
	"bytes"
	"context"
	"fmt"
	"net/http"
	"net/url"
	"time"
)

// Backup downloads the hub's full configuration file
// (sky_router_settings.conf), the same file as "Save a Copy of Current
// Settings". It contains secrets (WiFi key, admin password): store it safely.
func (c *Client) Backup(ctx context.Context) ([]byte, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	host, err := c.getLocked(ctx, "sky_backup_settings.html")
	if err != nil {
		return nil, err
	}
	key, err := host.SessionKey()
	if err != nil {
		return nil, err
	}
	q := url.Values{"todo": {"backup"}, "sessionKey": {key}, "this_file": {"sky_backup_settings.html"}, "next_file": {"sky_backup_settings.html"}}
	p, err := c.getOnceLocked(ctx, "sky_router_settings.conf?"+q.Encode())
	if err != nil {
		return nil, err
	}
	if len(p.Body) == 0 || bytes.Contains(bytes.ToLower(p.Body[:min(len(p.Body), 512)]), []byte("<html")) {
		return nil, fmt.Errorf("skyhub: backup returned %d bytes of HTML instead of a settings file", len(p.Body))
	}
	return p.Body, nil
}

// WaitReachable polls the unauthenticated home page until the hub answers
// twice in a row, after an initial pause that lets a reboot take the hub
// down first.
func (c *Client) WaitReachable(ctx context.Context, pause, limit time.Duration) error {
	return c.waitReachable(ctx, pause, limit, 2)
}

// waitReachable is WaitReachable needing oks answers in a row.
func (c *Client) waitReachable(ctx context.Context, pause, limit time.Duration, oks int) error {
	ctx, cancel := context.WithTimeout(ctx, limit)
	defer cancel()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-time.After(pause):
	}
	plain := &http.Client{Timeout: 5 * time.Second}
	interval := min(max(pause/5, 50*time.Millisecond), 3*time.Second)
	ok := 0
	for {
		req, _ := http.NewRequestWithContext(ctx, http.MethodGet, c.url("sky_index.html"), nil)
		if resp, err := plain.Do(req); err == nil {
			resp.Body.Close()
			if resp.StatusCode == http.StatusOK {
				ok++
				if ok == oks {
					return nil
				}
			}
		} else {
			ok = 0
		}
		select {
		case <-ctx.Done():
			return fmt.Errorf("skyhub: hub not back after %s: %w", limit, ctx.Err())
		case <-time.After(interval):
		}
	}
}
