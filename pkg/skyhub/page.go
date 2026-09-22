package skyhub

import (
	"bytes"
	"fmt"
	"regexp"
	"strconv"
	"strings"

	"golang.org/x/net/html"
)

// Page is a fetched admin page: raw body plus lazily parsed DOM.
type Page struct {
	Path string
	Body []byte

	doc    *html.Node
	docErr error
}

// NewPage wraps a body; used by tests and the capture tool.
func NewPage(path string, body []byte) *Page {
	return &Page{Path: path, Body: body}
}

// Doc returns the parsed DOM (parsed once, cached).
func (p *Page) Doc() (*html.Node, error) {
	if p.doc == nil && p.docErr == nil {
		p.doc, p.docErr = html.Parse(bytes.NewReader(p.Body))
	}
	return p.doc, p.docErr
}

// Text returns the body as a string.
func (p *Page) Text() string { return string(p.Body) }

var jsVarCache = map[string]*regexp.Regexp{}

func jsVarRE(name string) *regexp.Regexp {
	if re, ok := jsVarCache[name]; ok {
		return re
	}
	re := regexp.MustCompile(`\bvar\s+` + regexp.QuoteMeta(name) + `\s*=\s*(?:'((?:[^'\\]|\\.)*)'|"((?:[^"\\]|\\.)*)"|([^;\n]+?))\s*;`)
	jsVarCache[name] = re
	return re
}

// jsUnescape undoes the escapes found in a JS single/double quoted literal.
func jsUnescape(s string) string {
	if !strings.Contains(s, `\`) {
		return s
	}
	var b strings.Builder
	for i := 0; i < len(s); i++ {
		if s[i] == '\\' && i+1 < len(s) {
			i++
			switch s[i] {
			case 'n':
				b.WriteByte('\n')
			case 'r':
				b.WriteByte('\r')
			case 't':
				b.WriteByte('\t')
			default:
				b.WriteByte(s[i])
			}
			continue
		}
		b.WriteByte(s[i])
	}
	return b.String()
}

// JSVar returns the value of the first `var NAME = '...';` (or "...", or an
// unquoted literal) statement in the page's inline JavaScript.
func (p *Page) JSVar(name string) (string, bool) {
	m := jsVarRE(name).FindSubmatch(p.Body)
	if m == nil {
		return "", false
	}
	return jsMatchValue(m), true
}

// JSVarAll returns every value assigned to NAME with a var statement.
func (p *Page) JSVarAll(name string) []string {
	var out []string
	for _, m := range jsVarRE(name).FindAllSubmatch(p.Body, -1) {
		out = append(out, jsMatchValue(m))
	}
	return out
}

func jsMatchValue(m [][]byte) string {
	switch {
	case m[1] != nil:
		return jsUnescape(string(m[1]))
	case m[2] != nil:
		return jsUnescape(string(m[2]))
	default:
		return strings.TrimSpace(string(m[3]))
	}
}

// MustJSVar is JSVar returning a ParseError when the variable is missing.
func (p *Page) MustJSVar(name string) (string, error) {
	v, ok := p.JSVar(name)
	if !ok {
		return "", &ParseError{Page: p.Path, What: "missing JS var " + name}
	}
	return v, nil
}

var hiddenSessionKeyRE = regexp.MustCompile(`name="sessionKey"\s+value="([^"]*)"`)

// SessionKey returns the CSRF token the page expects on its form POSTs.
// The hub delivers it either as a JS variable (sessionKey, sessionId or
// sessionID) or as the value of a hidden <input name="sessionKey">; when
// both exist the JS variable is authoritative (the input is often "",
// "0" or "uninitialised").
func (p *Page) SessionKey() (string, error) {
	for _, n := range []string{"sessionKey", "sessionId", "sessionID"} {
		if v, ok := p.JSVar(n); ok && validSessionKey(v) {
			return v, nil
		}
	}
	for _, m := range hiddenSessionKeyRE.FindAllSubmatch(p.Body, -1) {
		if v := string(m[1]); validSessionKey(v) {
			return v, nil
		}
	}
	return "", &ParseError{Page: p.Path, What: "no sessionKey found"}
}

func validSessionKey(v string) bool {
	if v == "" || v == "0" {
		return false
	}
	_, err := strconv.ParseUint(v, 10, 64)
	return err == nil
}

// attr returns the value of an attribute on a node.
func attr(n *html.Node, key string) (string, bool) {
	for _, a := range n.Attr {
		if a.Key == key {
			return a.Val, true
		}
	}
	return "", false
}

func attrOr(n *html.Node, key, def string) string {
	if v, ok := attr(n, key); ok {
		return v
	}
	return def
}

// walk calls fn for every element node under n (pre-order); fn returns
// false to stop descending into that node.
func walk(n *html.Node, fn func(*html.Node) bool) {
	if n.Type == html.ElementNode && !fn(n) {
		return
	}
	for c := n.FirstChild; c != nil; c = c.NextSibling {
		walk(c, fn)
	}
}

// nodeText returns the concatenated, whitespace-collapsed text of a node.
func nodeText(n *html.Node) string {
	var b strings.Builder
	var rec func(*html.Node)
	rec = func(n *html.Node) {
		if n.Type == html.TextNode {
			b.WriteString(n.Data)
		}
		for c := n.FirstChild; c != nil; c = c.NextSibling {
			rec(c)
		}
	}
	rec(n)
	return strings.Join(strings.Fields(b.String()), " ")
}

// findByID returns the first element with the given id.
func findByID(root *html.Node, id string) *html.Node {
	var found *html.Node
	walk(root, func(n *html.Node) bool {
		if found != nil {
			return false
		}
		if v, ok := attr(n, "id"); ok && v == id {
			found = n
			return false
		}
		return true
	})
	return found
}

// tableRows returns, for each <tr> under root, the trimmed text of its cells.
func tableRows(root *html.Node) [][]string {
	var rows [][]string
	walk(root, func(n *html.Node) bool {
		if n.Data != "tr" {
			return true
		}
		var cells []string
		for c := n.FirstChild; c != nil; c = c.NextSibling {
			if c.Type == html.ElementNode && (c.Data == "td" || c.Data == "th") {
				cells = append(cells, nodeText(c))
			}
		}
		rows = append(rows, cells)
		return false
	})
	return rows
}

func snippet(b []byte, max int) string {
	s := strings.Join(strings.Fields(string(b)), " ")
	if len(s) > max {
		s = s[:max] + "…"
	}
	return s
}

func parseErrf(page, format string, a ...any) error {
	return &ParseError{Page: page, What: fmt.Sprintf(format, a...)}
}
