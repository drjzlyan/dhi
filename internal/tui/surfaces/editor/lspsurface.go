package editor

import (
	"context"
	"strings"

	"github.com/drjzlyan/dhi/internal/lsp"
	"github.com/drjzlyan/dhi/internal/textbuf"
	"github.com/drjzlyan/dhi/internal/tui/theme"
)

// lspOpenDoc announces a document when a Go server is available.
func (m *Model) lspOpenDoc(path, text string) {
	if m.lspMgr == nil || !strings.HasSuffix(path, ".go") {
		return
	}
	c, err := m.lspMgr.Ensure(context.Background(), "go", "gopls", m.currentGitRootOrFirst())
	if err != nil {
		return // gopls not installed; LSP silently off (ADR-0005)
	}
	if len(c.Events) == 0 && m.termMsgs != nil {
		go m.pumpLSPEvents(c)
	}
	_ = c.DidOpen(path, text, "go")
	m.lspSent[m.openVPath] = text
}

// pumpLSPEvents forwards diagnostics into the message channel.
func (m *Model) pumpLSPEvents(c *lsp.Client) {
	for ev := range c.Events {
		m.termMsgs <- teaMsg{kind: lspMsgDiag, diags: ev.Diags}
	}
}

// lspSync pushes full-text changes after buffer keystrokes.
func (m *Model) lspSync() {
	e := m.active()
	if e == nil || m.lspMgr == nil || !strings.HasSuffix(e.Path(), ".go") {
		return
	}
	text := e.Buffer().Text()
	if prev, ok := m.lspSent[m.openVPath]; ok && prev == text {
		return
	}
	if c := m.lspMgr.ClientFor("go"); c != nil {
		_ = c.DidChange(e.Path(), text)
		m.lspSent[m.openVPath] = text
	}
}

// requestCompletion fires an async completion request at the cursor.
func (m *Model) requestCompletion() {
	e := m.active()
	if e == nil || m.lspMgr == nil || !strings.HasSuffix(e.Path(), ".go") {
		return
	}
	c := m.lspMgr.ClientFor("go")
	if c == nil {
		return
	}
	cur := e.Buffer().Cursor()
	path := e.Path()
	go func() {
		items, err := c.Completion(path, cur.Line, cur.Col)
		if err != nil {
			items = nil
		}
		select {
		case m.termMsgs <- teaMsg{kind: lspMsgComp, compItems: items}:
		default:
		}
	}()
}

// applyLSPUpdate folds async LSP messages routed via Update.
func (m *Model) applyLSPUpdate(msg teaMsg) {
	switch msg.kind {
	case lspMsgDiag:
		for _, d := range msg.diags {
			m.lspDiags[d.Path] = msg.diags // whole-file set per publish
			break
		}
		if len(msg.diags) > 0 {
			m.lspDiags[msg.diags[0].Path] = msg.diags
		} else {
			// empty publish clears; path unknown here — acceptable MVP gap
		}
	case lspMsgComp:
		m.compItems = msg.compItems
		m.compCur = 0
		m.compOpen = len(msg.compItems) > 0
	}
}

// handleCompletionKey processes keys while the popup is open.
func (m *Model) handleCompletionKey(key string) bool {
	switch key {
	case "esc":
		m.dismissCompletion()
		return true
	case "enter":
		item, ok := m.selectedCompletion()
		if ok {
			e := m.active()
			e.Buffer().InsertString(item.Label)
			m.lspSync()
		}
		m.dismissCompletion()
		return true
	case "down", "j":
		if m.compCur < len(m.compItems)-1 {
			m.compCur++
		}
		return true
	case "up", "k":
		if m.compCur > 0 {
			m.compCur--
		}
		return true
	}
	m.dismissCompletion()
	return false // let the key also reach the editor
}

func (m *Model) dismissCompletion() {
	m.compOpen = false
	m.compItems = nil
	m.compCur = 0
}

func (m *Model) selectedCompletion() (lsp.CompletionItem, bool) {
	if m.compCur < len(m.compItems) {
		return m.compItems[m.compCur], true
	}
	return lsp.CompletionItem{}, false
}

// diagCount returns (errors, warnings) for the active buffer.
func (m *Model) diagCount(e *textbuf.Editor) (int, int) {
	diags := m.lspDiags[e.Path()]
	errs, warns := 0, 0
	for _, d := range diags {
		switch d.Severity {
		case 1:
			errs++
		case 2:
			warns++
		}
	}
	return errs, warns
}

// gutterStyle colors the line number when the line has problems.
func (m *Model) gutterFor(path string, line int, plain string) string {
	for _, d := range m.lspDiags[path] {
		if d.Line != line {
			continue
		}
		if d.Severity == 1 {
			return theme.DangerText().Render(plain)
		}
		if d.Severity == 2 {
			return theme.WarningText().Render(plain)
		}
	}
	return theme.Hint().Render(plain)
}

// completionView renders the popup rows above the command line.
func (m *Model) completionView() []string {
	if !m.compOpen || len(m.compItems) == 0 {
		return nil
	}
	rows := make([]string, 0, min(len(m.compItems), 8)+1)
	rows = append(rows, theme.Hint().Render("completions:"))
	end := min(m.compCur+8, len(m.compItems))
	start := maxInt(0, end-8)
	for i := start; i < end; i++ {
		label := m.compItems[i].Label + "  " + m.compItems[i].Detail
		if i == m.compCur {
			rows = append(rows, string(theme.GlyphCursor)+" "+theme.TabActive().Render(label))
		} else {
			rows = append(rows, "  "+theme.TextDim().Render(label))
		}
	}
	return rows
}

func (m *Model) currentGitRootOrFirst() string {
	if root := m.currentGitRoot(); root != "" {
		return root
	}
	if len(m.members) > 0 {
		return m.members[0].path
	}
	return ""
}
