package settings

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/drjzlyan/dhi/internal/agentkit/library"
	"github.com/drjzlyan/dhi/internal/agentkit/manifest"
	"github.com/drjzlyan/dhi/internal/agentkit/org"
	"github.com/drjzlyan/dhi/internal/agentkit/pack"
	"github.com/drjzlyan/dhi/internal/ansi"
	"github.com/drjzlyan/dhi/internal/testutil/golden"
	"github.com/drjzlyan/dhi/internal/workspace"
)

// libSurface is the LIBRARY fixture: workspace + library (builtins +
// local cards).
func libSurface(t *testing.T) (*Model, *workspace.Workspace) {
	t.Helper()
	m, ws, _, _ := agentSurface(t)
	m.d.Library = library.Open(ws)
	// Navigate AGENTS → LIBRARY.
	feed(m, "]")
	if m.sec != secLibrary {
		t.Fatalf("sec = %v, want library", m.sec)
	}
	return m, ws
}

func TestLibraryListsBuiltins(t *testing.T) {
	m, _ := libSurface(t)
	out := ansi.Strip(m.View())
	for _, want := range []string{"ROLES", "SKILLS", "reviewer", "scout", "fixer", "code-review", "test-writing", "builtin"} {
		if !strings.Contains(out, want) {
			t.Fatalf("library listing missing %q:\n%s", want, out)
		}
	}
	golden.Snapshot(t, "settings_library", m.View())
}

func TestLibraryCardPreview(t *testing.T) {
	m, _ := libSurface(t)
	if !m.HandleKey("enter") {
		t.Fatal("enter refused")
	}
	if m.dlg == nil || m.dkind != dlgDisplay {
		t.Fatalf("preview dialog missing: %+v", m.dlg)
	}
	out := ansi.Strip(strings.Join(m.dlg.Lines, "\n"))
	// Roles sort by slug: the first row is "fixer".
	if !strings.Contains(out, "fixer") || !strings.Contains(out, "reproduce") {
		t.Fatalf("first role card not previewed: %s", out)
	}
	feed(m, "esc")
}

func TestLibraryAuthorRoleRoundTrip(t *testing.T) {
	m, ws := libSurface(t)
	if !m.HandleKey("n") {
		t.Fatal("n refused")
	}
	typeLibDialog(m, "role", "my-role", "does things", "", "read-write", "read, write", "persona for work")
	feed(m, "enter")
	if m.dform != nil {
		t.Fatalf("dialog still open: err=%s", m.dform.Err)
	}
	if !strings.Contains(m.flash, "role my-role saved") {
		t.Fatalf("flash = %q", m.flash)
	}
	// The fresh snapshot resolves the card as LOCAL.
	lib := library.Open(ws)
	r, ok := lib.Role("my-role")
	if !ok || r.Preset != "read-write" || r.Description != "does things" {
		t.Fatalf("round trip: %+v ok=%v", r, ok)
	}
}

// typeLibDialog fills the library dialog: field 0 is the kind toggle
// (cycled with right), the rest are text fields typed in order.
func typeLibDialog(m *Model, kind string, vals ...string) {
	for m.dform.Fields[0].Selected() != kind {
		m.HandleKey("right")
	}
	for i, v := range vals {
		idx := i + 1
		for m.dform.Cur() != idx {
			feed(m, "tab")
		}
		for _, r := range v {
			m.HandleKey(string(r))
		}
	}
}

func TestLibraryAuthorSkillRoundTrip(t *testing.T) {
	m, ws := libSurface(t)
	if !m.HandleKey("n") {
		t.Fatal("n refused")
	}
	typeLibDialog(m, "skill", "my-skill", "teaches", "My Skill", "", "", "step one")
	feed(m, "enter")
	if m.dform != nil {
		t.Fatalf("dialog still open: err=%s", m.dform.Err)
	}
	lib := library.Open(ws)
	k, ok := lib.Skill("my-skill")
	if !ok || k.Name != "My Skill" || k.Body != "step one" {
		t.Fatalf("round trip: %+v ok=%v", k, ok)
	}
}

func TestLibraryBuiltinReadOnly(t *testing.T) {
	m, _ := libSurface(t)
	// The first row is a role (reviewer, builtin): edit refuses by name.
	if !m.HandleKey("e") {
		t.Fatal("e refused")
	}
	if !strings.Contains(m.flash, "builtin") {
		t.Fatalf("flash = %q, want the builtin read-only refusal", m.flash)
	}
	if !m.HandleKey("x") {
		t.Fatal("x refused")
	}
	if !strings.Contains(m.flash, "builtin") {
		t.Fatalf("delete flash = %q", m.flash)
	}
}

func TestLibraryDeleteLocal(t *testing.T) {
	m, ws := libSurface(t)
	// Author a local card first (through the store, then refresh).
	if err := m.lib.WriteSkill(ws, &library.Skill{
		Slug: "tmp", Name: "T", Description: "d", Body: "b",
	}); err != nil {
		t.Fatal(err)
	}
	m.lib = library.Open(ws)
	// Navigate to the skill row: down past the roles.
	for i, r := range m.libRows() {
		if r.kind == "skill" && r.slug == "tmp" {
			m.libCur = i
			break
		}
	}
	if !m.HandleKey("x") {
		t.Fatal("x refused")
	}
	feed(m, "enter")
	if !strings.Contains(m.flash, "skill tmp deleted") {
		t.Fatalf("flash = %q", m.flash)
	}
	if _, ok := library.Open(ws).Skill("tmp"); ok {
		t.Fatal("card survived delete")
	}
}

func TestAgentFormCarriesRoleAndSkills(t *testing.T) {
	m, _, _, _ := agentSurface(t)
	if !m.HandleKey("n") {
		t.Fatal("n refused")
	}
	if len(m.form.f.Fields) != 8 {
		t.Fatalf("agent form fields = %d, want 8 (id/name/model/system/runtime/tools/role/skills)", len(m.form.f.Fields))
	}
	if m.form.f.Fields[6].Label != "role   " || m.form.f.Fields[7].Label != "skills " {
		t.Fatalf("role/skills labels missing: %q %q", m.form.f.Fields[6].Label, m.form.f.Fields[7].Label)
	}
	// Detection-driven preselect: no Detect seam → index 0.
	if m.form.f.ToggleIndex(4) != 0 {
		t.Fatalf("runtime preselect = %d, want 0", m.form.f.ToggleIndex(4))
	}
}

func TestAgentFormDetectPreselectsFirstDetected(t *testing.T) {
	m, _, _, _ := agentSurface(t)
	m.d.Detect = func() map[string]string {
		return map[string]string{"claude": "", "codex": "0.147.0"}
	}
	if !m.HandleKey("n") {
		t.Fatal("n refused")
	}
	if got := m.form.f.ToggleIndex(4); got != 1 {
		t.Fatalf("runtime preselect = %d, want 1 (codex, the only detected)", got)
	}
	// The picker keeps ALL registered names (undetected stay pickable).
	if got := len(m.form.f.Fields[4].Toggle); got != 2 {
		t.Fatalf("toggle choices = %d, want 2", got)
	}
}

func TestAgentEditPrefillsRoleSkills(t *testing.T) {
	m, ws, o, _ := agentSurface(t)
	seed := &manifest.Agent{ID: "scout", Name: "Scout", Model: "m-1",
		Runtime: "claude", Tools: []string{"read"},
		Role: "reviewer", Skills: []string{"code-review"}}
	if err := o.CreateAgent(ws, seed); err != nil {
		t.Fatal(err)
	}
	if !m.HandleKey("e") {
		t.Fatal("e refused")
	}
	if m.form.f.Fields[6].Value != "reviewer" {
		t.Fatalf("role prefill = %q", m.form.f.Fields[6].Value)
	}
	if m.form.f.Fields[7].Value != "code-review" {
		t.Fatalf("skills prefill = %q", m.form.f.Fields[7].Value)
	}
}

func TestAgentSubmitCarriesRoleSkills(t *testing.T) {
	m, ws, _, _ := agentSurface(t)
	if !m.HandleKey("n") {
		t.Fatal("n refused")
	}
	for i, v := range []string{"spec", "Spec", "m-1", "does spec work", "", "read", "planner", "docs"} {
		for m.form.f.Cur() != i {
			feed(m, "tab")
		}
		for _, r := range v {
			m.HandleKey(string(r))
		}
	}
	feed(m, "enter")
	if m.form.f != nil && m.form.err != "" {
		t.Fatalf("submit err = %q", m.form.err)
	}
	roster, err := org.LoadRoster(ws)
	if err != nil {
		t.Fatal(err)
	}
	for _, a := range roster {
		if a.ID != "spec" {
			continue
		}
		if a.Role != "planner" || len(a.Skills) != 1 || a.Skills[0] != "docs" {
			t.Fatalf("spec agent = %+v", a)
		}
		return
	}
	t.Fatal("spec agent missing from roster")
}

func TestLibraryPackBadges(t *testing.T) {
	m, ws := libSurface(t)

	// Install a pack that ships one role + one skill.
	root := t.TempDir()
	write := func(rel, body string) {
		t.Helper()
		p := filepath.Join(root, filepath.FromSlash(rel))
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	write("roles/scribe.toml", "schema = 1\ndescription = \"writes docs\"\ntools = [\"read\"]\npolicy_preset = \"read-only\"\n")
	write("skills/docs.md", "---\nname: Docs\ndescription: write docs\n---\n\nKeep docs short.\n")
	write("pack.toml", "schema = 2\nname = \"everykind\"\nversion = \"1.0.0\"\nroles = [\"roles/scribe.toml\"]\nskills = [\"skills/docs.md\"]\n")
	if _, err := (&pack.Installer{WS: ws}).Install(context.Background(), root); err != nil {
		t.Fatalf("install: %v", err)
	}

	// The listing (fresh store) badges both cards with the pack name.
	m.lib = nil
	m.d.Library = nil
	out := ansi.Strip(m.libraryBody(100))
	if !strings.Contains(out, "scribe") || !strings.Contains(out, "pack:everykind") {
		t.Fatalf("role pack badge missing:\n%s", out)
	}
	if strings.Count(out, "pack:everykind") != 2 {
		t.Fatalf("expected both role and skill badged:\n%s", out)
	}
}

type fakeScriptRunner struct {
	argv []string
	net  bool
	out  string
	err  error
}

func (f *fakeScriptRunner) Run(_ context.Context, _ string, argv []string, allowNet bool) (string, error) {
	f.argv, f.net = argv, allowNet
	return f.out, f.err
}

func TestLibraryRunSkillScript(t *testing.T) {
	m, ws := libSurface(t)
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
	run := &fakeScriptRunner{out: "hello from script\n"}
	m.d.RunScript = run
	m.d.Library, m.lib = nil, nil // reopen to pick up the new skill

	// Focus the "checks" skill row.
	idx := -1
	for i, r := range m.libRows() {
		if r.slug == "checks" {
			idx = i
		}
	}
	if idx < 0 {
		t.Fatalf("checks row missing: %+v", m.libRows())
	}
	m.libCur = idx

	// r opens a confirm, enter runs async, the output lands in a dialog.
	feed(m, "r")
	if m.dlg == nil || m.dkind != dlgLibRunSkill {
		t.Fatalf("r did not open the run confirm: %+v", m.dlg)
	}
	feed(m, "enter")
	drainDialogEvent(t, m)
	if m.dlg == nil || m.dkind != dlgDisplay {
		t.Fatalf("output dialog missing: %+v", m.dlg)
	}
	out := ansi.Strip(strings.Join(m.dlg.Lines, "\n"))
	if !strings.Contains(out, "hello from script") {
		t.Fatalf("output dialog = %q", out)
	}
	if len(run.argv) != 1 || run.argv[0] != script || run.net {
		t.Fatalf("runner argv=%v net=%v", run.argv, run.net)
	}
}
