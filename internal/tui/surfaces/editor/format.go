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
	if m.lspMgr == nil || !strings.HasSuffix(path, ".go") {
		return nil
	}
	return m.lspMgr.ClientFor("go")
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

// formatEditor formats ed through the language server and applies the
// edits as one undo group. It returns a short note ("" = formatted or
// nothing to do); a failure is named, never swallowed.
func (m *Model) formatEditor(ed *textbuf.Editor) string {
	c := m.clientFor(ed.Path())
	if c == nil {
		return "format: no language server for this file"
	}
	path := ed.Path()
	// The server must be formatting the text we are about to edit. An agent
	// edit (ApplyReplace) changes the buffer without a didChange, and the
	// edit ranges the server returns are only valid against the text it
	// last saw — so resync here, straight on this client (lspSync's cache is
	// keyed on the open tab, not on ed). A failed resync aborts the format.
	text := ed.Buffer().Text()
	if err := c.DidChange(path, text); err != nil {
		return "format failed: could not sync the buffer to the language server: " + err.Error()
	}
	edits, err := withTimeout(func() ([]lsp.TextEdit, error) { return c.Formatting(path, 4, false) })
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

// beforeSave is the format-on-save hook installed on every buffer: Go
// buffers with a live server are formatted; anything else saves as-is
// (no server is the normal case, so it stays quiet).
func (m *Model) beforeSave(ed *textbuf.Editor) string {
	if !m.formatOnSave || m.clientFor(ed.Path()) == nil {
		return ""
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
