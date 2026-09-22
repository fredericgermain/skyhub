package skyhub

import (
	"context"
	"strings"
)

// WANConfig reads sky_wan_setup.html. State comes from JS vars; the input
// values are template defaults.
func (c *Client) WANConfig(ctx context.Context) (*WANConfig, error) {
	p, err := c.Get(ctx, "sky_wan_setup.html")
	if err != nil {
		return nil, err
	}
	return ParseWANConfig(p)
}

// ParseWANConfig parses sky_wan_setup.html.
func ParseWANConfig(p *Page) (*WANConfig, error) {
	f, err := p.Form("name=frmRules")
	if err != nil {
		return nil, err
	}
	w := &WANConfig{}
	w.RouterMode = f.Get("h_rmode")
	if w.RouterMode == "" {
		w.RouterMode = f.Get("router_mode")
	}
	v, _ := p.JSVar("mtu")
	w.MTU = parseIntDef(v, 0)
	v, _ = p.JSVar("Ipaddr")
	w.DMZIP = Addr{parseAddr(v)}
	w.DMZEnabled = f.Has("dmz_enable") && f.Get("dmz_enable") != "" && w.DMZIP.IsValid()
	w.DMZIPv6 = Addr{parseAddr(f.Get("dmzipV6"))}
	v, _ = p.JSVar("pingenbl")
	w.RespondToPing = isTruthy(v)
	v, _ = p.JSVar("pingenbl6")
	w.RespondToPing6 = isTruthy(v)
	return w, nil
}

// UPnPConfig reads sky_upnp.html.
func (c *Client) UPnPConfig(ctx context.Context) (*UPnPConfig, error) {
	p, err := c.Get(ctx, "sky_upnp.html")
	if err != nil {
		return nil, err
	}
	return ParseUPnPConfig(p)
}

// ParseUPnPConfig parses sky_upnp.html.
func ParseUPnPConfig(p *Page) (*UPnPConfig, error) {
	f, err := p.Form("name=formname")
	if err != nil {
		return nil, err
	}
	u := &UPnPConfig{
		Enabled:           isTruthy(f.Get("h_enblUpnp")),
		AdvertiseInterval: parseIntDef(f.Get("upnpAdvTime"), 30),
		AdvertiseTTL:      parseIntDef(f.Get("upnpAdvTTL"), 4),
	}
	u.PortMapTable, _ = p.JSVar("portMapTable")
	return u, nil
}

// ALGConfig reads sky_alg.html.
func (c *Client) ALGConfig(ctx context.Context) (*ALGConfig, error) {
	p, err := c.Get(ctx, "sky_alg.html")
	if err != nil {
		return nil, err
	}
	return ParseALGConfig(p)
}

// ParseALGConfig parses sky_alg.html (state in the hidden *algenable inputs).
func ParseALGConfig(p *Page) (*ALGConfig, error) {
	f, err := p.Form("name=formname")
	if err != nil {
		return nil, err
	}
	return &ALGConfig{SIP: f.Get("sipalgenable") == "1", H323: f.Get("h323algenable") == "1"}, nil
}

// EthernetConfig reads sky_eth_setup.html.
func (c *Client) EthernetConfig(ctx context.Context) (*EthernetConfig, error) {
	p, err := c.Get(ctx, "sky_eth_setup.html")
	if err != nil {
		return nil, err
	}
	return ParseEthernetConfig(p)
}

// ParseEthernetConfig parses sky_eth_setup.html.
func ParseEthernetConfig(p *Page) (*EthernetConfig, error) {
	t, err := p.MustJSVar("ethtype")
	if err != nil {
		return nil, err
	}
	e := &EthernetConfig{Type: t}
	v, _ := p.JSVar("eee")
	e.EEE = strings.EqualFold(v, "enabled")
	return e, nil
}
