package editor

import (
	"path/filepath"

	"github.com/drjzlyan/dhi/internal/editorconfig"
	"github.com/drjzlyan/dhi/internal/textbuf"
)

// EditorConfig (F-160): a file's .editorconfig, read from its folder up to
// the member root, shapes the buffer. Precedence for indent: .editorconfig,
// then the file's own detected style, then the language, then Settings.

// configFor resolves path's .editorconfig properties (cached per folder).
func (m *Model) configFor(path string) editorconfig.Config {
	if m.ec == nil {
		m.ec = editorconfig.NewResolver()
	}
	return m.ec.Lookup(path, m.memberRootOf(path))
}

// applyEditorConfigOpen applies the open-time properties: the line ending
// of a brand-new (empty) file. An existing file's endings are never
// rewritten (F-115); "cr" is not supported and is ignored.
func (m *Model) applyEditorConfigOpen(ed *textbuf.Editor) {
	switch m.configFor(ed.Path()).EndOfLine {
	case "crlf":
		ed.Buffer().SetCRLF(true)
	case "lf":
		ed.Buffer().SetCRLF(false)
	}
}

// editorConfigBeforeSave applies insert_final_newline and
// trim_trailing_whitespace, only when the file's config says true.
func (m *Model) editorConfigBeforeSave(ed *textbuf.Editor) {
	c := m.configFor(ed.Path())
	b := ed.Buffer()
	if c.Trim != nil && *c.Trim {
		b.TrimTrailingWhitespace()
	}
	if c.FinalNL != nil && *c.FinalNL {
		b.EnsureFinalNewline()
	}
}

// editorConfigSaved drops the cache when an .editorconfig was saved and
// re-derives every open buffer's tab width and indent unit.
func (m *Model) editorConfigSaved(path string) {
	if filepath.Base(path) != editorconfig.FileName || m.ec == nil {
		return
	}
	m.ec.Invalidate()
	for _, t := range m.bufs {
		t.tabWidth = m.configFor(t.path).TabWidth
		t.ed.SetIndent(m.indentFor(t.path, t.ed.Buffer().Text()))
	}
}
