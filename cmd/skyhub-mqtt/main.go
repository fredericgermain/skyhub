// Command skyhub-mqtt polls a Sky Hub and publishes retained MQTT topics
// with Home Assistant discovery.
package main

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"syscall"

	"github.com/fredericgermain/skyhub/pkg/skyhub"
)

var version = "dev"

func main() {
	if len(os.Args) > 1 && os.Args[1] == "version" {
		fmt.Println(version)
		return
	}
	cfg, err := ParseConfig(os.Args[1:], os.Stderr)
	if err != nil {
		fmt.Fprintln(os.Stderr, "skyhub-mqtt:", err)
		os.Exit(2)
	}
	lvl := slog.LevelInfo
	if cfg.Debug {
		lvl = slog.LevelDebug
	}
	log := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: lvl}))

	creds, err := skyhub.LoadCredentials()
	if err != nil {
		log.Error("credentials", "err", err)
		os.Exit(1)
	}
	if cfg.HubURL != "" {
		creds.URL = cfg.HubURL
	}
	hubClient, err := skyhub.New(creds.URL, creds.User, creds.Password, skyhub.WithTimeout(cfg.Timeout), skyhub.WithLogger(log))
	if err != nil {
		log.Error("hub client", "err", err)
		os.Exit(1)
	}

	statusTopic := cfg.BaseTopic + "/status"
	pub, err := connectPaho(cfg, statusTopic, func() { log.Info("mqtt connected", "broker", cfg.Broker) })
	if err != nil {
		log.Error("mqtt", "err", err)
		os.Exit(1)
	}
	defer pub.Close()

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()
	log.Info("skyhub-mqtt starting", "version", version, "hub", creds.URL, "base_topic", cfg.BaseTopic, "stats_interval", cfg.StatsInterval, "slow_interval", cfg.SlowInterval)
	NewPoller(cfg, hubClient, pub, log).Run(ctx)
	log.Info("skyhub-mqtt stopped")
}
