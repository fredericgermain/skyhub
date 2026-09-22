package skyhub

import (
	"context"
	"strings"

	"golang.org/x/net/html"
)

// FirewallConfig reads sky_firewall_rules.html.
func (c *Client) FirewallConfig(ctx context.Context) (*FirewallConfig, error) {
	p, err := c.Get(ctx, "sky_firewall_rules.html")
	if err != nil {
		return nil, err
	}
	return ParseFirewallConfig(p)
}

// ParseFirewallConfig parses the rule tables and master toggles.
func ParseFirewallConfig(p *Page) (*FirewallConfig, error) {
	doc, err := p.Doc()
	if err != nil {
		return nil, err
	}
	f, err := p.Form("name=fwrules")
	if err != nil {
		return nil, err
	}
	cfg := &FirewallConfig{}
	cfg.Globals.IPv6Firewall = f.Get("ipv6firewallenable") == "1"
	cfg.Globals.IPSecPassthrough = f.Get("ipsecfirewallenable") == "1"
	cfg.Globals.AllowInboundICMPv6Echo = f.Get("icmpv6echoreqallow") == "1"

	for _, t := range []struct {
		id  string
		dir Direction
		sel string
	}{{"in_fw_table", Inbound, "in_sel"}, {"out_fw_table", Outbound, "out_sel"}} {
		tbl := findByID(doc, t.id)
		if tbl == nil {
			return nil, &ParseError{Page: p.Path, What: "missing table " + t.id}
		}
		rules := parseFirewallTable(tbl, t.dir, t.sel)
		if t.dir == Inbound {
			cfg.Inbound = rules
		} else {
			cfg.Outbound = rules
		}
	}
	return cfg, nil
}

func parseFirewallTable(tbl *html.Node, dir Direction, selName string) []FirewallRule {
	var rules []FirewallRule
	walk(tbl, func(n *html.Node) bool {
		if n.Data != "tr" {
			return true
		}
		var (
			r      FirewallRule
			isRule bool
			cells  []string
		)
		r.Direction = dir
		r.IPVersion = 4
		walk(n, func(e *html.Node) bool {
			switch e.Data {
			case "input":
				name := attrOr(e, "name", "")
				switch {
				case name == selName:
					r.Service = attrOr(e, "value", "")
					isRule = true
				case name == "ip_version":
					r.IPVersion = parseIntDef(attrOr(e, "value", "4"), 4)
				case strings.HasPrefix(name, string(dir)+"_enable_"):
					_, r.Enabled = attr(e, "checked")
				}
			case "td":
				cells = append(cells, nodeText(e))
			}
			return true
		})
		if !isRule {
			return false
		}
		// cells: [radio] [#] [enable] [service] [action] [lan] [wan] [log]
		if len(cells) >= 8 {
			r.Position = parseIntDef(cells[1], len(rules)+1)
			r.Action = actionFromLabel(cells[4])
			r.LANUsers = cells[5]
			r.WANServers = cells[6]
			r.Logging = loggingFromLabel(cells[7])
		}
		if r.Position == 0 {
			r.Position = len(rules) + 1
		}
		rules = append(rules, r)
		return false
	})
	return rules
}

var actionLabels = map[string]string{
	"BLOCK always":      ActionBlockAlways,
	"BLOCK by schedule": ActionBlockSchedule,
	"ALLOW always":      ActionAllowAlways,
	"ALLOW by schedule": ActionAllowSchedule,
}

var actionValues = map[string]string{
	ActionBlockAlways:   "0",
	ActionBlockSchedule: "1",
	ActionAllowAlways:   "2",
	ActionAllowSchedule: "3",
}

var loggingLabels = map[string]string{
	"Never":     LogNever,
	"Always":    LogAlways,
	"Match":     LogMatch,
	"Not Match": LogNotMatch,
}

var loggingValues = map[string]string{
	LogNever:    "0",
	LogAlways:   "1",
	LogMatch:    "2",
	LogNotMatch: "3",
}

func actionFromLabel(s string) string {
	s = strings.TrimSpace(s)
	if v, ok := actionLabels[s]; ok {
		return v
	}
	return strings.ToLower(strings.ReplaceAll(s, " ", "_"))
}

func loggingFromLabel(s string) string {
	s = strings.TrimSpace(s)
	if v, ok := loggingLabels[s]; ok {
		return v
	}
	return strings.ToLower(strings.ReplaceAll(s, " ", "_"))
}
