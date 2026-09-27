package skyhub

import (
	"bytes"
	"crypto/md5"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"io"
	"net/http"
	"strings"
	"sync"
)

// digestTransport implements RFC 2617 Digest authentication (qop=auth, MD5)
// as an http.RoundTripper. It caches the last challenge and authenticates
// pre-emptively; on a 401 it re-challenges once.
type digestTransport struct {
	next http.RoundTripper
	user string
	pass string

	mu   sync.Mutex
	chal *digestChallenge
	nc   uint32
}

type digestChallenge struct {
	realm     string
	nonce     string
	opaque    string
	algorithm string
	qop       string
	stale     bool
}

func parseDigestChallenge(h string) (*digestChallenge, bool) {
	const prefix = "digest "
	if len(h) < len(prefix) || !strings.EqualFold(h[:len(prefix)], prefix) {
		return nil, false
	}
	c := &digestChallenge{}
	for _, kv := range splitDigestParams(h[len(prefix):]) {
		k, v, _ := strings.Cut(kv, "=")
		k = strings.ToLower(strings.TrimSpace(k))
		v = strings.Trim(strings.TrimSpace(v), `"`)
		switch k {
		case "realm":
			c.realm = v
		case "nonce":
			c.nonce = v
		case "opaque":
			c.opaque = v
		case "algorithm":
			c.algorithm = v
		case "qop":
			c.qop = v
		case "stale":
			c.stale = strings.EqualFold(v, "true")
		}
	}
	return c, c.nonce != ""
}

// splitDigestParams splits on commas that are not inside quotes.
func splitDigestParams(s string) []string {
	var out []string
	var cur strings.Builder
	inQ := false
	for _, r := range s {
		switch {
		case r == '"':
			inQ = !inQ
			cur.WriteRune(r)
		case r == ',' && !inQ:
			out = append(out, cur.String())
			cur.Reset()
		default:
			cur.WriteRune(r)
		}
	}
	if cur.Len() > 0 {
		out = append(out, cur.String())
	}
	return out
}

func md5hex(s string) string {
	sum := md5.Sum([]byte(s))
	return hex.EncodeToString(sum[:])
}

func (t *digestTransport) authorize(req *http.Request) {
	t.mu.Lock()
	defer t.mu.Unlock()
	if t.chal == nil {
		return
	}
	c := t.chal
	t.nc++
	nc := fmt.Sprintf("%08x", t.nc)
	var cn [8]byte
	_, _ = rand.Read(cn[:])
	cnonce := hex.EncodeToString(cn[:])
	uri := req.URL.RequestURI()

	ha1 := md5hex(t.user + ":" + c.realm + ":" + t.pass)
	if strings.EqualFold(c.algorithm, "MD5-sess") {
		ha1 = md5hex(ha1 + ":" + c.nonce + ":" + cnonce)
	}
	ha2 := md5hex(req.Method + ":" + uri)

	var resp string
	qop := ""
	for _, q := range strings.Split(c.qop, ",") {
		if strings.TrimSpace(q) == "auth" {
			qop = "auth"
		}
	}
	if qop != "" {
		resp = md5hex(ha1 + ":" + c.nonce + ":" + nc + ":" + cnonce + ":" + qop + ":" + ha2)
	} else {
		resp = md5hex(ha1 + ":" + c.nonce + ":" + ha2)
	}

	var b strings.Builder
	fmt.Fprintf(&b, `Digest username="%s", realm="%s", nonce="%s", uri="%s"`, t.user, c.realm, c.nonce, uri)
	if qop != "" {
		fmt.Fprintf(&b, `, qop=%s, nc=%s, cnonce="%s"`, qop, nc, cnonce)
	}
	fmt.Fprintf(&b, `, response="%s"`, resp)
	if c.opaque != "" {
		fmt.Fprintf(&b, `, opaque="%s"`, c.opaque)
	}
	if c.algorithm != "" {
		fmt.Fprintf(&b, `, algorithm=%s`, c.algorithm)
	}
	req.Header.Set("Authorization", b.String())
}

func (t *digestTransport) credentials() (string, string) {
	t.mu.Lock()
	defer t.mu.Unlock()
	return t.user, t.pass
}

// setCredentials swaps user/password in place (after an admin password
// change) and forgets the cached challenge so the next request re-auths.
func (t *digestTransport) setCredentials(user, pass string) {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.user, t.pass = user, pass
	t.chal, t.nc = nil, 0
}

func (t *digestTransport) setChallenge(c *digestChallenge) {
	t.mu.Lock()
	defer t.mu.Unlock()
	if t.chal == nil || t.chal.nonce != c.nonce {
		t.nc = 0
	}
	t.chal = c
}

func cloneRequest(req *http.Request) (*http.Request, error) {
	r2 := req.Clone(req.Context())
	if req.Body != nil && req.Body != http.NoBody {
		if req.GetBody == nil {
			return nil, fmt.Errorf("skyhub: cannot retry request without GetBody")
		}
		b, err := req.GetBody()
		if err != nil {
			return nil, err
		}
		r2.Body = b
	}
	return r2, nil
}

func (t *digestTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	if req.Body != nil && req.Body != http.NoBody && req.GetBody == nil {
		// Buffer so the request can be replayed after a challenge.
		buf, err := io.ReadAll(req.Body)
		req.Body.Close()
		if err != nil {
			return nil, err
		}
		req.Body = io.NopCloser(bytes.NewReader(buf))
		req.GetBody = func() (io.ReadCloser, error) { return io.NopCloser(bytes.NewReader(buf)), nil }
	}
	first, err := cloneRequest(req)
	if err != nil {
		return nil, err
	}
	t.authorize(first)
	resp, err := t.next.RoundTrip(first)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode != http.StatusUnauthorized {
		return resp, nil
	}
	chal, ok := parseDigestChallenge(resp.Header.Get("WWW-Authenticate"))
	if !ok {
		return resp, nil
	}
	// Drain and retry once with the fresh challenge.
	_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, 1<<16))
	resp.Body.Close()
	t.setChallenge(chal)

	second, err := cloneRequest(req)
	if err != nil {
		return nil, err
	}
	t.authorize(second)
	return t.next.RoundTrip(second)
}

// NewDigestTransport returns an http.RoundTripper that adds HTTP Digest
// authentication (qop=auth) for user/pass on top of next (nil = default).
// It is exported for tools such as the authenticating proxy in cmd/skyhub.
func NewDigestTransport(user, pass string, next http.RoundTripper) http.RoundTripper {
	if next == nil {
		next = defaultTransport()
	}
	return &digestTransport{next: next, user: user, pass: pass}
}
