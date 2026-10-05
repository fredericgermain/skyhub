package skyhub

import (
	"context"
	"fmt"
	"net/url"
	"time"
)

// factoryErasePage is where the Backup page's Erase button sends the
// browser. It asks "Revert to Factory Settings?"; its Yes submits a GET form
// to sky_restoreinfo.scgi with todo=defaultsettings and the page's
// sessionKey, which erases the hub and restarts it.
const factoryErasePage = "sky_backup_settings-erase.html"

// FactoryReset erases the hub's settings and restarts it on its factory
// defaults (http://192.168.0.1/, the sticker password and WiFi), as the
// erase page's Yes does. It returns once the hub has gone down; it comes
// back on DefaultURL, not on this client's address. Refused unless
// AllowDestructiveEnv is set to 1.
func (c *Client) FactoryReset(ctx context.Context) error {
	if !allowDestructive() {
		return ErrDestructive
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	page, err := c.getLocked(ctx, factoryErasePage)
	if err != nil {
		return fmt.Errorf("skyhub: load %s: %w", factoryErasePage, err)
	}
	forms, err := page.Forms()
	if err != nil {
		return err
	}
	var form *Form
	for _, f := range forms {
		if f.Get("todo") == "defaultsettings" {
			form = f
			break
		}
	}
	if form == nil || form.Action == "" {
		return &ParseError{Page: factoryErasePage, What: "no todo=defaultsettings form"}
	}
	key, err := page.SessionKey()
	if err != nil {
		return err
	}
	q := url.Values{}
	for k, v := range form.Values {
		q[k] = v
	}
	q.Set("sessionKey", key)
	if _, err := c.getOnceLocked(ctx, form.Action+"?"+q.Encode()); err != nil && !isConnDrop(err) {
		return fmt.Errorf("skyhub: submit the factory reset: %w", err)
	}
	c.log.Warn("skyhub: factory reset submitted, waiting for the hub to go down")
	return c.waitGone(ctx, 2*time.Minute)
}
