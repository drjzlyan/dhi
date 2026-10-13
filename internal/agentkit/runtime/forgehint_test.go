package runtime

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/drjzlyan/dhi/internal/agentkit/bus"
	"github.com/drjzlyan/dhi/internal/agentkit/forge"
)

// F-069: repeating the same commands across runs earns a tool_propose
// nudge — until a forged tool covers them.
func TestForgeHint(t *testing.T) {
	h := newHarness(t, baseDoc())
	h.rt.cfg.Forge = forge.Open(h.ws.Root)
	dir := filepath.Join(h.ws.Root, ".dhi", "agents", "scout", "runs")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 3; i++ {
		body := `{"kind":"command","detail":"bash: go vet ./..."}` + "\n" +
			`{"kind":"command","detail":"bash: go test -race ./..."}` + "\n"
		if err := os.WriteFile(filepath.Join(dir, fmt.Sprintf("run-2026101%d-0.jsonl", i)), []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	trig := mustPost(t, h, bus.Message{Channel: "#general", Author: bus.Human, Text: "@scout go"})
	_, system := h.rt.cliPrompt(context.Background(), h.rt.agents["scout"], trig, "")
	if !strings.Contains(system, "You keep repeating `go vet ./...` then `go test -race ./...` (in 3 of your recent runs)") {
		t.Fatalf("hint missing:\n%s", system)
	}
	tool := forge.Tool{Name: "check_all", Description: "vet and race-test", Scope: "exec", Steps: []forge.Step{
		{Run: []string{"go", "vet", "./..."}}, {Run: []string{"go", "test", "-race", "./..."}},
	}}
	if _, err := h.rt.cfg.Forge.Propose(tool, nil); err != nil {
		t.Fatal(err)
	}
	if _, system = h.rt.cliPrompt(context.Background(), h.rt.agents["scout"], trig, ""); strings.Contains(system, "You keep repeating") {
		t.Fatal("a pending tool covering the sequence must silence the hint")
	}
}
