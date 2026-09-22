// Command skyhub-mcp is a read-only MCP server (stdio) for the Sky Hub
// router: statistics, WAN status, attached devices, WiFi, DHCP
// reservations, firewall rules, services, syslog, and ping / DNS
// diagnostics. It never changes hub configuration.
//
// Credentials: SKYHUB_URL / SKYHUB_USER / SKYHUB_PASSWORD or ~/skyhub.
package main

import (
	"context"
	"flag"
	"fmt"
	"log/slog"
	"os"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/fredericgermain/skyhub/pkg/skyhub"
)

var version = "dev"

func main() {
	url := flag.String("url", "", "hub URL (default $SKYHUB_URL or http://192.168.50.1/)")
	timeout := flag.Duration("timeout", 30*time.Second, "per-request timeout ($SKYHUB_TIMEOUT)")
	debug := flag.Bool("debug", os.Getenv("SKYHUB_DEBUG") != "", "log every hub request to stderr")
	showVersion := flag.Bool("version", false, "print version and exit")
	flag.Parse()
	if *showVersion {
		fmt.Println(version)
		return
	}
	if v := os.Getenv("SKYHUB_TIMEOUT"); v != "" {
		if d, err := time.ParseDuration(v); err == nil {
			*timeout = d
		}
	}
	if *url != "" {
		os.Setenv("SKYHUB_URL", *url)
	}
	lvl := slog.LevelInfo
	if *debug {
		lvl = slog.LevelDebug
	}
	log := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: lvl}))

	c, err := skyhub.NewFromEnv(skyhub.WithTimeout(*timeout), skyhub.WithLogger(log))
	if err != nil {
		log.Error("skyhub-mcp: cannot configure client", "err", err)
		os.Exit(1)
	}
	server := newServer(c, version)
	if err := server.Run(context.Background(), &mcp.StdioTransport{}); err != nil {
		log.Error("skyhub-mcp: server stopped", "err", err)
		os.Exit(1)
	}
}
