package main

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/fredericgermain/skyhub/pkg/skyhub"
	"github.com/fredericgermain/skyhub/pkg/skyhubtest"
)

const fixtureDir = "../../pkg/skyhub/testdata/7.04.0208.R"

type recorder struct {
	mu   sync.Mutex
	msgs map[string]string
	log  []string
}

func (r *recorder) Publish(topic, payload string, retain bool) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.msgs == nil {
		r.msgs = map[string]string{}
	}
	r.msgs[topic] = payload
	r.log = append(r.log, topic+"="+payload)
	return nil
}

func (r *recorder) Close() {}

func (r *recorder) get(topic string) (string, bool) {
	r.mu.Lock()
	defer r.mu.Unlock()
	v, ok := r.msgs[topic]
	return v, ok
}

func (r *recorder) count(topic string) int {
	r.mu.Lock()
	defer r.mu.Unlock()
	n := 0
	for _, l := range r.log {
		if strings.HasPrefix(l, topic+"=") {
			n++
		}
	}
	return n
}

func newTestPoller(t *testing.T, ttl time.Duration) (*skyhubtest.FakeHub, *Poller, *recorder) {
	t.Helper()
	h := skyhubtest.NewFakeHub(t, fixtureDir, "admin", "secret12")
	c, err := skyhub.New(h.URL(), "admin", "secret12", skyhub.WithTimeout(5*time.Second))
	if err != nil {
		t.Fatal(err)
	}
	cfg := Config{BaseTopic: "skyhub", HAPrefix: "homeassistant", DeviceTTL: ttl, StatsInterval: 10 * time.Second, SlowInterval: time.Minute}
	rec := &recorder{}
	p := NewPoller(cfg, c, rec, slog.New(slog.NewTextHandler(io.Discard, nil)))
	return h, p, rec
}

func TestTicksPublishExpectedTopics(t *testing.T) {
	_, p, rec := newTestPoller(t, 5*time.Minute)
	ctx := context.Background()
	if err := p.InfoTick(ctx); err != nil {
		t.Fatal(err)
	}
	if err := p.SlowTick(ctx); err != nil {
		t.Fatal(err)
	}
	if err := p.FastTick(ctx); err != nil {
		t.Fatal(err)
	}

	// Expected values come from the same fixtures via the parsers.
	stats, _ := skyhub.ParseSystemStats(fixturePage(t, "sky_system.html"))
	wan, _ := skyhub.ParseWANStatus(fixturePage(t, "sky_st_poe.html"))

	want := map[string]string{
		"skyhub/status":                   "online",
		"skyhub/system/firmware":          "7.04.0208.R",
		"skyhub/system/uptime_s":          strconv.FormatInt(int64(stats.Uptime.Std().Seconds()), 10),
		"skyhub/dsl/down_kbps":            strconv.Itoa(stats.DSL.DownKbps),
		"skyhub/dsl/up_kbps":              strconv.Itoa(stats.DSL.UpKbps),
		"skyhub/dsl/noise_margin_down_db": strconv.FormatFloat(stats.DSL.NoiseMarginDownDB, 'f', -1, 64),
		"skyhub/dsl/attenuation_down_db":  strconv.FormatFloat(stats.DSL.AttenuationDownDB[0], 'f', -1, 64),
		"skyhub/port/wan/tx_pkts":         strconv.FormatUint(stats.Port("wan").TxPkts, 10),
		"skyhub/port/wlan5/status":        "Up",
		"skyhub/wan/status":               "up",
		"skyhub/wan/ip":                   wan.IPv4.String(),
		"skyhub/wan/gateway":              wan.Gateway.String(),
		"skyhub/wan/protocol":             "ipoe",
		"skyhub/wan/ipv6_prefix":          wan.DelegatedPrefix.String(),
		"skyhub/wifi/24/ssid":             "TestSSID",
		"skyhub/wifi/5/channel":           "36",
		"skyhub/wifi/5/enabled":           "true",
	}
	for topic, w := range want {
		got, ok := rec.get(topic)
		if !ok {
			t.Errorf("%s: not published", topic)
			continue
		}
		if got != w {
			t.Errorf("%s = %q, want %q", topic, got, w)
		}
	}
	var snap skyhub.SystemStats
	if s, ok := rec.get("skyhub/stats"); !ok || json.Unmarshal([]byte(s), &snap) != nil || len(snap.Ports) != 4 {
		t.Errorf("skyhub/stats snapshot bad: %q", s)
	}

	// Devices: every fixture device is home with attributes and a tracker.
	devs, _ := skyhub.ParseAttachedDevices(fixturePage(t, "sky_attached_devices.html"))
	if len(devs) == 0 {
		t.Fatal("fixture has no devices")
	}
	for _, d := range devs {
		key := strings.ReplaceAll(d.MAC.String(), ":", "")
		if v, _ := rec.get("skyhub/device/" + key + "/state"); v != "home" {
			t.Errorf("device %s state = %q", key, v)
		}
		attrs, ok := rec.get("skyhub/device/" + key + "/attributes")
		if !ok {
			t.Errorf("device %s: no attributes", key)
		} else {
			var st deviceState
			if err := json.Unmarshal([]byte(attrs), &st); err != nil || st.MAC != d.MAC.String() || st.LastSeen.IsZero() || strings.Contains(attrs, "invalid") {
				t.Errorf("device %s attrs = %s", key, attrs)
			}
		}
		if _, ok := rec.get("homeassistant/device_tracker/skyhub/tracker_" + key + "/config"); !ok {
			t.Errorf("device %s: no tracker discovery", key)
		}
	}

	// Discovery for the fixed sensors.
	cfgJSON, ok := rec.get("homeassistant/sensor/skyhub/dsl_down_kbps/config")
	if !ok {
		t.Fatal("no dsl_down_kbps discovery")
	}
	var e haEntity
	if err := json.Unmarshal([]byte(cfgJSON), &e); err != nil {
		t.Fatal(err)
	}
	if e.StateTopic != "skyhub/dsl/down_kbps" || e.DeviceClass != "data_rate" || e.UnitOfMeasurement != "kbit/s" || e.UniqueID != "skyhub_dsl_down_kbps" || e.AvailabilityTopic != "skyhub/status" {
		t.Errorf("discovery = %+v", e)
	}
	if len(e.Device.Identifiers) != 1 || !strings.HasPrefix(e.Device.Identifiers[0], "skyhub_") || e.Device.Identifiers[0] == "skyhub_unknown" || e.Device.SWVersion != "7.04.0208.R" {
		t.Errorf("device block = %+v", e.Device)
	}
	if _, ok := rec.get("homeassistant/binary_sensor/skyhub/wan/config"); !ok {
		t.Error("no wan binary_sensor discovery")
	}

	// Slow topics are change-only: a second slow tick must not republish ssid.
	before := rec.count("skyhub/wifi/24/ssid")
	if err := p.SlowTick(ctx); err != nil {
		t.Fatal(err)
	}
	if rec.count("skyhub/wifi/24/ssid") != before {
		t.Error("unchanged slow topic was republished")
	}
}

func TestDeviceNotHomeAfterTTL(t *testing.T) {
	h, p, rec := newTestPoller(t, 5*time.Minute)
	ctx := context.Background()
	now := time.Date(2026, 9, 23, 12, 0, 0, 0, time.UTC)
	p.now = func() time.Time { return now }
	if err := p.SlowTick(ctx); err != nil {
		t.Fatal(err)
	}
	devs, _ := skyhub.ParseAttachedDevices(fixturePage(t, "sky_attached_devices.html"))
	key := strings.ReplaceAll(devs[0].MAC.String(), ":", "")
	if v, _ := rec.get("skyhub/device/" + key + "/state"); v != "home" {
		t.Fatalf("state = %q", v)
	}

	// Device list becomes empty; within TTL it stays home.
	h.SetFixture("sky_attached_devices.html", []byte("<html><script>var i, j, k, attach_dev = '&nbsp;';</script></html>"))
	now = now.Add(time.Minute)
	if err := p.SlowTick(ctx); err != nil {
		t.Fatal(err)
	}
	if v, _ := rec.get("skyhub/device/" + key + "/state"); v != "home" {
		t.Fatalf("state within TTL = %q", v)
	}
	now = now.Add(5 * time.Minute)
	if err := p.SlowTick(ctx); err != nil {
		t.Fatal(err)
	}
	if v, _ := rec.get("skyhub/device/" + key + "/state"); v != "not_home" {
		t.Fatalf("state after TTL = %q", v)
	}
}

func TestOfflineAfterThreeFailures(t *testing.T) {
	h, p, rec := newTestPoller(t, time.Minute)
	ctx := context.Background()
	if err := p.FastTick(ctx); err != nil {
		t.Fatal(err)
	}
	if v, _ := rec.get("skyhub/status"); v != "online" {
		t.Fatalf("status = %q", v)
	}
	h.SetFixture("sky_system.html", []byte("<html>broken</html>"))
	for i := 0; i < 2; i++ {
		_ = p.FastTick(ctx)
		if v, _ := rec.get("skyhub/status"); v != "online" {
			t.Fatalf("went offline after %d failures", i+1)
		}
	}
	_ = p.FastTick(ctx)
	if v, _ := rec.get("skyhub/status"); v != "offline" {
		t.Fatalf("status after 3 failures = %q", v)
	}
}

// countingHub counts the hub calls the poller makes.
type countingHub struct {
	hub
	calls int
}

func (c *countingHub) SystemStats(ctx context.Context) (*skyhub.SystemStats, error) {
	c.calls++
	return c.hub.SystemStats(ctx)
}

func (c *countingHub) WANStatus(ctx context.Context) (*skyhub.WANStatus, error) {
	c.calls++
	return c.hub.WANStatus(ctx)
}

func (c *countingHub) WirelessRadios(ctx context.Context) ([]skyhub.WirelessRadio, error) {
	c.calls++
	return c.hub.WirelessRadios(ctx)
}

func (c *countingHub) AttachedDevices(ctx context.Context) ([]skyhub.AttachedDevice, error) {
	c.calls++
	return c.hub.AttachedDevices(ctx)
}

func newYieldingPoller(t *testing.T) (*skyhubtest.FakeHub, *Poller, *recorder, *countingHub, *time.Time) {
	t.Helper()
	h := skyhubtest.NewFakeHub(t, fixtureDir, "admin", "secret12")
	c, err := skyhub.New(h.URL(), "admin", "secret12", skyhub.WithTimeout(5*time.Second), skyhub.WithYield())
	if err != nil {
		t.Fatal(err)
	}
	ch := &countingHub{hub: c}
	cfg := Config{BaseTopic: "skyhub", DeviceTTL: time.Minute, StatsInterval: 10 * time.Second, SlowInterval: time.Minute, BusyPause: time.Minute}
	rec := &recorder{}
	p := NewPoller(cfg, ch, rec, slog.New(slog.NewTextHandler(io.Discard, nil)))
	now := time.Date(2026, 10, 27, 20, 0, 0, 0, time.UTC)
	p.now = func() time.Time { return now }
	return h, p, rec, ch, &now
}

func TestPausesWhileHubBusy(t *testing.T) {
	h, p, rec, ch, now := newYieldingPoller(t)
	ctx := context.Background()
	if err := p.FastTick(ctx); err != nil {
		t.Fatal(err)
	}
	h.FailAuthCount = 1 // someone logs in to the admin UI
	if err := p.FastTick(ctx); !errors.Is(err, skyhub.ErrHubBusy) {
		t.Fatalf("err = %v, want ErrHubBusy", err)
	}
	if v, _ := rec.get("skyhub/status"); v != "online" || p.failures != 0 {
		t.Fatalf("status = %q, failures = %d: busy must not count as a failure", v, p.failures)
	}
	calls := ch.calls
	for i := 0; i < 5; i++ {
		*now = now.Add(10 * time.Second)
		_ = p.FastTick(ctx)
		_ = p.SlowTick(ctx)
	}
	if ch.calls != calls {
		t.Fatalf("hub calls during the pause = %d, want 0", ch.calls-calls)
	}
	*now = now.Add(11 * time.Second) // 61s after the rejection
	if err := p.FastTick(ctx); err != nil {
		t.Fatalf("after the pause: %v", err)
	}
	if ch.calls != calls+1 {
		t.Fatalf("hub calls after the pause = %d, want 1", ch.calls-calls)
	}
}

func TestSlowTickStopsAtBusy(t *testing.T) {
	h, p, _, ch, _ := newYieldingPoller(t)
	ctx := context.Background()
	if err := p.FastTick(ctx); err != nil {
		t.Fatal(err)
	}
	h.FailAuthCount = 1
	calls := ch.calls
	if err := p.SlowTick(ctx); !errors.Is(err, skyhub.ErrHubBusy) {
		t.Fatalf("err = %v, want ErrHubBusy", err)
	}
	if got := ch.calls - calls; got != 1 {
		t.Fatalf("hub calls in the tick = %d, want 1 (stop at the first busy)", got)
	}
}

func TestPausesAfterBadPassword(t *testing.T) {
	h := skyhubtest.NewFakeHub(t, fixtureDir, "admin", "secret12")
	c, _ := skyhub.New(h.URL(), "admin", "wrong", skyhub.WithTimeout(5*time.Second), skyhub.WithYield())
	ch := &countingHub{hub: c}
	cfg := Config{BaseTopic: "skyhub", DeviceTTL: time.Minute, StatsInterval: 10 * time.Second, SlowInterval: time.Minute, BusyPause: time.Minute}
	p := NewPoller(cfg, ch, &recorder{}, slog.New(slog.NewTextHandler(io.Discard, nil)))
	now := time.Date(2026, 10, 27, 20, 0, 0, 0, time.UTC)
	p.now = func() time.Time { return now }
	ctx := context.Background()
	if err := p.FastTick(ctx); !errors.Is(err, skyhub.ErrAuth) {
		t.Fatalf("err = %v, want ErrAuth", err)
	}
	now = now.Add(30 * time.Second)
	_ = p.FastTick(ctx)
	if ch.calls != 1 {
		t.Fatalf("hub calls = %d, want 1 (paused after the rejected login)", ch.calls)
	}
}

func TestParseConfigDefaults(t *testing.T) {
	t.Setenv("MQTT_BROKER", "tcp://broker:1883")
	t.Setenv("HA_DISCOVERY_PREFIX", "")
	c, err := ParseConfig([]string{"--stats-interval", "5s"}, io.Discard)
	if err != nil {
		t.Fatal(err)
	}
	if c.Broker != "tcp://broker:1883" || c.HAPrefix != "" || c.StatsInterval != 5*time.Second || c.SlowInterval != time.Minute || c.BaseTopic != "skyhub" || c.BusyPause != time.Minute {
		t.Errorf("cfg = %+v", c)
	}
	if _, err := ParseConfig([]string{"--stats-interval", "1s"}, io.Discard); err == nil {
		t.Error("expected floor error")
	}
}

func fixturePage(t *testing.T, page string) *skyhub.Page {
	t.Helper()
	return skyhub.NewPage(page, readFixture(t, page))
}
