package skyhub

import (
	"fmt"
	"net"
	"net/netip"
	"regexp"
	"strconv"
	"strings"
	"time"
)

var errorLocRE = regexp.MustCompile(`([A-Za-z0-9_\-]+_error[A-Za-z0-9_\-]*\.html)`)

// parseHMS parses "HH:MM:SS" where HH may exceed 24.
func parseHMS(s string) (time.Duration, error) {
	parts := strings.Split(strings.TrimSpace(s), ":")
	if len(parts) != 3 {
		return 0, fmt.Errorf("bad duration %q", s)
	}
	var n [3]int
	for i, p := range parts {
		v, err := strconv.Atoi(strings.TrimSpace(p))
		if err != nil {
			return 0, fmt.Errorf("bad duration %q", s)
		}
		n[i] = v
	}
	return time.Duration(n[0])*time.Hour + time.Duration(n[1])*time.Minute + time.Duration(n[2])*time.Second, nil
}

func parseUint(s string) uint64 {
	v, _ := strconv.ParseUint(strings.TrimSpace(s), 10, 64)
	return v
}

func parseIntDef(s string, def int) int {
	v, err := strconv.Atoi(strings.TrimSpace(s))
	if err != nil {
		return def
	}
	return v
}

func parseAddr(s string) netip.Addr {
	s = strings.TrimSpace(s)
	if s == "" || s == "-" || s == "&nbsp;" || s == "0.0.0.0" {
		return netip.Addr{}
	}
	a, err := netip.ParseAddr(s)
	if err != nil {
		return netip.Addr{}
	}
	return a
}

func parsePrefix(s string) netip.Prefix {
	s = strings.TrimSpace(s)
	if s == "" || s == "-" || s == "&nbsp;" {
		return netip.Prefix{}
	}
	if p, err := netip.ParsePrefix(s); err == nil {
		return p
	}
	if a, err := netip.ParseAddr(s); err == nil {
		return netip.PrefixFrom(a, a.BitLen())
	}
	return netip.Prefix{}
}

func parseMAC(s string) net.HardwareAddr {
	s = strings.TrimSpace(s)
	if s == "" || s == "-" {
		return nil
	}
	m, err := net.ParseMAC(s)
	if err != nil {
		return nil
	}
	return m
}

// splitLF splits the hub's "<lf>"-separated list syntax, dropping empties.
func splitLF(s string) []string {
	s = strings.ReplaceAll(s, "\n", "")
	var out []string
	for _, rec := range strings.Split(s, "<lf>") {
		if strings.TrimSpace(rec) != "" {
			out = append(out, rec)
		}
	}
	return out
}

func splitBR(s string) []string {
	parts := strings.Split(s, "<br>")
	for i := range parts {
		parts[i] = strings.TrimSpace(parts[i])
	}
	return parts
}

func isTruthy(s string) bool {
	switch strings.ToLower(strings.TrimSpace(s)) {
	case "1", "enable", "enabled", "on", "true", "yes", "up":
		return true
	}
	return false
}

func ipv4Octets(a netip.Addr) [4]string {
	var out [4]string
	if !a.Is4() {
		return out
	}
	b := a.As4()
	for i := range b {
		out[i] = strconv.Itoa(int(b[i]))
	}
	return out
}

func addrString(a netip.Addr) string {
	if !a.IsValid() {
		return ""
	}
	return a.String()
}

func prefixString(p netip.Prefix) string {
	if !p.IsValid() {
		return ""
	}
	return p.String()
}

func macString(m net.HardwareAddr) string {
	if m == nil {
		return ""
	}
	return m.String()
}
