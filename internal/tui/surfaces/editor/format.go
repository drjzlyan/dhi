package editor

import (
	"fmt"
	"strings"
	"time"

	"github.com/drjzlyan/dhi/internal/lsp"
	"github.com/drjzlyan/dhi/internal/textbuf"
)

// lspTimeout bounds the synchronous language-server calls the editor makes
// on explicit user commands (format, outline).
var lspTimeout = 3 * time.Second

// clientFor returns the language-server client for path, or nil.
func (m *Model) clientFor(path string) *lsp.Client {
	if m.lspMgr == nil {
		return nil
	}
	l, _, ok := m.langs.For(path)
	if !ok {
		return nil
	}
	return m.lspMgr.ClientFor(l.ID)
}

// withTimeout runs fn and gives up after lspTimeout so a wedged server
// can never freeze the UI loop.
func withTimeout[T any](fn func() (T, error)) (T, error) {
	type res struct {
		v   T
		err error
	}
	ch := make(chan res, 1)
	go func() {
		v, err := fn()
		ch <- res{v, err}
	}()
	select {
	case r := <-ch:
		return r.v, r.err
	case <-time.After(lspTimeout):
		var zero T
		return zero, fmt.Errorf("language server timed out after %s", lspTimeout)
	}
}

// formatEditor formats ed and applies the result as one undo group. The
// formatter is, in order: the language's configured external command, else
// the language server's formatting when it advertises it. Anything else is a
// named "no formatter" — never a host-tool fallback (ADR-0011). It returns a
// short note ("" = formatted or nothing to do); a failure is named.
func (m *Model) formatEditor(ed *textbuf.Editor) string {
	path := ed.Path()
	l, _, known := m.langs.For(path)
	if !known {
		return "format: no language support for this file type"
	}
	if len(l.Format) > 0 {
		return m.formatExternal(ed, l)
	}
	c := m.clientFor(path)
	if c == nil {
		return "format: no language server for this file"
	}
	if !c.CanFormat() {
		return "format: the " + l.Name + " server cannot format — set editor.languages." + l.ID + ".formatter"
	}
	// The server must be formatting the text we are about to edit. An agent
	// edit (ApplyReplace) changes the buffer without a didChange, and the
	// edit ranges the server returns are only valid against the text it
	// last saw — so resync here, straight on this client (lspSync's cache is
	// keyed on the open tab, not on ed). A failed resync aborts the format.
	text := ed.Buffer().Text()
	if err := c.DidChange(path, text); err != nil {
		return "format failed: could not sync the buffer to the language server: " + err.Error()
	}
	width := l.Width
	if width <= 0 {
		width = 4
	}
	edits, err := withTimeout(func() ([]lsp.TextEdit, error) { return c.Formatting(path, width, !l.UseTabs) })
	if err != nil {
		return "format failed: " + err.Error()
	}
	if len(edits) == 0 {
		return ""
	}
	applyTextEdits(ed.Buffer(), edits)
	m.lspSync()
	return ""
}

// beforeSave is the format-on-save hook installed on every buffer: a buffer
// with a configured formatter, or a live server that can format, is
// formatted; anything else saves as-is (no server is the normal case, so it
// stays quiet).
func (m *Model) beforeSave(ed *textbuf.Editor) string {
	if !m.formatOnSave {
		return ""
	}
	l, _, ok := m.langs.For(ed.Path())
	if !ok {
		return ""
	}
	if len(l.Format) == 0 {
		c := m.clientFor(ed.Path())
		if c == nil || !c.CanFormat() {
			return ""
		}
	}
	if note := m.formatEditor(ed); note != "" {
		return " — saved unformatted (" + note + ")"
	}
	return ""
}

// fmtCommand handles :fmt and :set fmt|nofmt.
func (m *Model) fmtCommand(cmd string) (string, bool) {
	switch strings.TrimSpace(cmd) {
	case "fmt":
		e := m.active()
		if e == nil {
			return "fmt: open a file first", true
		}
		if note := m.formatEditor(e); note != "" {
			return note, true
		}
		return "formatted", true
	case "set fmt":
		m.formatOnSave = true
		return "format on save: on", true
	case "set nofmt":
		m.formatOnSave = false
		return "format on save: off", true
	}
	return "", false
}
