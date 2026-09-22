package skyhub

import (
	"context"
	"fmt"
	"regexp"
	"strconv"
)

var serviceNameRE = regexp.MustCompile(`^[A-Za-z0-9_-]{1,12}$`)

// AddService defines a custom port service (name ≤ 12 chars, [A-Za-z0-9_-]).
func (c *Client) AddService(ctx context.Context, s Service) error {
	if !serviceNameRE.MatchString(s.Name) {
		return fmt.Errorf("skyhub: invalid service name %q (1-12 chars, letters, digits, - and _)", s.Name)
	}
	if s.EndPort == 0 {
		s.EndPort = s.StartPort
	}
	if s.StartPort < 1 || s.EndPort > 65535 || s.StartPort > s.EndPort {
		return fmt.Errorf("skyhub: invalid port range %d-%d", s.StartPort, s.EndPort)
	}
	_, err := c.PostForm(ctx, "sky_service_custom-add.sky", "sky_services_custom-add.html", "action=sky_service_custom-add.sky", func(_ *Page, f *Form) error {
		f.Set("service_name", s.Name)
		f.Set("svc_type", protocolToValue(s.Protocol))
		f.Set("serv_sport", strconv.Itoa(s.StartPort))
		f.Set("serv_endport", strconv.Itoa(s.EndPort))
		f.Set("todo", "add") // the Apply link sets todo=add
		return nil
	})
	return err
}

// RemoveService deletes a custom service by name (ErrNotFound if absent).
func (c *Client) RemoveService(ctx context.Context, name string) error {
	_, err := c.PostForm(ctx, "sky_service_custom-add.sky", "sky_services.html", "name=frmService", func(host *Page, f *Form) error {
		svcs, err := ParseServices(host)
		if err != nil {
			return err
		}
		found := false
		for _, s := range svcs {
			if s.Name == name {
				found = true
			}
		}
		if !found {
			return ErrNotFound
		}
		f.Set("ruleSelect", name)
		f.Set("todo", "delete")
		return nil
	})
	return err
}
