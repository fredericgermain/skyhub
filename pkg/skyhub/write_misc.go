package skyhub

import (
	"context"
	"fmt"
	"strconv"
)

// SetALG sets the SIP and H.323 application layer gateways.
func (c *Client) SetALG(ctx context.Context, a ALGConfig) error {
	_, err := c.PostForm(ctx, "sky_alg.cgi", "sky_alg.html", "name=formname", func(_ *Page, f *Form) error {
		f.SetCheckbox("enable_sip_alg", a.SIP)
		f.SetCheckbox("enable_h323_alg", a.H323)
		f.Set("sipalgenable", b01(a.SIP))
		f.Set("h323algenable", b01(a.H323))
		f.Set("todo", "save")
		return nil
	})
	return err
}

// SetUPnP sets UPnP on/off and its advertisement parameters.
func (c *Client) SetUPnP(ctx context.Context, u UPnPConfig) error {
	if u.AdvertiseInterval < 1 || u.AdvertiseInterval > 1440 {
		return fmt.Errorf("skyhub: advertise interval must be 1..1440 minutes")
	}
	if u.AdvertiseTTL < 1 || u.AdvertiseTTL > 255 {
		return fmt.Errorf("skyhub: advertise TTL must be 1..255")
	}
	_, err := c.PostForm(ctx, "sky_upnp.cgi", "sky_upnp.html", "name=formname", func(_ *Page, f *Form) error {
		f.SetCheckbox("enblUpnp", u.Enabled)
		f.Set("upnpAdvTime", strconv.Itoa(u.AdvertiseInterval))
		f.Set("upnpAdvTTL", strconv.Itoa(u.AdvertiseTTL))
		f.Set("todo", "apply") // the Apply link sets todo=apply
		return nil
	})
	return err
}

// SetWANConfig saves the WAN setup page (router mode, MTU, DMZ, ping).
// Saving may briefly re-apply the WAN configuration.
func (c *Client) SetWANConfig(ctx context.Context, w WANConfig) error {
	if w.MTU != 0 && (w.MTU < 1280 || w.MTU > 1534) {
		return fmt.Errorf("skyhub: MTU must be 1280..1534")
	}
	_, err := c.PostForm(ctx, "sky_wansetup.sky", "sky_wan_setup.html", "name=frmRules", func(host *Page, f *Form) error {
		cur, err := ParseWANConfig(host)
		if err != nil {
			return err
		}
		if w.RouterMode == "" {
			w.RouterMode = cur.RouterMode
		}
		if w.MTU == 0 {
			w.MTU = cur.MTU
		}
		f.Set("router_mode", w.RouterMode)
		f.Set("h_rmode", w.RouterMode)
		f.Set("h_wanport", f.Get("wanport"))
		f.Set("mtu_size", strconv.Itoa(w.MTU))
		f.SetCheckbox("dmz_enable", w.DMZEnabled)
		if w.DMZEnabled {
			if !w.DMZIP.Is4() {
				return fmt.Errorf("skyhub: DMZ enabled without an IPv4 address")
			}
			for i := 1; i <= 4; i++ {
				f.Enable("dmzip" + strconv.Itoa(i))
			}
			f.SetIPv4("dmzip", w.DMZIP.Addr)
			f.Set("address", w.DMZIP.String())
		} else {
			for i := 1; i <= 4; i++ {
				f.Del("dmzip" + strconv.Itoa(i))
			}
			f.Set("address", "")
		}
		if w.DMZIPv6.IsValid() {
			f.Set("dmzipV6", w.DMZIPv6.String())
		}
		// checkData() writes 1/0 into the checkbox values; unchecked boxes
		// are still not submitted, the h_ mirrors carry enable/disable.
		f.SetCheckbox("rspToPing", w.RespondToPing)
		if w.RespondToPing {
			f.Set("rspToPing", "1")
		}
		f.SetCheckbox("rspToPing6", w.RespondToPing6)
		if w.RespondToPing6 {
			f.Set("rspToPing6", "1")
		}
		f.Set("todo", "save")
		return nil
	})
	return err
}

// SetEthernet changes the LAN port speed / EEE setting. A change REBOOTS the
// hub; when the values already match nothing is posted.
func (c *Client) SetEthernet(ctx context.Context, e EthernetConfig) error {
	_, err := c.PostForm(ctx, "sky_eth_setup.sky", "sky_eth_setup.html", "name=frm1Rules", func(host *Page, f *Form) error {
		cur, err := ParseEthernetConfig(host)
		if err != nil {
			return err
		}
		if cur.Type == e.Type && cur.EEE == e.EEE {
			return errNoChange
		}
		f.Set("ethernet_type", e.Type)
		if e.EEE {
			f.Set("ethernet_eee", "enabled")
		} else {
			f.Set("ethernet_eee", "disabled")
		}
		return nil
	})
	if err == errNoChange {
		return nil
	}
	return err
}

// SetLANConfig saves LAN IP, netmask and DHCP pool. Changing the LAN IP,
// subnet or the DHCP on/off flag RESTARTS the hub.
func (c *Client) SetLANConfig(ctx context.Context, l LANConfig) error {
	if !l.IP.Is4() || !l.Netmask.Is4() || !l.PoolStart.Is4() || !l.PoolEnd.Is4() {
		return fmt.Errorf("skyhub: LAN config needs IPv4 ip, netmask, pool start and end")
	}
	_, err := c.PostForm(ctx, "sky_lan_ip_setup.sky", "sky_lan_ip_setup.html", "name=frmLan", func(_ *Page, f *Form) error {
		f.SetIPv4("sysLANIPAddr", l.IP.Addr)
		f.SetIPv4("sysLANSubnetMask", l.Netmask.Addr)
		f.SetCheckbox("dhcp_server", l.DHCPEnabled)
		f.SetIPv4("sysPoolStartingAddr", l.PoolStart.Addr)
		f.SetIPv4("sysPoolFinishAddr", l.PoolEnd.Addr)
		f.Set("todo", "save")
		return nil
	})
	return err
}

// SetWirelessEnabled switches the WiFi access point on or off globally.
func (c *Client) SetWirelessEnabled(ctx context.Context, on bool) error {
	_, err := c.PostForm(ctx, "sky_wireless_update.cmd", "sky_wireless_onoff.html", "index=0", func(_ *Page, f *Form) error {
		f.SetCheckbox("enable_ap", on)
		f.SetAfterHidden("h_enable_ap", b01(on)) // checkData() overwrites the mirror with 1/0
		f.Set("todo", "save")
		return nil
	})
	return err
}

var errNoChange = fmt.Errorf("skyhub: no change")
