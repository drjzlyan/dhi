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

// F-151: V marks lines inside a hunk; s stages just those, u unstages
// just those, and the work tree is never touched.
func TestStageAndUnstageLines(t *testing.T) {
	m, alpha := gitDepthEditor(t)
	data, _ := os.ReadFile(filepath.Join(alpha, "app.go"))
	text := strings.Replace(strings.Replace(string(data), "line 2\n", "LINE 2\n", 1), "line 3\n", "LINE 3\n", 1)
	_ = os.WriteFile(filepath.Join(alpha, "app.go"), []byte(text), 0o644)
	feed(m, "ctrl+j", "enter")
	if m.diff == nil || len(m.diff.hunks) != 1 {
		t.Fatalf("diff = %+v", m.diff)
	}
	// body: 0 ctx, 1 -line 2, 2 -line 3, 3 +LINE 2, 4 +LINE 3 ...
	feed(m, "V", "j")
	if !m.diff.lineMode || !strings.Contains(plainView(m), "marked lines") {
		t.Fatalf("not in line mode:\n%s", plainView(m))
	}
	feed(m, "s")
	cached := m.gitOut(t, alpha, "diff", "--cached")
	if !strings.Contains(cached, "-line 2") || !strings.Contains(cached, "-line 3") || strings.Contains(cached, "+LINE") {
		t.Fatalf("staged the two deletions only:\n%s", cached)
	}
	if m.diff == nil || m.diff.lineMode {
		t.Fatal("line mode should end after staging")
	}
	if b, _ := os.ReadFile(filepath.Join(alpha, "app.go")); string(b) != text {
		t.Fatal("the work tree changed")
	}

	// the staged side: unstage only the first deleted line
	d := &diffView{path: "app.go", staged: true}
	m.loadDiff(d)
	m.diff = d
	feed(m, "V", "s")
	if !strings.Contains(d.err, "staged side") {
		t.Fatalf("s on the staged side: %q", d.err)
	}
	feed(m, "u")
	cached = m.gitOut(t, alpha, "diff", "--cached")
	if strings.Contains(cached, "-line 2") || !strings.Contains(cached, "-line 3") {
		t.Fatalf("unstaged line 2 only:\n%s", cached)
	}
}

// F-151: line marking refuses on a past commit; esc leaves line mode only.
func TestLineModeGuards(t *testing.T) {
	m, alpha := gitDepthEditor(t)
	_ = os.WriteFile(filepath.Join(alpha, "app.go"), []byte("changed\n"), 0o644)
	feed(m, "ctrl+j", "enter", "V")
	if !m.diff.lineMode {
		t.Fatal("V should start line mode")
	}
	feed(m, "esc")
	if m.diff == nil || m.diff.lineMode {
		t.Fatal("esc leaves line mode, not the diff")
	}
	m.diff.rev = "abc1234"
	feed(m, "V")
	if m.diff.lineMode || !strings.Contains(m.diff.err, "read-only") {
		t.Fatal("a past commit must refuse line marking")
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

// F-182: the message buffer starts from the team's draft for the branch,
// and {ai:…} parts left in it are written off the UI, then committed.
func TestCommitBufferDraftAndAIParts(t *testing.T) {
	m, alpha := gitDepthEditor(t)
	var dir, text string
	m.commitConv = CommitConventions{
		Draft: func(d string) string { dir = d; return "PROJ-1: {ai:subject}" },
		Write: func(_ context.Context, d, msg string) (string, error) {
			text = msg
			return strings.Replace(msg, "{ai:subject}", "add the app", 1), nil
		},
	}
	_ = os.WriteFile(filepath.Join(alpha, "app.go"), []byte("changed\n"), 0o644)
	feed(m, "ctrl+j", "S", "C")
	if got := m.active().Buffer().Text(); !strings.HasPrefix(got, "PROJ-1: {ai:subject}\n") || !strings.Contains(got, "written by your default agent") {
		t.Fatalf("draft = %q", got)
	}
	if dir != alpha {
		t.Fatalf("draft asked for %q", dir)
	}
	feed(m, "esc", ":", "w", "enter")
	if !strings.Contains(m.gitMessage, "writing the AI parts") {
		t.Fatalf("status = %q", m.gitMessage)
	}
	m.Update(waitMsg(t, m))
	settleGitPanel(m) // the commit itself runs off the UI too
	if text != "PROJ-1: {ai:subject}" || !strings.HasPrefix(m.gitMessage, "committed") {
		t.Fatalf("writer got %q; status %q", text, m.gitMessage)
	}
	if log := strings.TrimSpace(m.gitOut(t, alpha, "log", "-1", "--format=%B")); log != "PROJ-1: add the app" {
		t.Fatalf("message = %q", log)
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
