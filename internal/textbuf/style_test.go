package textbuf

import (
	"os"
	"path/filepath"
	"testing"
)

func TestTrimAndFinalNewlineUndo(t *testing.T) {
	b := New("a  \n\tb\t\nc")
	if n := b.TrimTrailingWhitespace(); n != 2 || b.Text() != "a\n\tb\nc" {
		t.Fatalf("trim: %d %q", n, b.Text())
	}
	if !b.EnsureFinalNewline() || b.Text() != "a\n\tb\nc\n" {
		t.Fatalf("newline: %q", b.Text())
	}
	if b.EnsureFinalNewline() {
		t.Fatal("already ends in a newline")
	}
	b.Undo()
	b.Undo()
	if b.Text() != "a  \n\tb\t\nc" {
		t.Fatalf("undo: %q", b.Text())
	}
}

func TestSetCRLFOnlyForNewFiles(t *testing.T) {
	dir := t.TempDir()
	empty := filepath.Join(dir, "new.txt")
	if err := os.WriteFile(empty, nil, 0o644); err != nil {
		t.Fatal(err)
	}
	b, err := Open(empty)
	if err != nil {
		t.Fatal(err)
	}
	if !b.SetCRLF(true) {
		t.Fatal("empty file is new")
	}
	b.SetText("x\ny")
	if err := b.Save(empty); err != nil {
		t.Fatal(err)
	}
	if got, _ := os.ReadFile(empty); string(got) != "x\r\ny\r\n" {
		t.Fatalf("new file: %q", got)
	}

	// The file now has content: its endings are not rewritten.
	b, _ = Open(empty)
	if b.SetCRLF(false) {
		t.Fatal("existing file must keep its endings")
	}
	lf := filepath.Join(dir, "lf.txt")
	_ = os.WriteFile(lf, []byte("a\nb\n"), 0o644)
	b, _ = Open(lf)
	if b.SetCRLF(true) {
		t.Fatal("existing LF file changed")
	}
	_ = b.Save(lf)
	if got, _ := os.ReadFile(lf); string(got) != "a\nb\n" {
		t.Fatalf("lf file: %q", got)
	}
}
