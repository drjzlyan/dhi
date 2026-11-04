package editor

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/drjzlyan/dhi/internal/gitcore"
)

// F-129: z opens the stash list; n stashes with a message, a applies, p pops,
// d drops only after y.
func TestStashFromThePanel(t *testing.T) {
	m, alpha := gitDepthEditor(t)
	bin, _ := exec.LookPath("git")
	m.gitCLI = gitcore.NewRunner(bin, append(os.Environ(), "GIT_CONFIG_NOSYSTEM=1",
		"GIT_COMMITTER_NAME=T", "GIT_COMMITTER_EMAIL=t@example.com", "GIT_AUTHOR_NAME=T", "GIT_AUTHOR_EMAIL=t@example.com"))
	file := filepath.Join(alpha, "app.go")
	orig, _ := os.ReadFile(file)
	_ = os.WriteFile(file, append([]byte("changed\n"), orig...), 0o644)

	feed(m, "ctrl+j")
	feed(m, "z")
	if m.stash == nil || !strings.Contains(plainView(m), "no stashes") {
		t.Fatalf("stash view:\n%s", plainView(m))
	}
	feed(m, "n")
	for _, r := range "wip one" {
		feed(m, string(r))
	}
	feed(m, "enter")
	if got, _ := os.ReadFile(file); string(got) != string(orig) {
		t.Fatal("the work tree should be clean after stashing")
	}
	if len(m.stash.list) != 1 || !strings.Contains(m.stash.list[0].Message, "wip one") || !strings.Contains(plainView(m), "wip one") {
		t.Fatalf("list = %+v", m.stash.list)
	}

	feed(m, "d") // asks first
	if !m.stash.confirm || len(m.stash.list) != 1 {
		t.Fatal("drop must ask")
	}
	feed(m, "x") // anything but y cancels
	if m.stash.confirm || len(m.stash.list) != 1 {
		t.Fatal("cancel must keep the stash")
	}

	feed(m, "a")
	if m.stash != nil {
		t.Fatal("apply closes the list")
	}
	if got, _ := os.ReadFile(file); !strings.HasPrefix(string(got), "changed\n") {
		t.Fatal("apply should restore the change")
	}
	if out := m.gitOut(t, alpha, "stash", "list"); !strings.Contains(out, "wip one") {
		t.Fatal("apply keeps the stash")
	}

	_ = os.WriteFile(file, orig, 0o644) // clean again, so pop applies
	feed(m, "z")
	feed(m, "p")
	if got, _ := os.ReadFile(file); !strings.HasPrefix(string(got), "changed\n") {
		t.Fatal("pop should restore the change")
	}
	if out := m.gitOut(t, alpha, "stash", "list"); strings.TrimSpace(out) != "" {
		t.Fatalf("pop should drop it: %q", out)
	}

	_ = os.WriteFile(file, append([]byte("again\n"), orig...), 0o644)
	feed(m, "z")
	feed(m, "n")
	feed(m, "enter") // no message
	feed(m, "d")
	feed(m, "y")
	if len(m.stash.list) != 0 {
		t.Fatalf("y should drop: %+v", m.stash.list)
	}
	feed(m, "n")
	feed(m, "enter") // clean tree: named refusal, not a silent success
	if !strings.Contains(m.stash.err, "no local changes") {
		t.Fatalf("err = %q", m.stash.err)
	}
	feed(m, "esc")
	if m.stash != nil {
		t.Fatal("esc closes")
	}
}
