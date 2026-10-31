package editor

import (
	"fmt"
	"github.com/drjzlyan/dhi/internal/textbuf"
	"strings"

	"github.com/drjzlyan/dhi/internal/ansi"
	"github.com/drjzlyan/dhi/internal/testrun"
	"github.com/drjzlyan/dhi/internal/tui/kit"
)

// Asking an agent from where you are (F-120): change a selection (ctrl+k
// or :change), fix the problem on this line (:fix), a failing test (f in
// the test list), or what the terminal just printed (palette).

// helper is who answers these asks: the pair partner, else the default
// agent.
func (m *Model) helper() (string, string) {
	if m.chat == nil || m.chat.rt == nil {
		return "", "no agents yet — ctrl+a sets up a team"
	}
	if m.pairAgent != "" {
		return m.pairAgent, ""
	}
	if id := m.chat.rt.DefaultAgent(); id != "" {
		return id, ""
	}
	return "", "no agents yet — ctrl+a sets up a team"
}

// askHelper posts text to the helper's DM, shows the sidebar without
// taking the keyboard, and names who is on it.
func (m *Model) askHelper(text string) string {
	id, why := m.helper()
	if id == "" {
		return why
	}
	m.chat.Focus()
	m.chat.focus = false // the sidebar shows the answer; the keyboard stays in the buffer
	if !m.chat.SelectChannel("dm:" + id) {
		return "no DM channel for " + id
	}
	m.chat.postHuman(text)
	return "@" + id + " is on it — the answer shows in the chat (ctrl+a)"
}

// region is the text a change applies to: the selection, else lines
// around the cursor (a little context keeps the old text unique).
type region struct {
	text     string
	from, to int // 0-based lines
}

func (m *Model) currentRegion(around int) (region, bool) {
	e := m.active()
	if e == nil {
		return region{}, false
	}
	if sel, from, to, ok := e.Selection(); ok && sel != "" {
		return region{text: sel, from: from.Line, to: to.Line}, true
	}
	b := e.Buffer()
	l := b.Cursor().Line
	lo, hi := max(l-around, 0), min(l+around, b.LineCount()-1)
	lines := b.Lines()[lo : hi+1]
	return region{text: strings.Join(lines, "\n"), from: lo, to: hi}, true
}

// agentChange asks the helper to rewrite a region; the answer comes back
// as a suggestion reviewed hunk by hunk (F-117), never a silent edit.
func (m *Model) agentChange(instruction string, r region) string {
	e := m.active()
	if e == nil {
		return "open a file first"
	}
	if strings.TrimSpace(instruction) == "" {
		return "say what to change"
	}
	p := m.agentPath(e.Path())
	if e.Mode() != textbuf.ModeNormal {
		e.Key("esc")
	}
	return m.askHelper(fmt.Sprintf("%s\n\nIn `%s`, lines %d-%d:\n```%s\n%s\n```\n\n"+
		"Reply by calling editor_propose_edit for file %s with `old` set to exactly the code above and `new` "+
		"set to your version — I review it as a diff. Do not edit the file yourself.",
		instruction, p, r.from+1, r.to+1, fenceLang(p), r.text, e.Path()))
}

// fixProblem asks the helper to fix the problem on the cursor line.
func (m *Model) fixProblem() string {
	e := m.active()
	if e == nil {
		return "open a file first"
	}
	d, ok := m.diagAtLine(e.Path(), e.Buffer().Cursor().Line)
	if !ok {
		return "no problem on this line (]d goes to the next one)"
	}
	r, _ := m.currentRegion(3)
	return m.agentChange("Fix this problem: "+strings.Join(strings.Fields(d.Message), " "), r)
}

// fixTest asks the helper to make a failing test pass.
func (m *Model) fixTest(f testrun.Failure) string {
	name := f.Test
	if name == "" {
		name = "the build"
	}
	out := f.Output
	if len(out) > 60 {
		out = out[len(out)-60:]
	}
	where := ""
	if f.File != "" {
		where = fmt.Sprintf(" (%s:%d)", m.agentPath(f.File), f.Line)
	}
	return m.askHelper(fmt.Sprintf("%s in package %s fails%s:\n```\n%s\n```\n\n"+
		"Find the cause and fix it — the code, or the test if the test is wrong — then run the test to confirm.",
		name, f.Package, where, strings.Join(out, "\n")))
}

// askAboutTerminal sends the terminal's visible output to the helper.
func (m *Model) askAboutTerminal() string {
	t := m.activeTermTab()
	if t == nil || t.screen == nil {
		return "open the terminal first (ctrl+t)"
	}
	w, h := t.screen.Size()
	var lines []string
	for _, l := range t.screen.Lines(max(w, 80), max(h, 24)) {
		lines = append(lines, strings.TrimRight(ansi.Strip(l), " "))
	}
	for len(lines) > 0 && lines[len(lines)-1] == "" {
		lines = lines[:len(lines)-1]
	}
	if len(lines) == 0 {
		return "the terminal is empty"
	}
	return m.askHelper(fmt.Sprintf("Here is my terminal (in `%s`):\n```\n%s\n```\n\nWhat went wrong, and how do I fix it?",
		m.agentPath(t.dir), strings.Join(lines, "\n")))
}

// startAgentEdit opens the ctrl+k prompt for the selection or cursor
// region.
func (m *Model) startAgentEdit() bool {
	r, ok := m.currentRegion(0)
	if !ok {
		return false
	}
	if id, why := m.helper(); id == "" {
		m.active().SetMessage(why)
		return true
	}
	m.treeOp = &treeOp{kind: opAgentEdit, input: kit.NewInput("", "e.g. handle the error, rename x to count"), region: r}
	return true
}
