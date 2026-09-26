package editor

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestApplyReplaceClosedFile(t *testing.T) {
	m := newEditor(t)
	abs := filepath.Join(m.members[0].path, "a.go")
	if err := os.WriteFile(abs, []byte("foo bar\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := m.ApplyReplace(abs, "bar", "qux", false); err != nil {
		t.Fatalf("ApplyReplace: %v", err)
	}
	got, _ := os.ReadFile(abs)
	if string(got) != "foo qux\n" {
		t.Fatalf("file = %q", got)
	}
}

func TestApplyReplaceAmbiguousRefuses(t *testing.T) {
	m := newEditor(t)
	abs := filepath.Join(m.members[0].path, "a.go")
	if err := os.WriteFile(abs, []byte("x x\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := m.ApplyReplace(abs, "x", "y", false); err == nil || !strings.Contains(err.Error(), "2") {
		t.Fatalf("ambiguous err = %v", err)
	}
}

func TestApplyReplaceOpenBufferEditsBufferNotDisk(t *testing.T) {
	m := newEditor(t)
	abs := filepath.Join(m.members[0].path, "a.go")
	if err := os.WriteFile(abs, []byte("foo bar\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if n := m.OpenPaths(abs); n != 1 {
		t.Fatalf("OpenPaths opened %d", n)
	}
	if err := m.ApplyReplace(abs, "bar", "qux", false); err != nil {
		t.Fatalf("ApplyReplace(buffer): %v", err)
	}
	got, _ := os.ReadFile(abs)
	if string(got) != "foo bar\n" {
		t.Fatalf("disk must be unchanged until save: %q", got)
	}
	if b := m.bufs[m.activeTab].ed.Buffer(); !strings.Contains(b.Text(), "qux") || !b.Dirty() {
		t.Fatalf("buffer = %q dirty=%v", b.Text(), b.Dirty())
	}
}
