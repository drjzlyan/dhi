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
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "broken.md"), []byte("no frontmatter\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "good.md"), []byte("---\nname: Good\ndescription: fine\n---\nbody\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	s := Open(ws)
	if _, ok := s.Skill("good"); !ok {
		t.Fatal("good skill lost alongside a broken one")
	}
	if _, ok := s.Skill("broken"); ok {
		t.Fatal("broken skill loaded")
	}
	found := false
	for _, w := range s.Warnings() {
		if strings.Contains(w, "broken.md") {
			found = true
		}
	}
	if !found {
		t.Fatalf("malformed card not named: %v", s.Warnings())
	}
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
	k := &Skill{Slug: "my-skill", Name: "My Skill", Description: "d", Body: "step one"}
	if err := s.WriteSkill(ws, k); err != nil {
		t.Fatal(err)
	}
	s = Open(ws) // the section reloads after a write (Settings seam)
	back, ok := s.Skill("my-skill")
	if !ok || back.Name != "My Skill" || back.Body != "step one" {
		t.Fatalf("round trip: %+v ok=%v", back, ok)
	}
}

func TestDeleteGuardsBuiltins(t *testing.T) {
	ws := newWs(t)
	s := Open(ws)
	if err := s.Delete(ws, "role", "reviewer"); err == nil || !strings.Contains(err.Error(), "builtin") {
		t.Fatalf("builtin delete err = %v", err)
	}
	if err := s.WriteSkill(ws, &Skill{Slug: "tmp", Name: "T", Description: "d", Body: "b"}); err != nil {
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

func TestParseSkillFrontmatterStrict(t *testing.T) {
	if _, err := parseSkill("x", []byte("---\nname: N\ndescription: D\n---\nbody")); err != nil {
		t.Fatalf("valid frontmatter refused: %v", err)
	}
	if _, err := parseSkill("x", []byte("---\nname: N\n---\nbody")); err == nil {
		t.Fatal("missing description accepted")
	}
	if _, err := parseSkill("x", []byte("---\nname: N\ndescription: D\nbogus: y\n---\nbody")); err == nil {
		t.Fatal("bogus frontmatter accepted")
	}
}

func TestSkillScriptResolution(t *testing.T) {
	ws := newWs(t)
	dir := filepath.Join(ws.Root, workspace.DirSkills)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	doc := "---\nname: Checks\ndescription: run checks\nscript: checks.sh\n---\n\nRun the checks.\n"
	if err := os.WriteFile(filepath.Join(dir, "checks.md"), []byte(doc), 0o644); err != nil {
		t.Fatal(err)
	}
	scriptPath := filepath.Join(dir, "checks.sh")
	if err := os.WriteFile(scriptPath, []byte("#!/bin/sh\necho ok\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	lib := Open(ws)
	// Not executable yet → named refusal.
	if _, err := lib.Script("checks"); err == nil || !strings.Contains(err.Error(), "not executable") {
		t.Fatalf("non-executable script = %v", err)
	}
	if err := os.Chmod(scriptPath, 0o755); err != nil {
		t.Fatal(err)
	}
	got, err := lib.Script("checks")
	if err != nil || got != scriptPath {
		t.Fatalf("Script = %q err=%v", got, err)
	}
	// Instruction-only skill refuses.
	if err := os.WriteFile(filepath.Join(dir, "plain.md"),
		[]byte("---\nname: Plain\ndescription: none\n---\n\nbody\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	lib = Open(ws)
	if _, err := lib.Script("plain"); err == nil || !strings.Contains(err.Error(), "declares no script") {
		t.Fatalf("scriptless skill = %v", err)
	}
	// Missing script file refuses.
	if err := os.Remove(scriptPath); err != nil {
		t.Fatal(err)
	}
	lib = Open(ws)
	if _, err := lib.Script("checks"); err == nil {
		t.Fatal("missing script accepted")
	}
	// Builtin skills refuse (no script scope).
	if _, err := lib.Script("code-review"); err == nil || !strings.Contains(err.Error(), "builtin") {
		t.Fatalf("builtin skill = %v", err)
	}
}

func TestSkillScriptFrontmatterValidation(t *testing.T) {
	good := "---\nname: A\ndescription: b\nscript: sub/run.sh\n---\n\nbody\n"
	k, err := parseSkill("a", []byte(good))
	if err != nil || k.Script != "sub/run.sh" {
		t.Fatalf("valid script = %+v err=%v", k, err)
	}
	for _, bad := range []string{
		"/abs/run.sh", "../escape.sh", "a/../../b.sh", `c:\x.sh`, ".",
	} {
		doc := "---\nname: A\ndescription: b\nscript: " + bad + "\n---\n\nbody\n"
		if _, err := parseSkill("a", []byte(doc)); err == nil {
			t.Fatalf("script %q accepted", bad)
		}
	}
	// Round-trip through WriteSkill preserves the script.
	ws := newWs(t)
	if err := (&Store{}).WriteSkill(ws, &Skill{Slug: "a", Name: "A", Description: "b", Script: "run.sh", Body: "body"}); err != nil {
		t.Fatal(err)
	}
	back, ok := Open(ws).Skill("a")
	if !ok || back.Script != "run.sh" {
		t.Fatalf("round-trip script = %+v ok=%v", back, ok)
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
