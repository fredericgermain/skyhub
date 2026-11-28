# Security policy

This is an **unofficial, independent project** (see the disclaimer in the README) — not affiliated
with, endorsed by or supported by Sky UK Ltd or Sagemcom. It is **not** a channel for reporting
vulnerabilities in the Sky Hub firmware, the Sagemcom device or a Sky account; report those to the
vendor.

## Reporting a vulnerability in this project

If you find a security issue in this code — for example mishandled credentials, or a flaw in the
HTTP digest / `sessionKey` handling — please report it **privately** first:

- open a GitHub security advisory:
  <https://github.com/fredericgermain/skyhub/security/advisories/new>, or
- contact the maintainer through their GitHub profile.

Please don't open a public issue for a suspected vulnerability until it has been addressed.

## Scope and expectations

- The tooling (CLI, read-only MCP server, MQTT exporter) talks to a router's admin web UI **over
  your local network**, using credentials **you** supply. It stores no secrets of its own:
  `SKYHUB_PASSWORD` and any MQTT credentials come from your environment.
- Treat `SKYHUB_PASSWORD` as sensitive. When filing a bug, redact credentials, your SSID, MAC
  addresses and any personal network details from logs and captures.
- There is no warranty (MIT `LICENSE`); use at your own risk.
