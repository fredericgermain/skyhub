// Command skyhub is a small CLI over the Sky Hub client library.
//
//	skyhub get <page>            print a raw page (digest-authed)
//	skyhub capture --out DIR     save sanitised fixtures of every known page
//	skyhub backup --out FILE     download the hub configuration backup (secrets!)
//	skyhub proxy --listen :8089  authenticating reverse proxy that records form POSTs
//	skyhub <reader>              stats | wan | info | devices | wifi | syslog |
//	                             lan | dhcp | firewall | services | wanconfig |
//	                             upnp | alg | eth   (JSON output)
package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/fredericgermain/skyhub/pkg/skyhub"
	"github.com/fredericgermain/skyhub/pkg/skyhubtest"
)

var version = "dev"

func main() {
	if len(os.Args) < 2 {
		usage()
		os.Exit(2)
	}
	cmd, args := os.Args[1], os.Args[2:]
	if cmd == "version" {
		fmt.Println(version)
		return
	}
	lvl := slog.LevelInfo
	if os.Getenv("SKYHUB_DEBUG") != "" {
		lvl = slog.LevelDebug
	}
	log := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: lvl}))
	c, err := skyhub.NewFromEnv(skyhub.WithLogger(log))
	if err != nil {
		fatal(err)
	}
	ctx := context.Background()
	switch cmd {
	case "get":
		if len(args) != 1 {
			usage()
			os.Exit(2)
		}
		p, err := c.Get(ctx, args[0])
		if err != nil {
			fatal(err)
		}
		os.Stdout.Write(p.Body)
	case "proxy":
		fs := flag.NewFlagSet("proxy", flag.ExitOnError)
		listen := fs.String("listen", "127.0.0.1:8089", "address to serve the hub UI on (no auth needed by the browser)")
		record := fs.String("record", "", "append every form POST as a JSON line to this file")
		allowAll := fs.Bool("allow-all", false, "forward even the blocked (LAN/WAN/eth/wireless/reboot) handlers")
		_ = fs.Parse(args)
		cr, err := skyhub.LoadCredentials()
		if err != nil {
			fatal(err)
		}
		if err := runProxy(cr, *listen, *record, *allowAll); err != nil {
			fatal(err)
		}
	case "backup":
		fs := flag.NewFlagSet("backup", flag.ExitOnError)
		out := fs.String("out", "", "file to write the hub configuration backup to (required; contains secrets)")
		_ = fs.Parse(args)
		if *out == "" {
			fatal(fmt.Errorf("--out is required"))
		}
		b, err := c.Backup(ctx)
		if err != nil {
			fatal(err)
		}
		if err := os.WriteFile(*out, b, 0o600); err != nil {
			fatal(err)
		}
		fmt.Fprintf(os.Stderr, "wrote %d bytes to %s (mode 0600; contains secrets)\n", len(b), *out)
	case "factory-reset":
		// Wipes the hub; refused unless SKYHUB_ALLOW_DESTRUCTIVE=1.
		if err := c.FactoryReset(ctx); err != nil {
			fatal(err)
		}
		fmt.Fprintf(os.Stderr, "factory reset started; the hub comes back on %s with the sticker password and WiFi\n", skyhub.DefaultURL)
	case "capture":
		fs := flag.NewFlagSet("capture", flag.ExitOnError)
		out := fs.String("out", "", "output directory (required)")
		raw := fs.Bool("raw", false, "do not sanitise (never commit the result)")
		only := fs.String("only", "", "comma-separated pages to capture instead of all")
		_ = fs.Parse(args)
		if *out == "" {
			fatal(fmt.Errorf("--out is required"))
		}
		pages := CapturePages
		if *only != "" {
			pages = strings.Split(*only, ",")
		}
		if err := capture(ctx, c, *out, pages, !*raw); err != nil {
			fatal(err)
		}
	default:
		if err := runReader(ctx, c, cmd, args); err != nil {
			fatal(err)
		}
	}
}

// CapturePages lists every page the library reads or hosts a form on.
var CapturePages = []string{
	"sky_index.html",
	"sky_router_status.html",
	"sky_system.html",
	"sky_st_poe.html",
	"sky_attached_devices.html",
	"sky_sys.log",
	"sky_wireless_band.cgi?band=2.4GHz",
	"sky_wireless_band.cgi?band=5GHz",
	"sky_wireless_settings.html",
	"sky_wireless_onoff.html",
	"sky_lan_ip_setup.html",
	"sky_lan_ip_setup_addmac.html",
	"sky_firewall_rules.html",
	"sky_firewall_rules-in.html",
	"sky_firewall_rules-out.html",
	"sky_services.html",
	"sky_services_custom-add.html",
	"sky_wan_setup.html",
	"sky_eth_setup.html",
	"sky_alg.html",
	"sky_upnp.html",
	"sky_diagnostics.html",
	"sky_dynamic_dns-status.html",
	"sky_dynamic_dns.html",
	"sky_logs.html",
	"sky_block_sites.html",
	"sky_schedule.html",
	"sky_backup_settings.html",
	"sky_set_password.html",
}

func fixtureName(page string) string {
	return strings.NewReplacer("/", "_", "?", "_", "=", "_").Replace(page)
}

func capture(ctx context.Context, c *skyhub.Client, dir string, pages []string, sanitize bool) error {
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	san := skyhubtest.NewSanitizer()
	for _, page := range pages {
		start := time.Now()
		p, err := c.Get(ctx, page)
		if err != nil {
			fmt.Fprintf(os.Stderr, "SKIP %-40s %v\n", page, err)
			continue
		}
		body := p.Body
		if sanitize {
			body = san.Sanitize(body)
		}
		fn := filepath.Join(dir, fixtureName(page))
		if err := os.WriteFile(fn, body, 0o644); err != nil {
			return err
		}
		fmt.Fprintf(os.Stderr, "ok   %-40s %6d bytes %s\n", page, len(body), time.Since(start).Round(time.Millisecond))
	}
	return nil
}

func runReader(ctx context.Context, c *skyhub.Client, cmd string, args []string) error {
	var v any
	var err error
	switch cmd {
	case "stats":
		v, err = c.SystemStats(ctx)
	case "wan":
		v, err = c.WANStatus(ctx)
	case "info":
		v, err = c.RouterInfo(ctx)
	case "devices":
		v, err = c.AttachedDevices(ctx)
	case "wifi":
		v, err = c.WirelessRadios(ctx)
	case "syslog":
		v, err = c.Syslog(ctx)
	case "lan":
		v, err = c.LANConfig(ctx)
	case "dhcp":
		v, err = c.DHCPReservations(ctx)
	case "firewall":
		v, err = c.FirewallConfig(ctx)
	case "services":
		v, err = c.Services(ctx)
	case "wanconfig":
		v, err = c.WANConfig(ctx)
	case "upnp":
		v, err = c.UPnPConfig(ctx)
	case "alg":
		v, err = c.ALGConfig(ctx)
	case "eth":
		v, err = c.EthernetConfig(ctx)
	default:
		usage()
		os.Exit(2)
	}
	if err != nil {
		return err
	}
	enc := json.NewEncoder(os.Stdout)
	enc.SetIndent("", "  ")
	return enc.Encode(v)
}

func usage() {
	fmt.Fprintln(os.Stderr, "usage: skyhub get <page> | capture --out DIR [--only P,Q] | backup --out FILE | factory-reset (needs SKYHUB_ALLOW_DESTRUCTIVE=1) | proxy [--listen ADDR --record FILE] | stats|wan|info|devices|wifi|syslog|lan|dhcp|firewall|services|wanconfig|upnp|alg|eth")
}

func fatal(err error) {
	fmt.Fprintln(os.Stderr, "skyhub:", err)
	os.Exit(1)
}
