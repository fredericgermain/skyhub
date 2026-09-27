package skyhub

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"log/slog"
	"math/rand/v2"
	"net"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"
)

// Client talks to one Sky Hub. All requests are serialised: the hub's
// embedded web server is single-session and the CSRF sessionKey it issues
// changes on every page load.
type Client struct {
	base        *url.URL
	hc          *http.Client
	mu          *sync.Mutex
	timeout     time.Duration
	postDelay   time.Duration
	log         *slog.Logger
	userAgent   string
	authedOK    bool          // at least one request authenticated; guarded by mu
	rebootPause time.Duration // pause before polling after a reboot-causing change
}

func (c *Client) digest() *digestTransport { return c.hc.Transport.(*digestTransport) }

// HasPassword reports whether the client currently authenticates with pass.
func (c *Client) HasPassword(pass string) bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	_, p := c.digest().credentials()
	return p == pass
}

// Option configures a Client.
type Option func(*Client)

// WithTransport sets the underlying transport (tests use httptest).
func WithTransport(rt http.RoundTripper) Option {
	return func(c *Client) { c.hc.Transport.(*digestTransport).next = rt }
}

// WithTimeout sets the per-request timeout (default 30s).
func WithTimeout(d time.Duration) Option { return func(c *Client) { c.timeout = d } }

// WithLogger sets a logger (default: discard).
func WithLogger(l *slog.Logger) Option { return func(c *Client) { c.log = l } }

// WithRebootPause sets how long to wait before polling the hub after a
// change that reboots it (default 15s); WiFi saves wait half of it.
func WithRebootPause(d time.Duration) Option { return func(c *Client) { c.rebootPause = d } }

// WithPostDelay sets the pause after a successful form POST, giving the hub
// time to commit to NVRAM before the next read (default 300ms).
func WithPostDelay(d time.Duration) Option { return func(c *Client) { c.postDelay = d } }

var (
	hubLocksMu sync.Mutex
	hubLocks   = map[string]*sync.Mutex{}
)

func hubLock(host string) *sync.Mutex {
	hubLocksMu.Lock()
	defer hubLocksMu.Unlock()
	m, ok := hubLocks[host]
	if !ok {
		m = &sync.Mutex{}
		hubLocks[host] = m
	}
	return m
}

func defaultTransport() *http.Transport {
	return &http.Transport{
		Proxy: nil,
		DialContext: (&net.Dialer{
			Timeout:   10 * time.Second,
			KeepAlive: 30 * time.Second,
		}).DialContext,
		MaxConnsPerHost:     1,
		MaxIdleConnsPerHost: 1,
		IdleConnTimeout:     30 * time.Second,
		ForceAttemptHTTP2:   false,
	}
}

// New creates a client for the hub at baseURL.
func New(baseURL, user, password string, opts ...Option) (*Client, error) {
	if baseURL == "" {
		baseURL = DefaultURL
	}
	if !strings.HasSuffix(baseURL, "/") {
		baseURL += "/"
	}
	u, err := url.Parse(baseURL)
	if err != nil {
		return nil, fmt.Errorf("skyhub: bad url %q: %w", baseURL, err)
	}
	if u.Scheme == "" || u.Host == "" {
		return nil, fmt.Errorf("skyhub: bad url %q", baseURL)
	}
	c := &Client{
		base: u,
		hc: &http.Client{
			Transport: &digestTransport{next: defaultTransport(), user: user, pass: password},
			CheckRedirect: func(*http.Request, []*http.Request) error {
				return http.ErrUseLastResponse
			},
		},
		mu:          hubLock(u.Host),
		timeout:     30 * time.Second,
		postDelay:   300 * time.Millisecond,
		rebootPause: 15 * time.Second,
		log:         slog.New(slog.DiscardHandler),
		userAgent:   "skyhub-go",
	}
	for _, o := range opts {
		o(c)
	}
	return c, nil
}

// NewFromEnv builds a client from LoadCredentials().
func NewFromEnv(opts ...Option) (*Client, error) {
	cr, err := LoadCredentials()
	if err != nil {
		return nil, err
	}
	return New(cr.URL, cr.User, cr.Password, opts...)
}

// BaseURL returns the hub URL.
func (c *Client) BaseURL() string { return c.base.String() }

func (c *Client) url(path string) string {
	ref, err := url.Parse(path)
	if err != nil {
		ref = &url.URL{Path: path}
	}
	return c.base.ResolveReference(ref).String()
}

const maxBody = 4 << 20

func (c *Client) do(ctx context.Context, req *http.Request) (*http.Response, []byte, error) {
	ctx, cancel := context.WithTimeout(ctx, c.timeout)
	defer cancel()
	req = req.WithContext(ctx)
	req.Header.Set("User-Agent", c.userAgent)
	start := time.Now()
	resp, err := c.hc.Do(req)
	if err != nil {
		return nil, nil, err
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(io.LimitReader(resp.Body, maxBody))
	c.log.Debug("skyhub request", "method", req.Method, "path", req.URL.Path, "status", resp.StatusCode, "bytes", len(body), "took", time.Since(start))
	if err != nil {
		return nil, nil, err
	}
	return resp, body, nil
}

// Get fetches a page. Callers must hold no lock; Get takes the hub lock.
func (c *Client) Get(ctx context.Context, path string) (*Page, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.getLocked(ctx, path)
}

func (c *Client) getLocked(ctx context.Context, path string) (*Page, error) {
	p, err := c.getOnceLocked(ctx, path)
	// Another client (MCP server, exporter, Terraform) authenticating at the
	// same moment can make the hub reject a nonce we just used. Once this
	// client has authenticated successfully, retry a rejection twice with
	// jitter before calling the credentials wrong.
	for attempt := 0; err == ErrAuth && c.authedOK && attempt < 2; attempt++ {
		d := 500*time.Millisecond + time.Duration(rand.Int64N(int64(1500*time.Millisecond)))
		c.log.Debug("skyhub: auth rejected after earlier success, retrying", "path", path, "in", d)
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-time.After(d):
		}
		p, err = c.getOnceLocked(ctx, path)
	}
	return p, err
}

func (c *Client) getOnceLocked(ctx context.Context, path string) (*Page, error) {
	req, err := http.NewRequest(http.MethodGet, c.url(path), nil)
	if err != nil {
		return nil, err
	}
	resp, body, err := c.do(ctx, req)
	if err != nil {
		return nil, err
	}
	switch {
	case resp.StatusCode == http.StatusUnauthorized:
		return nil, ErrAuth
	case resp.StatusCode != http.StatusOK:
		return nil, &HTTPError{Method: "GET", Path: path, Status: resp.StatusCode, Body: snippet(body, 200)}
	}
	c.authedOK = true
	return &Page{Path: path, Body: body}, nil
}

// GetText fetches a non-HTML resource such as the syslog.
func (c *Client) GetText(ctx context.Context, path string) ([]byte, error) {
	p, err := c.Get(ctx, path)
	if err != nil {
		return nil, err
	}
	return p.Body, nil
}

// PostResult is the hub's answer to a form POST.
type PostResult struct {
	Status   int
	Location string
	Body     []byte
	Sent     url.Values
}

// FormBuilder mutates the form scraped from the host page before it is
// posted. host is the freshly fetched host page (for reading current state).
type FormBuilder func(host *Page, f *Form) error

// PostForm performs the hub's write protocol under the hub lock:
//
//  1. GET hostPage and select the form (formSel, see Page.Form);
//  2. let build mutate it;
//  3. set sessionKey from the host page and apply dataToHidden();
//  4. POST the urlencoded form to handler (relative to the hub base).
//
// A 401 or empty 200 answer means the sessionKey was rejected; the whole
// sequence is retried once. A redirect to an *_error.html page is reported
// as *HubError.
func (c *Client) PostForm(ctx context.Context, handler, hostPage, formSel string, build FormBuilder) (*PostResult, error) {
	return c.postForm(ctx, handler, hostPage, formSel, build, nil)
}

// postForm is PostForm with an optional after hook that sees the POST
// outcome and runs while the hub lock is still held. Writers that knock the
// hub (or this machine's WiFi) offline wait there, so concurrent callers
// queue behind the outage instead of failing in it.
func (c *Client) postForm(ctx context.Context, handler, hostPage, formSel string, build FormBuilder, after func(*PostResult, error) error) (*PostResult, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	var (
		res *PostResult
		err error
	)
	for attempt := 0; attempt < 2; attempt++ {
		res, err = c.postFormLocked(ctx, handler, hostPage, formSel, build)
		if err != ErrStaleKey || attempt == 1 {
			break
		}
		c.log.Warn("skyhub: sessionKey rejected, retrying", "handler", handler)
	}
	if err == ErrStaleKey {
		res = nil
	}
	if after != nil {
		return res, after(res, err)
	}
	return res, err
}

func (c *Client) postFormLocked(ctx context.Context, handler, hostPage, formSel string, build FormBuilder) (*PostResult, error) {
	host, err := c.getLocked(ctx, hostPage)
	if err != nil {
		return nil, fmt.Errorf("fetch %s: %w", hostPage, err)
	}
	form, err := host.Form(formSel)
	if err != nil {
		return nil, err
	}
	if build != nil {
		if err := build(host, form); err != nil {
			return nil, err
		}
	}
	key, err := host.SessionKey()
	if err != nil {
		return nil, err
	}
	form.Set("sessionKey", key)
	form.ApplyDataToHidden()
	if form.Get("todo") == "reboot" || form.Get("todo") == "factory" {
		return nil, fmt.Errorf("skyhub: refusing to post todo=%s", form.Get("todo"))
	}
	if handler == "" {
		handler = form.Action
	}
	body := form.Encode()
	req, err := http.NewRequest(http.MethodPost, c.url(handler), strings.NewReader(body))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("Referer", c.url(hostPage))
	resp, rbody, err := c.do(ctx, req)
	if err != nil {
		// A non-nil result tells callers the POST itself went out.
		return &PostResult{Sent: form.Values}, err
	}
	res := &PostResult{Status: resp.StatusCode, Location: resp.Header.Get("Location"), Body: rbody, Sent: form.Values}
	switch {
	case resp.StatusCode == http.StatusUnauthorized:
		return res, ErrStaleKey
	case resp.StatusCode >= 300 && resp.StatusCode < 400:
		if strings.Contains(res.Location, "_error") {
			return res, &HubError{Handler: handler, ErrorPage: res.Location}
		}
	case resp.StatusCode == http.StatusOK:
		if len(bytes.TrimSpace(rbody)) == 0 {
			return res, ErrStaleKey
		}
		if loc := errorRedirectIn(rbody); loc != "" {
			return res, &HubError{Handler: handler, ErrorPage: loc}
		}
	default:
		return res, &HTTPError{Method: "POST", Path: handler, Status: resp.StatusCode, Body: snippet(rbody, 200)}
	}
	if c.postDelay > 0 {
		time.Sleep(c.postDelay)
	}
	return res, nil
}

// errorRedirectIn detects a client-side redirect to an error page inside a
// 200 body (meta refresh or window.location).
func errorRedirectIn(body []byte) string {
	if !bytes.Contains(body, []byte("_error")) {
		return ""
	}
	for _, m := range errorLocRE.FindAllSubmatch(body, -1) {
		return string(m[1])
	}
	return ""
}
