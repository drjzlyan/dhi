package runtime

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/drjzlyan/dhi/internal/agentkit/bus"
	"github.com/drjzlyan/dhi/internal/agentkit/workflow"
	"github.com/drjzlyan/dhi/internal/workspace"
)

func writeWf(t *testing.T, root, slug, body string) {
	t.Helper()
	dir := filepath.Join(root, workspace.DHIDir, "workflows")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, slug+".toml"), []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
}

const ciWorkflow = `schema = 1
slug = "ci"
title = "CI"
[[step]]
id = "worktree_create"
title = "Worktree"
gate = "block"
bind = "worktree"
`

func TestActiveWorkflowBuiltinDefault(t *testing.T) {
	h := newHarness(t, baseDoc())
	h.rt.cfg.Workflows = true
	slug, def, err := h.rt.activeWorkflow(h.rt.agents["scout"].m)
	if err != nil {
		t.Fatalf("activeWorkflow: %v", err)
	}
	if slug != "feature" || def == nil {
		t.Fatalf("default = %q %v", slug, def)
	}
}

func TestActiveWorkflowAgentPick(t *testing.T) {
	h := newHarness(t, baseDoc())
	h.rt.cfg.Workflows = true
	writeWf(t, h.ws.Root, "ci", ciWorkflow)
	h.rt.agents["scout"].m.Workflow = "ci"
	slug, _, err := h.rt.activeWorkflow(h.rt.agents["scout"].m)
	if err != nil || slug != "ci" {
		t.Fatalf("agent pick: %q %v", slug, err)
	}
}

func TestActiveWorkflowNamedMissingRefuses(t *testing.T) {
	h := newHarness(t, baseDoc())
	h.rt.cfg.Workflows = true
	h.rt.agents["scout"].m.Workflow = "ghost"
	_, _, err := h.rt.activeWorkflow(h.rt.agents["scout"].m)
	if err == nil || !strings.Contains(err.Error(), "ghost") {
		t.Fatalf("missing named workflow must refuse by name: %v", err)
	}
}

func TestTurnRefusesMalformedWorkflow(t *testing.T) {
	h := newHarness(t, baseDoc())
	h.rt.cfg.Workflows = true
	writeWf(t, h.ws.Root, "broken", "schema = 99\n")
	trig := mustPost(t, h, bus.Message{Channel: "#general", Author: bus.Human, Text: "@scout hello"})
	err := h.rt.Turn(t.Context(), "scout", trig)
	if err == nil || !strings.Contains(err.Error(), "broken") {
		t.Fatalf("malformed workflow must refuse the turn by name: %v", err)
	}
}

func TestWorkflowGuidanceReachesSystem(t *testing.T) {
	h := newHarness(t, baseDoc())
	h.rt.cfg.Workflows = true
	_, def, err := h.rt.activeWorkflow(h.rt.agents["scout"].m)
	if err != nil {
		t.Fatal(err)
	}
	wfText := workflow.Render(def)
	_, system := h.rt.cliPrompt(t.Context(), h.rt.agents["scout"],
		bus.Message{Channel: "#general", Author: bus.Human, Text: "hello"}, false, wfText)
	if !strings.Contains(system, "Feature workflow (feature)") {
		t.Fatalf("workflow guidance missing from system block:\n%s", system)
	}
}
