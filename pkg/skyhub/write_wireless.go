package skyhub

import (
	"context"
	"fmt"
	"strconv"
)

// WirelessSettings is what SetWireless writes for one band.
type WirelessSettings struct {
	Band      string // "2.4" | "5"
	Enabled   bool
	SSID      string
	PSK       string // WPA2 key, 8-63 characters; "" keeps the current key
	Hidden    bool
	Channel   int    // 2.4 GHz: 0 (auto) or 1-13; 5 GHz: 36 or 44
	Bandwidth string // 2.4 GHz: "20" | "20/40"; 5 GHz: "80" (ch 36 only) | "40"
}

// SetWireless saves one band's settings through sky_wireless_settings.cgi,
// mirroring the page's checkData(): WPA2-PSK/AES security, the hidden
// wl* mirrors, and the band-specific channel/bandwidth encoding. Isolation,
// WPS and the 2.4/5 GHz sync flag keep their current values. Saving drops
// WiFi clients on that band for a few seconds.
func (c *Client) SetWireless(ctx context.Context, s WirelessSettings) error {
	host := WirelessPage24
	if s.Band == "5" {
		host = WirelessPage5
	} else if s.Band != "2.4" {
		return fmt.Errorf("skyhub: band must be \"2.4\" or \"5\"")
	}
	if len(s.SSID) < 1 || len(s.SSID) > 32 {
		return fmt.Errorf("skyhub: SSID must be 1-32 characters")
	}
	if s.PSK != "" && (len(s.PSK) < 8 || len(s.PSK) > 63) {
		return fmt.Errorf("skyhub: WPA2 key must be 8-63 characters")
	}
	_, err := c.PostForm(ctx, "sky_wireless_settings.cgi", host, "action=sky_wireless_settings.cgi", func(p *Page, f *Form) error {
		cur, err := ParseWirelessRadio(p)
		if err != nil {
			return err
		}
		if cur.Band != s.Band {
			return fmt.Errorf("skyhub: %s served band %s, not %s", host, cur.Band, s.Band)
		}
		psk := s.PSK
		if psk == "" {
			v, ok := p.JSVar("wpaPskKey")
			if !ok || jsString(v) == "" {
				return fmt.Errorf("skyhub: no current WPA2 key to keep; set one")
			}
			psk = jsString(v)
		}
		f.Set("wlSsid", s.SSID)
		f.Set("wlWpaPsk", psk)
		f.SetCheckbox("wifi_enabled", s.Enabled)
		f.Set("wlEnbl", b01(s.Enabled))
		// "wifi_hide" is the "broadcast SSID" box: checked = visible.
		f.SetCheckbox("wifi_hide", !s.Hidden)
		f.Set("wlHide", b01(s.Hidden))
		f.SetCheckbox("wifi_isolation", cur.Isolation)
		f.Set("wlAPIsolation", b01(cur.Isolation))
		f.SetCheckbox("wifi_sync_setting", cur.SyncSettings)
		f.Set("wlSyncSettings", b01(cur.SyncSettings))
		f.SetCheckbox("enbl_wps", cur.WPSEnabled)
		if cur.WPSEnabled {
			f.Set("wlWscMode", "enabled")
		} else {
			f.Set("wlWscMode", "disabled")
		}
		// secType 2 = WPA2-PSK.
		f.Set("secType", "2")
		f.Set("wlAuthMode", "psk2")
		f.Set("wlAuth", "0")
		f.Set("wlWep", "disabled")
		f.Set("wlWpa", "aes")
		f.Set("wlPreauth", "1")
		f.Set("wlSecMode", "5")
		f.Set("wlSyncNvram", "1")
		if s.Band == "5" {
			switch {
			case s.Channel == 36 && s.Bandwidth == "80":
				f.Set("wlNBwCap", "7")
			case (s.Channel == 36 || s.Channel == 44) && s.Bandwidth == "40":
				f.Set("wlNBwCap", "3")
				f.Set("wlNCtrlsb", "-1")
			default:
				return fmt.Errorf("skyhub: 5 GHz supports channel 36 at 80 MHz, or 36/44 at 40 MHz")
			}
			f.Set("wlChannel", strconv.Itoa(s.Channel))
		} else {
			if s.Channel < 0 || s.Channel > 13 {
				return fmt.Errorf("skyhub: 2.4 GHz channel must be 0 (auto) to 13")
			}
			bw := map[string]string{"20": "0", "20/40": "2"}[s.Bandwidth]
			if bw == "" {
				return fmt.Errorf("skyhub: 2.4 GHz bandwidth must be \"20\" or \"20/40\"")
			}
			f.Set("wlChannel", strconv.Itoa(s.Channel))
			f.Set("wlBandWidth", bw)
			f.Set("wlNBwCap", bw)
			f.Set("wlBntWth", "0")
		}
		return nil
	})
	return err
}
