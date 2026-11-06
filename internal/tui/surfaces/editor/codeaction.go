package editor

import (
	"sort"
	"strings"

	"github.com/drjzlyan/dhi/internal/lsp"
	"github.com/drjzlyan/dhi/internal/textbuf"
)

// Code actions (F-158): `ga` lists what the server offers for the cursor
// line or the visual selection, quick fixes first; the cursor line wears
// a marker in the title when one is ready; :organize applies the server's
// source.organizeImports. Edits apply as one undo step per file.

const (
	kindQuickfix = "quickfix"
	kindOrganize = "source.organizeImports"
)

// rankActions drops disabled actions and orders the rest: quick fixes
// (preferred first), then refactors, then source actions, then the rest.
// The sort is stable, so the server's own order breaks ties.
func rankActions(in []lsp.CodeAction) []lsp.CodeAction {
	out := make([]lsp.CodeAction, 0, len(in))
	for _, a := range in {
		if a.Disabled == "" {
			out = append(out, a)
		}
	}
	rank := func(a lsp.CodeAction) int {
		r := 3
		switch {
		case strings.HasPrefix(a.Kind, kindQuickfix):
			r = 0
		case strings.HasPrefix(a.Kind, "refactor"):
			r = 2
		case strings.HasPrefix(a.Kind, "source"):
			r = 3
		case a.Kind == "":
			r = 1 // a bare command or an unkinded fix is closest to a fix
		}
		r *= 2
		if !a.Preferred {
			r++
		}
		return r
	}
	sort.SliceStable(out, func(i, j int) bool { return rank(out[i]) < rank(out[j]) })
	return out
}

// diagsInLines is the diagnostics of path touching lines [from,to].
func (m *Model) diagsInLines(path string, from, to int) []lsp.Diagnostic {
	var out []lsp.Diagnostic
	for _, d := range m.lspDiags[path] {
		if d.Line >= from && d.Line <= to {
			out = append(out, d)
		}
	}
	return out
}

// requestCodeActions lists the code actions for the cursor line (or, with
// selection, the visual selection) and opens the popup.
func (m *Model) requestCodeActions(selection bool) {
	e := m.active()
	if e == nil {
		return
	}
	path := e.Path()
	c := m.lspClient()
	if c == nil {
		if m.lspMgr != nil {
			m.restricted(path, "use code actions") // names the trust gate when that is why
		}
		return
	}
	buf := e.Buffer()
	cur := buf.Cursor()
	from, to := textbuf.Pos{Line: cur.Line}, textbuf.Pos{Line: cur.Line, Col: len([]rune(buf.Line(cur.Line)))}
	if selection {
		if _, a, z, ok := e.Selection(); ok {
			from, to = a, z
		}
		e.Key("esc") // leave visual mode like vim does when an action fires
	}
	rng := lsp.Range{
		Start: lsp.Position{Line: from.Line, Character: lsp.UTF16Col(buf.Line(from.Line), from.Col)},
		End:   lsp.Position{Line: to.Line, Character: lsp.UTF16Col(buf.Line(to.Line), to.Col)},
	}
	diags := m.diagsInLines(path, from.Line, to.Line)
	m.lspSync() // the server must see the text the range refers to
	go func() {
		actions, err := c.CodeAction(path, rng, diags)
		if err != nil {
			actions = nil
		}
		select {
		case m.termMsgs <- teaMsg{kind: lspMsgAction, actions: rankActions(actions)}:
		default:
		}
	}()
}

// probeBulb asks, once per cursor line, whether the server has a quick fix
// for the problems on it; the answer lights the marker in the title. It
// only runs where a server is already up (never starts one).
func (m *Model) probeBulb() {
	e := m.active()
	if e == nil || e.Path() == "" || m.lspMgr == nil || !m.bufFocus {
		return
	}
	path, line := e.Path(), e.Buffer().Cursor().Line
	diags := m.diagsInLines(path, line, line)
	if len(diags) == 0 {
		m.bulbKey, m.bulbOn = "", false
		return
	}
	key := path + "\x00" + itoa(line) + "\x00" + itoa(len(diags))
	if key == m.bulbKey {
		return
	}
	c := m.lspClient()
	if c == nil {
		return
	}
	m.bulbKey, m.bulbOn = key, false
	m.lspSync()
	buf := e.Buffer()
	rng := lsp.Range{
		Start: lsp.Position{Line: line},
		End:   lsp.Position{Line: line, Character: lsp.UTF16Col(buf.Line(line), len([]rune(buf.Line(line))))},
	}
	go func() {
		actions, err := c.CodeActionOnly(path, rng, diags, []string{kindQuickfix})
		ok := err == nil && len(rankActions(actions)) > 0
		select {
		case m.termMsgs <- teaMsg{kind: lspMsgBulb, note: key, hoverOK: ok}:
		default:
		}
	}()
}

// runAction runs the picked action. A server that advertises
// codeAction/resolve may hand back a bare action; it is resolved first
// (off the UI loop), then applied.
func (m *Model) runAction(a lsp.CodeAction) {
	c := m.lspClient()
	if c == nil {
		return
	}
	if a.Edit == nil && a.Command == nil && c.CanResolveCodeAction() {
		m.resolveThenRun(c, a)
		return
	}
	m.applyAction(a)
}

func (m *Model) resolveThenRun(c *lsp.Client, a lsp.CodeAction) {
	go func() {
		r, err := c.ResolveCodeAction(a)
		msg := teaMsg{kind: lspMsgRun, actions: []lsp.CodeAction{r}}
		if err != nil {
			msg = teaMsg{kind: lspMsgNote, note: "lsp: " + a.Title + ": " + err.Error()}
		}
		select {
		case m.termMsgs <- msg:
		default:
		}
	}()
}

// applyAction applies the action's WorkspaceEdit (one undo group per
// file), then executes its command; a command's own edits come back as the
// server's workspace/applyEdit request and land the same way.
func (m *Model) applyAction(a lsp.CodeAction) {
	if a.Edit == nil && a.Command == nil {
		m.noteActive("lsp: " + a.Title + ": the server returned no edit")
		return
	}
	if a.Edit != nil {
		m.applyWorkspaceEdit(a.Edit)
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

// organizeImports implements :organize with the server's
// source.organizeImports action on the whole file.
func (m *Model) organizeImports() string {
	e := m.active()
	if e == nil || e.Path() == "" {
		return "organize: open a file first"
	}
	path := e.Path()
	if _, _, known := m.langs.For(path); !known {
		return "organize: no language support for this file type"
	}
	if m.restricted(path, "organize imports") {
		return trustLine("organize imports")
	}
	c := m.lspClient()
	if c == nil {
		return "organize: no language server for this file"
	}
	buf := e.Buffer()
	last := buf.LineCount() - 1
	rng := lsp.Range{End: lsp.Position{Line: last, Character: lsp.UTF16Col(buf.Line(last), len([]rune(buf.Line(last))))}}
	m.lspSync()
	go func() {
		note := func(s string) {
			select {
			case m.termMsgs <- teaMsg{kind: lspMsgNote, note: s}:
			default:
			}
		}
		actions, err := c.CodeActionOnly(path, rng, nil, []string{kindOrganize})
		if err != nil {
			note("organize failed: " + err.Error())
			return
		}
		var pick *lsp.CodeAction
		for _, a := range rankActions(actions) {
			if strings.HasPrefix(a.Kind, kindOrganize) {
				pick = &a
				break
			}
		}
		if pick == nil {
			note("organize: the language server offers no organize-imports here")
			return
		}
		a := *pick
		if a.Edit == nil && a.Command == nil && c.CanResolveCodeAction() {
			if a, err = c.ResolveCodeAction(a); err != nil {
				note("organize failed: " + err.Error())
				return
			}
		}
		select {
		case m.termMsgs <- teaMsg{kind: lspMsgRun, actions: []lsp.CodeAction{a}}:
		default:
		}
	}()
	return ""
}
