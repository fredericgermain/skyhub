package skyhub

import (
	"errors"
	"fmt"
	"os"
)

var (
	// ErrAuth is returned when the hub rejects the digest credentials.
	ErrAuth = errors.New("skyhub: authentication failed")
	// ErrStaleKey is returned when the hub rejects a form POST because the
	// sessionKey it carried is no longer the current one.
	ErrStaleKey = errors.New("skyhub: stale sessionKey rejected by hub")
	// ErrNotFound is returned when an entry (reservation, rule, service)
	// does not exist on the hub.
	ErrNotFound = errors.New("skyhub: not found")
	// ErrHubRestarting is returned by writers whose change makes the hub
	// restart (LAN IP/subnet/DHCP flag). The change was submitted; the hub is
	// unreachable at its old address for a while and must not be read back.
	ErrHubRestarting = errors.New("skyhub: change submitted, hub is restarting")
	// ErrDestructive is returned for a reboot or factory reset while
	// AllowDestructiveEnv is not set to 1.
	ErrDestructive = errors.New("skyhub: refusing to reboot or factory-reset the hub (set " + AllowDestructiveEnv + "=1 to allow)")
)

// AllowDestructiveEnv, set to 1, lets this process reboot or factory-reset
// the hub (FactoryReset, a todo=reboot or todo=factory form). Unset, those
// are refused, so no other caller wipes or restarts the hub by mistake.
const AllowDestructiveEnv = "SKYHUB_ALLOW_DESTRUCTIVE"

func allowDestructive() bool { return os.Getenv(AllowDestructiveEnv) == "1" }

// HTTPError is returned for unexpected HTTP status codes.
type HTTPError struct {
	Method string
	Path   string
	Status int
	Body   string
}

func (e *HTTPError) Error() string {
	return fmt.Sprintf("skyhub: %s %s: unexpected status %d", e.Method, e.Path, e.Status)
}

// ParseError is returned when a page does not contain the expected markup or
// JavaScript variable. It usually indicates a firmware change.
type ParseError struct {
	Page    string
	What    string
	Snippet string
}

func (e *ParseError) Error() string {
	if e.Snippet != "" {
		return fmt.Sprintf("skyhub: parse %s: %s (near %q)", e.Page, e.What, e.Snippet)
	}
	return fmt.Sprintf("skyhub: parse %s: %s", e.Page, e.What)
}

// HubError is returned when the hub answers a form POST by redirecting to an
// error page instead of applying the change.
type HubError struct {
	Handler   string
	ErrorPage string
	Message   string
}

func (e *HubError) Error() string {
	if e.Message != "" {
		return fmt.Sprintf("skyhub: %s rejected: %s (%s)", e.Handler, e.Message, e.ErrorPage)
	}
	return fmt.Sprintf("skyhub: %s rejected by hub (%s)", e.Handler, e.ErrorPage)
}
