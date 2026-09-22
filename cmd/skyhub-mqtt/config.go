package main

import (
	"flag"
	"fmt"
	"io"
	"os"
	"strconv"
	"time"
)

// Config is the exporter configuration. Every flag has an environment
// variable fallback so systemd EnvironmentFile= and docker -e both work.
type Config struct {
	Broker       string
	MQTTUser     string
	MQTTPassword string
	ClientID     string
	BaseTopic    string
	HAPrefix     string // empty disables Home Assistant discovery

	StatsInterval time.Duration
	SlowInterval  time.Duration
	InfoInterval  time.Duration
	DeviceTTL     time.Duration

	HubURL  string
	Timeout time.Duration
	Debug   bool
}

func envOr(key, def string) string {
	if v, ok := os.LookupEnv(key); ok {
		return v
	}
	return def
}

func envDuration(key string, def time.Duration) time.Duration {
	if v, ok := os.LookupEnv(key); ok && v != "" {
		if d, err := time.ParseDuration(v); err == nil {
			return d
		}
	}
	return def
}

func envBool(key string, def bool) bool {
	if v, ok := os.LookupEnv(key); ok && v != "" {
		if b, err := strconv.ParseBool(v); err == nil {
			return b
		}
	}
	return def
}

// ParseConfig reads flags (with env fallbacks) from args.
func ParseConfig(args []string, stderr io.Writer) (Config, error) {
	var c Config
	fs := flag.NewFlagSet("skyhub-mqtt", flag.ContinueOnError)
	fs.SetOutput(stderr)
	fs.StringVar(&c.Broker, "broker", envOr("MQTT_BROKER", "tcp://localhost:1883"), "MQTT broker URL (MQTT_BROKER)")
	fs.StringVar(&c.MQTTUser, "mqtt-user", envOr("MQTT_USER", ""), "MQTT username (MQTT_USER)")
	fs.StringVar(&c.MQTTPassword, "mqtt-password", envOr("MQTT_PASSWORD", ""), "MQTT password (MQTT_PASSWORD)")
	fs.StringVar(&c.ClientID, "client-id", envOr("MQTT_CLIENT_ID", "skyhub-mqtt"), "MQTT client id (MQTT_CLIENT_ID)")
	fs.StringVar(&c.BaseTopic, "base-topic", envOr("MQTT_BASE_TOPIC", "skyhub"), "base topic (MQTT_BASE_TOPIC)")
	fs.StringVar(&c.HAPrefix, "ha-prefix", envOr("HA_DISCOVERY_PREFIX", "homeassistant"), "Home Assistant discovery prefix, empty disables (HA_DISCOVERY_PREFIX)")
	fs.DurationVar(&c.StatsInterval, "stats-interval", envDuration("STATS_INTERVAL", 10*time.Second), "statistics poll interval (STATS_INTERVAL)")
	fs.DurationVar(&c.SlowInterval, "slow-interval", envDuration("SLOW_INTERVAL", 60*time.Second), "devices/wifi/wan poll interval (SLOW_INTERVAL)")
	fs.DurationVar(&c.InfoInterval, "info-interval", envDuration("INFO_INTERVAL", 6*time.Hour), "firmware + discovery republish interval (INFO_INTERVAL)")
	fs.DurationVar(&c.DeviceTTL, "device-ttl", envDuration("DEVICE_TTL", 5*time.Minute), "mark a device not_home after this long unseen (DEVICE_TTL)")
	fs.StringVar(&c.HubURL, "url", envOr("SKYHUB_URL", ""), "hub URL, default from credentials (SKYHUB_URL)")
	fs.DurationVar(&c.Timeout, "timeout", envDuration("SKYHUB_TIMEOUT", 30*time.Second), "hub request timeout (SKYHUB_TIMEOUT)")
	fs.BoolVar(&c.Debug, "debug", envBool("SKYHUB_DEBUG", false), "debug logging (SKYHUB_DEBUG)")
	if err := fs.Parse(args); err != nil {
		return c, err
	}
	if c.StatsInterval < 3*time.Second {
		return c, fmt.Errorf("stats-interval must be at least 3s (hub UI floor)")
	}
	if c.SlowInterval < c.StatsInterval {
		return c, fmt.Errorf("slow-interval must not be shorter than stats-interval")
	}
	if c.BaseTopic == "" {
		return c, fmt.Errorf("base-topic must not be empty")
	}
	return c, nil
}
