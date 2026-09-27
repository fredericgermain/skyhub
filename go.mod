module github.com/fredericgermain/skyhub

go 1.26.0

require (
	github.com/eclipse/paho.mqtt.golang v1.5.1
	github.com/modelcontextprotocol/go-sdk v1.8.0
	golang.org/x/net v0.59.0
)

require (
	github.com/google/jsonschema-go v0.4.3 // indirect
	github.com/gorilla/websocket v1.5.3 // indirect
	github.com/segmentio/asm v1.1.3 // indirect
	github.com/segmentio/encoding v0.5.4 // indirect
	github.com/yosida95/uritemplate/v3 v3.0.2 // indirect
	golang.org/x/oauth2 v0.35.0 // indirect
	golang.org/x/sync v0.20.0 // indirect
	golang.org/x/sys v0.48.0 // indirect
	golang.org/x/time v0.15.0 // indirect
)

// Versions before v0.3.0 were withdrawn when the repository history was
// rewritten; use v0.3.0 or later.
retract [v0.1.0, v0.2.3]
