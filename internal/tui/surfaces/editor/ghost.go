package editor

import (
	"context"
	"fmt"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/mattn/go-runewidth"

	"github.com/drjzlyan/dhi/internal/agentkit/runtime"
	"github.com/drjzlyan/dhi/internal/lsp"
	"github.com/drjzlyan/dhi/internal/textbuf"
	"github.com/drjzlyan/dhi/internal/tui/kit"
	"github.com/drjzlyan/dhi/internal/tui/theme"
)

// Ghost text (F-181, ADR-0051): a suggestion drawn dimmed after the cursor
// in insert mode. Like an inlay hint (F-169) it is a virtual cell run: it
// takes room on screen but is not text, so the cursor, clicks, selections
// and soft wrap keep addressing buffer columns only. Two sources:
//
//  1. the language server's inline completion (LSP 3.18), asked after a
//     short pause in typing when the server advertises it; free and passive;
//  2. :suggest (alt+\ in insert mode): ONE explicit, read-only agent call
//     with a bounded prompt, metered as its own ledger line. Never
//     auto-triggered.
//
// Insert-mode tab precedence, first match wins:
//  1. the completion popup is open: tab accepts the selected item;
//  2. a snippet session is live: tab / shift+tab jump between stops;
//  3. ghost text is showing: tab accepts it;
//  4. otherwise tab indents.
//
// Ghost text shows where the cursor is at the end of its line (a
// suggestion between the cursor and following text would collide with the
// cursor block). esc drops it; typing the suggestion's next character
// walks through it, any other edit drops it.

// ghostDebounce is how long typing must pause before the server is asked.
var ghostDebounce = 250 * time.Millisecond

// suggestTickEvery is the spinner's frame time while :suggest waits.
var suggestTickEvery = 150 * time.Millisecond

// ghostState is the suggestion on offer, valid only for the buffer
// version and cursor it was made for.
type ghostState struct {
	ed       *textbuf.Editor
	pos      textbuf.Pos // the insert point (the cursor)
	from     textbuf.Pos // where a snippet starts replacing: before pos when it re-types a typed prefix
	text     string      // what tab inserts after pos (a snippet: its expansion minus the typed prefix)
	snip     string      // snippet source ("" = plain text)
	seq      uint64      // buffer version
	fromUser bool        // from :suggest (the agent), not the language server
}

// suggestState is a :suggest call in flight.
type suggestState struct {
	ed     *textbuf.Editor
	pos    textbuf.Pos
	seq    uint64
	gen    int
	agent  string
	frame  int
	cancel context.CancelFunc
}

// suggestRun is the one call :suggest makes; production sends it to the
// default agent's runtime, tests inject a stand-in.
type suggestRun func(ctx context.Context, agent string, q runtime.SuggestRequest) (string, error)

// suggestHook replaces the default agent's runtime (tests).
type suggestHook struct {
	agent string
	run   suggestRun
}

// ghostEnabled is the effective setting: a session :set ghost|noghost
// beats editor.inline_completion.
func (m *Model) ghostEnabled(p Prefs) bool {
	if m.ghostOverride != nil {
		return *m.ghostOverride
	}
	return p.InlineCompletion
}

// ghostCommand handles :set ghost, :set noghost (this session only; the
// setting lives in Settings) and :suggest.
func (m *Model) ghostCommand(cmd string) (string, bool) {
	switch strings.TrimSpace(cmd) {
	case "set ghost":
		on := true
		m.ghostOverride = &on
		if c := m.lspClient(); c != nil && !c.CanInlineCompletion() {
			return "ghost text: on, but " + c.ServerName() + " does not provide inline completion (:suggest still works)", true
		}
		return "ghost text: on", true
	case "set noghost":
		off := false
		m.ghostOverride = &off
		m.ghost = nil
		return "ghost text: off (:suggest still works)", true
	case "suggest":
		return m.startSuggest(), true
	}
	return "", false
}

// ghostFor is the suggestion to show on e, or nil: only in insert mode
// with a single cursor at the end of its line, when nothing else owns the
// keys (completion popup, snippet session) and the text is the version
// the suggestion was made for.
func (m *Model) ghostFor(e *textbuf.Editor) *ghostState {
	g := m.ghost
	if g == nil || g.ed != e || !m.ghostRoom(e) {
		return nil
	}
	b := e.Buffer()
	if b.Cursor() != g.pos || b.Seq() != g.seq {
		return nil
	}
	return g
}

// ghostRoom reports whether e is in a state where ghost text may show.
func (m *Model) ghostRoom(e *textbuf.Editor) bool {
	if e == nil || e.Mode() != textbuf.ModeInsert || e.CursorCount() > 1 || m.snip != nil || m.compOpen {
		return false
	}
	cur := e.Buffer().Cursor()
	return cur.Col == len([]rune(e.Buffer().Line(cur.Line)))
}

// ghostCell is the virtual cell run for line l: the suggestion, or the
// spinner while :suggest waits. It sits after the cursor block, so the
// cursor and the text before it do not move.
func (m *Model) ghostCell(e *textbuf.Editor, l int) []inlayCell {
	if m.ghost == nil && m.sug == nil {
		return nil
	}
	if e == nil || !m.ghostRoom(e) || e.Buffer().Cursor().Line != l {
		return nil
	}
	b := e.Buffer()
	var text string
	var w int
	if g := m.ghostFor(e); g != nil {
		label := ghostLabel(g.text)
		text, w = theme.GhostText().Render(label), runewidth.StringWidth(label)
	} else if s := m.sug; s != nil && s.ed == e && s.pos == b.Cursor() && s.seq == b.Seq() {
		label := " suggesting with @" + s.agent
		text = kit.SpinnerGlyph(s.frame) + theme.GhostText().Render(label)
		w = 1 + runewidth.StringWidth(label)
	} else {
		return nil
	}
	rs := []rune(expandTabs(b.Line(l)))
	// idx is one past the last rune and cell one past the cursor block, so
	// every cursor computation (they count hints at or before the cursor's
	// rune) leaves the cursor in front of the ghost.
	return []inlayCell{{idx: len(rs) + 1, cell: runesWidth(rs) + 1, w: w, text: text}}
}

// ghostLabel is what is drawn for a suggestion: its first line, with a
// glyph when more lines follow (they are inserted, not shown).
func ghostLabel(text string) string {
	first, rest, more := strings.Cut(text, "\n")
	label := expandTabs(first)
	if more && rest != "" {
		label += " " + theme.GlyphGhostMore
	}
	return label
}

// ghostEsc runs when esc is pressed in the buffer, before the key goes on:
// the suggestion goes away and a pending :suggest is cancelled.
func (m *Model) ghostEsc() {
	m.ghost = nil
	if m.sug != nil {
		m.cancelSuggest()
		if e := m.active(); e != nil {
			e.SetMessage("suggest: cancelled")
		}
	}
}

// ghostKey runs before the buffer sees a key (after the completion popup
// and the snippet session had theirs). It reports the key consumed: tab
// accepts the showing suggestion, alt+\ asks the agent for one.
func (m *Model) ghostKey(e *textbuf.Editor, key string) bool {
	switch {
	case key == "tab" && m.ghostFor(e) != nil:
		m.acceptGhost(e)
		return true
	case key == `alt+\`:
		e.SetMessage(m.startSuggest())
		return true
	}
	return false
}

// acceptGhost inserts the suggestion at the cursor (one undo step).
func (m *Model) acceptGhost(e *textbuf.Editor) {
	g := m.ghost
	m.ghost = nil
	if g.snip != "" {
		m.expandSnippet(e, g.from, g.pos, g.snip)
	} else {
		e.Buffer().ApplyEdit(g.pos, g.pos, g.text)
	}
	e.SetMessage("")
	m.lspSync()
}

// ghostAfterKey runs after the buffer handled a key. Typing the
// suggestion's next character walks through it; any other change drops it
// and, in insert mode, schedules a fresh server request.
func (m *Model) ghostAfterKey(e *textbuf.Editor, key string) {
	b := e.Buffer()
	if s := m.sug; s != nil && s.ed == e && (b.Seq() != s.seq || (e.Mode() == textbuf.ModeInsert && b.Cursor() != s.pos)) {
		m.cancelSuggest() // typing past a pending suggestion: its position is gone
	}
	if g := m.ghost; g != nil && g.ed == e {
		if b.Cursor() == g.pos && b.Seq() == g.seq {
			return // the key changed nothing
		}
		if rest, ok := typedPast(g, e, key); ok {
			g.text, g.pos, g.seq = rest, b.Cursor(), b.Seq()
			return
		}
		m.ghost = nil
	}
	if e.Mode() == textbuf.ModeInsert {
		m.ghostSchedule(e)
	}
}

// typedPast reports the rest of a plain suggestion after key typed its
// next character at the insert point.
func typedPast(g *ghostState, e *textbuf.Editor, key string) (string, bool) {
	if g.snip != "" || e.Mode() != textbuf.ModeInsert {
		return "", false
	}
	if key == "space" {
		key = " "
	}
	r, size := utf8.DecodeRuneInString(key)
	if size != len(key) || r < ' ' {
		return "", false
	}
	b := e.Buffer()
	if b.Cursor() != (textbuf.Pos{Line: g.pos.Line, Col: g.pos.Col + 1}) {
		return "", false
	}
	want, wsize := utf8.DecodeRuneInString(g.text)
	if want != r || wsize == len(g.text) {
		return "", false // not the next character, or nothing would be left
	}
	return g.text[wsize:], true
}

// ghostSchedule asks the server for inline completion once typing pauses.
// The request goes out from the tick (ghostDue), off the UI goroutine.
func (m *Model) ghostSchedule(e *textbuf.Editor) {
	if m.termMsgs == nil || e.Path() == "" || !m.ghostEnabled(m.editorPrefs()) {
		return
	}
	c := m.clientFor(e.Path())
	if c == nil || !c.CanInlineCompletion() {
		return
	}
	m.ghostSeq++
	seq, bufSeq, ch, wait := m.ghostSeq, e.Buffer().Seq(), m.termMsgs, ghostDebounce
	go func() {
		time.Sleep(wait)
		select {
		case ch <- teaMsg{kind: lspMsgGhostDue, sigSeq: seq, bufSeq: bufSeq, note: e.Path()}:
		default:
		}
	}()
}

// applyGhostMsg folds the async steps of both sources.
func (m *Model) applyGhostMsg(msg teaMsg) {
	e := m.active()
	switch msg.kind {
	case lspMsgGhostDue:
		if msg.sigSeq != m.ghostSeq || e == nil || e.Path() != msg.note || e.Mode() != textbuf.ModeInsert ||
			e.Buffer().Seq() != msg.bufSeq || !m.ghostEnabled(m.editorPrefs()) {
			return // typing went on
		}
		c := m.clientFor(msg.note)
		if c == nil {
			return
		}
		m.lspSync() // the server must see the text the position refers to
		cur := e.Buffer().Cursor()
		col := u16(e, cur.Line, cur.Col)
		path, ch := msg.note, m.termMsgs
		go func() {
			items, err := c.InlineCompletion(path, cur.Line, col)
			out := teaMsg{kind: lspMsgGhost, sigSeq: msg.sigSeq, bufSeq: msg.bufSeq, note: path, ghostPos: cur,
				inline: items, hoverOK: err == nil}
			if err != nil {
				out.hoverText = err.Error()
			}
			select {
			case ch <- out:
			default:
			}
		}()
	case lspMsgGhost:
		if msg.sigSeq != m.ghostSeq || e == nil || e.Path() != msg.note || e.Buffer().Seq() != msg.bufSeq ||
			e.Buffer().Cursor() != msg.ghostPos || e.Mode() != textbuf.ModeInsert {
			return
		}
		if !msg.hoverOK {
			if !m.ghostErrNoted {
				m.ghostErrNoted = true
				e.SetMessage("lsp: inline completion: " + msg.hoverText)
			}
			return
		}
		m.ghostErrNoted = false
		for _, it := range msg.inline {
			if g := m.ghostFromItem(e, msg.ghostPos, it); g != nil {
				m.ghost = g
				return
			}
		}
	case sugMsgTick:
		if s := m.sug; s != nil && s.gen == msg.sigSeq {
			s.frame++
		}
	case sugMsgDone:
		m.onSuggestDone(msg)
	}
}

// ghostFromItem turns a server proposal into ghost text for the cursor at
// pos, or nil when it cannot be shown there. A proposal may re-type the
// part of the word already typed (its range ends at the cursor); the
// ghost is what follows that part.
func (m *Model) ghostFromItem(e *textbuf.Editor, pos textbuf.Pos, it lsp.InlineItem) *ghostState {
	b := e.Buffer()
	raw := b.Line(pos.Line)
	line := []rune(raw)
	from := pos
	if r := it.Range; r != nil {
		if r.Start.Line != pos.Line || r.End.Line != pos.Line {
			return nil
		}
		s, en := lsp.RuneCol(raw, r.Start.Character), lsp.RuneCol(raw, r.End.Character)
		if en != pos.Col || s > pos.Col {
			return nil
		}
		from.Col = s
	}
	shown := it.Text
	if it.Snippet {
		indent := 0
		for indent < len(line) && (line[indent] == ' ' || line[indent] == '\t') {
			indent++
		}
		shown = textbuf.ExpandSnippet(it.Text, m.snippetVars(e, from), string(line[:min(indent, from.Col)])).Text
	}
	typed := string(line[from.Col:pos.Col])
	rest, ok := strings.CutPrefix(shown, typed)
	if !ok || rest == "" {
		return nil
	}
	g := &ghostState{ed: e, pos: pos, from: from, text: rest, seq: b.Seq()}
	if it.Snippet {
		g.snip = it.Text
	}
	return g
}

// ---- :suggest ----

// suggestEngine is the agent that answers :suggest and how to call it.
func (m *Model) suggestEngine() (string, suggestRun, string) {
	if m.suggestHook != nil {
		return m.suggestHook.agent, m.suggestHook.run, ""
	}
	if m.chat == nil || m.chat.rt == nil {
		return "", nil, "no agents yet — ctrl+a sets up a team"
	}
	id := m.chat.rt.DefaultAgent()
	if id == "" {
		return "", nil, "no agents yet — ctrl+a sets up a team"
	}
	return id, m.chat.rt.Suggest, ""
}

// suggestPoint is where :suggest inserts: the cursor in insert mode, the
// spot `a` would append at in normal mode.
func suggestPoint(e *textbuf.Editor) (textbuf.Pos, bool) {
	cur := e.Buffer().Cursor()
	n := len([]rune(e.Buffer().Line(cur.Line)))
	switch e.Mode() {
	case textbuf.ModeInsert:
		return cur, true
	case textbuf.ModeNormal:
		cur.Col = min(cur.Col+1, n) // normal mode may rest one past the last rune ($)
		return cur, true
	}
	return cur, false
}

// startSuggest sends the one read-only call and returns the status line.
func (m *Model) startSuggest() string {
	e := m.active()
	if e == nil {
		return "open a file first"
	}
	id, run, why := m.suggestEngine()
	if id == "" {
		return why
	}
	if m.sug != nil {
		return "suggest: already asking @" + m.sug.agent + " (esc cancels)"
	}
	pos, ok := suggestPoint(e)
	if !ok || e.CursorCount() > 1 {
		return "suggest: use it from insert or normal mode with one cursor"
	}
	b := e.Buffer()
	if pos.Col != len([]rune(b.Line(pos.Line))) {
		return "suggest: put the cursor at the end of a line (the suggestion shows there)"
	}
	lines := b.Lines()
	lo, hi := max(pos.Line-suggestLinesBefore, 0), min(pos.Line+suggestLinesAfter, len(lines)-1)
	q := runtime.SuggestRequest{Path: m.agentPath(e.Path())}
	if l, _, ok := m.langs.For(e.Path()); ok {
		q.Language = l.ID
	}
	before := append(append([]string{}, lines[lo:pos.Line]...), string([]rune(lines[pos.Line])[:pos.Col]))
	q.Before = strings.Join(before, "\n")
	q.After = strings.Join(append([]string{string([]rune(lines[pos.Line])[pos.Col:])}, lines[pos.Line+1:hi+1]...), "\n")

	ctx, cancel := context.WithCancel(context.Background())
	m.sugGen++
	gen := m.sugGen
	m.sug = &suggestState{ed: e, pos: pos, seq: b.Seq(), gen: gen, agent: id, cancel: cancel}
	path, ch, every := e.Path(), m.termMsgs, suggestTickEvery
	if ch == nil {
		cancel()
		m.sug = nil
		return "suggest: not ready"
	}
	go func() {
		text, err := run(ctx, id, q)
		out := teaMsg{kind: sugMsgDone, sigSeq: gen, note: path, hoverText: text, hoverOK: err == nil}
		if err != nil {
			out.hoverText = err.Error()
		}
		select { // the call was paid for: wait for room rather than drop it
		case ch <- out:
		case <-time.After(10 * time.Second):
		}
	}()
	go func() {
		t := time.NewTicker(every)
		defer t.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-t.C:
				select {
				case ch <- teaMsg{kind: sugMsgTick, sigSeq: gen}:
				default:
				}
			}
		}
	}()
	return "suggest: asking @" + id + "…  (esc cancels)"
}

// Lines of the file sent around the cursor; the runtime bounds the text.
const (
	suggestLinesBefore = 80
	suggestLinesAfter  = 30
)

// cancelSuggest stops a pending call.
func (m *Model) cancelSuggest() {
	if m.sug != nil {
		m.sug.cancel()
		m.sug = nil
	}
}

// onSuggestDone puts the answer up as ghost text, if it still fits where
// it was asked.
func (m *Model) onSuggestDone(msg teaMsg) {
	s := m.sug
	if s == nil || s.gen != msg.sigSeq {
		return // cancelled or overtaken
	}
	m.cancelSuggest()
	e := m.active()
	if e == nil || e != s.ed {
		return
	}
	if !msg.hoverOK {
		e.SetMessage("suggest: " + msg.hoverText)
		return
	}
	text := msg.hoverText
	if strings.TrimSpace(text) == "" {
		e.SetMessage("suggest: nothing to add here")
		return
	}
	b := e.Buffer()
	if at, ok := suggestPoint(e); !ok || at != s.pos || b.Seq() != s.seq {
		e.SetMessage("suggest: dropped, the text or cursor moved while it was thinking")
		return
	}
	if e.Mode() == textbuf.ModeNormal {
		e.Key("a") // the ghost shows in insert mode, at the point `a` appends
		if b.Cursor() != s.pos || e.Mode() != textbuf.ModeInsert {
			e.SetMessage("suggest: dropped, could not place the cursor")
			return
		}
	}
	m.ghost = &ghostState{ed: e, pos: s.pos, from: s.pos, text: text, seq: b.Seq(), fromUser: true}
	e.SetMessage(fmt.Sprintf("suggest from @%s: tab accepts · esc drops", s.agent))
}
