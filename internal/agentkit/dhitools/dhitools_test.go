package dhitools

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/drjzlyan/dhi/internal/agentkit/bus"
	"github.com/drjzlyan/dhi/internal/agentkit/knowledge"
	"github.com/drjzlyan/dhi/internal/agentkit/library"
	"github.com/drjzlyan/dhi/internal/agentkit/manifest"
	"github.com/drjzlyan/dhi/internal/agentkit/memory"
	"github.com/drjzlyan/dhi/internal/agentkit/scopes"
	"github.com/drjzlyan/dhi/internal/agentkit/tools"
	"github.com/drjzlyan/dhi/internal/mcp"
	"github.com/drjzlyan/dhi/internal/sandbox"
	"github.com/drjzlyan/dhi/internal/tasks"
	"github.com/drjzlyan/dhi/internal/workspace"
)

// openKB opens the knowledge store with no searcher (kb_search returns
// "no hits" in tests; the contribution flow is what's exercised).
func openKB(t *testing.T, ws *workspace.Workspace) knowledge.KnowledgeStore {
	t.Helper()
	kb, err := knowledge.Open(ws, knowledge.Auto, nil)
	if err != nil {
		t.Fatal(err)
	}
	return kb
}

// fixture assembles a real workspace store set for one agent.
type fixture struct {
	ws        *workspace.Workspace
	tasks     *tasks.Store
	mem       *memory.Store
	kb        knowledge.KnowledgeStore
	bus       *bus.Bus
	approvals *tools.Approvals
}

func newFixture(t *testing.T, allow ...string) (*fixture, *manifest.Agent) {
	t.Helper()
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, "api"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := workspace.Create(root, "api"); err != nil {
		t.Fatal(err)
	}
	ws, err := workspace.Load(root)
	if err != nil {
		t.Fatal(err)
	}
	ts, err := tasks.Open(ws)
	if err != nil {
		t.Fatal(err)
	}
	b, err := bus.Open(ws)
	if err != nil {
		t.Fatal(err)
	}
	m := &manifest.Agent{ID: "scout", Name: "Scout", Runtime: "claude", Tools: allow}
	return &fixture{
		ws: ws, tasks: ts, mem: memory.Open(ws),
		kb: openKB(t, ws), bus: b, approvals: tools.NewApprovals(),
	}, m
}

func toolNames(h mcp.Handler) []string {
	var out []string
	for _, ti := range h.Tools() {
		out = append(out, ti.Name)
	}
	return out
}

// call runs a read-only (non-blocking) tool.
func call(h mcp.Handler, t *testing.T, name, argsJSON string) (string, bool) {
	t.Helper()
	out, isErr, err := h.CallTool(context.Background(), name, json.RawMessage(argsJSON))
	if err != nil {
		t.Fatalf("%s: transport error: %v", name, err)
	}
	return out, isErr
}

// callAsync runs a mutating tool in a goroutine (it parks inside
// Approvals.Ask) and returns a resolver for the parked approval.
// The returned func resolves the pending approval (ok=false → deny)
// and reports the tool outcome.
func callAsync(h mcp.Handler, name, argsJSON string, f *fixture) func(t *testing.T) (string, bool) {
	type result struct {
		out   string
		isErr bool
	}
	done := make(chan result, 1)
	go func() {
		out, isErr, _ := h.CallTool(context.Background(), name, json.RawMessage(argsJSON))
		done <- result{out, isErr}
	}()
	return func(t *testing.T) (string, bool) {
		t.Helper()
		// Wait for the approval to park, then answer it.
		deadline := 50
		for len(f.approvals.List()) == 0 && deadline > 0 {
			deadline--
			sleepTick()
		}
		if aps := f.approvals.List(); len(aps) > 0 {
			f.approvals.Resolve(aps[0].ID, true)
		}
		r := <-done
		return r.out, r.isErr
	}
}

// TestAllowlistGatesSurface pins F-028 Part A: a tool absent from the
// manifest allowlist is absent from tools/list.
func TestAllowlistGatesSurface(t *testing.T) {
	f, m := newFixture(t, "task_list", "memory_append")
	h := Deps{Agent: m, Tasks: f.tasks, Memory: f.mem, Bus: f.bus,
		Approvals: f.approvals, Channel: "#general"}.Handler()
	got := strings.Join(toolNames(h), ",")
	if !strings.Contains(got, "task_list") || !strings.Contains(got, "memory_append") {
		t.Fatalf("allowlisted tools missing: %s", got)
	}
	for _, banned := range []string{"task_create", "channel_post", "kb_search", "workspace_search"} {
		if strings.Contains(got, banned) {
			t.Fatalf("%s served despite absent allowlist: %s", banned, got)
		}
	}
}

// TestTaskToolsRoundTrip covers list/create/status/assign with the
// approval gate: the mutation parks until the human resolves.
func TestTaskToolsRoundTrip(t *testing.T) {
	f, m := newFixture(t, "task_list", "task_create", "task_status", "task_assign")
	h := Deps{Agent: m, Tasks: f.tasks, Bus: f.bus, Approvals: f.approvals,
		Channel: "#general"}.Handler()

	out, isErr := callAsync(h, "task_create", `{"slug":"ship-it","title":"Ship the thing"}`, f)(t)
	if isErr || !strings.Contains(out, "created") {
		t.Fatalf("create = %q isErr=%v", out, isErr)
	}
	if tk, ok := f.tasks.Get("ship-it"); !ok || tk.Title != "Ship the thing" {
		t.Fatalf("card after approve: %+v ok=%v", tk, ok)
	}

	// Denial is a tool-level refusal, not silence: resolve false this
	// time via a deny-aware runner.
	deny := func(h mcp.Handler, name, args string) (string, bool) {
		type result struct {
			out   string
			isErr bool
		}
		done := make(chan result, 1)
		go func() {
			out, isErr, _ := h.CallTool(context.Background(), name, json.RawMessage(args))
			done <- result{out, isErr}
		}()
		for len(f.approvals.List()) == 0 {
			sleepTick()
		}
		f.approvals.Resolve(f.approvals.List()[0].ID, false)
		r := <-done
		return r.out, r.isErr
	}
	out, isErr = deny(h, "task_status", `{"slug":"ship-it","status":"active"}`)
	if !isErr || !strings.Contains(out, "denied by operator") {
		t.Fatalf("denied mutation = %q isErr=%v", out, isErr)
	}
	if tk, _ := f.tasks.Get("ship-it"); tk.Status != tasks.Backlog {
		t.Fatalf("denied mutation still landed: %+v", tk)
	}

	// Status + assign after approvals.
	if _, isErr = callAsync(h, "task_status", `{"slug":"ship-it","status":"active"}`, f)(t); isErr {
		t.Fatal("status mutation refused")
	}
	if _, isErr = callAsync(h, "task_assign", `{"slug":"ship-it","assignee":"scout"}`, f)(t); isErr {
		t.Fatal("assign mutation refused")
	}
	out, _ = call(h, t, "task_list", `{}`)
	if !strings.Contains(out, "ship-it [active]") || !strings.Contains(out, "assignee: scout") {
		t.Fatalf("list = %q", out)
	}
}

// TestUnknownArgKeyRefused pins the strict-args contract.
func TestUnknownArgKeyRefused(t *testing.T) {
	f, m := newFixture(t, "task_create")
	h := Deps{Agent: m, Tasks: f.tasks, Bus: f.bus, Approvals: f.approvals,
		Channel: "#general"}.Handler()
	if _, isErr := call(h, t, "task_create", `{"slug":"x","title":"y","bogus":1}`); !isErr {
		t.Fatal("unknown arg key accepted")
	}
}

// TestMemoryAndChannelTools covers the notebook + channel surface.
func TestMemoryAndChannelTools(t *testing.T) {
	f, m := newFixture(t, "memory_append", "memory_read_notes", "channel_read", "channel_post")
	h := Deps{Agent: m, Tasks: f.tasks, Memory: f.mem, Bus: f.bus,
		Approvals: f.approvals, Channel: "#general", Thread: 0}.Handler()

	if _, err := f.bus.Post(bus.Message{Channel: "#general", Author: bus.Human, Text: "kick off"}); err != nil {
		t.Fatal(err)
	}

	if _, isErr := call(h, t, "memory_append", `{"text":"learned the deploy dance","kind":"lesson"}`); isErr {
		t.Fatal("memory_append refused (memory is private: no approval expected)")
	}
	if len(f.approvals.List()) != 0 {
		t.Fatal("memory mutation crossed approvals — private state must not")
	}
	out, isErr := call(h, t, "memory_read_notes", `{}`)
	if isErr || !strings.Contains(out, "(notes empty)") {
		t.Fatalf("notes = %q isErr=%v", out, isErr)
	}
	out, _ = call(h, t, "channel_read", `{}`)
	if !strings.Contains(out, "you (the human)") {
		t.Fatalf("channel_read = %q", out)
	}
	if _, isErr := callAsync(h, "channel_post", `{"text":"progress report"}`, f)(t); isErr {
		t.Fatal("post refused")
	}
	history := f.bus.History("#general", 0)
	last := history[len(history)-1]
	if last.Author != "scout" || last.Text != "progress report" {
		t.Fatalf("post = %+v", last)
	}
}

// TestUnknownToolRefused pins the dispatch refusal.
func TestUnknownToolRefused(t *testing.T) {
	f, m := newFixture(t, "task_list")
	h := Deps{Agent: m, Tasks: f.tasks, Bus: f.bus, Approvals: f.approvals,
		Channel: "#general"}.Handler()
	if _, _, err := h.CallTool(context.Background(), "nope", nil); err == nil {
		t.Fatal("unknown tool accepted")
	}
}

var _ = sandbox.OpExec

// sleepTick yields briefly while waiting for an approval to park.
func sleepTick() { time.Sleep(2 * time.Millisecond) }

// TestPROpenTool covers the served pr_open tool: the approval-gated
// fan-out opens one PR per changeset through the review seam, and the
// workflow `pr` gate hard-blocks before any approval.
func TestPROpenTool(t *testing.T) {
	f, m := newFixture(t, "pr_open")
	type prCall struct{ member, branch, title, base string }
	var calls []prCall
	h := Deps{Agent: m, Tasks: f.tasks, Approvals: f.approvals, Channel: "#general",
		PR: func(_ context.Context, member, branch, title, base string) (string, error) {
			calls = append(calls, prCall{member, branch, title, base})
			return "PR #7 " + member, nil
		}}.Handler()

	f.tasks.Create("feat", "Feature", "", "")
	if err := f.tasks.RecordChangeSet("feat", tasks.ChangeSet{Member: "api", Branch: "task/feat", Path: "wt"}); err != nil {
		t.Fatal(err)
	}
	out, isErr := callAsync(h, "pr_open", `{"slug":"feat","title":"Feature","base":"main"}`, f)(t)
	if isErr || !strings.Contains(out, "api") {
		t.Fatalf("pr_open = %q isErr=%v", out, isErr)
	}
	if len(calls) != 1 || calls[0].member != "api" || calls[0].branch != "task/feat" ||
		calls[0].title != "Feature" || calls[0].base != "main" {
		t.Fatalf("seam calls = %+v", calls)
	}
}

func TestPROpenRefusals(t *testing.T) {
	f, m := newFixture(t, "pr_open")
	f.tasks.Create("feat", "Feature", "", "")
	_ = f.tasks.RecordChangeSet("feat", tasks.ChangeSet{Member: "api", Branch: "b", Path: "wt"})
	auto := scopes.Set{scopes.Read: scopes.Auto, scopes.Push: scopes.Auto}

	// No review seam → named refusal.
	h := Deps{Agent: m, Tasks: f.tasks, Channel: "#general", Scopes: auto}.Handler()
	if out, isErr := call(h, t, "pr_open", `{"slug":"feat","title":"T"}`); !isErr ||
		!strings.Contains(out, "no review seam") {
		t.Fatalf("nil seam = %q isErr=%v", out, isErr)
	}

	// Workflow `pr` gate blocks before the seam is reached.
	h2 := Deps{Agent: m, Tasks: f.tasks, Channel: "#general", Scopes: auto,
		PR:   func(context.Context, string, string, string, string) (string, error) { return "x", nil },
		Gate: func(seam string) []string { return []string{"tests not passing"} }}.Handler()
	if out, isErr := call(h2, t, "pr_open", `{"slug":"feat","title":"T"}`); !isErr ||
		!strings.Contains(out, "workflow blocks PR") {
		t.Fatalf("gate = %q isErr=%v", out, isErr)
	}

	// No changeset → named refusal.
	f.tasks.Create("bare", "Bare", "", "")
	h3 := Deps{Agent: m, Tasks: f.tasks, Channel: "#general", Scopes: auto,
		PR: func(context.Context, string, string, string, string) (string, error) { return "x", nil }}.Handler()
	if out, isErr := call(h3, t, "pr_open", `{"slug":"bare","title":"T"}`); !isErr ||
		!strings.Contains(out, "no worktree") {
		t.Fatalf("no changeset = %q isErr=%v", out, isErr)
	}
}

func TestSkillRunTool(t *testing.T) {
	f, m := newFixture(t, "skill_run")
	ws := f.ws
	dir := filepath.Join(ws.Root, workspace.DirSkills)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	doc := "---\nname: Checks\ndescription: run checks\nscript: checks.sh\n---\n\nRun.\n"
	if err := os.WriteFile(filepath.Join(dir, "checks.md"), []byte(doc), 0o644); err != nil {
		t.Fatal(err)
	}
	script := filepath.Join(dir, "checks.sh")
	if err := os.WriteFile(script, []byte("#!/bin/sh\necho ok\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	lib := library.Open(ws)
	run := &fakeRunner{out: "ran\n"}
	auto := scopes.Set{scopes.Read: scopes.Auto, scopes.Exec: scopes.Auto}
	h := Deps{Agent: m, Skills: lib, Run: run, Workdir: ws.Root, Channel: "#general", Scopes: auto}.Handler()

	out, isErr := call(h, t, "skill_run", `{"skill":"checks","args":["--fast"]}`)
	if isErr || !strings.Contains(out, "ran") {
		t.Fatalf("skill_run = %q isErr=%v", out, isErr)
	}
	if !run.called || len(run.argv) != 2 || run.argv[0] != script || run.argv[1] != "--fast" || run.network {
		t.Fatalf("argv=%v dir=%q net=%v", run.argv, run.dir, run.network)
	}

	// Unknown skill refuses.
	if out, isErr := call(h, t, "skill_run", `{"skill":"nope"}`); !isErr || !strings.Contains(out, "not found") {
		t.Fatalf("unknown skill = %q isErr=%v", out, isErr)
	}
	// Exec denied by scope refuses by name (before the runner).
	denied := Deps{Agent: m, Skills: lib, Run: run, Workdir: ws.Root, Scopes: scopes.Set{scopes.Exec: scopes.Deny}}.Handler()
	if out, isErr := call(denied, t, "skill_run", `{"skill":"checks"}`); !isErr || !strings.Contains(out, "denied by capability scope") {
		t.Fatalf("denied = %q isErr=%v", out, isErr)
	}
	// No runner refuses.
	noRun := Deps{Agent: m, Skills: lib, Workdir: ws.Root, Scopes: auto}.Handler()
	if out, isErr := call(noRun, t, "skill_run", `{"skill":"checks"}`); !isErr || !strings.Contains(out, "command runner unavailable") {
		t.Fatalf("no runner = %q isErr=%v", out, isErr)
	}
	// No library refuses.
	noLib := Deps{Agent: m, Run: run, Workdir: ws.Root, Scopes: auto}.Handler()
	if out, isErr := call(noLib, t, "skill_run", `{"skill":"checks"}`); !isErr || !strings.Contains(out, "skill library unavailable") {
		t.Fatalf("no library = %q isErr=%v", out, isErr)
	}
}
