package skyhub

import (
	"context"
	"regexp"
	"strings"
)

// WANStatus reads the compact status page sky_st_poe.html.
func (c *Client) WANStatus(ctx context.Context) (*WANStatus, error) {
	p, err := c.Get(ctx, "sky_st_poe.html")
	if err != nil {
		return nil, err
	}
	return ParseWANStatus(p)
}

// ParseWANStatus parses sky_st_poe.html (or sky_router_status.html).
func ParseWANStatus(p *Page) (*WANStatus, error) {
	blob, ok := p.JSVar("wanStatus")
	if !ok {
		blob, ok = p.JSVar("wanDslLinkConfig")
	}
	if !ok {
		return nil, &ParseError{Page: p.Path, What: "missing wanStatus / wanDslLinkConfig"}
	}
	w := parseWANBlob(blob)
	w.ModemState, _ = p.JSVar("Modem_stat")
	w.RouterMode, _ = p.JSVar("routerMode")
	if w.RouterMode == "" {
		w.RouterMode, _ = p.JSVar("mode")
	}
	return w, nil
}

// parseWANBlob decodes the underscore-delimited status string:
//
//	0 up-flag, 1 proto, 2-3 ?, 4 vlan, 5 IPv4, 6 MAC, 7 mask, 8 gateway,
//	9 iface, 10 DNS list, 11 uptime, 12 IPv6/64, 13 IPv6 gw, 14-15 ?,
//	16 delegated prefix, 17 WAN link-local.
func parseWANBlob(blob string) *WANStatus {
	f := strings.Split(blob, "_")
	get := func(i int) string {
		if i < len(f) {
			v := strings.TrimSpace(f[i])
			if v == "&nbsp;" {
				return ""
			}
			return v
		}
		return ""
	}
	w := &WANStatus{Raw: f}
	w.Up = get(0) == "1"
	w.Protocol = get(1)
	w.VLAN = get(4)
	w.IPv4 = Addr{parseAddr(get(5))}
	w.MAC = MAC{parseMAC(get(6))}
	w.Netmask = Addr{parseAddr(get(7))}
	w.Gateway = Addr{parseAddr(get(8))}
	w.Interface = get(9)
	for _, d := range strings.Split(get(10), ",") {
		if a := parseAddr(d); a.IsValid() {
			w.DNS = append(w.DNS, Addr{a})
		}
	}
	if d, err := parseHMS(get(11)); err == nil {
		w.Uptime = Duration(d)
	}
	w.IPv6 = Prefix{parsePrefix(get(12))}
	w.IPv6Gateway = Addr{parseAddr(get(13))}
	w.DelegatedPrefix = Prefix{parsePrefix(get(16))}
	w.WANLinkLocal = Addr{parseAddr(get(17))}
	return w
}

var docWriteValueRE = regexp.MustCompile(`id="router-status-(dslfwver|linerate-down|linerate-up)-value">'\);\s*document\.write\('([^']*)'\)`)

// RouterInfo reads firmware and link details from sky_router_status.html.
func (c *Client) RouterInfo(ctx context.Context) (*RouterInfo, error) {
	p, err := c.Get(ctx, "sky_router_status.html")
	if err != nil {
		return nil, err
	}
	return ParseRouterInfo(p)
}

// ParseRouterInfo parses sky_router_status.html.
func ParseRouterInfo(p *Page) (*RouterInfo, error) {
	fw, err := p.MustJSVar("sky_firmware_version")
	if err != nil {
		return nil, err
	}
	ri := &RouterInfo{Firmware: fw}
	ri.TrafficMode, _ = p.JSVar("trafficMode")
	ri.WANInterfaceInfo, _ = p.JSVar("wanInterfaceInfo")
	for _, m := range docWriteValueRE.FindAllSubmatch(p.Body, -1) {
		v := strings.TrimSpace(string(m[2]))
		switch string(m[1]) {
		case "dslfwver":
			ri.DSLFirmware = v
		case "linerate-down":
			if ri.LineRateDownKbps == 0 {
				ri.LineRateDownKbps = parseIntDef(v, 0)
			}
		case "linerate-up":
			if ri.LineRateUpKbps == 0 {
				ri.LineRateUpKbps = parseIntDef(v, 0)
			}
		}
	}
	if w, err := ParseWANStatus(p); err == nil {
		ri.WAN = *w
	}
	return ri, nil
}
