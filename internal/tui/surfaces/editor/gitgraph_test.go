package editor

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/drjzlyan/dhi/internal/gitcore"
)

// graphRepo is gitDepthEditor with a committer identity and two branches:
// feat holds a clean commit ("add b") and a clashing one ("feature app").
func graphRepo(t *testing.T) (m *Model, alpha, base string) {
	t.Helper()
	m, alpha = gitDepthEditor(t)
	bin, _ := exec.LookPath("git")
	m.gitCLI = gitcore.NewRunner(bin, append(os.Environ(), "GIT_CONFIG_NOSYSTEM=1",
		"GIT_COMMITTER_NAME=Ada", "GIT_COMMITTER_EMAIL=ada@example.com", "GIT_AUTHOR_NAME=Ada", "GIT_AUTHOR_EMAIL=ada@example.com"))
	base = strings.TrimSpace(m.gitOut(t, alpha, "rev-parse", "--abbrev-ref", "HEAD"))
	app := filepath.Join(alpha, "app.go")
	m.gitOut(t, alpha, "checkout", "-q", "-b", "feat")
	_ = os.WriteFile(filepath.Join(alpha, "b.go"), []byte("package b\n"), 0o644)
	histCommit(t, m, alpha, "add b")
	_ = os.WriteFile(app, []byte("feature\n"), 0o644)
	histCommit(t, m, alpha, "feature app")
	m.gitOut(t, alpha, "checkout", "-q", base)
	_ = os.WriteFile(app, []byte("mainline\n"), 0o644)
	histCommit(t, m, alpha, "mainline app")
	return m, alpha, base
}

// graphSelect moves the selection to the commit with this subject.
func graphSelect(t *testing.T, m *Model, subject string) {
	t.Helper()
	for i, r := range m.graph.rows {
		if r.Subject == subject {
			m.graph.sel = i
			return
		}
	}
	t.Fatalf("no commit %q in the graph", subject)
}

// F-162: :graph lists every branch with lanes and decorations; enter shows the
// commit's whole diff read-only; esc steps back; the g key opens it from the panel.
func TestGraphOpensAndShowsACommit(t *testing.T) {
	m, _, base := graphRepo(t)
	if msg, ok := m.fileCommand(m.active(), "graph"); !ok || msg != "" || m.graph == nil {
		t.Fatalf("graph: %q %v", msg, ok)
	}
	view := plainView(m)
	for _, want := range []string{"graph · all branches", "mainline app", "feature app", "add b", "HEAD", base, "feat", "Ada"} {
		if !strings.Contains(view, want) {
			t.Fatalf("missing %q:\n%s", want, view)
		}
	}
	if !m.graph.rows[m.graph.sel].IsCommit() {
		t.Fatal("the selection must sit on a commit")
	}
	graphSelect(t, m, "add b")
	feed(m, "enter")
	if m.diff == nil || !m.diff.whole || !strings.Contains(plainView(m), "+package b") || !strings.Contains(plainView(m), "b.go") {
		t.Fatalf("diff:\n%s", plainView(m))
	}
	feed(m, "s")
	if !strings.Contains(m.diff.err, "read-only") {
		t.Fatal("staging from a past commit must refuse")
	}
	feed(m, "esc")
	if m.diff != nil || m.graph == nil {
		t.Fatal("esc leaves the diff for the graph")
	}
	feed(m, "esc")
	if m.graph != nil {
		t.Fatal("esc closes the graph")
	}
	feed(m, "ctrl+j", "g")
	if m.graph == nil {
		t.Fatalf("g in the panel opens the graph (err %q)", m.gitErr)
	}
}

// F-162: j/k/pgdn/pgup walk commits only, never connector rows, and stop at the ends.
func TestGraphMovesOverCommitsOnly(t *testing.T) {
	m, _, _ := graphRepo(t)
	m.openGraph()
	g := m.graph
	feed(m, "pgup", "k")
	if g.sel != g.commitFrom(0, 1) {
		t.Fatalf("top: sel %d", g.sel)
	}
	seen := 1
	for i := 0; i < len(g.rows)+3; i++ {
		before := g.sel
		feed(m, "j")
		if g.sel == before {
			break
		}
		if !g.rows[g.sel].IsCommit() {
			t.Fatalf("landed on a connector at %d", g.sel)
		}
		seen++
	}
	if seen != 4 {
		t.Fatalf("walked %d commits, want 4", seen)
	}
	feed(m, "pgup")
	if g.sel != g.commitFrom(0, 1) {
		t.Fatalf("pgup should reach the top, sel %d", g.sel)
	}
	feed(m, "pgdown")
	if g.rows[g.sel].Subject != "initial" {
		t.Fatalf("pgdn should reach the last commit, on %q", g.rows[g.sel].Subject)
	}
}

// F-162: c asks first (any key but y cancels), a clean pick lands on the current
// branch, a clashing one explains the conflict tools and :cherrypick abort restores.
func TestGraphCherryPick(t *testing.T) {
	m, alpha, _ := graphRepo(t)
	m.openGraph()
	headBefore := strings.TrimSpace(m.gitOut(t, alpha, "rev-parse", "HEAD"))

	graphSelect(t, m, "add b")
	feed(m, "c")
	if !m.graph.confirm || !strings.Contains(plainView(m), "cherry-pick") || !strings.Contains(plainView(m), "y picks") {
		t.Fatalf("confirm:\n%s", plainView(m))
	}
	feed(m, "n")
	if m.graph.confirm || strings.TrimSpace(m.gitOut(t, alpha, "rev-parse", "HEAD")) != headBefore {
		t.Fatal("anything but y must cancel without touching the repo")
	}

	feed(m, "c", "y")
	if m.graph.err != "" || !strings.Contains(m.graph.note, "cherry-picked") {
		t.Fatalf("clean pick: err %q note %q", m.graph.err, m.graph.note)
	}
	if _, err := os.Stat(filepath.Join(alpha, "b.go")); err != nil {
		t.Fatal("b.go should have arrived on the current branch")
	}
	if got := strings.TrimSpace(m.gitOut(t, alpha, "log", "-1", "--format=%s")); got != "add b" {
		t.Fatalf("head commit %q", got)
	}

	graphSelect(t, m, "feature app")
	feed(m, "c", "y")
	for _, want := range []string{"conflict in app.go", "]x", ":conflict", ":cherrypick abort"} {
		if !strings.Contains(m.graph.note, want) {
			t.Fatalf("conflict note %q lacks %q", m.graph.note, want)
		}
	}
	if !gitcore.InCherryPick(t.Context(), m.gitCLI, alpha) {
		t.Fatal("the pick should be stopped mid-way")
	}
	if got, _ := os.ReadFile(filepath.Join(alpha, "app.go")); !strings.Contains(string(got), "<<<<<<<") {
		t.Fatalf("app.go should hold markers:\n%s", got)
	}

	msg, ok := m.fileCommand(m.active(), "cherrypick abort")
	if !ok || msg != "cherry-pick aborted" || gitcore.InCherryPick(t.Context(), m.gitCLI, alpha) {
		t.Fatalf("abort: %q %v", msg, ok)
	}
	if got, _ := os.ReadFile(filepath.Join(alpha, "app.go")); string(got) != "mainline\n" {
		t.Fatalf("abort should restore app.go, got %q", got)
	}
	if msg, _ := m.fileCommand(m.active(), "cherrypick abort"); msg != "no cherry-pick in progress" {
		t.Fatalf("second abort: %q", msg)
	}
	if msg, _ := m.fileCommand(m.active(), "cherrypick"); !strings.Contains(msg, "usage") {
		t.Fatalf("bare: %q", msg)
	}
}

// F-162: merges and the root commit are refused before the confirm.
func TestGraphRefusesMergesAndRoot(t *testing.T) {
	m, alpha, _ := graphRepo(t)
	m.gitOut(t, alpha, "merge", "-q", "--no-edit", "--no-ff", "-X", "ours", "feat")
	m.openGraph()
	feed(m, "c")
	if m.graph.confirm || !strings.Contains(m.graph.err, "merge commit") {
		t.Fatalf("merge: confirm %v err %q", m.graph.confirm, m.graph.err)
	}
	graphSelect(t, m, "initial")
	feed(m, "c")
	if m.graph.confirm || !strings.Contains(m.graph.err, "root commit") {
		t.Fatalf("root: confirm %v err %q", m.graph.confirm, m.graph.err)
	}
}

// F-162: b names a branch at the commit without switching; a taken name reports.
func TestGraphBranchHere(t *testing.T) {
	m, alpha, base := graphRepo(t)
	m.openGraph()
	graphSelect(t, m, "add b")
	want := strings.TrimSpace(m.gitOut(t, alpha, "rev-parse", m.graph.rows[m.graph.sel].Hash))
	feed(m, "b")
	if !m.graph.naming {
		t.Fatal("b asks for a name")
	}
	typeKeys(m, "topic")
	feed(m, "enter")
	if m.graph.err != "" || m.graph.naming {
		t.Fatalf("branch: err %q naming %v", m.graph.err, m.graph.naming)
	}
	if got := strings.TrimSpace(m.gitOut(t, alpha, "rev-parse", "topic")); got != want {
		t.Fatalf("topic at %s want %s", got, want)
	}
	if got := strings.TrimSpace(m.gitOut(t, alpha, "rev-parse", "--abbrev-ref", "HEAD")); got != base {
		t.Fatalf("HEAD moved to %s", got)
	}
	if !strings.Contains(plainView(m), "topic") {
		t.Fatalf("the new branch should show in the graph:\n%s", plainView(m))
	}
	graphSelect(t, m, "add b")
	feed(m, "b")
	typeKeys(m, "topic")
	feed(m, "enter")
	if m.graph.err == "" || !m.graph.naming {
		t.Fatalf("an existing name must report and keep the prompt: %q", m.graph.err)
	}
	feed(m, "esc")
	if m.graph.naming || m.graph == nil {
		t.Fatal("esc leaves the prompt, not the graph")
	}
}

// F-162: splitCommitPatch cuts a multi-file patch into hunks that name their file.
func TestSplitCommitPatch(t *testing.T) {
	patch := "diff --git a/x.go b/x.go\nindex 1..2 100644\n--- a/x.go\n+++ b/x.go\n@@ -1 +1 @@\n-a\n+b\n@@ -9 +9 @@\n-c\n+d\n" +
		"diff --git a/img.png b/img.png\nindex 1..2 100644\nBinary files a/img.png and b/img.png differ\n"
	h := splitCommitPatch(patch)
	if len(h) != 3 {
		t.Fatalf("hunks: %q", h)
	}
	if !strings.HasPrefix(h[0], "x.go  @@ -1 +1 @@\n") || !strings.HasPrefix(h[1], "x.go  @@ -9") {
		t.Fatalf("names: %q", h)
	}
	if !strings.HasPrefix(h[2], "img.png  (no text changes)\n") || !strings.Contains(h[2], "Binary files") {
		t.Fatalf("binary: %q", h[2])
	}
	if splitCommitPatch("") != nil {
		t.Fatal("empty patch has no hunks")
	}
}
