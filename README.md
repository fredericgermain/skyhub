# skyhub

Go tooling for the Sky Hub (Sagemcom SR200 family) home router, whose admin UI has no
API: HTTP Digest auth, server-rendered HTML and per-page CSRF `sessionKey` tokens.

- `pkg/skyhub`: client library (digest auth, sessionKey handling, HTML scrapers, typed models, form writers).
- `cmd/skyhub`: CLI (`stats`, `wan`, `devices`, `wifi`, `dhcp`, `firewall`, `services`, ..., `capture`).
- `cmd/skyhub-mcp`: read-only MCP server (stdio) exposing the readers as tools.
- `cmd/skyhub-mqtt`: polling exporter publishing retained MQTT topics with Home Assistant discovery.

Terraform provider: <https://github.com/fredericgermain/terraform-provider-skyhub>.

## Credentials

`SKYHUB_URL` (default `http://192.168.50.1/`), `SKYHUB_USER` (default `admin`), `SKYHUB_PASSWORD`,
or a `~/skyhub` file with `USER=` / `PASSWORD=` lines (`SKYHUB_CREDENTIALS_FILE` overrides the path).

## Development

```sh
go test ./...                                   # offline, against sanitised fixtures + fake hub
SKYHUB_LIVE=1 go test -tags live ./pkg/skyhub   # against the real hub (adds/removes throwaway entries)
go run ./cmd/skyhub capture --out pkg/skyhub/testdata/<firmware>   # refresh sanitised fixtures
```

Reverse-engineering notes: see `API.md` in the parent workspace.

## MQTT exporter

`skyhub-mqtt` polls the hub and publishes retained topics under `skyhub/` with Home Assistant
MQTT discovery. Configuration is flags with environment fallbacks:

| Env (flag) | Default | Meaning |
|---|---|---|
| `MQTT_BROKER` (`--broker`) | `tcp://localhost:1883` | broker URL |
| `MQTT_USER` / `MQTT_PASSWORD` | empty | broker credentials |
| `MQTT_CLIENT_ID` (`--client-id`) | `skyhub-mqtt` | client id (persistent session) |
| `MQTT_BASE_TOPIC` (`--base-topic`) | `skyhub` | topic prefix |
| `HA_DISCOVERY_PREFIX` (`--ha-prefix`) | `homeassistant` | discovery prefix, empty disables |
| `STATS_INTERVAL` (`--stats-interval`) | `10s` | statistics poll (minimum 3s) |
| `SLOW_INTERVAL` (`--slow-interval`) | `60s` | WAN, WiFi and device poll |
| `INFO_INTERVAL` (`--info-interval`) | `6h` | firmware refresh and discovery republish |
| `DEVICE_TTL` (`--device-ttl`) | `5m` | device becomes `not_home` after this long unseen |
| `SKYHUB_URL`, `SKYHUB_USER`, `SKYHUB_PASSWORD`, `SKYHUB_TIMEOUT` | see above | hub access |

Topics (all retained, QoS 1):

```
skyhub/status                          online|offline (LWT)
skyhub/system/{uptime_s,firmware}
skyhub/wan/{status,ip,gateway,netmask,dns,uptime_s,protocol,ipv6,ipv6_prefix}
skyhub/dsl/{down_kbps,up_kbps,attenuation_down_db,attenuation_up_db,noise_margin_down_db,noise_margin_up_db}
skyhub/port/{wan,lan,wlan24,wlan5}/{status,tx_pkts,rx_pkts,collisions,tx_bps,rx_bps,uptime_s}
skyhub/wifi/{24,5}/{enabled,ssid,channel,bandwidth,hidden,wps}
skyhub/device/<mac-no-colons>/state    home|not_home
skyhub/device/<mac-no-colons>/attributes   JSON {mac,hostname,ipv4,ipv6,last_seen}
skyhub/stats                           JSON snapshot of the statistics page
```

Statistics topics are published every `STATS_INTERVAL`; the others only when they change.
After three consecutive poll failures `skyhub/status` goes `offline`.

Home Assistant gets sensors for DSL rates, noise margin, attenuation, uptime and per-port bit
rates, a connectivity binary sensor for the WAN, and one `device_tracker` per client seen.

systemd:

```sh
sudo install -m 755 skyhub-mqtt /usr/local/bin/
sudo install -m 600 deploy/skyhub-mqtt.env.example /etc/skyhub-mqtt.env   # then edit
sudo install -m 644 deploy/skyhub-mqtt.service /etc/systemd/system/
sudo systemctl enable --now skyhub-mqtt
```

Docker:

```sh
docker build -f deploy/Dockerfile -t skyhub-mqtt .
docker run -d --name skyhub-mqtt --env-file skyhub-mqtt.env skyhub-mqtt
```

## MCP server

`skyhub-mcp` is a read-only [MCP](https://modelcontextprotocol.io) server over stdio. Every tool
carries `readOnlyHint`; there is no tool that changes hub configuration.

| Tool | Returns |
|---|---|
| `skyhub_system_stats` | uptime, per-port counters and rates, DSL sync/attenuation/noise margin |
| `skyhub_wan_status` | public IPv4/IPv6, gateway, DNS, WAN uptime, delegated prefix, firmware |
| `skyhub_attached_devices` | MAC, hostname, IPv4, DHCP name, IPv6 of known devices |
| `skyhub_wireless` | both radios: enabled, SSID, hidden, channel, bandwidth, auth, WPS (no PSK) |
| `skyhub_syslog` | recent syslog entries (`tail`, `grep` filters) |
| `skyhub_lan_config`, `skyhub_dhcp_reservations` | LAN/DHCP settings and reservations |
| `skyhub_firewall_rules` | master toggles + ordered inbound/outbound rules (`direction` filter) |
| `skyhub_services` | custom port services (`include_builtin` adds predefined names) |
| `skyhub_wan_config`, `skyhub_upnp`, `skyhub_alg`, `skyhub_ethernet` | remaining settings pages |
| `skyhub_ping` | ping an address from the router (4 packets, a few seconds) |
| `skyhub_dns_lookup` | resolve a name through the router's resolver |

Install and register with Claude Code (credentials are read from `~/skyhub` or `SKYHUB_*`):

```sh
go install github.com/fredericgermain/skyhub/cmd/skyhub-mcp@latest
claude mcp add skyhub -s user -- "$(go env GOPATH)/bin/skyhub-mcp"
```

Flags: `--url`, `--timeout` (also `SKYHUB_TIMEOUT`), `--debug` (also `SKYHUB_DEBUG=1`), `--version`.

## CLI

```sh
go run ./cmd/skyhub stats|wan|info|devices|wifi|syslog|lan|dhcp|firewall|services|wanconfig|upnp|alg|eth
go run ./cmd/skyhub get sky_system.html                 # raw page, digest auth added
go run ./cmd/skyhub capture --out pkg/skyhub/testdata/X # sanitised fixtures
go run ./cmd/skyhub proxy --listen 127.0.0.1:8089 --record posts.jsonl
go run ./cmd/skyhub backup --out /safe/skyhub-backup.conf    # full hub config file (contains secrets)
```

`skyhub proxy` serves the hub UI to a browser without asking for the password, records every form
POST as JSON lines, and never forwards the handlers that restart the hub or change LAN/WAN/wireless
settings (`--allow-all` to forward everything). Useful for reverse-engineering with browser devtools.

## Hub quirks worth knowing

- State lives in JavaScript variables, not in the form `value=` attributes (those are template defaults).
- Every write needs the `sessionKey` from a fresh page load; the client serialises all requests.
- Firewall rules: the enable flags are stored in the `in_enable`/`out_enable` bitmask; the row checkboxes always render checked.
- The reservation UI offers to reboot after add/remove; the client never sends `todo=reboot`.
- Ethernet changes reboot the hub; LAN IP/subnet/DHCP changes restart it. `SetEthernet` is a no-op when nothing changes.
- `SetLANConfig` returns `ErrHubRestarting` when the change restarts the hub (do not read back at the old address); `SetEthernet` waits until the hub answers again.
- `ChangeAdminPassword` switches the client to the new password in place and verifies it; on rejection it reverts and reports the hub's reason.
- `SetWireless` always writes WPA2-PSK/AES; isolation, WPS and the 2.4/5 GHz sync flag keep their current values. 5 GHz offers channel 36 at 80 MHz, or 36/44 at 40 MHz.
- A WiFi save restarts the radio. `SetWireless` (like `SetEthernet` for its reboot) waits until the hub answers steadily again, holding the client lock, so a caller on that WiFi and concurrent requests ride through the gap.
- The hub keeps a single digest nonce, so several clients (exporter, MCP server, Terraform) invalidate each other's logins; a rejection is retried with jitter (three times for a new client, twice once it has authenticated) before it counts as wrong credentials.
