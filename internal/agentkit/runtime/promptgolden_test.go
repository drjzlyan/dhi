package runtime

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/drjzlyan/dhi/internal/agentkit/bus"
	"github.com/drjzlyan/dhi/internal/agentkit/clirun"
	"github.com/drjzlyan/dhi/internal/agentkit/manifest"
	"github.com/drjzlyan/dhi/internal/agentkit/standards"
	"github.com/drjzlyan/dhi/internal/sandbox"
	"github.com/drjzlyan/dhi/internal/testutil/golden"
)

// promptFixture builds a runtime whose prompt has every deterministic
// section filled: standards, a member's AGENTS.md, a workflow, history.
func promptFixture(t *testing.T) (*harness, bus.Message) {
	t.Helper()
	h := newHarness(t, baseDoc())
	if err := standards.Save(h.ws.Root, []string{"use conventional commits"}, nil, nil); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(h.ws.Root, "api", "AGENTS.md"), []byte("Run make verify before you finish.\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	m, err := manifest.Parse("scout", []byte(baseDoc()))
	if err != nil {
		t.Fatal(err)
	}
	rt, err := New(Config{
		WS: h.ws, Bus: h.bus, Approvals: h.approvals, Sandbox: sandbox.Noop{},
		CLIs: stubRegistry(h.binDir), CLIEnv: []string{"DUMPDIR=" + h.dumpDir},
		Standards: true,
	}, []*manifest.Agent{m})
	if err != nil {
		t.Fatal(err)
	}
	h.rt = rt
	mustPost(t, h, bus.Message{Channel: "#general", Author: bus.Human, Text: "the api is slow on cold start"})
	trig := mustPost(t, h, bus.Message{Channel: "#general", Author: bus.Human, Text: "@scout find out why"})
	return h, trig
}

// TestPromptGoldens pins the assembled prompt (G-A3): a change to
// behavior.Compose, a section, its order or its wording shows up as a
// reviewed golden diff. Every engine must assemble the same prompt; one
// that legitimately differs gets its own golden, deliberately.
func TestPromptGoldens(t *testing.T) {
	h, trig := promptFixture(t)
	ctx := context.Background()
	render := func(cli *clirun.CLI, level int, wf string) string {
		e := h.rt.agents["scout"]
		was := e.cli
		e.cli = cli
		defer func() { e.cli = was }()
		prompt, system := h.rt.cliPromptAt(ctx, e, trig, wf, level, nil, false)
		return "=== SYSTEM ===\n" + system + "\n\n=== PROMPT ===\n" + prompt
	}
	base, ok := h.rt.cfg.CLIs.Get("claude")
	if !ok {
		t.Fatal("no claude CLI")
	}
	golden.Snapshot(t, "prompt_fresh", render(base, 0, "Workflow: review before merge."))
	golden.Snapshot(t, "prompt_compacted", render(base, 1, ""))

	// F-144: the read-only modes add one block to the system part; the
	// default mode's goldens above stay as they were.
	for _, mode := range []string{"plan", "review", "ask"} {
		e := h.rt.agents["scout"]
		prompt, system := h.rt.cliPromptMode(ctx, e, trig, "", 0, nil, false, mode)
		golden.Snapshot(t, "prompt_mode_"+mode, "=== SYSTEM ===\n"+system+"\n\n=== PROMPT ===\n"+prompt)
	}

	want := render(base, 0, "Workflow: review before merge.")
	for _, c := range h.rt.cfg.CLIs.All() {
		got := render(c, 0, "Workflow: review before merge.")
		if got == want {
			continue
		}
		golden.Snapshot(t, "prompt_fresh_"+c.Name, got)
	}
}

// TestPathScopedRules (F-152): a rule with `paths:` rides only when the
// turn names a matching file; an unscoped rule always rides.
func TestPathScopedRules(t *testing.T) {
	h, _ := promptFixture(t)
	dir := filepath.Join(h.ws.Root, "api", ".claude", "rules")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	write := func(name, text string) {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(text), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	write("go-style.md", "---\npaths:\n  - \"**/*.go\"\n---\nWrap errors with %w.\n")
	write("always.md", "Say what you checked.\n")
	system := func(text string) string {
		trig := mustPost(t, h, bus.Message{Channel: "#general", Author: bus.Human, Text: text})
		_, sys := h.rt.cliPromptAt(context.Background(), h.rt.agents["scout"], trig, "", 0, nil, false)
		return sys
	}

	with := system("@scout fix `api/pkg/x.go`")
	if !strings.Contains(with, "api/.claude/rules/go-style.md (applies to **/*.go):\nWrap errors with %w.") || !strings.Contains(with, "Say what you checked.") {
		t.Fatalf("matching file: scoped and unscoped rules should ride:\n%s", with)
	}
	without := system("@scout tidy the docs in api/README.md")
	if strings.Contains(without, "Wrap errors") || !strings.Contains(without, "Say what you checked.") {
		t.Fatalf("no matching file: scoped rule must stay out, unscoped stay:\n%s", without)
	}
}
