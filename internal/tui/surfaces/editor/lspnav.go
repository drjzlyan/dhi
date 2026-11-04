package editor

import (
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
	"unicode/utf8"

	tea "charm.land/bubbletea/v2"

	"github.com/drjzlyan/dhi/internal/lsp"
	"github.com/drjzlyan/dhi/internal/search"
	"github.com/drjzlyan/dhi/internal/textbuf"
	"github.com/drjzlyan/dhi/internal/tui/kit"
	"github.com/drjzlyan/dhi/internal/tui/theme"
)

// lineOf returns line n of path: the open buffer's when there is one,
// else the file's ("" when unreadable).
func (m *Model) lineOf(path string, n int) string {
	for _, t := range m.bufs {
		if t.ed != nil && t.path == path {
			if n < t.ed.Buffer().LineCount() {
				return t.ed.Buffer().Line(n)
			}
			return ""
		}
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return ""
	}
	lines := strings.Split(string(data), "\n")
	if n < len(lines) {
		return strings.TrimSuffix(lines[n], "\r")
	}
	return ""
}

// u16 converts the active buffer's rune column on line to LSP units.
func u16(e *textbuf.Editor, line, col int) int {
	return lsp.UTF16Col(e.Buffer().Line(line), col)
}

// --- completion (F-116) ---

const compDebounce = 150 * time.Millisecond

type compTickMsg struct{ seq int }

// autoComplete schedules a completion request after typing settles: an
// identifier character or "." in insert mode.
func (m *Model) autoComplete(key string) {
	r, size := utf8.DecodeRuneInString(key)
	if size != len(key) || (!isIdent(r) && r != '.') || (m.lspClient() == nil && !m.hasUserSnippets()) {
		return
	}
	m.compSeq++
	seq := m.compSeq
	m.queue(tea.Tick(compDebounce, func(time.Time) tea.Msg { return compTickMsg{seq: seq} }))
}

func (m *Model) onCompTick(msg compTickMsg) {
	e := m.active()
	if msg.seq != m.compSeq || e == nil || !m.bufFocus || e.Mode() != textbuf.ModeInsert {
		return
	}
	m.requestCompletion()
}

// acceptCompletion replaces what the user typed of the word (or the
// server's range) with the item's text, never appending the whole label
// after the prefix (Pri + Println was PriPrintln).
func (m *Model) acceptCompletion(item lsp.CompletionItem) {
	e := m.active()
	if e == nil {
		return
	}
	b := e.Buffer()
	cur := b.Cursor()
	line := b.Line(cur.Line)
	start := cur.Col
	if item.Range != nil && item.Range.Start.Line == cur.Line {
		start = lsp.RuneCol(line, item.Range.Start.Character)
	} else {
		r := []rune(line)
		for start > 0 && start <= len(r) && isIdent(r[start-1]) {
			start--
		}
	}
	end := cur.Col
	if item.Range != nil && item.Range.End.Line == cur.Line {
		end = max(end, lsp.RuneCol(line, item.Range.End.Character))
	}
	if start > cur.Col {
		start = cur.Col
	}
	from, to := textbuf.Pos{Line: cur.Line, Col: start}, textbuf.Pos{Line: cur.Line, Col: end}
	if item.Snippet {
		m.expandSnippet(e, from, to, item.Insert)
	} else {
		b.ApplyEdit(from, to, item.Insert)
		m.snipAfterKey(e) // a plain item accepted inside a live snippet stop
	}
	m.lspSync()
}

// --- definition and references (F-116) ---

type navMsg struct {
	refs bool
	word string
	locs []lsp.Location
	err  error
}

// requestNav asks the server where the identifier under the cursor is
// defined (gd) or used (gR).
func (m *Model) requestNav(refs bool) {
	c := m.lspClient()
	e := m.active()
	if e == nil {
		return
	}
	if c == nil {
		e.SetMessage("lsp: no language server for this file")
		return
	}
	cur := e.Buffer().Cursor()
	word, start := wordAt(e.Buffer().Line(cur.Line), cur.Col)
	if word == "" {
		e.SetMessage("lsp: no identifier under cursor")
		return
	}
	path, line, col := e.Path(), cur.Line, u16(e, cur.Line, start)
	m.queue(func() tea.Msg {
		var locs []lsp.Location
		var err error
		if refs {
			locs, err = c.References(path, line, col)
		} else {
			locs, err = c.Definition(path, line, col)
		}
		return navMsg{refs: refs, word: word, locs: locs, err: err}
	})
}

func (m *Model) onNav(msg navMsg) {
	e := m.active()
	switch {
	case msg.err != nil:
		if e != nil {
			e.SetMessage("lsp: " + msg.err.Error())
		}
		return
	case len(msg.locs) == 0:
		if e != nil {
			what := "definition"
			if msg.refs {
				what = "references"
			}
			e.SetMessage("lsp: no " + what + " for " + msg.word)
		}
		return
	case !msg.refs && len(msg.locs) == 1:
		m.gotoLocation(msg.locs[0].PathFor(), msg.locs[0].Range.Start)
		return
	}
	title := "definitions of " + msg.word
	if msg.refs {
		title = "references to " + msg.word
	}
	hits := make([]search.Hit, 0, len(msg.locs))
	for _, l := range msg.locs {
		p := l.PathFor()
		text := m.lineOf(p, l.Range.Start.Line)
		col := len(string([]rune(text)[:lsp.RuneCol(text, l.Range.Start.Character)])) // byte column, as rg reports
		hits = append(hits, search.Hit{Path: p, Line: l.Range.Start.Line + 1, Column: col, Text: text})
	}
	m.showList(title, hits)
}

// gotoLocation opens path at an LSP position.
func (m *Model) gotoLocation(path string, pos lsp.Position) {
	if m.ws == nil {
		return
	}
	m.open(&node{kind: nodeFile, path: path, name: filepath.Base(path)})
	if e := m.active(); e != nil && e.Path() == path {
		e.Buffer().SetCursor(textbuf.Pos{Line: pos.Line, Col: lsp.RuneCol(e.Buffer().Line(min(pos.Line, e.Buffer().LineCount()-1)), pos.Character)})
		m.mode = modeNav
	}
}

// showList fills the results pane with hits that are not a text search
// (references, problems): enter jumps, there is nothing to replace.
func (m *Model) showList(title string, hits []search.Hit) {
	m.cancelSearch()
	m.hits = nil
	m.hitList = kit.List{Width: m.hitList.Width, Height: m.hitList.Height}
	m.lastQueryText, m.lastQueryRegex, m.resultsTitle = "", false, title
	m.replace, m.replaceNote, m.searchErr = nil, "", ""
	m.searching = false
	for _, h := range hits {
		m.addHit(h)
	}
	m.mode = modeResults
}

// --- diagnostics (F-116) ---

// sortedDiags is the active file's problems in line order.
func (m *Model) sortedDiags(path string) []lsp.Diagnostic {
	ds := m.allDiags(path)
	sort.SliceStable(ds, func(i, j int) bool {
		if ds[i].Line != ds[j].Line {
			return ds[i].Line < ds[j].Line
		}
		return ds[i].Col < ds[j].Col
	})
	return ds
}

// diagAtLine is the most severe problem on line ("" when none).
func (m *Model) diagAtLine(path string, line int) (lsp.Diagnostic, bool) {
	var best lsp.Diagnostic
	found := false
	for _, d := range m.allDiags(path) {
		if d.Line == line && (!found || (d.Severity != 0 && d.Severity < best.Severity)) {
			best, found = d, true
		}
	}
	return best, found
}

// diagLine renders the cursor line's problem for the command line.
func (m *Model) diagLine(e *textbuf.Editor) string {
	d, ok := m.diagAtLine(e.Path(), e.Buffer().Cursor().Line)
	if !ok {
		return ""
	}
	msg := strings.Join(strings.Fields(d.Message), " ")
	switch d.Severity {
	case 1:
		return theme.DangerText().Render(theme.GlyphCross + " " + msg)
	case 2:
		return theme.WarningText().Render("⚠ " + msg)
	}
	return theme.InfoText().Render("ℹ " + msg)
}

// jumpDiag moves to the next (dir 1) or previous (-1) problem, wrapping.
func (m *Model) jumpDiag(dir int) {
	e := m.active()
	if e == nil {
		return
	}
	ds := m.sortedDiags(e.Path())
	if len(ds) == 0 {
		e.SetMessage("no problems in this file")
		return
	}
	cur := e.Buffer().Cursor()
	pick := -1
	if dir > 0 {
		for i, d := range ds {
			if d.Line > cur.Line {
				pick = i
				break
			}
		}
		if pick < 0 {
			pick = 0
		}
	} else {
		for i := len(ds) - 1; i >= 0; i-- {
			if ds[i].Line < cur.Line {
				pick = i
				break
			}
		}
		if pick < 0 {
			pick = len(ds) - 1
		}
	}
	d := ds[pick]
	line := min(d.Line, e.Buffer().LineCount()-1)
	e.Buffer().SetCursor(textbuf.Pos{Line: line, Col: lsp.RuneCol(e.Buffer().Line(line), d.Col)})
	e.SetMessage("")
}

// showProblems lists every problem the servers reported (:diag).
func (m *Model) showProblems() {
	var paths []string
	seen := map[string]bool{}
	for _, set := range []map[string][]lsp.Diagnostic{m.lspDiags, m.taskDiags} {
		for p, ds := range set {
			if len(ds) > 0 && !seen[p] {
				seen[p] = true
				paths = append(paths, p)
			}
		}
	}
	sort.Strings(paths)
	var hits []search.Hit
	for _, p := range paths {
		for _, d := range m.sortedDiags(p) {
			sev := "info"
			switch d.Severity {
			case 1:
				sev = "error"
			case 2:
				sev = "warning"
			}
			hits = append(hits, search.Hit{Path: p, Line: d.Line + 1,
				Text: sev + ": " + strings.Join(strings.Fields(d.Message), " ")})
		}
	}
	if len(hits) == 0 {
		if e := m.active(); e != nil {
			e.SetMessage("no problems reported")
		}
		return
	}
	m.showList("problems ("+kit.Count(len(hits), "item")+")", hits)
}
