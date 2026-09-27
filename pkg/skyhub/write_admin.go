package skyhub

import (
	"context"
	"fmt"
	"strings"
)

// pwdGoodChars is the character set sky_set_password.html accepts.
const pwdGoodChars = "0123456789abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ!@#$%^&*()_+{}:\"|<>?-=[];,.'/"

var pwdStatusMessages = map[string]string{
	"1": "old and new passwords must be different",
	"2": "the old password was not accepted",
	"3": "old and new passwords must be different",
	"4": "remote management is enabled: the new password must not be the default one",
	"5": "the password contains invalid characters",
	"6": "the new password must not be \"sky\"",
	"7": "the new password must not be \"nowtv\"",
	"8": "the new password must be at least 10 characters with upper case, lower case and a digit",
}

// ChangeAdminPassword sets the hub's admin password (sky_set_password.html)
// using the client's current password as the old one. On success the
// client switches to the new password in place, so later calls in the same
// process keep working. It is a no-op when newPass is already the client's
// password.
func (c *Client) ChangeAdminPassword(ctx context.Context, newPass string) error {
	if len(newPass) < 1 || len(newPass) > 16 {
		return fmt.Errorf("skyhub: admin password must be 1-16 characters")
	}
	for _, r := range newPass {
		if !strings.ContainsRune(pwdGoodChars, r) {
			return fmt.Errorf("skyhub: admin password contains %q, not accepted by the hub", r)
		}
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	dt := c.digest()
	user, oldPass := dt.credentials()
	if newPass == oldPass {
		return nil
	}
	build := func(_ *Page, f *Form) error {
		f.Set("inOrgPassword", oldPass)
		f.Set("inPassword", newPass)
		f.Set("inConfirmPasswd", newPass)
		if f.Get("sessionIdleTimeout") == "" {
			f.Set("sessionIdleTimeout", "5")
		}
		f.Set("inUserName", user)
		f.Set("todo", "save_passwd")
		return nil
	}
	var err error
	for attempt := 0; attempt < 2; attempt++ {
		_, err = c.postFormLocked(ctx, "sky_password.scgi", "sky_set_password.html", "action=sky_password.scgi", build)
		if err != ErrStaleKey {
			break
		}
	}
	if err != nil && err != ErrStaleKey {
		return fmt.Errorf("skyhub: change admin password: %w", err)
	}
	// Verify with the new password; fall back to the old one to report why.
	dt.setCredentials(user, newPass)
	if _, verr := c.getOnceLocked(ctx, "sky_set_password.html"); verr == nil {
		return nil
	}
	dt.setCredentials(user, oldPass)
	p, gerr := c.getOnceLocked(ctx, "sky_set_password.html")
	if gerr != nil {
		return fmt.Errorf("skyhub: admin password change could not be verified with either password: %w", gerr)
	}
	status, _ := p.JSVar("pwd_change_status")
	if msg, ok := pwdStatusMessages[status]; ok {
		return fmt.Errorf("skyhub: admin password not changed: %s", msg)
	}
	return fmt.Errorf("skyhub: admin password not changed (hub status %q)", status)
}
