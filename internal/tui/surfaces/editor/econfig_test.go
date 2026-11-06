package editor

import (
	"os"
	"path/filepath"
	"testing"
)

func ecWrite(t *testing.T, path, text string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(text), 0o644); err != nil {
		t.Fatal(err)
	}
}

func ecOpen(t *testing.T, m *Model, path string) {
	t.Helper()
	m.open(&node{kind: nodeFile, path: path, name: filepath.Base(path)})
	if m.active() == nil || m.active().Path() != path {
		t.Fatalf("%s did not open", path)
	}
}

func ecSave(t *testing.T, m *Model) {
	t.Helper()
	if err := m.active().Save(); err != nil {
		t.Fatal(err)
	}
}

func TestEditorConfigIndentPrecedence(t *testing.T) {
	m := newEditor(t)
	dir := filepath.Join(m.ws.Root, "repos", "alpha")
	ecWrite(t, filepath.Join(dir, ".editorconfig"), "[*.txt]\nindent_style = space\nindent_size = 3\ntab_width = 6\n")
	txt := filepath.Join(dir, "a.txt")
	ecWrite(t, txt, "\tfoo\n\tbar\n") // detected tabs lose to the config
	ecOpen(t, m, txt)
	if got := m.active().Indent(); got != "   " {
		t.Fatalf("indent = %q, want 3 spaces", got)
	}
	if m.bufs[m.activeTab].tabWidth != 6 {
		t.Fatalf("tabWidth = %d", m.bufs[m.activeTab].tabWidth)
	}
	// No config for .md: detection still applies.
	md := filepath.Join(dir, "b.md")
	ecWrite(t, md, "\tfoo\n\tbar\n")
	ecOpen(t, m, md)
	if got := m.active().Indent(); got != "\t" {
		t.Fatalf("detected indent = %q", got)
	}
}

func TestEditorConfigSaveRulesOnlyWhenTrue(t *testing.T) {
	m := newEditor(t)
	dir := filepath.Join(m.ws.Root, "repos", "alpha")
	ecWrite(t, filepath.Join(dir, ".editorconfig"), "[*.txt]\ntrim_trailing_whitespace = true\ninsert_final_newline = true\n")
	a := filepath.Join(dir, "a.txt")
	ecWrite(t, a, "x  \ny\t")
	ecOpen(t, m, a)
	m.active().Buffer().SetText("x  \ny\t") // mark dirty path through the editor API
	ecSave(t, m)
	if got, _ := os.ReadFile(a); string(got) != "x\ny\n" {
		t.Fatalf("saved %q", got)
	}
	// A file the config does not cover keeps its bytes.
	b := filepath.Join(dir, "b.dat")
	ecWrite(t, b, "x  \ny")
	ecOpen(t, m, b)
	m.active().Buffer().SetText("x  \ny ")
	ecSave(t, m)
	if got, _ := os.ReadFile(b); string(got) != "x  \ny " {
		t.Fatalf("unconfigured file changed: %q", got)
	}
}

func TestEditorConfigEndOfLineOnlyForNewFile(t *testing.T) {
	m := newEditor(t)
	dir := filepath.Join(m.ws.Root, "repos", "alpha")
	ecWrite(t, filepath.Join(dir, ".editorconfig"), "[*.txt]\nend_of_line = crlf\n")
	fresh := filepath.Join(dir, "new.txt")
	ecWrite(t, fresh, "")
	ecOpen(t, m, fresh)
	m.active().Buffer().SetText("a\nb")
	ecSave(t, m)
	if got, _ := os.ReadFile(fresh); string(got) != "a\r\nb\r\n" {
		t.Fatalf("new file %q", got)
	}
	old := filepath.Join(dir, "old.txt")
	ecWrite(t, old, "a\nb\n")
	ecOpen(t, m, old)
	m.active().Buffer().SetText("a\nc\n")
	ecSave(t, m)
	if got, _ := os.ReadFile(old); string(got) != "a\nc\n" {
		t.Fatalf("existing file rewritten: %q", got)
	}
}

func TestEditorConfigSaveInvalidatesCache(t *testing.T) {
	m := newEditor(t)
	dir := filepath.Join(m.ws.Root, "repos", "alpha")
	cfg := filepath.Join(dir, ".editorconfig")
	ecWrite(t, cfg, "[*.txt]\nindent_style = tab\n")
	a := filepath.Join(dir, "a.txt")
	ecWrite(t, a, "x\n")
	ecOpen(t, m, a)
	if m.active().Indent() != "\t" {
		t.Fatalf("indent %q", m.active().Indent())
	}
	ecWrite(t, cfg, "[*.txt]\nindent_style = space\nindent_size = 2\n")
	ecOpen(t, m, cfg)
	m.active().Buffer().SetText("[*.txt]\nindent_style = space\nindent_size = 2\n")
	ecSave(t, m)
	m.activeTab = 0
	if got := m.bufs[0].ed.Indent(); got != "  " {
		t.Fatalf("after .editorconfig save indent = %q", got)
	}
}
