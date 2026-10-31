package textbuf

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// F-115: a file opened and saved unchanged is byte-for-byte the same.
func TestSaveRoundTripsBytes(t *testing.T) {
	for _, content := range []string{
		"", "x", "x\n", "x\n\n", "a\nb", "a\nb\n", "x\r\n", "a\r\nb\r\n", "a\r\nb",
		"héllo\n", "\n",
	} {
		path := filepath.Join(t.TempDir(), "f")
		if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
			t.Fatal(err)
		}
		b, err := Open(path)
		if err != nil {
			t.Fatal(err)
		}
		if err := b.Save(path); err != nil {
			t.Fatalf("%q: save: %v", content, err)
		}
		got, _ := os.ReadFile(path)
		if string(got) != content {
			t.Errorf("round trip %q → %q", content, got)
		}
		if fi, _ := os.Stat(path); fi.Mode().Perm() != 0o600 {
			t.Errorf("%q: mode %v, want 0600 kept", content, fi.Mode().Perm())
		}
	}
}

func TestSaveCRLFKeepsEndingsForNewLines(t *testing.T) {
	path := filepath.Join(t.TempDir(), "f")
	_ = os.WriteFile(path, []byte("a\r\nb\r\n"), 0o644)
	b, _ := Open(path)
	b.SetCursor(Pos{1, 1})
	b.InsertString("\nc")
	if err := b.Save(path); err != nil {
		t.Fatal(err)
	}
	if got, _ := os.ReadFile(path); string(got) != "a\r\nb\r\nc\r\n" {
		t.Fatalf("got %q", got)
	}
}

func TestScratchBufferSaveEndsWithNewline(t *testing.T) {
	path := filepath.Join(t.TempDir(), "new.txt")
	b := New("hello")
	if err := b.Save(path); err != nil {
		t.Fatal(err)
	}
	if err := b.Save(path); err != nil { // second save: no extra newline
		t.Fatal(err)
	}
	if got, _ := os.ReadFile(path); string(got) != "hello\n" {
		t.Fatalf("got %q", got)
	}
}

func TestOpenRefusesBinary(t *testing.T) {
	path := filepath.Join(t.TempDir(), "bin")
	_ = os.WriteFile(path, []byte("ab\x00cd"), 0o644)
	if _, err := Open(path); !errors.Is(err, ErrBinary) {
		t.Fatalf("err = %v, want ErrBinary", err)
	}
}

func TestSaveRefusesExternalChange(t *testing.T) {
	path := filepath.Join(t.TempDir(), "f")
	_ = os.WriteFile(path, []byte("one\n"), 0o644)
	b, _ := Open(path)
	if b.ChangedOnDisk() {
		t.Fatal("fresh buffer reports a change")
	}
	_ = os.WriteFile(path, []byte("two\n"), 0o644) // same size: hash catches it
	if !b.ChangedOnDisk() {
		t.Fatal("external rewrite not seen")
	}
	b.InsertString("x")
	if err := b.Save(path); !errors.Is(err, ErrChangedOnDisk) {
		t.Fatalf("save err = %v, want ErrChangedOnDisk", err)
	}
	if got, _ := os.ReadFile(path); string(got) != "two\n" {
		t.Fatalf("external change clobbered: %q", got)
	}
	if err := b.SaveForce(path); err != nil {
		t.Fatal(err)
	}
	if b.ChangedOnDisk() {
		t.Fatal("own write reported as a change")
	}
}

func TestNoteDiskAcceptsOwnWrite(t *testing.T) {
	path := filepath.Join(t.TempDir(), "f")
	_ = os.WriteFile(path, []byte("one\n"), 0o644)
	b, _ := Open(path)
	_ = os.WriteFile(path, []byte("uno\n"), 0o644)
	b.NoteDisk()
	if b.ChangedOnDisk() {
		t.Fatal("noted write still reported")
	}
}

func TestSaveFollowsSymlink(t *testing.T) {
	dir := t.TempDir()
	real := filepath.Join(dir, "real")
	link := filepath.Join(dir, "link")
	_ = os.WriteFile(real, []byte("a\n"), 0o644)
	if err := os.Symlink(real, link); err != nil {
		t.Skip("no symlinks here:", err)
	}
	b, _ := Open(link)
	b.InsertString("b")
	if err := b.Save(link); err != nil {
		t.Fatal(err)
	}
	if fi, _ := os.Lstat(link); fi.Mode()&os.ModeSymlink == 0 {
		t.Fatal("save replaced the symlink with a file")
	}
	if got, _ := os.ReadFile(real); !strings.HasPrefix(string(got), "ba") {
		t.Fatalf("target = %q", got)
	}
}

func TestEditorWriteBangAndReloadKeepsCursor(t *testing.T) {
	path := filepath.Join(t.TempDir(), "f")
	_ = os.WriteFile(path, []byte("a\nb\nc\n"), 0o644)
	e, _ := OpenFile(path)
	feed(e, "j", "j")
	_ = os.WriteFile(path, []byte("a\nb\nc\nd\n"), 0o644)
	feed(e, "A", "!", "esc", ":", "w", "enter")
	if !strings.Contains(e.Message(), ":w!") {
		t.Fatalf("msg = %q", e.Message())
	}
	feed(e, ":", "e", "!", "enter")
	if !e.TakeReloaded() || e.Buffer().Cursor().Line != 2 {
		t.Fatalf("reload: cursor %v", e.Buffer().Cursor())
	}
	if e.Buffer().Text() != "a\nb\nc\nd\n" {
		t.Fatalf("text %q", e.Buffer().Text())
	}
	feed(e, "A", "!", "esc", ":", "w", "!", "enter")
	if got, _ := os.ReadFile(path); string(got) != "a\nb\nc!\nd\n" {
		t.Fatalf(":w! wrote %q", got)
	}
}

func TestGotoLine(t *testing.T) {
	e := NewEditor("a\n  b\nc\nd")
	feed(e, "3", "G")
	if c := e.Buffer().Cursor(); c.Line != 2 {
		t.Fatalf("3G → %+v", c)
	}
	feed(e, ":", "2", "enter")
	if c := e.Buffer().Cursor(); c.Line != 1 || c.Col != 2 {
		t.Fatalf(":2 → %+v", c)
	}
	feed(e, ":", "9", "9", "enter")
	if c := e.Buffer().Cursor(); c.Line != 3 {
		t.Fatalf(":99 → %+v", c)
	}
	feed(e, "G")
	if c := e.Buffer().Cursor(); c.Line != 3 {
		t.Fatalf("G → %+v", c)
	}
}

func TestEmptyFileGetsFinalNewlineOnFirstSave(t *testing.T) {
	path := filepath.Join(t.TempDir(), "new.go")
	_ = os.WriteFile(path, nil, 0o644)
	b, _ := Open(path)
	b.InsertString("package x")
	if err := b.Save(path); err != nil {
		t.Fatal(err)
	}
	if got, _ := os.ReadFile(path); string(got) != "package x\n" {
		t.Fatalf("got %q", got)
	}
}
