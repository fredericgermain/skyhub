package skyhub

import (
	"context"
	"fmt"
	"strconv"
	"time"
)

// factoryErasePage is where the Backup page's Erase button sends the
// browser (window.location, no form post): "Revert to Factory Default
// Settings" is that one click.
const factoryErasePage = "sky_backup_settings-erase.html"

// FactoryReset erases the hub's settings and restarts it on its factory
// defaults (http://192.168.0.1/, the sticker password and WiFi). It does
// what the browser does: load the erase page, and if the hub does not go
// down on its own, post the todo=factory form that page carries. It returns
// once the hub has gone down; it comes back on DefaultURL, not on this
// client's address. Refused unless AllowDestructiveEnv is set to 1.
func (c *Client) FactoryReset(ctx context.Context) error {
	if !allowDestructive() {
		return ErrDestructive
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	page, err := c.getOnceLocked(ctx, factoryErasePage)
	if err != nil {
		if isConnDrop(err) {
			return nil // went down while answering
		}
		return fmt.Errorf("skyhub: load %s: %w", factoryErasePage, err)
	}
	c.log.Warn("skyhub: loaded the factory erase page, waiting for the hub to go down")
	if c.waitGone(ctx, 2*c.rebootPause) == nil {
		return nil
	}
	// Still up: the page is a confirmation step. Post its factory form.
	forms, err := page.Forms()
	if err != nil {
		return err
	}
	for _, f := range forms {
		if f.Get("todo") != "factory" {
			continue
		}
		handler := f.Action
		if handler == "" {
			handler = factoryErasePage // an empty action posts back to the page
		}
		res, err := c.postFormLocked(ctx, handler, factoryErasePage, "index="+strconv.Itoa(f.Index), nil)
		if err != nil && !postDropped(res, err) {
			return fmt.Errorf("skyhub: post the factory form: %w", err)
		}
		c.log.Warn("skyhub: factory form posted, waiting for the hub to go down", "post_err", err)
		return c.waitGone(ctx, 2*time.Minute)
	}
	return fmt.Errorf("skyhub: the hub stayed up after loading %s, which has no todo=factory form", factoryErasePage)
}
