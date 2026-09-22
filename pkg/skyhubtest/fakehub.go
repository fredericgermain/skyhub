package skyhubtest

import (
	"crypto/md5"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"testing"
)

// RecordedPost is one form POST the fake hub received.
type RecordedPost struct {
	Handler string
	Values  url.Values
}

// PostHandler lets a test script the hub's answer to a POST. Returning
// false means "use the default behaviour" (302 to next_file).
type PostHandler func(w http.ResponseWriter, r *http.Request, v url.Values) bool

// FakeHub is an httptest server that behaves like the Sky Hub admin UI:
// HTTP Digest auth (qop=auth, MD5) with nc checking, HTML fixtures served
// with a fresh sessionKey on every GET, and POST handlers that reject a
// stale key.
type FakeHub struct {
	Server *httptest.Server
	User   string
	Pass   string
	Realm  string

	mu         sync.Mutex
	fixtures   map[string][]byte
	nonce      string
	seen       map[string]bool // nonce+nc+cnonce replay guard
	currentKey string
	posts      []RecordedPost
	handlers   map[string]PostHandler
	challenges int
	requests   int

	// RotateNonceEvery forces a new nonce (stale=true) every N authenticated
	// requests; 0 disables.
	RotateNonceEvery int
	// RejectPostsOnce makes the next POST answer 401 even with a valid key.
	RejectPostsOnce bool
}

// FixtureName maps a page path (possibly with a query) to its fixture file.
func FixtureName(page string) string {
	return strings.NewReplacer("/", "_", "?", "_", "=", "_").Replace(page)
}

// NewFakeHub starts a fake hub serving the fixtures in dir.
func NewFakeHub(t testing.TB, dir, user, pass string) *FakeHub {
	t.Helper()
	h := &FakeHub{
		User:     user,
		Pass:     pass,
		Realm:    "Broadband Router",
		fixtures: map[string][]byte{},
		seen:     map[string]bool{},
		handlers: map[string]PostHandler{},
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("fakehub: %v", err)
	}
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		b, err := os.ReadFile(filepath.Join(dir, e.Name()))
		if err != nil {
			t.Fatal(err)
		}
		h.fixtures[e.Name()] = b
	}
	h.rotateNonce()
	h.Server = httptest.NewServer(http.HandlerFunc(h.serve))
	t.Cleanup(h.Server.Close)
	return h
}

// URL returns the base URL with trailing slash.
func (h *FakeHub) URL() string { return h.Server.URL + "/" }

// SetFixture adds or replaces a fixture body.
func (h *FakeHub) SetFixture(page string, body []byte) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.fixtures[FixtureName(page)] = body
}

// Handle registers a scripted POST handler for a handler path.
func (h *FakeHub) Handle(handler string, fn PostHandler) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.handlers[handler] = fn
}

// Posts returns the recorded POSTs.
func (h *FakeHub) Posts() []RecordedPost {
	h.mu.Lock()
	defer h.mu.Unlock()
	return append([]RecordedPost(nil), h.posts...)
}

// CurrentKey is the sessionKey issued by the most recent GET.
func (h *FakeHub) CurrentKey() string {
	h.mu.Lock()
	defer h.mu.Unlock()
	return h.currentKey
}

// Challenges is how many 401 challenges were sent.
func (h *FakeHub) Challenges() int {
	h.mu.Lock()
	defer h.mu.Unlock()
	return h.challenges
}

func (h *FakeHub) rotateNonce() {
	var b [16]byte
	_, _ = rand.Read(b[:])
	h.nonce = hex.EncodeToString(b[:])
}

func md5hex(s string) string {
	sum := md5.Sum([]byte(s))
	return hex.EncodeToString(sum[:])
}

func parseAuth(h string) map[string]string {
	out := map[string]string{}
	if !strings.HasPrefix(strings.ToLower(h), "digest ") {
		return out
	}
	for _, kv := range strings.Split(h[7:], ",") {
		k, v, ok := strings.Cut(strings.TrimSpace(kv), "=")
		if !ok {
			continue
		}
		out[strings.ToLower(k)] = strings.Trim(v, `"`)
	}
	return out
}

// authenticate returns (ok, stale). Caller holds h.mu.
func (h *FakeHub) authenticate(r *http.Request) (bool, bool) {
	a := parseAuth(r.Header.Get("Authorization"))
	if len(a) == 0 {
		return false, false
	}
	if a["nonce"] != h.nonce {
		return false, true
	}
	if a["username"] != h.User {
		return false, false
	}
	ha1 := md5hex(h.User + ":" + h.Realm + ":" + h.Pass)
	ha2 := md5hex(r.Method + ":" + a["uri"])
	want := md5hex(ha1 + ":" + h.nonce + ":" + a["nc"] + ":" + a["cnonce"] + ":auth:" + ha2)
	if a["response"] != want {
		return false, false
	}
	// Several clients may share one nonce (e.g. one per terraform command),
	// so nc is not required to increase globally; exact replays are rejected.
	nc, err := strconv.ParseUint(a["nc"], 16, 64)
	if err != nil || nc == 0 {
		return false, false
	}
	key := h.nonce + ":" + a["nc"] + ":" + a["cnonce"]
	if h.seen[key] {
		return false, false
	}
	h.seen[key] = true
	return true, false
}

func (h *FakeHub) challenge(w http.ResponseWriter, stale bool) {
	h.challenges++
	v := fmt.Sprintf(`Digest qop="auth", realm="%s", nonce="%s"`, h.Realm, h.nonce)
	if stale {
		v += `, stale=true`
	}
	w.Header().Set("WWW-Authenticate", v)
	w.Header().Set("Server", "sky_router")
	w.WriteHeader(http.StatusUnauthorized)
	_, _ = w.Write([]byte("<html><body>401 Unauthorized</body></html>"))
}

func newKey() string {
	var b [4]byte
	_, _ = rand.Read(b[:])
	return strconv.FormatUint(uint64(b[0])<<24|uint64(b[1])<<16|uint64(b[2])<<8|uint64(b[3])|1, 10)
}

func (h *FakeHub) serve(w http.ResponseWriter, r *http.Request) {
	h.mu.Lock()
	defer h.mu.Unlock()
	if r.Method != http.MethodGet && r.Method != http.MethodPost {
		w.WriteHeader(http.StatusNotImplemented)
		return
	}
	page := strings.TrimPrefix(r.URL.Path, "/")
	if r.URL.RawQuery != "" {
		page += "?" + r.URL.RawQuery
	}
	if page == "" || page == "sky_index.html" {
		if b, ok := h.fixtures["sky_index.html"]; ok {
			_, _ = w.Write(b)
			return
		}
	}
	ok, stale := h.authenticate(r)
	if !ok {
		h.challenge(w, stale)
		return
	}
	h.requests++
	if h.RotateNonceEvery > 0 && h.requests%h.RotateNonceEvery == 0 {
		h.rotateNonce()
	}
	switch r.Method {
	case http.MethodGet:
		b, ok := h.fixtures[FixtureName(page)]
		if !ok {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		h.currentKey = newKey()
		body := strings.ReplaceAll(string(b), SessionKeyPlaceholder, h.currentKey)
		if strings.HasSuffix(page, ".log") {
			w.Header().Set("Content-Type", "text/plain")
		} else {
			w.Header().Set("Content-Type", "text/html")
		}
		_, _ = w.Write([]byte(body))
	case http.MethodPost:
		if err := r.ParseForm(); err != nil {
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		v := r.PostForm
		h.posts = append(h.posts, RecordedPost{Handler: page, Values: v})
		if h.RejectPostsOnce {
			// Behave like a hub that consumed the key and rejected the write.
			h.RejectPostsOnce = false
			h.currentKey = ""
			h.challenge(w, false)
			return
		}
		if v.Get("sessionKey") != h.currentKey || h.currentKey == "" {
			h.challenge(w, false)
			return
		}
		h.currentKey = "" // single use
		if fn, ok := h.handlers[page]; ok && fn(w, r, v) {
			return
		}
		next := v.Get("next_file")
		if next == "" {
			next = "sky_index.html"
		}
		http.Redirect(w, r, "/"+next, http.StatusFound)
	}
}
