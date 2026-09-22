package skyhub

import (
	"context"
	"fmt"
	"net/netip"
	"strconv"
	"strings"
)

func ipTypeValue(t string) string {
	switch strings.ToLower(t) {
	case "", IPTypeAny:
		return "anyip"
	case IPTypeSingle:
		return "singleip"
	case IPTypeRange:
		return "rangeip"
	}
	return t
}

func parseIPv4Field(s, what string) (netip.Addr, error) {
	a, err := netip.ParseAddr(strings.TrimSpace(s))
	if err != nil || !a.Is4() {
		return netip.Addr{}, fmt.Errorf("skyhub: %s: %q is not an IPv4 address", what, s)
	}
	return a, nil
}

// AddFirewallRule appends a rule to the inbound or outbound list. Inbound
// IPv4 rules take a single LAN host (LANStart); outbound rules take a LAN
// type/range. WAN side is any/single/range for both. IPv6 rules use the
// *IPv6 fields (LANIPv6 is "prefix|interface-id", e.g. "2001:db8::|::10",
// or just an interface id to use the global prefix).
func (c *Client) AddFirewallRule(ctx context.Context, r FirewallRule) error {
	host := "sky_firewall_rules-in.html"
	if r.Direction == Outbound {
		host = "sky_firewall_rules-out.html"
	}
	if r.IPVersion == 0 {
		r.IPVersion = 4
	}
	formSel := "index=0"
	if r.IPVersion == 6 {
		formSel = "index=1"
	}
	if r.Service == "" {
		return fmt.Errorf("skyhub: firewall rule needs a service name")
	}
	act, ok := actionValues[r.Action]
	if !ok {
		return fmt.Errorf("skyhub: unknown action %q", r.Action)
	}
	logv, ok := loggingValues[r.Logging]
	if r.Logging == "" {
		logv, ok = "0", true
	}
	if !ok {
		return fmt.Errorf("skyhub: unknown logging mode %q", r.Logging)
	}
	_, err := c.PostForm(ctx, "sky_firewall_rules.cmd", host, formSel, func(_ *Page, f *Form) error {
		found := false
		for _, o := range f.Options("service_list") {
			if o == r.Service {
				found = true
			}
		}
		if !found {
			return fmt.Errorf("skyhub: service %q is not defined on the hub: %w", r.Service, ErrNotFound)
		}
		f.Set("service_list", r.Service)
		f.Set("fw_action", act)
		f.Set("fw_logging", logv)
		f.Set("fwall_action", "add")
		f.Set("edit", "0")
		f.Set("todo", "save")
		if r.IPVersion == 6 {
			return buildIPv6Rule(f, r)
		}
		// LAN side.
		if f.Has("fw_laniptype") {
			f.Set("fw_laniptype", ipTypeValue(r.LANType))
		}
		if r.LANStart != "" || !f.Has("fw_laniptype") || ipTypeValue(r.LANType) != "anyip" {
			a, err := parseIPv4Field(r.LANStart, "lan_start")
			if err != nil {
				return err
			}
			f.SetIPv4("lan_start_ip", a)
		}
		if ipTypeValue(r.LANType) == "rangeip" {
			a, err := parseIPv4Field(r.LANEnd, "lan_end")
			if err != nil {
				return err
			}
			for i := 1; i <= 4; i++ {
				f.Enable("lan_finish_ip" + strconv.Itoa(i))
			}
			f.SetIPv4("lan_finish_ip", a)
		}
		// WAN side.
		wt := ipTypeValue(r.WANType)
		f.Set("fw_waniptype", wt)
		if wt == "singleip" || wt == "rangeip" {
			a, err := parseIPv4Field(r.WANStart, "wan_start")
			if err != nil {
				return err
			}
			for i := 1; i <= 4; i++ {
				f.Enable("wan_start_ip" + strconv.Itoa(i))
			}
			f.SetIPv4("wan_start_ip", a)
		}
		if wt == "rangeip" {
			a, err := parseIPv4Field(r.WANEnd, "wan_end")
			if err != nil {
				return err
			}
			for i := 1; i <= 4; i++ {
				f.Enable("wan_finish_ip" + strconv.Itoa(i))
			}
			f.SetIPv4("wan_finish_ip", a)
		}
		return nil
	})
	return err
}

func buildIPv6Rule(f *Form, r FirewallRule) error {
	prefix, iface, ok := strings.Cut(r.LANIPv6, "|")
	if !ok {
		prefix, iface = "Global Prefix", r.LANIPv6
	}
	if f.Has("fw_laniptype") {
		f.Set("fw_laniptype", ipTypeValue(r.LANType))
	}
	f.Enable("lan_start_ip6_prefix")
	f.Enable("lan_start_ip6")
	f.Set("lan_start_ip6_prefix", prefix)
	f.Set("lan_start_ip6", iface)
	// checkDataIP6() combines prefix + interface id into lan_ip6_start_comb.
	comb := iface
	if strings.HasPrefix(iface, "::") {
		if prefix == "Global Prefix" {
			comb = "fe80" + iface
		} else {
			comb = prefix + iface
		}
	} else if prefix == "Global Prefix" {
		comb = "fe80::" + iface
	} else {
		comb = prefix + ":" + iface
	}
	if _, err := netip.ParseAddr(comb); err != nil {
		return fmt.Errorf("skyhub: lan_ipv6 %q gives invalid address %q", r.LANIPv6, comb)
	}
	f.Set("lan_ip6_start_comb", comb)
	wt := ipTypeValue(r.WANType)
	f.Set("fw_waniptype", wt)
	if wt == "singleip" || wt == "rangeip" {
		if _, err := netip.ParseAddr(r.WANIPv6); err != nil {
			return fmt.Errorf("skyhub: wan_ipv6 %q invalid", r.WANIPv6)
		}
		f.Enable("wan_start_ip6")
		f.Set("wan_start_ip6", r.WANIPv6)
	}
	if wt == "rangeip" {
		if _, err := netip.ParseAddr(r.WANIPv6End); err != nil {
			return fmt.Errorf("skyhub: wan_ipv6_end %q invalid", r.WANIPv6End)
		}
		f.Enable("wan_finish_ip6")
		f.Set("wan_finish_ip6", r.WANIPv6End)
	}
	return nil
}

// RemoveFirewallRule deletes the rule for service in a direction.
func (c *Client) RemoveFirewallRule(ctx context.Context, dir Direction, ipVersion int, service string) error {
	_, err := c.PostForm(ctx, "sky_firewall_rules.cmd", "sky_firewall_rules.html", "name=fwrules", func(host *Page, f *Form) error {
		cfg, err := ParseFirewallConfig(host)
		if err != nil {
			return err
		}
		rule := cfg.Find(dir, ipVersion, service)
		if rule == nil {
			return ErrNotFound
		}
		sel := "in_sel"
		if dir == Outbound {
			sel = "out_sel"
		}
		// handleFirewallRuleDeletion(): radio selection + radioTable(..., 'delete').
		f.Set(sel, service)
		f.Set("ip_version", strconv.Itoa(rule.IPVersion))
		f.Set("selected_index_edit_delete", strconv.Itoa(rule.Position-1))
		f.Set("fwall_action", "remove")
		f.Set("next_file", "sky_firewall_rules.html")
		f.Set("todo", "delete")
		return nil
	})
	return err
}

// SetFirewallRuleSet re-posts one direction's whole rule list with a new
// order and enabled flags, the way the list page's Apply button does.
// order must be a permutation of the current service names; enabled maps
// service name to its flag (missing = keep current).
func (c *Client) SetFirewallRuleSet(ctx context.Context, dir Direction, order []string, enabled map[string]bool) error {
	_, err := c.PostForm(ctx, "sky_firewall_rules.cmd", "sky_firewall_rules.html", "name=fwrules", func(host *Page, f *Form) error {
		cfg, err := ParseFirewallConfig(host)
		if err != nil {
			return err
		}
		if err := applyRuleSet(f, cfg, dir, order, enabled); err != nil {
			return err
		}
		other := Outbound
		if dir == Outbound {
			other = Inbound
		}
		if err := applyRuleSet(f, cfg, other, nil, nil); err != nil {
			return err
		}
		f.Set("fwall_action", "enable_disable")
		f.Set("todo", "save")
		return nil
	})
	return err
}

// applyRuleSet fills <dir>_enable, <dir>_fwrule_seq and
// <dir>_fwrule_ipversion_seq and removes the per-row checkboxes (saveEnable()
// unchecks them before submit).
func applyRuleSet(f *Form, cfg *FirewallConfig, dir Direction, order []string, enabled map[string]bool) error {
	rules := cfg.Rules(dir)
	if order == nil {
		for _, r := range rules {
			order = append(order, r.Service)
		}
	}
	if len(order) != len(rules) {
		return fmt.Errorf("skyhub: order has %d entries, hub has %d %s rules", len(order), len(rules), dir)
	}
	byName := map[string]FirewallRule{}
	for _, r := range rules {
		byName[r.Service] = r
	}
	var bits, seq, vseq []string
	changed := false
	for i, name := range order {
		r, ok := byName[name]
		if !ok {
			return fmt.Errorf("skyhub: %s rule %q not on hub: %w", dir, name, ErrNotFound)
		}
		on := r.Enabled
		if v, ok := enabled[name]; ok {
			on = v
		}
		if on {
			bits = append(bits, "1")
		} else {
			bits = append(bits, "0")
		}
		seq = append(seq, strconv.Itoa(r.Position))
		vseq = append(vseq, strconv.Itoa(r.IPVersion))
		if r.Position != i+1 {
			changed = true
		}
		f.Del(string(dir) + "_enable_" + strconv.Itoa(r.Position))
	}
	f.Set(string(dir)+"_enable", strings.Join(bits, ""))
	if changed {
		f.Set(string(dir)+"_fwrule_seq", strings.Join(seq, "-"))
		f.Set(string(dir)+"_fwrule_ipversion_seq", strings.Join(vseq, "-"))
	} else {
		f.Set(string(dir)+"_fwrule_seq", "")
		f.Set(string(dir)+"_fwrule_ipversion_seq", "")
	}
	return nil
}

// SetFirewallGlobals sets the master toggles, keeping rules as they are.
func (c *Client) SetFirewallGlobals(ctx context.Context, g FirewallGlobals) error {
	_, err := c.PostForm(ctx, "sky_firewall_rules.cmd", "sky_firewall_rules.html", "name=fwrules", func(host *Page, f *Form) error {
		cfg, err := ParseFirewallConfig(host)
		if err != nil {
			return err
		}
		if err := applyRuleSet(f, cfg, Inbound, nil, nil); err != nil {
			return err
		}
		if err := applyRuleSet(f, cfg, Outbound, nil, nil); err != nil {
			return err
		}
		f.Set("ipv6firewallenable", b01(g.IPv6Firewall))
		f.Set("ipsecfirewallenable", b01(g.IPSecPassthrough))
		f.Set("icmpv6echoreqallow", b01(g.AllowInboundICMPv6Echo))
		f.SetCheckbox("enable_ipv6_firewall", g.IPv6Firewall)
		f.SetCheckbox("enable_ipsec_firewall", g.IPSecPassthrough)
		f.SetCheckbox("allow_inbound_icmpv6_echoreq", g.AllowInboundICMPv6Echo)
		f.Set("fwall_action", "enable_disable")
		f.Set("todo", "save")
		return nil
	})
	return err
}

func b01(b bool) string {
	if b {
		return "1"
	}
	return "0"
}
