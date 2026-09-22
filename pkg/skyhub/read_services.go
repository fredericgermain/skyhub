package skyhub

import (
	"context"
	"strings"

	"golang.org/x/net/html"
)

// Services reads the custom service table on sky_services.html.
func (c *Client) Services(ctx context.Context) ([]Service, error) {
	p, err := c.Get(ctx, "sky_services.html")
	if err != nil {
		return nil, err
	}
	return ParseServices(p)
}

// ParseServices parses the services table.
func ParseServices(p *Page) ([]Service, error) {
	doc, err := p.Doc()
	if err != nil {
		return nil, err
	}
	var out []Service
	walk(doc, func(n *html.Node) bool {
		if n.Data != "tr" {
			return true
		}
		var name string
		var cells []string
		walk(n, func(e *html.Node) bool {
			switch e.Data {
			case "input":
				if attrOr(e, "name", "") == "ruleSelect" {
					name = attrOr(e, "value", "")
				}
			case "td":
				cells = append(cells, nodeText(e))
			}
			return true
		})
		if name == "" || len(cells) < 5 {
			return false
		}
		s := Service{Index: parseIntDef(cells[1], len(out)+1), Name: name, Protocol: protocolFromLabel(cells[3])}
		start, end, _ := strings.Cut(cells[4], "...")
		s.StartPort = parseIntDef(start, 0)
		s.EndPort = parseIntDef(end, s.StartPort)
		out = append(out, s)
		return false
	})
	if _, err := p.Form("name=frmService"); err != nil {
		return nil, err
	}
	return out, nil
}

// BuiltinServices returns the hub's predefined service names, read from
// sky_services_custom-add.html.
func (c *Client) BuiltinServices(ctx context.Context) ([]string, error) {
	p, err := c.Get(ctx, "sky_services_custom-add.html")
	if err != nil {
		return nil, err
	}
	s, err := p.MustJSVar("services_string")
	if err != nil {
		return nil, err
	}
	var out []string
	for _, n := range strings.Split(s, ",") {
		if n = strings.TrimSpace(n); n != "" {
			out = append(out, n)
		}
	}
	return out, nil
}

func protocolFromLabel(s string) string {
	switch strings.ToUpper(strings.TrimSpace(s)) {
	case "TCP":
		return "tcp"
	case "UDP":
		return "udp"
	case "TCP OR UDP", "TCP/UDP", "TCP_UDP":
		return "tcp_udp"
	}
	return strings.ToLower(strings.TrimSpace(s))
}

func protocolToValue(p string) string {
	switch strings.ToLower(p) {
	case "tcp":
		return "TCP"
	case "udp":
		return "UDP"
	default:
		return "TCP_UDP"
	}
}
