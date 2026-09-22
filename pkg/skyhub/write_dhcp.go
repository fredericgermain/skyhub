package skyhub

import (
	"context"
	"fmt"
	"net"
	"strconv"
	"strings"
)

// AddDHCPReservation creates a reserved address. The hub's UI strips the
// colons from the MAC before posting; the change takes effect at the next
// DHCP renewal (the UI offers an immediate reboot, which is never sent).
func (c *Client) AddDHCPReservation(ctx context.Context, r DHCPReservation) error {
	if r.MAC.HardwareAddr == nil || len(r.MAC.HardwareAddr) != 6 {
		return fmt.Errorf("skyhub: reservation needs a 48-bit MAC")
	}
	if !r.IP.Is4() {
		return fmt.Errorf("skyhub: reservation needs an IPv4 address")
	}
	if len(r.Name) > 17 {
		return fmt.Errorf("skyhub: reservation name longer than 17 characters")
	}
	_, err := c.PostForm(ctx, "sky_lanaddmac.sky", "sky_lan_ip_setup_addmac.html", "action=sky_lanaddmac.sky", func(_ *Page, f *Form) error {
		f.SetIPv4("new_lan_ip_addr", r.IP.Addr)
		f.Set("new_lan_mac", macNoColons(r.MAC.HardwareAddr))
		f.Set("new_lan_devname", r.Name)
		f.Set("mac", "addnewmac")
		f.Set("static_ip", r.IP.String())
		f.Set("action", "add")
		f.Set("todo", "save")
		return nil
	})
	return err
}

// RemoveDHCPReservation deletes the reservation for mac. ErrNotFound when
// there is none.
func (c *Client) RemoveDHCPReservation(ctx context.Context, mac net.HardwareAddr) error {
	_, err := c.PostForm(ctx, "sky_lanaddmac.sky", "sky_lan_ip_setup.html", "name=frmLan2", func(host *Page, f *Form) error {
		lp, err := ParseLANPage(host)
		if err != nil {
			return err
		}
		idx := 0
		for _, r := range lp.Reservations {
			if strings.EqualFold(r.MAC.String(), mac.String()) {
				idx = r.Index
			}
		}
		if idx == 0 {
			return ErrNotFound
		}
		f.Set("index", strconv.Itoa(idx))
		f.Set("mac", "")
		f.Set("action", "remove")
		f.Set("todo", "") // "reboot" would restart the hub
		return nil
	})
	return err
}

func macNoColons(m net.HardwareAddr) string {
	return strings.ReplaceAll(strings.ToLower(m.String()), ":", "")
}
