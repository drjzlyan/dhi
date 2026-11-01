package editor

import (
	"context"
	"fmt"
	"github.com/drjzlyan/dhi/internal/linediff"
	"os"
	"strings"

	"github.com/drjzlyan/dhi/internal/agentkit/bus"
	"github.com/drjzlyan/dhi/internal/textbuf"
	"github.com/drjzlyan/dhi/internal/tui/kit"
	"github.com/drjzlyan/dhi/internal/tui/theme"
)

// Pair programming (F-038). The agent sees what the human sees through
// the editor_context tool and suggests changes through editor_propose_edit;
// a proposal waits for the human's accept/reject instead of landing
// unreviewed (editor_apply_edit remains the direct, approval-gated path).

// contextRadius is how many lines either side of the cursor Context shows.
const contextRadius = 12

// proposal is one agent-suggested replacement awaiting review.
type proposal struct {
	path string // absolute
	vp   string // display path
	old  string
	new  string
	note string
	from string // proposing agent

	// F-117: per-hunk review. hunks diff old→new; accept holds the
	// decisions made so far (len = hunks decided).
	hunks  []linediff.Hunk
	accept []bool
}

// tabFor returns the open buffer for an absolute path.
func (m *Model) tabFor(abs string) *bufTab {
	for _, t := range m.bufs {
		if t.path == abs {
			return t
		}
	}
	return nil
}

// currentText is the file's content as the human would see it: the live
// buffer when open, else the file on disk.
func (m *Model) currentText(abs string) (string, error) {
	if t := m.tabFor(abs); t != nil {
		return t.ed.Buffer().Text(), nil
	}
	data, err := os.ReadFile(abs)
	if err != nil {
		return "", err
	}
	return string(data), nil
}

// Context describes the human's editing position for an agent: the
// active file, cursor, selection, nearby lines and diagnostics. All
// positions are 0-based, matching the lsp_* tools.
func (m *Model) Context() (string, error) {
	e := m.active()
	if e == nil {
		return "", fmt.Errorf("no buffer is open in the editor")
	}
	t := m.bufs[m.activeTab]
	b := e.Buffer()
	cur := b.Cursor()
	var sb strings.Builder
	fmt.Fprintf(&sb, "file: %s\ncursor: line %d col %d (0-based)\nmode: %s\nlines: %d\n",
		t.vp, cur.Line, cur.Col, modeName(e.Mode()), b.LineCount())
	if sel, from, to, ok := e.Selection(); ok {
		fmt.Fprintf(&sb, "selection: line %d col %d to line %d col %d\n<<<\n%s\n>>>\n",
			from.Line, from.Col, to.Line, to.Col, sel)
	}
	lo, hi := cur.Line-contextRadius, cur.Line+contextRadius
	if lo < 0 {
		lo = 0
	}
	if hi >= b.LineCount() {
		hi = b.LineCount() - 1
	}
	fmt.Fprintf(&sb, "lines %d-%d:\n", lo, hi)
	for l := lo; l <= hi; l++ {
		marker := " "
		if l == cur.Line {
			marker = ">"
		}
		fmt.Fprintf(&sb, "%s%4d| %s\n", marker, l, b.Line(l))
	}
	var near []string
	for _, d := range m.lspDiags[t.path] {
		if d.Line >= lo && d.Line <= hi {
			near = append(near, fmt.Sprintf("  line %d: %s (severity %d)", d.Line, d.Message, d.Severity))
		}
	}
	if len(near) > 0 {
		sb.WriteString("diagnostics near cursor:\n" + strings.Join(near, "\n") + "\n")
	}
	if n := len(m.proposals); n > 0 {
		fmt.Fprintf(&sb, "pending proposals awaiting the human: %d\n", n)
	}
	return sb.String(), nil
}

func modeName(md textbuf.Mode) string {
	switch md {
	case textbuf.ModeInsert:
		return "insert"
	case textbuf.ModeVisual:
		return "visual"
	}
	return "normal"
}

// Propose queues an agent suggestion for review. It validates against
// the content the human has now: the old text must occur exactly once,
// so an accepted proposal can never land ambiguously.
func (m *Model) Propose(abs, old, new, note, from string) error {
	if old == "" {
		return fmt.Errorf("old text is required")
	}
	if old == new {
		return fmt.Errorf("proposal changes nothing")
	}
	cur, err := m.currentText(abs)
	if err != nil {
		return err
	}
	switch n := strings.Count(cur, old); {
	case n == 0:
		return fmt.Errorf("old text not found in %s", abs)
	case n > 1:
		return fmt.Errorf("old text appears %d times in %s; include more surrounding lines", n, abs)
	}
	for _, p := range m.proposals {
		if p.path == abs && p.old == old && p.new == new {
			return fmt.Errorf("an identical proposal is already pending")
		}
	}
	vp := abs
	if v, verr := m.ws.VPathFor(abs); verr == nil {
		vp = v.String()
	}
	hs, _ := linediff.Hunks(strings.Split(old, "\n"), strings.Split(new, "\n"))
	m.proposals = append(m.proposals, proposal{path: abs, vp: vp, old: old, new: new, note: note, from: from, hunks: hs})
	// F-117: never take the keyboard mid-typing; the buffer chip and a
	// toast say it is waiting, ctrl+y opens it.
	who := from
	if who == "" {
		who = "an agent"
	} else {
		who = "@" + who
	}
	m.toast = &kit.Toast{Kind: kit.ToastInfo, Title: who + " suggests a change", Detail: vp + " · ctrl+y to review"}
	return nil
}

// ProposalCount reports pending proposals (tests and status).
func (m *Model) ProposalCount() int { return len(m.proposals) }

// handleReviewKey drives the proposal review overlay; it owns every key
// while open so a stray keystroke cannot edit the buffer underneath. y/n
// decide the current hunk (F-117); A and N decide the rest at once.
func (m *Model) handleReviewKey(key string) bool {
	if len(m.proposals) == 0 {
		m.reviewOpen = false
		return false
	}
	p := &m.proposals[0]
	switch key {
	case "y", "enter":
		p.accept = append(p.accept, true)
	case "n", "x":
		p.accept = append(p.accept, false)
	case "A": // keep everything: this suggestion and every queued one
		for len(m.proposals) > 0 {
			q := &m.proposals[0]
			for len(q.accept) < len(q.hunks) {
				q.accept = append(q.accept, true)
			}
			m.finishProposal()
		}
		m.reviewOpen = false
		return true
	case "N": // drop the rest of this suggestion
		for len(p.accept) < len(p.hunks) {
			p.accept = append(p.accept, false)
		}
	case "esc", "q":
		m.reviewOpen = false // decide later; ctrl+y reopens
		return true
	default:
		return true
	}
	if len(p.accept) >= len(p.hunks) {
		m.finishProposal()
	}
	if len(m.proposals) == 0 {
		m.reviewOpen = false
	}
	return true
}

// finishProposal applies the accepted hunks of the first proposal (one
// undo step) and tells the agent what was kept.
func (m *Model) finishProposal() {
	p := m.proposals[0]
	kept := 0
	for _, a := range p.accept {
		if a {
			kept++
		}
	}
	if kept == 0 {
		m.rejectProposal(0, "rejected")
		return
	}
	text := strings.Join(linediff.Apply(strings.Split(p.old, "\n"), p.hunks, p.accept), "\n")
	if err := m.ApplyReplace(p.path, p.old, text, false); err != nil {
		m.rejectProposal(0, "stale: "+err.Error())
		return
	}
	m.proposals = m.proposals[1:]
	m.pairNote = "applied " + p.vp + " (undo with u)"
	if kept < len(p.hunks) {
		m.pairNote = fmt.Sprintf("applied %d of %d changes to %s (undo with u)", kept, len(p.hunks), p.vp)
		m.tellAgent(p, fmt.Sprintf("(editor) kept %d of %d changes of your proposal for %s; the others were rejected", kept, len(p.hunks), p.vp))
	}
	m.lspSync()
}

func (m *Model) rejectProposal(i int, why string) {
	p := m.proposals[i]
	m.proposals = append(m.proposals[:i], m.proposals[i+1:]...)
	m.pairNote = why + ": " + p.vp
	m.tellAgent(p, fmt.Sprintf("(editor) proposal for %s %s", p.vp, why))
}

// tellAgent DMs the proposing agent and wakes it, so it can adjust
// (F-117: the rejection used to be posted without dispatch).
func (m *Model) tellAgent(p proposal, text string) {
	if m.chat == nil || m.chat.bus == nil || p.from == "" {
		return
	}
	posted, err := m.chat.bus.Post(bus.Message{Channel: "dm:" + p.from, Author: bus.Human, Text: text})
	if err != nil {
		m.pairNote += " (could not tell " + p.from + ": " + err.Error() + ")"
		return
	}
	if m.chat.rt != nil {
		m.chat.rt.Handle(context.Background(), posted)
	}
}

// reviewView renders the first pending proposal's current hunk with
// context, as a real diff.
func (m *Model) reviewView() string {
	p := m.proposals[0]
	who := p.from
	if who == "" {
		who = "agent"
	}
	cur := min(len(p.accept), max(len(p.hunks)-1, 0))
	body := []string{theme.Brand().Render(fmt.Sprintf("%s suggests a change", who)) +
		theme.TextDim().Render(fmt.Sprintf("  (%d of %d · change %d of %d)", 1, len(m.proposals), cur+1, len(p.hunks))),
		theme.TextDim().Render(p.vp)}
	if p.note != "" {
		body = append(body, "")
		body = append(body, kit.WrapWords(p.note, 70)...)
	}
	body = append(body, "")
	old := strings.Split(p.old, "\n")
	if len(p.hunks) > 0 {
		h := p.hunks[cur]
		for _, l := range old[max(h.OldAt-2, 0):h.OldAt] {
			body = append(body, theme.TextDim().Render("  "+truncateRunes(l, 72)))
		}
		for _, l := range h.Old {
			body = append(body, theme.DelWash().Render("- "+truncateRunes(l, 72)))
		}
		for _, l := range h.New {
			body = append(body, theme.AddWash().Render("+ "+truncateRunes(l, 72)))
		}
		end := h.OldAt + len(h.Old)
		for _, l := range old[end:min(end+2, len(old))] {
			body = append(body, theme.TextDim().Render("  "+truncateRunes(l, 72)))
		}
	}
	box := kit.NewPanel("review suggestion", true)
	box.SetContent(body...)
	box.Width = 80
	box.Height = min(len(body)+2, m.height)
	hint := theme.Hint().Render("y accept this change · n drop it · N drop the rest · A accept all · esc later (ctrl+y)")
	return kit.Center(joinV(box.View(), "", hint), m.width, m.height)
}

// pairChip shows pending proposals on the buffer title.
func (m *Model) pairChip(path string) string {
	n := 0
	for _, p := range m.proposals {
		if p.path == path {
			n++
		}
	}
	if n == 0 {
		return ""
	}
	return "  " + theme.AccentText().Render("◆ "+kit.Count(n, "suggestion")+" · ctrl+y")
}

// pairCommand handles the :pair, :unpair and :ask ex commands. It returns
// the status message and whether cmd was a pairing command.
func (m *Model) pairCommand(cmd string) (string, bool) {
	fields := strings.Fields(cmd)
	if len(fields) == 0 {
		return "", false
	}
	switch fields[0] {
	case "pair":
		return m.startPair(fields[1:]), true
	case "unpair":
		if m.pairAgent == "" {
			return "not pairing", true
		}
		who := m.pairAgent
		m.pairAgent = ""
		return "pairing with " + who + " ended", true
	case "ask":
		return m.askPair(strings.TrimSpace(strings.TrimPrefix(cmd, "ask"))), true
	}
	return "", false
}

func (m *Model) startPair(args []string) string {
	if m.chat == nil || m.chat.rt == nil {
		return "pair: no crew is attached to this workspace"
	}
	if m.active() == nil {
		return "pair: open a file first"
	}
	if len(args) != 1 {
		return "usage: :pair <agent> — roster: " + strings.Join(m.chat.agents, ", ")
	}
	id := strings.TrimPrefix(args[0], "@")
	known := false
	for _, a := range m.chat.agents {
		known = known || a == id
	}
	if !known {
		return "pair: unknown agent " + id + " — roster: " + strings.Join(m.chat.agents, ", ")
	}
	m.chat.Focus()
	m.chat.focus = false // keep typing in the buffer; ctrl+a to talk
	if !m.chat.SelectChannel("dm:" + id) {
		return "pair: no DM channel for " + id
	}
	m.pairAgent = id
	t := m.bufs[m.activeTab]
	m.chat.postHuman(fmt.Sprintf("Let's pair on %s. Call editor_context to see my cursor and selection, "+
		"and send changes with editor_propose_edit so I can accept or reject each one.", t.vp))
	return "pairing with " + id + " — :ask <question> sends your selection, :unpair ends"
}

// askPair sends a question about the current selection (or the cursor
// line) to the pair partner, inlining the code so the answer needs no
// tool round trip.
func (m *Model) askPair(question string) string {
	if m.pairAgent == "" {
		return "ask: start a session first with :pair <agent>"
	}
	if question == "" {
		return "usage: :ask <question>"
	}
	e := m.active()
	if e == nil {
		return "ask: open a file first"
	}
	t := m.bufs[m.activeTab]
	code, where := "", ""
	if sel, ok := e.TakeSelection(); ok {
		code, where = sel.Text, fmt.Sprintf("lines %d-%d", sel.From.Line, sel.To.Line)
	} else {
		l := e.Buffer().Cursor().Line
		code, where = e.Buffer().Line(l), fmt.Sprintf("line %d", l)
	}
	if !m.chat.SelectChannel("dm:" + m.pairAgent) {
		return "ask: pair partner is no longer on the roster"
	}
	m.chat.postHuman(fmt.Sprintf("%s\n\n%s (%s, 0-based):\n```\n%s\n```", question, t.vp, where, code))
	return "asked " + m.pairAgent
}

// pairBadge shows the active pairing session on the buffer title.
func (m *Model) pairBadge() string {
	if m.pairAgent == "" {
		return ""
	}
	return "  " + theme.AccentText().Render("◆ pairing: "+m.pairAgent)
}
