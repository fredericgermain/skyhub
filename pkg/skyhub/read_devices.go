package skyhub

import (
	"context"
	"regexp"
)

var attachDevRE = regexp.MustCompile(`\battach_dev\s*=\s*'((?:[^'\\]|\\.)*)'`)

// AttachedDevices reads sky_attached_devices.html.
func (c *Client) AttachedDevices(ctx context.Context) ([]AttachedDevice, error) {
	p, err := c.Get(ctx, "sky_attached_devices.html")
	if err != nil {
		return nil, err
	}
	return ParseAttachedDevices(p)
}

// ParseAttachedDevices parses the attach_dev JS string:
// "MAC<br>hostname<br>IPv4<br>dhcp-name<br>IPv6<lf>...".
func ParseAttachedDevices(p *Page) ([]AttachedDevice, error) {
	m := attachDevRE.FindSubmatch(p.Body)
	if m == nil {
		return nil, &ParseError{Page: p.Path, What: "missing attach_dev"}
	}
	blob := jsUnescape(string(m[1]))
	var out []AttachedDevice
	for _, rec := range splitLF(blob) {
		f := splitBR(rec)
		if len(f) < 3 {
			continue
		}
		d := AttachedDevice{
			MAC:      MAC{parseMAC(f[0])},
			Hostname: f[1],
			IPv4:     Addr{parseAddr(f[2])},
		}
		if len(f) > 3 && f[3] != "-" {
			d.DHCPName = f[3]
		}
		if len(f) > 4 {
			d.IPv6 = Addr{parseAddr(f[4])}
		}
		if d.MAC.HardwareAddr == nil {
			continue
		}
		out = append(out, d)
	}
	return out, nil
}
