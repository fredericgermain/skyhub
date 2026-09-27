package skyhub

import (
	"context"
	"html"
	"regexp"
	"strings"
)

var decodeHTMLRE = regexp.MustCompile(`^decodeHtml\(\s*'((?:[^'\\]|\\.)*)'\s*\)$`)

// jsString unwraps decodeHtml('...') and HTML entities.
func jsString(v string) string {
	if m := decodeHTMLRE.FindStringSubmatch(v); m != nil {
		v = jsUnescape(m[1])
	}
	return html.UnescapeString(v)
}

// Band selector pages. Requesting one switches the hub's server-side
// "current band", which sky_wireless_settings.html then reflects, so both
// bands are read through the selector and never through the settings page.
const (
	WirelessPage24 = "sky_wireless_band.cgi?band=2.4GHz"
	WirelessPage5  = "sky_wireless_band.cgi?band=5GHz"
)

// WirelessRadios reads both bands (two page loads).
func (c *Client) WirelessRadios(ctx context.Context) ([]WirelessRadio, error) {
	var out []WirelessRadio
	for _, page := range []string{WirelessPage24, WirelessPage5} {
		p, err := c.Get(ctx, page)
		if err != nil {
			return nil, err
		}
		r, err := ParseWirelessRadio(p)
		if err != nil {
			return nil, err
		}
		out = append(out, *r)
	}
	return out, nil
}

// ParseWirelessRadio parses one band's settings page.
func ParseWirelessRadio(p *Page) (*WirelessRadio, error) {
	band, err := p.MustJSVar("sky_wlBand")
	if err != nil {
		return nil, err
	}
	r := &WirelessRadio{}
	switch band {
	case "1":
		r.Band = "5"
	default:
		r.Band = "2.4"
	}
	v, _ := p.JSVar("sky_wlEnabled")
	r.Enabled = isTruthy(v)
	v, _ = p.JSVar("sky_wlHide")
	r.Hidden = isTruthy(v)
	v, _ = p.JSVar("sky_wlIsolation")
	r.Isolation = isTruthy(v)
	v, _ = p.JSVar("sky_wlChannel")
	r.Channel = parseIntDef(v, 0)
	v, _ = p.JSVar("sky_wlBandwidth")
	r.Bandwidth = bandwidthName(v, r.Band)
	v, _ = p.JSVar("sky_wlSyncSettings")
	r.SyncSettings = isTruthy(v)
	r.AuthMode, _ = p.JSVar("sky_wlAuthMode")
	r.Cipher, _ = p.JSVar("wpa")
	v, _ = p.JSVar("WscMode")
	r.WPSEnabled = strings.EqualFold(v, "enabled")
	if s, ok := p.JSVar("sky_ssid"); ok {
		r.SSID = jsString(s)
	}
	if k, ok := p.JSVar("wpaPskKey"); ok {
		r.PSKSet = jsString(k) != ""
	}
	return r, nil
}

func bandwidthName(v, band string) string {
	if band == "5" {
		switch strings.TrimSpace(v) {
		case "3":
			return "80"
		case "1", "2":
			return "40"
		}
	}
	switch strings.TrimSpace(v) {
	case "0":
		return "20"
	case "1":
		return "40"
	case "2":
		return "20/40"
	case "3":
		return "20/40/80"
	}
	return v
}

// WirelessOnOff reads sky_wireless_onoff.html (global AP switch + model).
func (c *Client) WirelessOnOff(ctx context.Context) (*WirelessOnOff, error) {
	p, err := c.Get(ctx, "sky_wireless_onoff.html")
	if err != nil {
		return nil, err
	}
	enbl, err := p.MustJSVar("enbl")
	if err != nil {
		return nil, err
	}
	w := &WirelessOnOff{Enabled: isTruthy(enbl)}
	w.Model, _ = p.JSVar("productModel")
	return w, nil
}
