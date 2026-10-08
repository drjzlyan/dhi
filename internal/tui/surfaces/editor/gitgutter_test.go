package editor

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	git "github.com/go-git/go-git/v5"
	"github.com/go-git/go-git/v5/plumbing/object"

	"github.com/drjzlyan/dhi/internal/linediff"
)

// gutterEditor commits app.go in member alpha and opens it.
func gutterEditor(t *testing.T) (*Model, string) {
	t.Helper()
	m := newEditor(t)
	root := m.members[0].path
	r, err := git.PlainInit(root, false)
	if err != nil {
		t.Fatal(err)
	}
	abs := filepath.Join(root, "app.go")
	if err := os.WriteFile(abs, []byte("one\ntwo\nthree\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	wt, _ := r.Worktree()
	if _, err := wt.Add("app.go"); err != nil {
		t.Fatal(err)
	}
	if _, err := wt.Commit("init", &git.CommitOptions{Author: &object.Signature{Name: "t", Email: "t@t", When: time.Now()}}); err != nil {
		t.Fatal(err)
	}
	if n := m.OpenPaths(abs); n != 1 {
		t.Fatalf("OpenPaths opened %d", n)
	}
	m.Resize(120, 30)
	return m, abs
}

func TestGitGutterMarksEditedLines(t *testing.T) {
	m, _ := gutterEditor(t)
	tab := m.bufs[m.activeTab]
	if marks := m.gutterMarks(tab); len(marks) != 0 && marks[0] != linediff.None {
		t.Fatalf("clean file must have no marks, got %v", marks)
	}
	if strings.Contains(plainView(m), "▎") {
		t.Fatal("clean file must not draw markers")
	}

	// Modify line 2, add a new line at the end.
	if _, err := tab.ed.Buffer().ReplaceText("two", "TWO", false); err != nil {
		t.Fatal(err)
	}
	if _, err := tab.ed.Buffer().ReplaceText("three", "three\nfour", false); err != nil {
		t.Fatal(err)
	}
	marks := m.gutterMarks(tab)
	if len(marks) < 4 || marks[1] != linediff.Modified || marks[3] != linediff.Added || marks[0] != linediff.None {
		t.Fatalf("marks = %v", marks)
	}
	if got := strings.Count(plainView(m), "▎"); got != 2 {
		t.Fatalf("rendered %d markers, want 2:\n%s", got, plainView(m))
	}
}

func TestGitGutterCachedPerBufferVersionAndInvalidated(t *testing.T) {
	m, _ := gutterEditor(t)
	tab := m.bufs[m.activeTab]
	_ = m.gutterMarks(tab)
	seq := tab.gutter.marksSeq
	_ = m.gutterMarks(tab)
	if tab.gutter.marksSeq != seq {
		t.Fatal("unchanged buffer should reuse the cached marks")
	}
	m.invalidateGutters()
	if tab.gutter.loaded {
		t.Fatal("invalidate must drop the HEAD snapshot")
	}
}

func TestGitGutterAbsentForUntrackedAndNonRepo(t *testing.T) {
	m := newEditor(t) // member is not a repo
	abs := filepath.Join(m.members[0].path, "a.go")
	if err := os.WriteFile(abs, []byte("x\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	m.OpenPaths(abs)
	if marks := m.gutterMarks(m.bufs[m.activeTab]); marks != nil {
		t.Fatalf("non-repo must have no marks, got %v", marks)
	}
	if gitMarkGlyph(nil, 0) != " " || gitMarkGlyph([]linediff.Mark{linediff.Added}, 5) != " " {
		t.Fatal("out-of-range lines render a blank marker")
	}
}
