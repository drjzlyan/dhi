package editor

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// conflictedRepo leaves alpha/app.go mid-merge with one conflict on "line 3".
func conflictedRepo(t *testing.T) (*Model, string, string) {
	t.Helper()
	m, alpha := gitDepthEditor(t)
	file := filepath.Join(alpha, "app.go")
	base, _ := os.ReadFile(file)
	id := []string{"-c", "user.name=T", "-c", "user.email=t@example.com"}
	g := func(args ...string) string { return m.gitOut(t, alpha, append(id, args...)...) }
	g("checkout", "-qb", "side")
	_ = os.WriteFile(file, []byte(strings.Replace(string(base), "line 3\n", "SIDE 3\n", 1)), 0o644)
	g("commit", "-qam", "side")
	g("checkout", "-q", "-")
	_ = os.WriteFile(file, []byte(strings.Replace(string(base), "line 3\n", "MAIN 3\n", 1)), 0o644)
	g("commit", "-qam", "main")
	if _, err := m.gitRun(alpha, append(id, "merge", "side")...); err == nil {
		t.Fatal("expected a conflict")
	}
	return m, alpha, file
}

// F-156: ]x jumps, :conflict accepts a side, and saving the last one offers to stage.
func TestConflictResolveSaveAndStage(t *testing.T) {
	m, alpha, file := conflictedRepo(t)
	if n := m.OpenPaths(file); n != 1 {
		t.Fatal("not opened")
	}
	m.Resize(120, 30)
	e := m.active()
	feed(m, "]", "x")
	if e.Buffer().Cursor().Line != 2 {
		t.Fatalf("]x landed on line %d", e.Buffer().Cursor().Line)
	}
	if !strings.Contains(plainView(m), "<<<<<<< HEAD") {
		t.Fatalf("markers should be visible:\n%s", plainView(m))
	}
	feed(m, ":", "c", "o", "n", "f", "l", "i", "c", "t", " ", "b", "o", "t", "h", "enter")
	if got := e.Buffer().Text(); !strings.Contains(got, "MAIN 3\nSIDE 3\n") || strings.Contains(got, "<<<<<<<") {
		t.Fatalf("both:\n%s", got)
	}
	feed(m, "u")
	if !strings.Contains(e.Buffer().Text(), "<<<<<<< HEAD") {
		t.Fatal("one undo should bring the markers back")
	}
	feed(m, ":", "c", "o", "n", "f", "l", "i", "c", "t", " ", "t", "h", "e", "i", "r", "s", "enter")
	feed(m, ":", "w", "enter")
	if !strings.Contains(e.CommandLine(), "stage app.go? (y/n)") || m.conflictStage == "" {
		t.Fatalf("no stage offer: %q", e.CommandLine())
	}
	feed(m, "y")
	if out := m.gitOut(t, alpha, "diff", "--name-only", "--diff-filter=U"); strings.TrimSpace(out) != "" {
		t.Fatalf("still unmerged: %q", out)
	}
	if m.conflictStage != "" || !strings.Contains(e.CommandLine(), "staged app.go") {
		t.Fatalf("after y: %q", e.CommandLine())
	}
}

// Any other answer leaves the file unstaged; n and esc are swallowed.
func TestConflictStageDeclined(t *testing.T) {
	m, alpha, file := conflictedRepo(t)
	m.OpenPaths(file)
	m.Resize(120, 30)
	feed(m, "]", "x")
	feed(m, ":", "c", "o", "n", "f", "l", "i", "c", "t", " ", "o", "u", "r", "s", "enter", "ctrl+s")
	if m.conflictStage == "" {
		t.Fatalf("ctrl+s should offer: %q", m.active().CommandLine())
	}
	feed(m, "n")
	if m.conflictStage != "" {
		t.Fatal("n dismisses")
	}
	if out := m.gitOut(t, alpha, "diff", "--name-only", "--diff-filter=U"); !strings.Contains(out, "app.go") {
		t.Fatal("declined, yet staged")
	}
}

// The git panel marks the unmerged file and s stages it with the user's git.
func TestGitPanelMarksConflicts(t *testing.T) {
	m, alpha, _ := conflictedRepo(t)
	feed(m, "ctrl+j")
	if v := plainView(m); !strings.Contains(v, "UU app.go") {
		t.Fatalf("panel:\n%s", v)
	}
	if len(m.gitStagedFiles()) != 0 {
		t.Fatalf("staged = %+v", m.gitStagedFiles())
	}
	feed(m, "s")
	if out := m.gitOut(t, alpha, "diff", "--name-only", "--diff-filter=U"); strings.TrimSpace(out) != "" {
		t.Fatalf("still unmerged: %q", out)
	}
	if strings.Contains(plainView(m), "UU app.go") {
		t.Fatal("mark should clear after staging")
	}
}

// A clean tree and a resolved-by-hand file never prompt.
func TestNoStageOfferWithoutConflict(t *testing.T) {
	m, _ := pairFile(t, "a\nb\n")
	feed(m, "x", ":", "w", "enter")
	if m.conflictStage != "" {
		t.Fatal("unexpected offer")
	}
}
