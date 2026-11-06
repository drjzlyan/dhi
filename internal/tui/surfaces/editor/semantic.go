package editor

import (
	"strings"
	"time"

	"charm.land/lipgloss/v2"

	"github.com/drjzlyan/dhi/internal/ansi"
	"github.com/drjzlyan/dhi/internal/lsp"
	"github.com/drjzlyan/dhi/internal/tui/syntax"
	"github.com/drjzlyan/dhi/internal/tui/theme"
)

// Semantic tokens (F-173): with editor.semantic_tokens on, the language
// server's textDocument/semanticTokens/full is layered over the syntax
// highlighting, only for the token types the theme maps. The request is
// made off the UI goroutine, debounced after edits, and re-asked whenever
// the buffer changed. Tokens are kept per line together with the line text
// they were computed for, so an edit never paints a stale colour onto a
// changed line while the next answer is on its way.

const semDebounce = 300 * time.Millisecond

// semTok is one coloured span of a line, in rune columns.
type semTok struct {
	start, end int
	st         lipgloss.Style
}

// semLine is the tokens of one line and the text they describe.
type semLine struct {
	text string
	toks []semTok
}

// semState is one tab's semantic tokens.
type semState struct {
	lines map[int]semLine
	asked bool   // a request is scheduled or done for seq
	seq   uint64 // the buffer sequence it was scheduled for
}

// semMsg carries a debounce tick or a finished answer to the UI goroutine.
type semMsg struct {
	path  string
	seq   uint64
	lines []string // the buffer text the answer is for (done only)
	toks  []lsp.SemanticToken
	err   error
}

// semanticEnabled is the effective setting: a session :set semantic |
// nosemantic beats editor.semantic_tokens.
func (m *Model) semanticEnabled(p Prefs) bool {
	if m.semOverride != nil {
		return *m.semOverride
	}
	return p.SemanticTokens
}

// semanticCommand handles :set semantic and :set nosemantic (this session only).
func (m *Model) semanticCommand(cmd string) (string, bool) {
	var on bool
	switch strings.TrimSpace(cmd) {
	case "set semantic":
		on = true
	case "set nosemantic":
		on = false
	default:
		return "", false
	}
	m.semOverride = &on
	if on {
		return "semantic tokens: on", true
	}
	return "semantic tokens: off", true
}

// semanticStyle maps a token onto the theme; ok is false for the types the
// theme leaves to the syntax highlighting.
func semanticStyle(t lsp.SemanticToken) (lipgloss.Style, bool) {
	switch t.Type {
	case "namespace":
		return theme.AccentDimText(), true
	case "type", "class", "enum", "interface", "struct", "typeParameter":
		return theme.InfoText(), true
	case "function", "method":
		return theme.AccentText(), true
	case "parameter":
		return theme.WarningText().Italic(true), true
	case "variable":
		if t.Has("readonly") {
			return theme.WarningText(), true
		}
	}
	return lipgloss.Style{}, false
}

// semanticWant schedules a token request for t when its buffer changed
// since the last one. Called from render: it only compares and, at most,
// starts a timer; the work happens in onSemanticTick.
func (m *Model) semanticWant(t *bufTab, prefs Prefs) {
	if !m.semanticEnabled(prefs) || t.ed == nil || m.termMsgs == nil {
		return
	}
	seq := t.ed.Buffer().Seq()
	if t.sem.asked && t.sem.seq == seq {
		return
	}
	c := m.clientFor(t.path)
	if c == nil || !c.CanSemanticTokens() || t.ed.Buffer().LineCount() == 0 || len(t.ed.Buffer().Text()) > syntax.MaxSize {
		return
	}
	t.sem.asked, t.sem.seq = true, seq
	ch, path, wait := m.termMsgs, t.path, semDebounce
	go func() {
		time.Sleep(wait)
		ch <- teaMsg{kind: semMsgTick, sem: &semMsg{path: path, seq: seq}}
	}()
}

// onSemanticTick starts the request unless the buffer moved on (a newer
// tick is then already scheduled).
func (m *Model) onSemanticTick(r *semMsg) {
	t := m.tabAt(r.path)
	if t == nil || t.ed == nil || t.ed.Buffer().Seq() != r.seq {
		return
	}
	c := m.clientFor(t.path)
	if c == nil {
		return
	}
	if t == m.focusedTab() {
		m.lspSync() // the server must hold the text the answer is for
	}
	b := t.ed.Buffer()
	lines := make([]string, b.LineCount())
	for i := range lines {
		lines[i] = b.Line(i)
	}
	ch := m.termMsgs
	go func() {
		toks, err := withTimeout(func() ([]lsp.SemanticToken, error) { return c.SemanticTokens(r.path) })
		ch <- teaMsg{kind: semMsgDone, sem: &semMsg{path: r.path, seq: r.seq, lines: lines, toks: toks, err: err}}
	}()
}

// onSemanticDone files the answer under the tab's lines.
func (m *Model) onSemanticDone(r *semMsg) {
	t := m.tabAt(r.path)
	if t == nil {
		return
	}
	if r.err != nil {
		if t == m.focusedTab() && t.ed != nil {
			t.ed.SetMessage("lsp: semantic tokens: " + r.err.Error())
		}
		return
	}
	lines := map[int]semLine{}
	for _, tok := range r.toks {
		st, ok := semanticStyle(tok)
		if !ok || tok.Line < 0 || tok.Line >= len(r.lines) {
			continue
		}
		text := r.lines[tok.Line]
		s, e := lsp.RuneCol(text, tok.Col), lsp.RuneCol(text, tok.Col+tok.Len)
		if e <= s {
			continue
		}
		sl := lines[tok.Line]
		sl.text = text
		sl.toks = append(sl.toks, semTok{start: s, end: e, st: st})
		lines[tok.Line] = sl
	}
	t.sem.lines = lines
}

// tabAt finds the open tab for path.
func (m *Model) tabAt(path string) *bufTab {
	for _, t := range m.bufs {
		if t.path == path {
			return t
		}
	}
	return nil
}

// semanticLine layers the tab's semantic colours onto line l. text is the
// line as rendered so far (tabs expanded, syntax colours applied), raw the
// buffer line. A line edited since the answer keeps its syntax colours.
func (m *Model) semanticLine(t *bufTab, prefs Prefs, l int, raw, text string) string {
	if !m.semanticEnabled(prefs) {
		return text
	}
	sl, ok := t.sem.lines[l]
	if !ok || sl.text != raw {
		return text
	}
	var sb strings.Builder
	pos := 0
	for _, tk := range sl.toks {
		s, e := visCol(raw, tk.start), visCol(raw, tk.end)
		if s < pos || e <= s {
			continue
		}
		sb.WriteString(ansi.Slice(text, pos, s))
		sb.WriteString(tk.st.Render(ansi.Strip(ansi.Slice(text, s, e))))
		pos = e
	}
	sb.WriteString(ansi.Slice(text, pos, ansi.Width(text)))
	return sb.String()
}
