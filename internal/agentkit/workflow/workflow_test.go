package workflow

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

const goodWorkflow = `schema = 1
slug = "ci"
title = "CI workflow"

[[step]]
id = "worktree_create"
title = "Work in a worktree"
guidance = "attach a worktree"
gate = "block"
bind = "worktree"

[[step]]
id = "test"
title = "Run tests"
guidance = "make them pass"
gate = "block"
bind = "run:test"
`

func writeWorkflow(t *testing.T, root, slug, body string) {
	t.Helper()
	dir := filepath.Join(root, Dir)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, slug+".toml"), []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestBuiltinValidates(t *testing.T) {
	if err := validate(Builtin(), BuiltinSlug); err != nil {
		t.Fatalf("builtin must validate: %v", err)
	}
}

func TestBuiltinCommitRequiresWorktree(t *testing.T) {
	d := Builtin()
	got := d.CheckGate(SeamCommit, Progress{})
	if len(got) != 1 || got[0].Step.ID != "worktree_create" {
		t.Fatalf("commit without worktree must block on worktree_create: %+v", got)
	}
	if v := d.CheckGate(SeamCommit, Progress{Worktree: true}); len(v) != 0 {
		t.Fatalf("commit with worktree must pass: %+v", v)
	}
}

func TestBuiltinPRRequiresTests(t *testing.T) {
	d := Builtin()
	got := d.CheckGate(SeamPR, Progress{Worktree: true, Committed: true, Pushed: true})
	if len(got) != 1 || got[0].Step.ID != "test" {
		t.Fatalf("PR with failing tests must block on test: %+v", got)
	}
	if v := d.CheckGate(SeamPR, Progress{Worktree: true, TestsPass: true}); len(v) != 0 {
		t.Fatalf("PR with passing tests must pass: %+v", v)
	}
}

func TestBuiltinReviewAlwaysApprovalUntilDone(t *testing.T) {
	d := Builtin()
	got := d.CheckGate(SeamReview, Progress{Worktree: true, TestsPass: true, PR: true})
	if len(got) != 1 || got[0].Step.ID != "review" || got[0].Step.Gate != GateApprove {
		t.Fatalf("review must require approval: %+v", got)
	}
	if v := d.CheckGate(SeamReview, Progress{Reviewed: true}); len(v) != 0 {
		t.Fatalf("approved review must clear: %+v", v)
	}
}

func TestCheckGateUnboundSeamIsSilent(t *testing.T) {
	if v := Builtin().CheckGate("no-such-seam", Progress{}); v != nil {
		t.Fatalf("a seam the workflow does not bind must be silent: %+v", v)
	}
}

func TestLoadBuiltinAndLocal(t *testing.T) {
	root := t.TempDir()
	if d, err := Load(root, BuiltinSlug); err != nil || d.Slug != BuiltinSlug {
		t.Fatalf("builtin load: %+v %v", d, err)
	}
	writeWorkflow(t, root, "ci", goodWorkflow)
	d, err := Load(root, "ci")
	if err != nil {
		t.Fatalf("local load: %v", err)
	}
	if d.Slug != "ci" || len(d.Steps) != 2 || d.Steps[1].Bind != "run:test" {
		t.Fatalf("local definition = %+v", d)
	}
}

func TestLoadMissingNamedRefusal(t *testing.T) {
	_, err := Load(t.TempDir(), "nope")
	if err == nil || !strings.Contains(err.Error(), "nope") {
		t.Fatalf("missing workflow must refuse by name: %v", err)
	}
}

func TestStrictDecodeUnknownKey(t *testing.T) {
	root := t.TempDir()
	writeWorkflow(t, root, "ci", goodWorkflow+"bogus = true\n")
	_, err := Load(root, "ci")
	if err == nil || !strings.Contains(err.Error(), "unknown key") {
		t.Fatalf("unknown key must refuse: %v", err)
	}
}

func TestGateNeedsSeam(t *testing.T) {
	root := t.TempDir()
	body := `schema = 1
slug = "bad"
[[step]]
id = "x"
title = "x"
gate = "block"
bind = "none"
`
	writeWorkflow(t, root, "bad", body)
	_, err := Load(root, "bad")
	if err == nil || !strings.Contains(err.Error(), "needs a bound seam") {
		t.Fatalf("block+none must refuse: %v", err)
	}
}

func TestBadGateAndBindRefuse(t *testing.T) {
	root := t.TempDir()
	writeWorkflow(t, root, "g", "schema=1\nslug=\"g\"\n[[step]]\nid=\"x\"\ntitle=\"x\"\ngate=\"maybe\"\nbind=\"none\"\n")
	if _, err := Load(root, "g"); err == nil || !strings.Contains(err.Error(), "gate") {
		t.Fatalf("bad gate must refuse: %v", err)
	}
	writeWorkflow(t, root, "b", "schema=1\nslug=\"b\"\n[[step]]\nid=\"x\"\ntitle=\"x\"\ngate=\"warn\"\nbind=\"nowhere\"\n")
	if _, err := Load(root, "b"); err == nil || !strings.Contains(err.Error(), "seam") {
		t.Fatalf("bad bind must refuse: %v", err)
	}
}

func TestDuplicateStepRefuses(t *testing.T) {
	root := t.TempDir()
	body := `schema = 1
slug = "dup"
[[step]]
id = "x"
title = "x"
gate = "warn"
bind = "none"
[[step]]
id = "x"
title = "again"
gate = "warn"
bind = "none"
`
	writeWorkflow(t, root, "dup", body)
	if _, err := Load(root, "dup"); err == nil || !strings.Contains(err.Error(), "duplicate") {
		t.Fatalf("duplicate step id must refuse: %v", err)
	}
}

func TestAvailableListsBuiltinAndLocal(t *testing.T) {
	root := t.TempDir()
	writeWorkflow(t, root, "ci", goodWorkflow)
	got, err := Available(root)
	if err != nil {
		t.Fatalf("Available: %v", err)
	}
	if strings.Join(got, ",") != "ci,feature" {
		t.Fatalf("Available = %v", got)
	}
}

func TestAvailableRefusesMalformed(t *testing.T) {
	root := t.TempDir()
	writeWorkflow(t, root, "broken", "schema = 99\nslug = \"broken\"\n")
	if _, err := Available(root); err == nil || !strings.Contains(err.Error(), "broken") {
		t.Fatalf("malformed def must refuse by name: %v", err)
	}
}

func TestResolvePrecedence(t *testing.T) {
	root := t.TempDir()
	writeWorkflow(t, root, "ci", goodWorkflow)
	got, err := Resolve(root, []string{"ci", "feature"})
	if err != nil || got != "ci" {
		t.Fatalf("resolve ci: %q %v", got, err)
	}
	got, err = Resolve(root, []string{"", "feature"})
	if err != nil || got != BuiltinSlug {
		t.Fatalf("fallback: %q %v", got, err)
	}
	got, err = Resolve(root, nil)
	if err != nil || got != BuiltinSlug {
		t.Fatalf("empty precedence → builtin: %q %v", got, err)
	}
	if _, err := Resolve(root, []string{"ghost"}); err == nil || !strings.Contains(err.Error(), "ghost") {
		t.Fatalf("unknown active workflow must refuse by name: %v", err)
	}
}

func TestRenderNamesStepsAndGates(t *testing.T) {
	out := Render(Builtin())
	for _, want := range []string{"Feature workflow (feature)", "Work in a worktree", "enforced", "test-first"} {
		if !strings.Contains(out, want) {
			t.Fatalf("render missing %q:\n%s", want, out)
		}
	}
}
