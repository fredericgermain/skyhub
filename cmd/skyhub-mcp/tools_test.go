package main

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/fredericgermain/skyhub/pkg/skyhub"
	"github.com/fredericgermain/skyhub/pkg/skyhubtest"
)

func newSession(t *testing.T) *mcp.ClientSession {
	t.Helper()
	h := skyhubtest.NewFakeHub(t, "../../pkg/skyhub/testdata/7.04.0208.R", "admin", "secret12")
	c, err := skyhub.New(h.URL(), "admin", "secret12", skyhub.WithPostDelay(0), skyhub.WithTimeout(5*time.Second))
	if err != nil {
		t.Fatal(err)
	}
	server := newServer(c, "test")
	st, ct := mcp.NewInMemoryTransports()
	ctx := context.Background()
	ss, err := server.Connect(ctx, st, nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { ss.Close() })
	cs, err := mcp.NewClient(&mcp.Implementation{Name: "t", Version: "0"}, nil).Connect(ctx, ct, nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { cs.Close() })
	return cs
}

func call(t *testing.T, cs *mcp.ClientSession, name string, args map[string]any) map[string]any {
	t.Helper()
	res, err := cs.CallTool(context.Background(), &mcp.CallToolParams{Name: name, Arguments: args})
	if err != nil {
		t.Fatalf("%s: %v", name, err)
	}
	if res.IsError {
		t.Fatalf("%s: tool error: %v", name, res.Content)
	}
	txt, ok := res.Content[0].(*mcp.TextContent)
	if !ok {
		t.Fatalf("%s: no text content", name)
	}
	var out map[string]any
	if err := json.Unmarshal([]byte(txt.Text), &out); err != nil {
		t.Fatalf("%s: bad json: %v", name, err)
	}
	if res.StructuredContent == nil {
		t.Errorf("%s: no structured content", name)
	}
	return out
}

func TestToolsAreReadOnly(t *testing.T) {
	cs := newSession(t)
	res, err := cs.ListTools(context.Background(), nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Tools) != len(toolNames) || len(res.Tools) < 15 {
		t.Fatalf("tools = %d, registered = %d", len(res.Tools), len(toolNames))
	}
	for _, tool := range res.Tools {
		if tool.Annotations == nil || !tool.Annotations.ReadOnlyHint {
			t.Errorf("%s is not marked read-only", tool.Name)
		}
		if tool.Description == "" {
			t.Errorf("%s has no description", tool.Name)
		}
	}
}

func TestToolCalls(t *testing.T) {
	cs := newSession(t)

	stats := call(t, cs, "skyhub_system_stats", nil)
	if ports, _ := stats["ports"].([]any); len(ports) != 4 {
		t.Errorf("ports = %v", stats["ports"])
	}
	if stats["dsl"] == nil {
		t.Error("dsl missing")
	}

	fw := call(t, cs, "skyhub_firewall_rules", map[string]any{"direction": "in"})
	if in, _ := fw["inbound"].([]any); len(in) != 6 {
		t.Errorf("inbound = %v", fw["inbound"])
	}
	if fw["outbound"] != nil {
		t.Errorf("outbound should be omitted when direction=in, got %v", fw["outbound"])
	}

	dhcp := call(t, cs, "skyhub_dhcp_reservations", nil)
	if r, _ := dhcp["reservations"].([]any); len(r) != 2 {
		t.Errorf("reservations = %v", dhcp["reservations"])
	}

	devs := call(t, cs, "skyhub_attached_devices", nil)
	if d, _ := devs["devices"].([]any); len(d) < 5 {
		t.Errorf("devices = %d", len(d))
	}

	// Errors from the hub are tool errors, not protocol errors.
	res, err := cs.CallTool(context.Background(), &mcp.CallToolParams{Name: "skyhub_ping", Arguments: map[string]any{"target": "not-an-ip"}})
	if err != nil {
		t.Fatal(err)
	}
	if !res.IsError {
		t.Error("expected IsError for bad target")
	}
}
