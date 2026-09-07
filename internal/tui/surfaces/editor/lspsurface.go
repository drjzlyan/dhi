package editor

import (
	"context"
	"fmt"
	"sort"
	"strings"

	"github.com/drjzlyan/dhi/internal/lsp"
	"github.com/drjzlyan/dhi/internal/textbuf"
	"github.com/drjzlyan/dhi/internal/tui/theme"
)

// lspOpenDoc announces a document when a Go server is available.
// Failure is surfaced once (ADR-0011): the command line names the fix
// instead of LSP being silently off.
func (m *Model) lspOpenDoc(path, text string) {
	if m.lspMgr == nil || !strings.HasSuffix(path, ".go") {
		return
	}
	c, err := m.lspMgr.Ensure(context.Background(), "go", "gopls", m.currentGitRootOrFirst())
	if err != nil {
		if !m.lspNoted {
			m.lspNoted = true
			m.active().SetMessage(fmt.Sprintf(
				"no language server available — hover/rename/code actions off (%s) — run bootstrap", err.Error()))
		}
		return
	}
	if len(c.Events) == 0 && m.termMsgs != nil {
		go m.pumpLSPEvents(c)
	}
	_ = c.DidOpen(path, text, "go")
	m.lspSent[m.openVPath] = text
}

// pumpLSPEvents forwards diagnostics and server edits into the message channel.
func (m *Model) pumpLSPEvents(c *lsp.Client) {
	for ev := range c.Events {
		switch ev.Kind {
		case lsp.EvDiagnostics:
			m.termMsgs <- teaMsg{kind: lspMsgDiag, diagPath: ev.Path, diags: ev.Diags}
		case lsp.EvApplyEdit:
			m.termMsgs <- teaMsg{kind: lspMsgEdit, edit: ev.Edit}
		}
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
		// Whole-file set per publish; an empty set clears its path
		// (M2 gap fixed in F-009 — the publish carries the URI).
		m.lspDiags[msg.diagPath] = msg.diags
	case lspMsgComp:
		m.compItems = msg.compItems
		m.compCur = 0
		m.compOpen = len(msg.compItems) > 0
	case lspMsgHover:
		m.hoverOpen = msg.hoverOK && len(strings.Split(msg.hoverText, "\n")) > 0
		if m.hoverOpen {
			m.hoverLines = strings.Split(msg.hoverText, "\n")
		} else {
			m.hoverLines = nil
		}
	case lspMsgAction:
		m.actionItems = msg.actions
		m.actionCur = 0
		m.actionOpen = len(msg.actions) > 0
		if !m.actionOpen {
			if e := m.active(); e != nil {
				e.SetMessage("lsp: no code actions here")
			}
		}
	case lspMsgEdit:
		m.applyWorkspaceEdit(msg.edit)
	}
}

// lspClient returns the Go server client, or nil when LSP is off for
// the active buffer (no manager, non-Go file, server not installed).
func (m *Model) lspClient() *lsp.Client {
	e := m.active()
	if e == nil || m.lspMgr == nil || !strings.HasSuffix(e.Path(), ".go") {
		return nil
	}
	return m.lspMgr.ClientFor("go")
}

// wordAt extracts the identifier around col on line, returning the
// word plus its [start,end) rune columns (empty word when none).
func wordAt(line string, col int) (string, int, int) {
	r := []rune(line)
	if col >= len(r) || !isIdent(r[col]) {
		// sitting on the char after the word: step back one
		if col > 0 && col <= len(r) && isIdent(r[col-1]) {
			col--
		} else {
			return "", 0, 0
		}
	}
	start := col
	for start > 0 && isIdent(r[start-1]) {
		start--
	}
	end := col
	for end < len(r) && isIdent(r[end]) {
		end++
	}
	return string(r[start:end]), start, end
}

func isIdent(r rune) bool {
	return r == '_' || (r >= '0' && r <= '9') || (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z')
}

// requestHover fires an async hover request for the word under the cursor.
func (m *Model) requestHover() {
	c := m.lspClient()
	e := m.active()
	if c == nil || e == nil {
		return
	}
	cur := e.Buffer().Cursor()
	line := e.Buffer().Line(cur.Line)
	word, start, _ := wordAt(line, cur.Col)
	if word == "" {
		e.SetMessage("lsp: no identifier under cursor")
		return
	}
	path := e.Path()
	go func() {
		h, err := c.Hover(path, cur.Line, start)
		text := ""
		if err == nil && h != nil {
			text = h.Contents
		}
		select {
		case m.termMsgs <- teaMsg{kind: lspMsgHover, hoverText: text, hoverOK: text != ""}:
		default:
		}
	}()
}

// startRename captures the identifier under the cursor into the
// rename input line.
func (m *Model) startRename() {
	if m.lspClient() == nil {
		return
	}
	e := m.active()
	if e == nil {
		return
	}
	cur := e.Buffer().Cursor()
	word, _, _ := wordAt(e.Buffer().Line(cur.Line), cur.Col)
	if word == "" {
		e.SetMessage("lsp: no identifier under cursor")
		return
	}
	m.renameOld = word
	m.renameInput = nil
	m.renameMode = true
}

// handleRenameKey processes keys while the rename input is open.
func (m *Model) handleRenameKey(key string) bool {
	switch key {
	case "esc":
		m.renameMode = false
		m.renameInput = nil
		return true
	case "enter":
		newName := strings.TrimSpace(string(m.renameInput))
		m.renameMode = false
		m.renameInput = nil
		if newName == "" || newName == m.renameOld {
			return true
		}
		m.dispatchRename(newName)
		return true
	case "backspace":
		if len(m.renameInput) > 0 {
			m.renameInput = m.renameInput[:len(m.renameInput)-1]
		}
		return true
	}
	if runes := []rune(key); len(runes) == 1 && runes[0] >= ' ' {
		m.renameInput = append(m.renameInput, runes...)
		return true
	}
	return true // swallow everything else while composing
}

// dispatchRename asks the server for a rename and applies the result.
func (m *Model) dispatchRename(newName string) {
	c := m.lspClient()
	e := m.active()
	if c == nil || e == nil {
		return
	}
	cur := e.Buffer().Cursor()
	_, start, _ := wordAt(e.Buffer().Line(cur.Line), cur.Col)
	path := e.Path()
	go func() {
		edit, err := c.Rename(path, cur.Line, start, newName)
		if err != nil || edit == nil {
			return // silent like completion failures
		}
		select {
		case m.termMsgs <- teaMsg{kind: lspMsgEdit, edit: edit}:
		default:
		}
	}()
}

// requestCodeActions lists quickfixes for the cursor line's diagnostics.
func (m *Model) requestCodeActions() {
	c := m.lspClient()
	e := m.active()
	if c == nil || e == nil {
		return
	}
	cur := e.Buffer().Cursor()
	path := e.Path()
	rng := lsp.Range{
		Start: lsp.Position{Line: cur.Line},
		End:   lsp.Position{Line: cur.Line},
	}
	diags := m.lspDiags[path]
	go func() {
		actions, err := c.CodeAction(path, rng, diags)
		if err != nil {
			actions = nil
		}
		select {
		case m.termMsgs <- teaMsg{kind: lspMsgAction, actions: actions}:
		default:
		}
	}()
}

// handleActionKey processes keys while the code-action popup is open.
func (m *Model) handleActionKey(key string) bool {
	switch key {
	case "esc":
		m.dismissActions()
		return true
	case "enter":
		if m.actionCur < len(m.actionItems) {
			m.applyAction(m.actionItems[m.actionCur])
		}
		m.dismissActions()
		return true
	case "down", "j":
		if m.actionCur < len(m.actionItems)-1 {
			m.actionCur++
		}
		return true
	case "up", "k":
		if m.actionCur > 0 {
			m.actionCur--
		}
		return true
	}
	m.dismissActions()
	return false
}

func (m *Model) dismissActions() {
	m.actionOpen = false
	m.actionItems = nil
	m.actionCur = 0
}

// applyAction runs one code action: inline WorkspaceEdit first, else
// the server-side command.
func (m *Model) applyAction(a lsp.CodeAction) {
	if a.Edit != nil {
		m.applyWorkspaceEdit(a.Edit)
		return
	}
	if c := m.lspClient(); c != nil && a.Command != nil {
		_ = c.ExecuteCommand(*a.Command)
	}
}

// applyWorkspaceEdit folds a server WorkspaceEdit into open buffers.
// Edits for closed files are skipped with a note; each open file's
// edits apply bottom-up inside one undo group.
func (m *Model) applyWorkspaceEdit(edit *lsp.WorkspaceEdit) {
	if edit == nil {
		return
	}
	byPath := map[string]*bufTab{}
	for _, b := range m.bufs {
		if b.ed != nil {
			byPath[b.ed.Path()] = b
		}
	}
	skipped := 0
	for _, f := range edit.Files {
		b, ok := byPath[f.PathFor()]
		if !ok {
			skipped++
			continue
		}
		edits := make([]lsp.TextEdit, len(f.Edits))
		copy(edits, f.Edits)
		sort.Slice(edits, func(i, j int) bool {
			a, z := edits[i].Range.Start, edits[j].Range.Start
			if a.Line != z.Line {
				return a.Line > z.Line
			}
			return a.Character > z.Character
		})
		buf := b.ed.Buffer()
		buf.BeginUndoGroup()
		for _, te := range edits {
			buf.ApplyEdit(
				textbuf.Pos{Line: te.Range.Start.Line, Col: te.Range.Start.Character},
				textbuf.Pos{Line: te.Range.End.Line, Col: te.Range.End.Character},
				te.NewText,
			)
		}
		buf.EndUndoGroup()
	}
	if skipped > 0 {
		if e := m.active(); e != nil {
			e.SetMessage(fmt.Sprintf("lsp: %d file(s) not open — edits skipped", skipped))
		}
	}
	m.lspSync()
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
			rows = append(rows, theme.GlyphCursor+" "+theme.TabActive().Render(label))
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
