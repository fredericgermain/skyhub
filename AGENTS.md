# AGENTS.md

Go client for the Sky Hub router (Sagemcom SR200): library `pkg/skyhub`, CLI `cmd/skyhub`,
MCP server `cmd/skyhub-mcp`, MQTT exporter `cmd/skyhub-mqtt`. The hub has no API, only
digest-authed HTML forms with a per-page `sessionKey`; the README's "Hub quirks worth knowing"
lists what the hub does that the code works around. Read it before touching a writer.

- **Public repo: nothing from a real network goes in**, not in code, fixtures, tests, docs
  or commit messages: no SSID, LAN subnet, IPv6 prefix, MAC or EUI-64 interface id, host or
  device name, custom service name or port, secret-store path, public IP. Stand-ins: SSID
  `Tom&Jerry`, `192.168.50.0/24`, `fd12:3456:789a::/48`, `2001:db8::/32`, `203.0.113.0/24`,
  MACs `02:00:00:…`, services `SvcA`–`SvcD` on ports 7001–7110. `TestFixturesSanitised`
  guards the fixtures; grep the diff and the commit message before committing. A real value
  shipped once: undoing it took a history rewrite, and the Go module proxy keeps old versions
  forever.
- Credentials: `SKYHUB_URL`, `SKYHUB_USER`, `SKYHUB_PASSWORD`, else `~/skyhub`
  (`URL=`/`USER=`/`PASSWORD=` lines, mode 600). The MCP server exits at startup without a
  password; the default URL is the hub's factory address.
- Tests: `go test ./...` runs against the fake hub (`pkg/skyhubtest`, fixtures in
  `pkg/skyhub/testdata`). Live: `SKYHUB_LIVE=1 go test -tags live ./pkg/skyhub` touches only
  throwaway entries (MAC `02:00:00:00:00:01`, service `tfTest` TCP 65001) and derives its
  address from the hub's LAN (`liveHostIP`); never hardcode one.
- Reboot and factory reset are refused unless `SKYHUB_ALLOW_DESTRUCTIVE=1`.
- Releases: annotated tag `vX.Y.Z` on main; GitHub Actions builds the binaries and ghcr
  images. The Terraform provider (`terraform-provider-skyhub`) pins this module: release here
  first when it needs a library change.
- CI builds with the `toolchain` line of go.mod (setup-go `go-version-file`); without it, the `go` line
  (1.26.0) and its stdlib CVEs. When Trivy flags `stdlib` in the ghcr image, raise `toolchain` to the
  current patch (`go mod edit -toolchain=go1.X.Y`) and the flagged modules, then tag a patch release.
