package skyhub

import (
	"context"
	"fmt"
	"html"
	"net/netip"
	"regexp"
	"strconv"
	"strings"
	"time"
)

// PingResult is the output of the hub's on-device ping (busybox ping, 4
// packets). Output is the raw text; the other fields are parsed from it.
type PingResult struct {
	Target   string    `json:"target"`
	Output   string    `json:"output"`
	Sent     int       `json:"sent"`
	Received int       `json:"received"`
	RTTs     []float64 `json:"rtt_ms"`
	Success  bool      `json:"success"`
}

// DNSResult is the output of the hub's DNS lookup diagnostic.
type DNSResult struct {
	Name      string   `json:"name"`
	Type      string   `json:"type"` // ipv4 | ipv6
	Addresses []string `json:"addresses"`
	Servers   []string `json:"servers"` // the hub's upstream resolvers
	Output    string   `json:"output"`
}

var (
	pingTextareaRE = regexp.MustCompile(`(?s)<textarea[^>]*name="ping_result"[^>]*>(.*?)</textarea>`)
	pingFlagRE     = regexp.MustCompile(`name="reflash_flag"\s+value="([^"]*)"`)
	pingStatsRE    = regexp.MustCompile(`(\d+) packets transmitted, (\d+) packets received`)
	pingTimeRE     = regexp.MustCompile(`time=([\d.]+) ms`)
	dnsTableRE     = regexp.MustCompile(`(?s)id="dns-lookup-output-value-table".*?</table>`)
	dnsCellRE      = regexp.MustCompile(`<td>\s*([^<\s]+)\s*</td>`)
	dnsServerRE    = regexp.MustCompile(`id="dns-server-(?:primary|secondary)-text">[^<]*</span><span>\s*([^<\s]+)`)
)

// PingTimeout bounds the wait for the hub's ping to finish.
const PingTimeout = 60 * time.Second

// Ping runs the hub's diagnostic ping against target and waits for the
// result. The hub answers the POST with a meta-refresh to
// sky_diagnostics_ping.html, whose ping_result textarea fills while the
// (4-packet) ping runs; reading the page consumes the result, so the output
// is accumulated across polls.
func (c *Client) Ping(ctx context.Context, target netip.Addr) (*PingResult, error) {
	if !target.IsValid() {
		return nil, fmt.Errorf("skyhub: ping: invalid target")
	}
	ctx, cancel := context.WithTimeout(ctx, PingTimeout)
	defer cancel()
	_, err := c.PostForm(ctx, "sky_diagnostics_ping.cgi", "sky_diagnostics.html", "name=ping", func(_ *Page, f *Form) error {
		if target.Is4() {
			f.SetIPv4("IPAddr", target)
			f.Set("IPAddrV6", "")
		} else {
			f.SetIPv4("IPAddr", netip.Addr{})
			f.Set("IPAddrV6", target.String())
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	res := &PingResult{Target: target.String()}
	var out strings.Builder
	for {
		p, err := c.Get(ctx, "sky_diagnostics_ping.html")
		if err != nil {
			return nil, err
		}
		if m := pingTextareaRE.FindSubmatch(p.Body); m != nil {
			out.WriteString(html.UnescapeString(string(m[1])))
		}
		flag := "1"
		if m := pingFlagRE.FindSubmatch(p.Body); m != nil {
			flag = string(m[1])
		}
		text := out.String()
		if pingStatsRE.MatchString(text) || (flag == "0" && strings.TrimSpace(text) != "") {
			break
		}
		select {
		case <-ctx.Done():
			res.Output = strings.TrimSpace(text)
			return res, fmt.Errorf("skyhub: ping: timed out waiting for result: %w", ctx.Err())
		case <-time.After(time.Second):
		}
	}
	res.Output = strings.TrimSpace(out.String())
	if m := pingStatsRE.FindStringSubmatch(res.Output); m != nil {
		res.Sent, _ = strconv.Atoi(m[1])
		res.Received, _ = strconv.Atoi(m[2])
	}
	for _, m := range pingTimeRE.FindAllStringSubmatch(res.Output, -1) {
		v, _ := strconv.ParseFloat(m[1], 64)
		res.RTTs = append(res.RTTs, v)
	}
	res.Success = res.Received > 0
	return res, nil
}

// DNSLookup resolves name through the hub. The result table on
// sky_diagnostics.html is filled once and cleared by the read, so the page
// is polled a few times after the POST.
func (c *Client) DNSLookup(ctx context.Context, name string, ipv6 bool) (*DNSResult, error) {
	name = strings.TrimSpace(name)
	if name == "" || strings.ContainsAny(name, " \t\r\n&=") {
		return nil, fmt.Errorf("skyhub: dns lookup: invalid name %q", name)
	}
	typ := "ipv4"
	if ipv6 {
		typ = "ipv6"
	}
	_, err := c.PostForm(ctx, "sky_diagnostics_dns.cgi", "sky_diagnostics.html", "name=dnslookup", func(_ *Page, f *Form) error {
		f.Set("lookup_name", name)
		f.Set("LookupType", typ)
		return nil
	})
	if err != nil {
		return nil, err
	}
	res := &DNSResult{Name: name, Type: typ}
	for attempt := 0; attempt < 5; attempt++ {
		p, err := c.Get(ctx, "sky_diagnostics.html")
		if err != nil {
			return nil, err
		}
		if len(res.Servers) == 0 {
			for _, m := range dnsServerRE.FindAllSubmatch(p.Body, -1) {
				res.Servers = append(res.Servers, string(m[1]))
			}
		}
		if tbl := dnsTableRE.Find(p.Body); tbl != nil {
			for _, m := range dnsCellRE.FindAllSubmatch(tbl, -1) {
				res.Addresses = append(res.Addresses, string(m[1]))
			}
			res.Output = strings.TrimSpace(strings.Join(res.Addresses, "\n"))
		}
		if len(res.Addresses) > 0 {
			break
		}
		select {
		case <-ctx.Done():
			return res, ctx.Err()
		case <-time.After(time.Second):
		}
	}
	return res, nil
}
