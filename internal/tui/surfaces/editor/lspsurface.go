package editor

import (
	tea "charm.land/bubbletea/v2"
	"context"
	"fmt"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/drjzlyan/dhi/internal/langserver"
	"github.com/drjzlyan/dhi/internal/lsp"
	"github.com/drjzlyan/dhi/internal/textbuf"
	"github.com/drjzlyan/dhi/internal/tui/kit"
	"github.com/drjzlyan/dhi/internal/tui/theme"
)

// lspOpenDoc announces a document to its language's server. A missing
// server is surfaced once per language: the command line names the fix
// instead of LSP being silently off. Nothing is installed here — `:lsp
// install` raises the install card (F-110).
func (m *Model) lspOpenDoc(path, text string) {
	if m.lspMgr == nil {
		return
	}
	l, langID, ok := m.langs.For(path)
	if !ok {
		return
	}
	if c := m.lspMgr.ClientFor(l.ID); c != nil && alive(c) {
		m.didOpen(c, path, text, langID)
		return
	}
	if _, found := m.lspMgr.Locate(l.Server); !found { // cheap: say so now
		m.noteMissing(l, fmt.Errorf("lsp: %s is not installed", l.Server))
		return
	}
	// Start the server off the UI loop (F-116): a slow or stuck server
	// used to freeze the editor on the first file it opened.
	m.lspWaiting[l.ID] = append(m.lspWaiting[l.ID], path)
	if m.lspStarting[l.ID] {
		return
	}
	m.lspStarting[l.ID] = true
	mgr, root := m.lspMgr, m.currentGitRootOrFirst()
	id := l.ID
	m.queue(func() tea.Msg {
		c, err := mgr.EnsureServer(context.Background(), l.ID, l.Server, l.Args, root)
		return lspReadyMsg{lang: id, c: c, err: err}
	})
	m.queue(tea.Tick(lspStartWait, func(time.Time) tea.Msg { return lspSlowMsg{lang: id} }))
}

// lspStartWait is how long a server may take to start before the user is
// told (it keeps starting in the background).
const lspStartWait = 10 * time.Second

type lspReadyMsg struct {
	lang string
	c    *lsp.Client
	err  error
}

type lspSlowMsg struct{ lang string }

func alive(c *lsp.Client) bool {
	select {
	case <-c.Done():
		return false
	default:
		return true
	}
}

// onLSPReady opens every buffer that waited for the server.
func (m *Model) onLSPReady(msg lspReadyMsg) {
	m.lspStarting[msg.lang] = false
	waiting := m.lspWaiting[msg.lang]
	delete(m.lspWaiting, msg.lang)
	l, ok := m.langs.ByID(msg.lang)
	if msg.err != nil || msg.c == nil {
		if ok {
			m.noteMissing(l, msg.err)
		}
		return
	}
	for _, p := range waiting {
		for _, t := range m.bufs {
			if t.ed != nil && t.path == p {
				_, langID, _ := m.langs.For(p)
				m.didOpenTab(msg.c, t, langID)
			}
		}
	}
}

func (m *Model) onLSPSlow(msg lspSlowMsg) {
	if !m.lspStarting[msg.lang] {
		return
	}
	if l, ok := m.langs.ByID(msg.lang); ok {
		if e := m.active(); e != nil {
			e.SetMessage(l.Name + " language server is slow to start — still trying in the background")
		}
	}
}

// didOpen announces the active buffer's file to c.
func (m *Model) didOpen(c *lsp.Client, path, text, langID string) {
	m.pump(c)
	_ = c.DidOpen(path, text, langID)
	m.lspSent[m.openVPath] = text
}

func (m *Model) didOpenTab(c *lsp.Client, t *bufTab, langID string) {
	m.pump(c)
	text := t.ed.Buffer().Text()
	_ = c.DidOpen(t.path, text, langID)
	m.lspSent[t.vp] = text
}

// pump starts the one event reader a client gets (it used to start one
// per file opened).
func (m *Model) pump(c *lsp.Client) {
	if m.lspPumped[c] || m.termMsgs == nil {
		return
	}
	m.lspPumped[c] = true
	go m.pumpLSPEvents(c)
}

// missingServerNote tells the user exactly how to get the server.
func missingServerNote(l langserver.Language, err error) string {
	head := "no " + l.Name + " language server"
	if l.Install.Tool == "" {
		return head + " (" + err.Error() + ") — check editor.languages." + l.ID + ".command"
	}
	if !strings.Contains(err.Error(), "not installed") {
		return l.Name + " language server did not start: " + err.Error()
	}
	return head + " — :lsp install " + l.ID + " (asks first)"
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
	if e == nil || m.lspMgr == nil {
		return
	}
	l, _, ok := m.langs.For(e.Path())
	if !ok {
		return
	}
	text := e.Buffer().Text()
	if prev, ok := m.lspSent[m.openVPath]; ok && prev == text {
		return
	}
	if c := m.lspMgr.ClientFor(l.ID); c != nil {
		_ = c.DidChange(e.Path(), text)
		m.lspSent[m.openVPath] = text
	}
}

// lspDidSave sends didSave after a buffer is saved, with any pending changes first.
func (m *Model) lspDidSave(ed *textbuf.Editor) {
	if m.lspMgr == nil {
		return
	}
	// Find the tab containing this editor.
	var tab *bufTab
	for _, t := range m.bufs {
		if t.ed == ed {
			tab = t
			break
		}
	}
	if tab == nil {
		return
	}
	l, _, ok := m.langs.For(tab.path)
	if !ok {
		return
	}
	c := m.lspMgr.ClientFor(l.ID)
	if c == nil {
		return
	}
	// Send any unsent changes first.
	text := ed.Buffer().Text()
	if prev, ok := m.lspSent[tab.vp]; ok && prev != text {
		_ = c.DidChange(tab.path, text)
	}
	// Now send didSave with the current text.
	_ = c.DidSave(tab.path, text)
	m.lspSent[tab.vp] = text
}

// lspDidClose sends didClose for a buffer that was opened to LSP.
func (m *Model) lspDidClose(vp, path string) {
	if m.lspMgr == nil {
		return
	}
	// Only send if this tab had received a didOpen (i.e., it's in lspSent).
	if _, wasOpened := m.lspSent[vp]; !wasOpened {
		return
	}
	l, _, ok := m.langs.For(path)
	if !ok {
		return
	}
	c := m.lspMgr.ClientFor(l.ID)
	if c == nil {
		return
	}
	_ = c.DidClose(path)
	delete(m.lspSent, vp)
}

// requestCompletion fires an async completion request at the cursor.
func (m *Model) requestCompletion() {
	e := m.active()
	if e == nil {
		return
	}
	c := m.lspClient()
	if c == nil {
		return
	}
	cur := e.Buffer().Cursor()
	path, col := e.Path(), u16(e, cur.Line, cur.Col)
	go func() {
		items, err := c.Completion(path, cur.Line, col)
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
	case lspMsgSig:
		m.applySignature(msg)
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
	case lspMsgNote:
		if e := m.active(); e != nil {
			e.SetMessage(msg.note)
		}
	}
}

// lspClient returns the active buffer's language-server client, or nil when
// LSP is off for it (no manager, no language, server not running).
func (m *Model) lspClient() *lsp.Client {
	e := m.active()
	if e == nil {
		return nil
	}
	return m.clientFor(e.Path())
}

// wordAt extracts the identifier around col on line, returning the
// word plus its start rune column (empty word when none).
func wordAt(line string, col int) (string, int) {
	r := []rune(line)
	if col >= len(r) || !isIdent(r[col]) {
		// sitting on the char after the word: step back one
		if col > 0 && col <= len(r) && isIdent(r[col-1]) {
			col--
		} else {
			return "", 0
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
	return string(r[start:end]), start
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
	word, start := wordAt(line, cur.Col)
	if word == "" {
		e.SetMessage("lsp: no identifier under cursor")
		return
	}
	path, start := e.Path(), u16(e, cur.Line, start)
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
	word, _ := wordAt(e.Buffer().Line(cur.Line), cur.Col)
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
	}
	m.renameInput, _ = kit.EditRunes(m.renameInput, key)
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
	_, clientStart := wordAt(e.Buffer().Line(cur.Line), cur.Col)
	clientStart = u16(e, cur.Line, clientStart)
	path := e.Path()
	go func() {
		// Validate and anchor the edit with the server (F-009): a range
		// from prepareRename beats the client word guess, and an explicit
		// decline stops the rename instead of silently sending garbage.
		// An unsupported method (error) degrades to the client range.
		start := clientStart
		if rng, ok, err := c.PrepareRename(path, cur.Line, clientStart); err == nil {
			if !ok {
				select {
				case m.termMsgs <- teaMsg{kind: lspMsgNote, note: "lsp: not renameable here"}:
				default:
				}
				return
			}
			start = rng.Start.Character
		}
		edit, err := c.Rename(path, cur.Line, start, newName)
		if err != nil || edit == nil {
			note := "lsp: rename found nothing to change"
			if err != nil {
				note = "lsp: rename failed: " + err.Error()
			}
			select {
			case m.termMsgs <- teaMsg{kind: lspMsgNote, note: note}:
			default:
			}
			return
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
	line := e.Buffer().Line(cur.Line)
	rng := lsp.Range{ // the whole cursor line, so a fix anywhere on it is offered
		Start: lsp.Position{Line: cur.Line},
		End:   lsp.Position{Line: cur.Line, Character: lsp.UTF16Col(line, len([]rune(line)))},
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
		cmd := *a.Command
		go func() { // a request: the server may answer slowly (F-116)
			if err := c.ExecuteCommand(cmd); err != nil {
				select {
				case m.termMsgs <- teaMsg{kind: lspMsgNote, note: "lsp: " + a.Title + ": " + err.Error()}:
				default:
				}
			}
		}()
	}
}

// applyWorkspaceEdit applies a server WorkspaceEdit: open files in their
// buffer (one undo group each), files that are not open straight on disk
// (F-116: a rename used to skip them while the server believed it done).
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
	written, failed := 0, []string{}
	for _, f := range edit.Files {
		path := f.PathFor()
		if b, ok := byPath[path]; ok {
			applyTextEdits(b.ed.Buffer(), f.Edits)
			continue
		}
		if err := editFileOnDisk(path, f.Edits); err != nil {
			failed = append(failed, filepath.Base(path)+": "+err.Error())
			continue
		}
		written++
	}
	if e := m.active(); e != nil {
		switch {
		case len(failed) > 0:
			e.SetMessage("lsp: not applied to " + strings.Join(failed, "; "))
		case written > 0:
			e.SetMessage("lsp: also changed " + kit.Count(written, "file") + " that " +
				map[bool]string{true: "was", false: "were"}[written == 1] + " not open (saved)")
		}
	}
	if written > 0 {
		m.invalidateTreeGit()
	}
	m.lspSync()
}

// editFileOnDisk applies edits to a file no buffer holds and saves it.
func editFileOnDisk(path string, edits []lsp.TextEdit) error {
	b, err := textbuf.Open(path)
	if err != nil {
		return err
	}
	applyTextEdits(b, edits)
	return b.Save(path)
}

// applyTextEdits folds server edits into buf bottom-up inside one undo
// group, so earlier ranges stay valid and a single `u` reverts them all.
// Columns arrive in UTF-16 units and are converted on each line's text
// before it changes (bottom-up keeps earlier text untouched).
func applyTextEdits(buf *textbuf.Buffer, in []lsp.TextEdit) {
	edits := make([]lsp.TextEdit, len(in))
	copy(edits, in)
	sort.SliceStable(edits, func(i, j int) bool {
		a, z := edits[i].Range.Start, edits[j].Range.Start
		if a.Line != z.Line {
			return a.Line > z.Line
		}
		return a.Character > z.Character
	})
	col := func(p lsp.Position) textbuf.Pos {
		if p.Line >= buf.LineCount() {
			last := buf.LineCount() - 1
			return textbuf.Pos{Line: last, Col: len([]rune(buf.Line(last)))}
		}
		return textbuf.Pos{Line: p.Line, Col: lsp.RuneCol(buf.Line(p.Line), p.Character)}
	}
	buf.BeginUndoGroup()
	for _, te := range edits {
		buf.ApplyEdit(col(te.Range.Start), col(te.Range.End), te.NewText)
	}
	buf.EndUndoGroup()
}

// handleCompletionKey processes keys while the popup is open.
func (m *Model) handleCompletionKey(key string) bool {
	switch key {
	case "esc":
		m.dismissCompletion()
		return true
	case "enter", "tab":
		if item, ok := m.selectedCompletion(); ok {
			m.acceptCompletion(item)
		}
		m.dismissCompletion()
		return true
	case "down", "ctrl+n": // j/k type: the popup opens while typing (F-116)
		if m.compCur < len(m.compItems)-1 {
			m.compCur++
		}
		return true
	case "up", "ctrl+p":
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

// LSPCall serves one agent-requested LSP operation synchronously,
// returning a text result (ADR-0023). rename and code_action have
// effects; the tool layer gates them with approvals.
func (m *Model) LSPCall(_ context.Context, op, abs string, line, col int, arg string) (string, error) {
	c := m.clientFor(abs)
	if c == nil {
		return "", fmt.Errorf("LSP unavailable: no language server is running for %s", filepath.Base(abs))
	}
	switch op {
	case "hover":
		h, err := c.Hover(abs, line, col)
		if err != nil {
			return "", err
		}
		if h == nil || strings.TrimSpace(h.Contents) == "" {
			return "(no hover info)", nil
		}
		return h.Contents, nil
	case "definition", "references":
		var locs []lsp.Location
		var err error
		if op == "definition" {
			locs, err = c.Definition(abs, line, col)
		} else {
			locs, err = c.References(abs, line, col)
		}
		if err != nil {
			return "", err
		}
		if len(locs) == 0 {
			return "(none found)", nil
		}
		var b strings.Builder
		for _, l := range locs {
			fmt.Fprintf(&b, "%s:%d:%d\n", l.PathFor(), l.Range.Start.Line+1, l.Range.Start.Character+1)
		}
		return b.String(), nil
	case "rename":
		if strings.TrimSpace(arg) == "" {
			return "", fmt.Errorf("new name is required for rename")
		}
		edit, err := c.Rename(abs, line, col, arg)
		if err != nil {
			return "", err
		}
		if edit == nil || len(edit.Files) == 0 {
			return "(no edits)", nil
		}
		m.applyWorkspaceEdit(edit)
		return "renamed across " + kit.Count(len(edit.Files), "file"), nil
	case "code_action":
		rng := lsp.Range{Start: lsp.Position{Line: line, Character: col}, End: lsp.Position{Line: line, Character: col}}
		actions, err := c.CodeAction(abs, rng, nil)
		if err != nil {
			return "", err
		}
		if len(actions) == 0 {
			return "(no code actions)", nil
		}
		var b strings.Builder
		for _, a := range actions {
			fmt.Fprintf(&b, "- %s\n", a.Title)
		}
		return b.String(), nil
	default:
		return "", fmt.Errorf("unknown LSP op %q", op)
	}
}

// noteMissing tells the user once per language why it has no server.
func (m *Model) noteMissing(l langserver.Language, err error) {
	if m.lspNoted[l.ID] {
		return
	}
	m.lspNoted[l.ID] = true
	if e := m.active(); e != nil {
		e.SetMessage(missingServerNote(l, err))
	}
}
