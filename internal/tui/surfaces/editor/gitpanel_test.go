package editor

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/drjzlyan/dhi/internal/gitcore"
	"github.com/drjzlyan/dhi/internal/testutil/golden"
	"github.com/drjzlyan/dhi/internal/textbuf"
	git "github.com/go-git/go-git/v5"
	"github.com/go-git/go-git/v5/plumbing/object"
)

// makeAlphaRepo turns the alpha member into a real repo with one commit.
func makeAlphaRepo(t *testing.T, m *Model) {
	t.Helper()
	alpha := m.members[0].path
	r, err := git.PlainInit(alpha, false)
	if err != nil {
		t.Fatal(err)
	}
	wt, err := r.Worktree()
	if err != nil {
		t.Fatal(err)
	}
	if _, err := wt.Add("app.go"); err != nil {
		t.Fatal(err)
	}
	if _, err := wt.Commit("initial", &git.CommitOptions{
		Author: &object.Signature{Name: "t", Email: "t@t"},
	}); err != nil {
		t.Fatal(err)
	}
}

func TestGitPanelStageAndCommit(t *testing.T) {
	m := newEditor(t)
	makeAlphaRepo(t, m)

	feed(m, "enter", "down", "down", "enter") // open app.go in buffer
	if !strings.Contains(plainView(m), "NORMAL") {
		t.Fatal("buffer not open")
	}

	// modify + save so worktree differs from HEAD
	feed(m, "$")
	feed(m, "i")
	typeKeys(m, "// touched")
	feed(m, "esc")
	typeKeys(m, ":w")
	feed(m, "enter")

	feed(m, "ctrl+j") // open git panel
	if !m.gitOpen || !m.gitFocus {
		t.Fatal("panel did not open focused")
	}
	v := plainView(m)
	if !strings.Contains(v, "unstaged") || !strings.Contains(v, "app.go") {
		t.Fatalf("status missing modified file:\n%s", v)
	}

	feed(m, "s") // stage cursor file
	if !strings.Contains(plainView(m), "staged") || !strings.Contains(plainView(m), "app.go") {
		t.Fatalf("staged section missing after s:\n%s", plainView(m))
	}
	if st := m.gitEntries; len(stagedPaths(st)) != 1 {
		t.Fatalf("index not updated: %+v", st)
	}

	feed(m, "c")
	if m.bufFocus && m.active() != nil && m.active().Mode() != textbuf.ModeNormal {
		t.Log("buffer state irrelevant here")
	}
	typeKeys(m, "panel commit")
	feed(m, "enter")

	log, err := m.gitRepo.Log(5)
	if err != nil {
		t.Fatal(err)
	}
	if len(log) != 2 || log[0].Message != "panel commit" {
		t.Fatalf("log = %+v", log)
	}
	for _, f := range m.gitEntries {
		if f.Staged {
			t.Errorf("entry still staged after commit: %+v", f)
		}
	}
}

func stagedPaths(entries []gitcore.FileStatus) []string {
	var out []string
	for _, e := range entries {
		if e.Staged {
			out = append(out, e.Path)
		}
	}
	return out
}

func TestGitPanelLogTab(t *testing.T) {
	m := newEditor(t)
	makeAlphaRepo(t, m)

	feed(m, "ctrl+j")
	feed(m, "tab")
	v := plainView(m)
	if !strings.Contains(v, "initial") || !strings.Contains(v, "[log]") {
		t.Fatalf("log view wrong:\n%s", v)
	}
	feed(m, "tab") // back to status
	if !strings.Contains(plainView(m), "[status]") {
		t.Error("tab did not return to status")
	}
}

func TestGitPanelNonRepoMessage(t *testing.T) {
	// beta member is not a repo; force selection there by opening its file
	m := newEditor(t)
	feed(m, "/", "R", "E", "A", "D", "M", "E")
	feed(m, "enter")
	feed(m, "ctrl+j")
	if !strings.Contains(plainView(m), "no git repository") {
		t.Fatalf("non-repo message missing:\n%s", plainView(m))
	}
}

func TestGitPanelEscBlurs(t *testing.T) {
	m := newEditor(t)
	makeAlphaRepo(t, m)
	feed(m, "ctrl+j")
	feed(m, "esc")
	if m.gitFocus {
		t.Fatal("esc did not blur panel")
	}
	if !m.HandleKey("/") {
		t.Error("tree keys dead while panel merely blurred")
	}
	feed(m, "esc")
	feed(m, "ctrl+t") // unrelated drawer unaffected
	if !m.drawerOpen {
		t.Error("drawer broken by git panel usage")
	}
}

func TestCommitInputCancel(t *testing.T) {
	m := newEditor(t)
	makeAlphaRepo(t, m)
	feed(m, "ctrl+j")
	feed(m, "c")
	typeKeys(m, "partial")
	feed(m, "esc")
	if m.gitInputMode {
		t.Fatal("esc did not cancel commit input")
	}
	if log, _ := m.gitRepo.Log(5); len(log) != 1 {
		t.Error("cancelled input committed anyway")
	}
	_ = os.Remove(filepath.Join(t.TempDir(), "unused"))
}

func TestGitPanelGolden(t *testing.T) {
	m := newEditor(t)
	makeAlphaRepo(t, m)

	feed(m, "enter", "down", "down", "enter")
	feed(m, "$")
	feed(m, "i")
	typeKeys(m, "// touched")
	feed(m, "esc")
	typeKeys(m, ":w")
	feed(m, "enter")

	feed(m, "ctrl+j")
	golden.Snapshot(t, "editor_git_status_100x30", m.View())

	feed(m, "tab")
	golden.Snapshot(t, "editor_git_log_100x30", m.View())
}
