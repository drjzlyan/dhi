package editor

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/drjzlyan/dhi/internal/gitcore"
)

func histCommit(t *testing.T, m *Model, dir, msg string) {
	t.Helper()
	m.gitOut(t, dir, "add", "-A")
	m.gitOut(t, dir, "-c", "user.name=Ada", "-c", "user.email=ada@example.com", "commit", "-q", "-am", msg)
}

// F-140: :history lists a file's commits newest first across a rename, enter
// opens that commit's patch read-only, esc backs out step by step.
func TestFileHistory(t *testing.T) {
	m, alpha := gitDepthEditor(t)
	file := filepath.Join(alpha, "app.go")
	orig, _ := os.ReadFile(file)
	_ = os.WriteFile(file, append(orig, []byte("line 13\n")...), 0o644)
	histCommit(t, m, alpha, "add line 13")
	m.gitOut(t, alpha, "mv", "app.go", "main.go")
	histCommit(t, m, alpha, "rename to main.go")
	renamed := filepath.Join(alpha, "main.go")
	_ = os.WriteFile(renamed, append([]byte("top\n"), mustRead(t, renamed)...), 0o644)
	histCommit(t, m, alpha, "add top")
	m.open(&node{kind: nodeFile, path: renamed, name: "main.go"})

	msg, ok := m.fileCommand(m.active(), "history")
	if !ok || msg != "" || m.hist == nil {
		t.Fatalf("history: %q %v", msg, ok)
	}
	if got := len(m.hist.list); got < 4 {
		t.Fatalf("want the commits before the rename too, got %d: %+v", got, m.hist.list)
	}
	if m.hist.list[0].Subject != "add top" || m.hist.list[1].Subject != "rename to main.go" {
		t.Fatalf("order: %+v", m.hist.list)
	}
	view := plainView(m)
	for _, want := range []string{"history · main.go", "add top", "Ada", "ago"} {
		if !strings.Contains(view, want) {
			t.Fatalf("missing %q:\n%s", want, view)
		}
	}

	feed(m, "j", "j") // "add line 13", under its old name
	feed(m, "enter")
	if m.diff == nil || m.diff.rev == "" || !strings.Contains(plainView(m), "+line 13") {
		t.Fatalf("diff:\n%s", plainView(m))
	}
	if !strings.Contains(plainView(m), "app.go") {
		t.Fatalf("the diff should name the path at that commit:\n%s", plainView(m))
	}
	feed(m, "s")
	if !strings.Contains(m.diff.err, "read-only") {
		t.Fatal("staging from a past commit must refuse")
	}
	feed(m, "esc")
	if m.diff != nil || m.hist == nil {
		t.Fatal("esc leaves the diff for the list")
	}
	feed(m, "esc")
	if m.hist != nil {
		t.Fatal("esc closes the list")
	}
}

func mustRead(t *testing.T, p string) []byte {
	t.Helper()
	b, err := os.ReadFile(p)
	if err != nil {
		t.Fatal(err)
	}
	return b
}

// F-140: h in the git panel opens the selected change's history; an
// untracked file and a closed buffer say so instead.
func TestFileHistoryFromThePanel(t *testing.T) {
	m, alpha := gitDepthEditor(t)
	file := filepath.Join(alpha, "app.go")
	_ = os.WriteFile(file, append(mustRead(t, file), []byte("more\n")...), 0o644)
	feed(m, "ctrl+j")
	feed(m, "h")
	if m.hist == nil || m.hist.name != "app.go" || len(m.hist.list) == 0 {
		t.Fatalf("hist = %+v", m.hist)
	}
	feed(m, "esc")
	if m.hist != nil {
		t.Fatal("esc closes")
	}

	if msg := (&Model{}).openHistory(""); msg != "open a file first" {
		t.Fatalf("no file: %q", msg)
	}
	_ = os.WriteFile(filepath.Join(alpha, "new.txt"), []byte("x"), 0o644)
	if msg := m.openHistory(filepath.Join(alpha, "new.txt")); !strings.Contains(msg, "no commits") {
		t.Fatalf("untracked: %q", msg)
	}
}

func TestFileLogParsesRenames(t *testing.T) {
	m, alpha := gitDepthEditor(t)
	list, err := gitcore.FileLog(t.Context(), m.gitCLI, alpha, "app.go", 0)
	if err != nil || len(list) != 1 || list[0].Path != "app.go" || list[0].Short == "" {
		t.Fatalf("%+v %v", list, err)
	}
}
