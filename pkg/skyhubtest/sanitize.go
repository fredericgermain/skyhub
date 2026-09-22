// Package skyhubtest holds test helpers shared by the library tests, the
// capture tool and downstream projects: a fixture sanitiser and a fake hub.
package skyhubtest

import (
	"fmt"
	"net/netip"
	"regexp"
	"strings"
	"sync"
)

// Sanitizer rewrites captured hub pages so they can be committed: MAC
// addresses, public IPs, WiFi secrets, SSIDs and session keys are replaced
// by deterministic placeholders. The same input always maps to the same
// placeholder within one Sanitizer, so cross-page references stay coherent.
type Sanitizer struct {
	mu   sync.Mutex
	macs map[string]string
	ip4  map[string]string
	ip6  map[string]string
}

// NewSanitizer returns an empty mapping.
func NewSanitizer() *Sanitizer {
	return &Sanitizer{macs: map[string]string{}, ip4: map[string]string{}, ip6: map[string]string{}}
}

// SessionKeyPlaceholder is the value every sessionKey is rewritten to.
const SessionKeyPlaceholder = "1234567890"

var (
	macRE  = regexp.MustCompile(`(?i)(?:[0-9a-f]{2}:){5}[0-9a-f]{2}`)
	ipv4RE = regexp.MustCompile(`(?:\d{1,3}\.){3}\d{1,3}`)
	ipv6RE = regexp.MustCompile(`(?i)[0-9a-f]{1,4}(?::[0-9a-f]{0,4}){2,7}`)

	// JS vars carrying secrets or personal identifiers.
	secretVarRE = regexp.MustCompile(`(?i)(var\s+(?:wpaPskKey|sky_OtherWlPsk|keyText|WscDevPin|radiusKey|wpaGTKRekey|sky_wlWpaPsk|key)\s*=\s*(?:decodeHtml\()?)(['"])(?:[^'"\\]|\\.)*(['"])`)
	ssidVarRE   = regexp.MustCompile(`(?i)(var\s+(?:sky_ssid|sky_OtherWlSsid|sky_wlSsid|sky_WirelessAllSSIDs)\s*=\s*(?:decodeHtml\()?)(['"])(?:[^'"\\]|\\.)*(['"])`)
	sessVarRE   = regexp.MustCompile(`(var\s+(?:sessionKey|sessionId|sessionID)\s*=\s*)(['"])[0-9]*(['"])`)
	sessInputRE = regexp.MustCompile(`(name="sessionKey"\s+value=")[0-9]+(")`)
	secretInRE  = regexp.MustCompile(`(?i)(name="(?:wlWpaPsk|passphrase|wlWpaPsk2|wlKeyText|dyndnsPassword|inPassword|inOrgPassword|inConfirmPasswd|radiusKey)"[^>]*?value=")[^"]*(")`)
	ssidInRE    = regexp.MustCompile(`(name="wlSsid"[^>]*?value=")[^"]*(")`)
)

// Sanitize rewrites one page body.
func (s *Sanitizer) Sanitize(body []byte) []byte {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := string(body)
	out = secretVarRE.ReplaceAllString(out, "${1}${2}REDACTED${3}")
	out = ssidVarRE.ReplaceAllString(out, "${1}${2}TestSSID${3}")
	out = sessVarRE.ReplaceAllString(out, "${1}${2}"+SessionKeyPlaceholder+"${3}")
	out = sessInputRE.ReplaceAllString(out, "${1}"+SessionKeyPlaceholder+"${2}")
	out = secretInRE.ReplaceAllString(out, "${1}REDACTED${2}")
	out = ssidInRE.ReplaceAllString(out, "${1}TestSSID${2}")
	out = replaceBounded(out, macRE, ":0123456789abcdefABCDEF", s.mac)
	out = replaceBounded(out, ipv4RE, ".0123456789", s.ipv4)
	out = replaceBounded(out, ipv6RE, ":.0123456789abcdefABCDEF", s.ipv6)
	return []byte(out)
}

// replaceBounded applies fn to every match of re that is not immediately
// preceded or followed by one of the characters in extend. Unlike \b it
// treats "_" as a separator, which the hub uses as its list delimiter.
func replaceBounded(s string, re *regexp.Regexp, extend string, fn func(string) string) string {
	var b strings.Builder
	last := 0
	for _, m := range re.FindAllStringIndex(s, -1) {
		start, end := m[0], m[1]
		if start > 0 && strings.IndexByte(extend, s[start-1]) >= 0 {
			continue
		}
		if end < len(s) && strings.IndexByte(extend, s[end]) >= 0 {
			continue
		}
		b.WriteString(s[last:start])
		b.WriteString(fn(s[start:end]))
		last = end
	}
	b.WriteString(s[last:])
	return b.String()
}

func (s *Sanitizer) mac(m string) string {
	key := strings.ToLower(m)
	if key == "00:00:00:00:00:00" || key == "ff:ff:ff:ff:ff:ff" {
		return m
	}
	if v, ok := s.macs[key]; ok {
		return v
	}
	v := fmt.Sprintf("02:00:00:00:00:%02x", len(s.macs)+1)
	s.macs[key] = v
	return v
}

func isPrivateOrSpecial4(a netip.Addr) bool {
	return a.IsPrivate() || a.IsLoopback() || a.IsUnspecified() || a.IsMulticast() ||
		a.IsLinkLocalUnicast() || a.As4()[0] == 255 || a.As4()[0] == 0
}

func (s *Sanitizer) ipv4(m string) string {
	a, err := netip.ParseAddr(m)
	if err != nil || !a.Is4() || isPrivateOrSpecial4(a) {
		return m
	}
	// Netmasks look like IPs; leave the common ones alone.
	if strings.HasPrefix(m, "255.") {
		return m
	}
	if v, ok := s.ip4[m]; ok {
		return v
	}
	v := fmt.Sprintf("203.0.113.%d", len(s.ip4)+1)
	s.ip4[m] = v
	return v
}

func (s *Sanitizer) ipv6(m string) string {
	a, err := netip.ParseAddr(m)
	if err != nil || !a.Is6() {
		return m
	}
	// Only global unicast (2000::/3) is identifying.
	if a.As16()[0]&0xe0 != 0x20 {
		return m
	}
	if strings.HasPrefix(m, "2001:db8:") {
		return m
	}
	if v, ok := s.ip6[m]; ok {
		return v
	}
	// Preserve the interface-id part so /64-style values still look right.
	v := fmt.Sprintf("2001:db8:%x::%x", len(s.ip6)+1, a.As16()[15])
	if a.As16()[15] == 0 && a.As16()[14] == 0 {
		v = fmt.Sprintf("2001:db8:%x::", len(s.ip6)+1)
	}
	s.ip6[m] = v
	return v
}
