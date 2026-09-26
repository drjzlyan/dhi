package dhitools

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/drjzlyan/dhi/internal/mcp"
)

// TestLoopbackEndToEnd is the full CLI-shaped path: ServeLoopback
// endpoint ↔ mcp.DialHTTP client → tools/list honors the allowlist →
// tools/call round-trips → a mutation parks an approval the host
// resolves. This is the contract the real agent run rides (ADR-0017).
func TestLoopbackEndToEnd(t *testing.T) {
	f, m := newFixture(t, "task_list", "task_create")
	h := Deps{Agent: m, Tasks: f.tasks, Bus: f.bus, Approvals: f.approvals,
		Channel: "#general"}.Handler()

	endpoint, stop, err := mcp.ServeLoopback(h)
	if err != nil {
		t.Fatal(err)
	}
	defer stop()

	client, err := mcp.DialHTTP(context.Background(), endpoint, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = client.Close() }()

	list, err := client.Tools(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	var names []string
	for _, ti := range list {
		names = append(names, ti.Name)
	}
	if strings.Join(names, ",") != "task_list,task_create" {
		t.Fatalf("tools/list = %v", names)
	}

	// Read-only call through the wire.
	out, isErr, err := client.CallTool(context.Background(), "task_list", json.RawMessage(`{}`))
	if err != nil || isErr || !strings.Contains(out, "(no tasks)") {
		t.Fatalf("task_list via loopback = %q isErr=%v err=%v", out, isErr, err)
	}

	// Mutating call: parks an approval in the HOST process; the client
	// blocks until the host resolves — resolve from another goroutine.
	done := make(chan struct{})
	var content string
	var callErr bool
	go func() {
		defer close(done)
		content, callErr, _ = client.CallTool(context.Background(), "task_create",
			json.RawMessage(`{"slug":"wire-it","title":"Wire the thing"}`))
	}()
	for len(f.approvals.List()) == 0 {
		sleepTick()
	}
	f.approvals.Resolve(f.approvals.List()[0].ID, true)
	<-done
	if callErr || !strings.Contains(content, "created task wire-it") {
		t.Fatalf("task_create via loopback = %q callErr=%v", content, callErr)
	}
	if tk, ok := f.tasks.Get("wire-it"); !ok || tk.Title != "Wire the thing" {
		t.Fatalf("card after loopback approval: %+v ok=%v", tk, ok)
	}
}

// TestServeStdioContract pins the stdio transport against the same
// handler (the helper-process shape for future adapters).
func TestServeStdioContract(t *testing.T) {
	f, m := newFixture(t, "task_list")
	h := Deps{Agent: m, Tasks: f.tasks, Bus: f.bus, Approvals: f.approvals,
		Channel: "#general"}.Handler()

	in := strings.Join([]string{
		`{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":"2024-11-05"}}`,
		`{"jsonrpc":"2.0","method":"notifications/initialized"}`,
		`{"jsonrpc":"2.0","id":2,"method":"tools/list"}`,
		`{"jsonrpc":"2.0","id":3,"method":"tools/call","params":{"name":"task_list","arguments":{}}}`,
	}, "\n") + "\n"
	var out strings.Builder
	if err := mcp.ServeStdio(strings.NewReader(in), &out, h); err != nil {
		t.Fatal(err)
	}
	lines := strings.Split(strings.TrimSpace(out.String()), "\n")
	if len(lines) != 3 { // notifications produce no reply
		t.Fatalf("reply lines = %d, want 3: %q", len(lines), out.String())
	}
	var list struct {
		Result struct {
			Tools []struct{ Name string } `json:"tools"`
		} `json:"result"`
	}
	if err := json.Unmarshal([]byte(lines[1]), &list); err != nil {
		t.Fatal(err)
	}
	if len(list.Result.Tools) != 1 || list.Result.Tools[0].Name != "task_list" {
		t.Fatalf("tools/list result = %+v", list.Result.Tools)
	}
	if !strings.Contains(lines[2], "(no tasks)") {
		t.Fatalf("tools/call result = %q", lines[2])
	}
}
