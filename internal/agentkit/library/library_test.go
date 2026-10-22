package library

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/drjzlyan/dhi/internal/sandbox"
	"github.com/drjzlyan/dhi/internal/workspace"
)

func TestBuiltinLibraryLoads(t *testing.T) {
	s := Open(nil)
	roles := s.Roles()
	if len(roles) < 5 {
		t.Fatalf("builtin roles = %d, want >= 5: %+v", len(roles), roles)
	}
	skills := s.Skills()
	if len(skills) < 4 {
		t.Fatalf("builtin skills = %d, want >= 4", len(skills))
	}
	r, ok := s.Role("reviewer")
	if !ok {
		t.Fatal("builtin reviewer missing")
	}
	if r.Preset != PresetReadOnly {
		t.Fatalf("reviewer preset = %q", r.Preset)
	}
	if !strings.Contains(r.System, "{{agent}}") {
		t.Fatalf("reviewer template missing tokens: %q", r.System)
	}
	k, ok := s.Skill("code-review")
	if !ok || k.Name == "" || k.Body == "" {
		t.Fatalf("builtin code-review broken: %+v", k)
	}
	if s.Warnings() != nil {
		t.Fatalf("builtin warnings: %v", s.Warnings())
	}
	for _, e := range append(roles, skills...) {
		if e.Source != "builtin" {
			t.Fatalf("entry %s source = %q", e.Slug, e.Source)
		}
	}
}

func newWs(t *testing.T) *workspace.Workspace {
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
	return ws
}

func TestLocalShadowsBuiltin(t *testing.T) {
	ws := newWs(t)
	dir := filepath.Join(ws.Root, workspace.DirRoles)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	local := "schema = 1\ndescription = \"local override\"\nsystem = \"LOCAL PERSONA\"\n"
	if err := os.WriteFile(filepath.Join(dir, "reviewer.toml"), []byte(local), 0o644); err != nil {
		t.Fatal(err)
	}
	s := Open(ws)
	r, ok := s.Role("reviewer")
	if !ok || r.System != "LOCAL PERSONA" {
		t.Fatalf("local shadow failed: %+v ok=%v", r, ok)
	}
	if s.Source("role", "reviewer") != "local" {
		t.Fatalf("source = %q", s.Source("role", "reviewer"))
	}
	// Untouched builtins stay builtin.
	if s.Source("role", "scout") != "builtin" {
		t.Fatalf("scout source = %q", s.Source("role", "scout"))
	}
}

func TestMalformedCardWarnsNotDies(t *testing.T) {
	ws := newWs(t)
	dir := filepath.Join(ws.Root, workspace.DirSkills)
	writeSkillDir(t, dir, "broken", "no frontmatter\n")
	writeSkillDir(t, dir, "good", "---\nname: good\ndescription: fine\n---\nbody\n")
	s := Open(ws)
	if _, ok := s.Skill("good"); !ok {
		t.Fatal("good skill lost alongside a broken one")
	}
	if _, ok := s.Skill("broken"); ok {
		t.Fatal("broken skill loaded")
	}
	found := false
	for _, w := range s.Warnings() {
		if strings.Contains(w, "broken") {
			found = true
		}
	}
	if !found {
		t.Fatalf("malformed card not named: %v", s.Warnings())
	}
}

func writeSkillDir(t *testing.T, parent, name, doc string) string {
	t.Helper()
	dir := filepath.Join(parent, name)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "SKILL.md"), []byte(doc), 0o644); err != nil {
		t.Fatal(err)
	}
	return dir
}

func TestWriteRoleRoundTrip(t *testing.T) {
	ws := newWs(t)
	s := Open(ws)
	r := &Role{Slug: "my-role", Description: "d", Tools: []string{"read"}, Preset: PresetReadWrite, System: "s {{agent}}"}
	if err := s.WriteRole(ws, r); err != nil {
		t.Fatal(err)
	}
	s = Open(ws) // the section reloads after a write (Settings seam)
	back, ok := s.Role("my-role")
	if !ok || back.System != "s {{agent}}" || back.Preset != PresetReadWrite {
		t.Fatalf("round trip: %+v ok=%v", back, ok)
	}
	// Unknown preset refuses.
	bad := &Role{Slug: "x", Description: "d", Preset: "yolo"}
	if err := s.WriteRole(ws, bad); err == nil || !strings.Contains(err.Error(), "unknown policy preset") {
		t.Fatalf("bad preset err = %v", err)
	}
}

func TestWriteSkillRoundTrip(t *testing.T) {
	ws := newWs(t)
	s := Open(ws)
	k := &Skill{Name: "my-skill", Description: "d", Body: "step one"}
	if err := s.WriteSkill(ws, k); err != nil {
		t.Fatal(err)
	}
	s = Open(ws) // the section reloads after a write (Settings seam)
	back, ok := s.Skill("my-skill")
	if !ok || back.Body != "step one" || back.Location() != filepath.Join(ws.Root, workspace.DirSkills, "my-skill", "SKILL.md") {
		t.Fatalf("round trip: %+v ok=%v", back, ok)
	}
	if err := s.WriteSkill(ws, &Skill{Name: "My Skill", Description: "d", Body: "b"}); err == nil {
		t.Fatal("a non-spec name must be refused")
	}
}

func TestDeleteGuardsBuiltins(t *testing.T) {
	ws := newWs(t)
	s := Open(ws)
	if err := s.Delete(ws, "role", "reviewer"); err == nil || !strings.Contains(err.Error(), "builtin") {
		t.Fatalf("builtin delete err = %v", err)
	}
	if err := s.Delete(ws, "skill", "code-review"); err == nil || !strings.Contains(err.Error(), "builtin") {
		t.Fatalf("builtin skill delete err = %v", err)
	}
	if err := s.WriteSkill(ws, &Skill{Name: "tmp", Description: "d", Body: "b"}); err != nil {
		t.Fatal(err)
	}
	s = Open(ws) // fresh snapshot sees the local card
	if err := s.Delete(ws, "skill", "tmp"); err != nil {
		t.Fatalf("local delete: %v", err)
	}
	if _, ok := Open(ws).Skill("tmp"); ok {
		t.Fatal("deleted skill still resolves")
	}
}

// Skills other tools installed are read too: a member repo's
// .agents/skills and .claude/skills, and ~/.agents/skills; the
// workspace's own .dhi/skills wins a name clash (ADR-0043).
func TestSkillsFromOtherToolsAreDiscovered(t *testing.T) {
	ws := newWs(t)
	mem := ws.Members()[0].Path
	writeSkillDir(t, filepath.Join(mem, ".claude", "skills"), "pdf", "---\nname: pdf\ndescription: from the repo\n---\nb\n")
	writeSkillDir(t, filepath.Join(mem, ".agents", "skills"), "sql", "---\nname: sql\ndescription: x\n---\nb\n")
	AgentsSkillsDir = t.TempDir()
	t.Cleanup(func() { AgentsSkillsDir = "" })
	writeSkillDir(t, AgentsSkillsDir, "lint", "---\nname: lint\ndescription: x\n---\nb\n")
	writeSkillDir(t, filepath.Join(ws.Root, workspace.DirSkills), "pdf", "---\nname: pdf\ndescription: the workspace one\n---\nb\n")
	s := Open(ws)
	for name, src := range map[string]string{"pdf": SourceLocal, "sql": SourceMember, "lint": SourceAgents} {
		if _, ok := s.Skill(name); !ok || s.Source("skill", name) != src {
			t.Errorf("%s: source %q, want %q", name, s.Source("skill", name), src)
		}
	}
	if k, _ := s.Skill("pdf"); k.Description != "the workspace one" {
		t.Fatal("the workspace's own skill must win")
	}
	if err := s.Delete(ws, "skill", "sql"); err == nil {
		t.Fatal("a skill another tool installed is not DHI's to delete")
	}
}

func TestSkillScriptResolution(t *testing.T) {
	ws := newWs(t)
	dir := writeSkillDir(t, filepath.Join(ws.Root, workspace.DirSkills), "checks", "---\nname: checks\ndescription: run checks\n---\n\nRun the checks.\n")
	scriptPath := filepath.Join(dir, "scripts", "checks.sh")
	if err := os.MkdirAll(filepath.Dir(scriptPath), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(scriptPath, []byte("#!/bin/sh\necho ok\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	lib := Open(ws)
	if _, err := lib.Script("checks", "checks.sh"); err == nil || !strings.Contains(err.Error(), "not executable") {
		t.Fatalf("non-executable script = %v", err)
	}
	if err := os.Chmod(scriptPath, 0o755); err != nil {
		t.Fatal(err)
	}
	if got, err := lib.Script("checks", "scripts/checks.sh"); err != nil || got != scriptPath {
		t.Fatalf("Script = %q err=%v", got, err)
	}
	if _, err := lib.Script("checks", "missing.sh"); err == nil {
		t.Fatal("missing script accepted")
	}
	if _, err := lib.Script("code-review", "x.sh"); err == nil || !strings.Contains(err.Error(), "built in") {
		t.Fatalf("builtin skill = %v", err)
	}
}

func TestMaterializeBuiltinsGivesThemALocation(t *testing.T) {
	dir := t.TempDir()
	if err := MaterializeBuiltins(dir); err != nil {
		t.Fatal(err)
	}
	BuiltinSkillsDir = dir
	t.Cleanup(func() { BuiltinSkillsDir = "" })
	k, ok := Open(nil).Skill("code-review")
	if !ok || k.Location() != filepath.Join(dir, "code-review", "SKILL.md") {
		t.Fatalf("builtin location: %+v", k)
	}
	if _, err := os.Stat(k.Location()); err != nil {
		t.Fatal(err)
	}
}

// The presets must be valid sandbox policies: they once named "list" and
// "search" ops the sandbox rejects, so every role-based agent would have
// failed to load the moment a preset was applied.
func TestPolicyPresetsAreValidSandboxPolicies(t *testing.T) {
	for _, preset := range []string{PresetReadOnly, PresetReadWrite} {
		raw, err := PolicyPresetJSON(preset)
		if err != nil {
			t.Fatal(err)
		}
		p, err := sandbox.ParsePolicy([]byte(raw))
		if err != nil {
			t.Fatalf("preset %s is not a valid sandbox policy: %v", preset, err)
		}
		if got := p.Evaluate(sandbox.OpExec, "anywhere").Effect; got != sandbox.Deny {
			t.Errorf("%s allows exec", preset)
		}
		if got := p.Evaluate(sandbox.OpNet, "example.com").Effect; got != sandbox.Deny {
			t.Errorf("%s allows net", preset)
		}
		write := p.Evaluate(sandbox.OpWrite, "a/b.go").Effect
		if (preset == PresetReadOnly) != (write == sandbox.Deny) {
			t.Errorf("%s write decision = %v", preset, write)
		}
		if p.Evaluate(sandbox.OpRead, "a/b.go").Effect != sandbox.Allow {
			t.Errorf("%s blocks read", preset)
		}
	}
}
