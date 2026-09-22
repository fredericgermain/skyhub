package main

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"strconv"
	"strings"
	"time"

	"github.com/fredericgermain/skyhub/pkg/skyhub"
)

// hub is the subset of the client the poller uses.
type hub interface {
	SystemStats(ctx context.Context) (*skyhub.SystemStats, error)
	WANStatus(ctx context.Context) (*skyhub.WANStatus, error)
	RouterInfo(ctx context.Context) (*skyhub.RouterInfo, error)
	AttachedDevices(ctx context.Context) ([]skyhub.AttachedDevice, error)
	WirelessRadios(ctx context.Context) ([]skyhub.WirelessRadio, error)
	WirelessOnOff(ctx context.Context) (*skyhub.WirelessOnOff, error)
}

const maxFailures = 3

type deviceState struct {
	MAC      string    `json:"mac"`
	Hostname string    `json:"hostname"`
	IPv4     string    `json:"ipv4"`
	IPv6     string    `json:"ipv6"`
	LastSeen time.Time `json:"last_seen"`
	home     bool
}

// Poller polls the hub and publishes topics.
type Poller struct {
	cfg  Config
	hub  hub
	pub  Publisher
	log  *slog.Logger
	now  func() time.Time
	base string

	last     map[string]string // change-only topics: last payload
	devices  map[string]*deviceState
	failures int
	online   bool

	// identity for discovery
	firmware string
	model    string
	wanMAC   string
	trackers map[string]bool // discovery already published
}

// NewPoller wires a poller.
func NewPoller(cfg Config, h hub, pub Publisher, log *slog.Logger) *Poller {
	return &Poller{
		cfg:      cfg,
		hub:      h,
		pub:      pub,
		log:      log,
		now:      time.Now,
		base:     strings.TrimSuffix(cfg.BaseTopic, "/"),
		last:     map[string]string{},
		devices:  map[string]*deviceState{},
		trackers: map[string]bool{},
	}
}

func (p *Poller) topic(parts ...string) string {
	return p.base + "/" + strings.Join(parts, "/")
}

func (p *Poller) publish(topic, payload string) {
	if err := p.pub.Publish(topic, payload, true); err != nil {
		p.log.Warn("publish failed", "topic", topic, "err", err)
	}
}

// publishChanged publishes only when the payload differs from the last one.
func (p *Poller) publishChanged(topic, payload string) {
	if p.last[topic] == payload {
		return
	}
	p.last[topic] = payload
	p.publish(topic, payload)
}

func (p *Poller) ok() {
	p.failures = 0
	if !p.online {
		p.online = true
		p.publish(p.topic("status"), "online")
	}
}

func (p *Poller) fail(what string, err error) {
	p.failures++
	p.log.Error("poll failed", "what", what, "err", err, "consecutive", p.failures)
	if p.failures >= maxFailures && p.online {
		p.online = false
		p.publish(p.topic("status"), "offline")
	}
}

func b2s(b bool) string {
	if b {
		return "true"
	}
	return "false"
}

func u64(v uint64) string  { return strconv.FormatUint(v, 10) }
func i2s(v int) string     { return strconv.Itoa(v) }
func f2s(v float64) string { return strconv.FormatFloat(v, 'f', -1, 64) }
func secs(d skyhub.Duration) string {
	return strconv.FormatInt(int64(d.Std()/time.Second), 10)
}

// addrStr renders an address or prefix as "" when unset (netip's String
// would say "invalid IP").
func addrStr(a skyhub.Addr) string {
	if !a.IsValid() {
		return ""
	}
	return a.String()
}

func prefixStr(p skyhub.Prefix) string {
	if !p.IsValid() {
		return ""
	}
	return p.String()
}

func macKey(m skyhub.MAC) string {
	return strings.ReplaceAll(m.String(), ":", "")
}

// FastTick publishes the statistics topics.
func (p *Poller) FastTick(ctx context.Context) error {
	s, err := p.hub.SystemStats(ctx)
	if err != nil {
		p.fail("stats", err)
		return err
	}
	p.ok()
	p.publish(p.topic("system", "uptime_s"), secs(s.Uptime))
	for _, port := range s.Ports {
		t := func(k string) string { return p.topic("port", port.Key, k) }
		p.publish(t("status"), port.Status)
		p.publish(t("tx_pkts"), u64(port.TxPkts))
		p.publish(t("rx_pkts"), u64(port.RxPkts))
		p.publish(t("collisions"), u64(port.Collisions))
		p.publish(t("tx_bps"), u64(port.TxBps))
		p.publish(t("rx_bps"), u64(port.RxBps))
		p.publish(t("uptime_s"), secs(port.Uptime))
	}
	if d := s.DSL; d != nil {
		first := func(v []float64) float64 {
			if len(v) > 0 {
				return v[0]
			}
			return 0
		}
		p.publish(p.topic("dsl", "down_kbps"), i2s(d.DownKbps))
		p.publish(p.topic("dsl", "up_kbps"), i2s(d.UpKbps))
		p.publish(p.topic("dsl", "attenuation_down_db"), f2s(first(d.AttenuationDownDB)))
		p.publish(p.topic("dsl", "attenuation_up_db"), f2s(first(d.AttenuationUpDB)))
		p.publish(p.topic("dsl", "noise_margin_down_db"), f2s(d.NoiseMarginDownDB))
		p.publish(p.topic("dsl", "noise_margin_up_db"), f2s(d.NoiseMarginUpDB))
	}
	if j, err := json.Marshal(s); err == nil {
		p.publish(p.topic("stats"), string(j))
	}
	return nil
}

// SlowTick publishes WAN, wifi and device topics (on change only).
func (p *Poller) SlowTick(ctx context.Context) error {
	var firstErr error
	if w, err := p.hub.WANStatus(ctx); err != nil {
		p.fail("wan", err)
		firstErr = err
	} else {
		p.ok()
		p.publishWAN(w)
	}
	if radios, err := p.hub.WirelessRadios(ctx); err != nil {
		p.fail("wifi", err)
		if firstErr == nil {
			firstErr = err
		}
	} else {
		p.ok()
		for _, r := range radios {
			band := strings.ReplaceAll(r.Band, ".", "")
			t := func(k string) string { return p.topic("wifi", band, k) }
			p.publishChanged(t("enabled"), b2s(r.Enabled))
			p.publishChanged(t("ssid"), r.SSID)
			p.publishChanged(t("channel"), i2s(r.Channel))
			p.publishChanged(t("bandwidth"), r.Bandwidth)
			p.publishChanged(t("hidden"), b2s(r.Hidden))
			p.publishChanged(t("wps"), b2s(r.WPSEnabled))
		}
	}
	if devs, err := p.hub.AttachedDevices(ctx); err != nil {
		p.fail("devices", err)
		if firstErr == nil {
			firstErr = err
		}
	} else {
		p.ok()
		p.publishDevices(devs)
	}
	return firstErr
}

func (p *Poller) publishWAN(w *skyhub.WANStatus) {
	t := func(k string) string { return p.topic("wan", k) }
	status := "down"
	if w.Up {
		status = "up"
	}
	p.publishChanged(t("status"), status)
	p.publishChanged(t("ip"), addrStr(w.IPv4))
	p.publishChanged(t("gateway"), addrStr(w.Gateway))
	p.publishChanged(t("netmask"), addrStr(w.Netmask))
	var dns []string
	for _, d := range w.DNS {
		dns = append(dns, d.String())
	}
	p.publishChanged(t("dns"), strings.Join(dns, ","))
	p.publishChanged(t("protocol"), w.Protocol)
	p.publishChanged(t("ipv6"), prefixStr(w.IPv6))
	p.publishChanged(t("ipv6_prefix"), prefixStr(w.DelegatedPrefix))
	p.publish(t("uptime_s"), secs(w.Uptime))
	if m := macKey(w.MAC); m != "" && p.wanMAC == "" {
		p.wanMAC = m
	}
}

func (p *Poller) publishDevices(devs []skyhub.AttachedDevice) {
	now := p.now()
	seen := map[string]bool{}
	for _, d := range devs {
		key := macKey(d.MAC)
		if key == "" {
			continue
		}
		seen[key] = true
		st, ok := p.devices[key]
		if !ok {
			st = &deviceState{}
			p.devices[key] = st
		}
		st.MAC = d.MAC.String()
		if d.Hostname != "" && d.Hostname != "UNKNOWN" {
			st.Hostname = d.Hostname
		} else if d.DHCPName != "" && d.DHCPName != "UNKNOWN" {
			st.Hostname = d.DHCPName
		}
		st.IPv4 = addrStr(d.IPv4)
		st.IPv6 = addrStr(d.IPv6)
		st.LastSeen = now
		if !p.trackers[key] && p.cfg.HAPrefix != "" {
			p.publishTrackerDiscovery(key, st)
			p.trackers[key] = true
		}
		if !st.home {
			st.home = true
			p.publish(p.topic("device", key, "state"), "home")
		}
		if j, err := json.Marshal(st); err == nil {
			p.publishChanged(p.topic("device", key, "attributes"), string(j))
		}
	}
	for key, st := range p.devices {
		if seen[key] || !st.home {
			continue
		}
		if now.Sub(st.LastSeen) >= p.cfg.DeviceTTL {
			st.home = false
			p.publish(p.topic("device", key, "state"), "not_home")
		}
	}
}

// InfoTick refreshes firmware/model and republishes discovery.
func (p *Poller) InfoTick(ctx context.Context) error {
	ri, err := p.hub.RouterInfo(ctx)
	if err != nil {
		p.fail("info", err)
		return err
	}
	p.ok()
	p.firmware = ri.Firmware
	if m := macKey(ri.WAN.MAC); m != "" {
		p.wanMAC = m
	}
	p.publishChanged(p.topic("system", "firmware"), ri.Firmware)
	if p.model == "" {
		if w, err := p.hub.WirelessOnOff(ctx); err == nil && w.Model != "" {
			p.model = w.Model
		}
	}
	if p.cfg.HAPrefix != "" {
		p.publishDiscovery()
		for key, st := range p.devices {
			p.publishTrackerDiscovery(key, st)
			p.trackers[key] = true
		}
	}
	return nil
}

// Run polls until ctx is cancelled, then publishes offline.
func (p *Poller) Run(ctx context.Context) {
	_ = p.InfoTick(ctx)
	_ = p.SlowTick(ctx)
	_ = p.FastTick(ctx)

	fast := time.NewTicker(p.cfg.StatsInterval)
	slow := time.NewTicker(p.cfg.SlowInterval)
	info := time.NewTicker(p.cfg.InfoInterval)
	defer fast.Stop()
	defer slow.Stop()
	defer info.Stop()
	for {
		select {
		case <-ctx.Done():
			p.online = false
			if err := p.pub.Publish(p.topic("status"), "offline", true); err != nil {
				p.log.Warn("publish offline failed", "err", err)
			}
			return
		case <-fast.C:
			_ = p.FastTick(ctx)
		case <-slow.C:
			_ = p.SlowTick(ctx)
		case <-info.C:
			_ = p.InfoTick(ctx)
		}
	}
}

func (p *Poller) String() string {
	return fmt.Sprintf("poller(base=%s, online=%v)", p.base, p.online)
}
