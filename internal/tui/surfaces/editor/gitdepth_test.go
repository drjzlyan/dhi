package editor

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/drjzlyan/dhi/internal/gitcore"
)

// gitDepthEditor is the alpha repo with one committed 12-line file and
// the user's git attached.
func gitDepthEditor(t *testing.T) (*Model, string) {
	t.Helper()
	bin, err := exec.LookPath("git")
	if err != nil {
		t.Skip("git not installed")
	}
	m := newEditor(t)
	alpha := m.members[0].path
	var b strings.Builder
	for i := 1; i <= 12; i++ {
		b.WriteString("line " + itoa(i) + "\n")
	}
	_ = os.WriteFile(filepath.Join(alpha, "app.go"), []byte(b.String()), 0o644)
	makeAlphaRepo(t, m)
	m.gitCLI = gitcore.NewRunner(bin, append(os.Environ(), "GIT_CONFIG_NOSYSTEM=1"))
	m.gitIdentity = func(context.Context) (gitcore.Identity, error) {
		return gitcore.Identity{Name: "Ada", Email: "ada@example.com"}, nil
	}
	return m, alpha
}

func (m *Model) gitOut(t *testing.T, dir string, args ...string) string {
	t.Helper()
	out, err := m.gitRun(dir, args...)
	if err != nil {
		t.Fatal(err)
	}
	return out
}

// F-121: the panel shows a file's diff hunk by hunk; s stages just one.
func TestStageOneHunk(t *testing.T) {
	m, alpha := gitDepthEditor(t)
	data, _ := os.ReadFile(filepath.Join(alpha, "app.go"))
	text := strings.Replace(strings.Replace(string(data), "line 2\n", "LINE 2\n", 1), "line 11\n", "LINE 11\n", 1)
	_ = os.WriteFile(filepath.Join(alpha, "app.go"), []byte(text), 0o644)
	feed(m, "ctrl+j")
	feed(m, "enter")
	if m.diff == nil || len(m.diff.hunks) != 2 {
		t.Fatalf("diff = %+v", m.diff)
	}
	if !strings.Contains(plainView(m), "+LINE 2") {
		t.Fatalf("diff view:\n%s", plainView(m))
	}
	feed(m, "s")
	cached := m.gitOut(t, alpha, "diff", "--cached")
	if !strings.Contains(cached, "+LINE 2") || strings.Contains(cached, "+LINE 11") {
		t.Fatalf("staged:\n%s", cached)
	}
	if m.diff == nil || len(m.diff.hunks) != 1 {
		t.Fatal("the other hunk should remain")
	}
}

// F-121: b lists branches; a name that matches none is created.
func TestBranchCreateAndSwitch(t *testing.T) {
	m, alpha := gitDepthEditor(t)
	feed(m, "ctrl+j", "b")
	if m.branches == nil {
		t.Fatal("no picker")
	}
	typeKeys(m, "feat-x")
	feed(m, "enter")
	if got := strings.TrimSpace(m.gitOut(t, alpha, "branch", "--show-current")); got != "feat-x" {
		t.Fatalf("branch = %q", got)
	}
}

// F-121: C writes a multi-line message in a buffer; :w commits it.
func TestCommitMessageBuffer(t *testing.T) {
	m, alpha := gitDepthEditor(t)
	_ = os.WriteFile(filepath.Join(alpha, "app.go"), []byte("changed\n"), 0o644)
	feed(m, "ctrl+j", "S", "C")
	if m.active() == nil || filepath.Base(m.active().Path()) != "COMMIT_EDITMSG" {
		t.Fatal("message buffer not open")
	}
	typeKeys(m, "Subject")
	feed(m, "enter", "enter")
	typeKeys(m, "Body line")
	feed(m, "esc", ":", "w", "enter")
	log := m.gitOut(t, alpha, "log", "-1", "--format=%B")
	if strings.TrimSpace(log) != "Subject\n\nBody line" {
		t.Fatalf("message = %q", log)
	}
	if len(m.bufs) != 0 {
		t.Fatal("message buffer stayed open")
	}
	feed(m, "esc")
	if msg := m.blameLineFor(t); !strings.Contains(msg, "Ada") {
		t.Fatalf("blame = %q", msg)
	}
}

func (m *Model) blameLineFor(t *testing.T) string {
	t.Helper()
	m.open(&node{kind: nodeFile, path: filepath.Join(m.members[0].path, "app.go"), name: "app.go"})
	return m.blameLine()
}

// F-121: a long change list scrolls so the cursor row stays visible.
func TestGitPanelScrollsToCursor(t *testing.T) {
	m, alpha := gitDepthEditor(t)
	for i := 0; i < 15; i++ {
		_ = os.WriteFile(filepath.Join(alpha, "f"+itoa(i)+".txt"), []byte("x"), 0o644)
	}
	feed(m, "ctrl+j")
	for i := 0; i < 14; i++ {
		feed(m, "j")
	}
	want := m.gitSelectedPath()
	if !strings.Contains(plainView(m), want) {
		t.Fatalf("cursor row %q not visible:\n%s", want, plainView(m))
	}
}
