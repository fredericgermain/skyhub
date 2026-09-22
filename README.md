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
