package ideator

import (
	"fmt"
	"strings"

	"github.com/drjzlyan/dhi/internal/agentkit/bus"
	"github.com/drjzlyan/dhi/internal/ideation"
	"github.com/drjzlyan/dhi/internal/tui/kit"
	"github.com/drjzlyan/dhi/internal/tui/theme"
)

// TRANSCRIPT state lives on the model: composer focus/input plus a scroll
// offset over the ordered session channel. Every message carries its
// bus id so the exchange replays in order; the floor header shows the
// recorded turn state (F-033 Part A).
func (m *Model) chatHistory() []bus.Message {
	sess, ok := m.openSession()
	if !ok || m.bus == nil {
		return nil
	}
	var out []bus.Message
	for _, msg := range m.bus.History(sess.Channel, 0) {
		if bus.Conversational(msg) { // tool lines live in the trace (F-072)
			out = append(out, msg)
		}
	}
	return out
}

// chatKey handles keys while TRANSCRIPT is active and the composer is
// blurred.
func (m *Model) chatKey(key string) bool {
	if m.bus == nil {
		return false
	}
	if m.chatFocus {
		return m.chatComposerKey(key)
	}
	switch key {
	case "j", "down":
		if m.chatScroll > 0 {
			m.chatScroll--
		}
		return true
	case "k", "up":
		m.chatScroll++
		return true
	case "G":
		m.chatScroll = 0
		return true
	case "i", "enter":
		if _, ok := m.openSession(); ok {
			m.chatFocus = true
		}
		return true
	}
	return false
}

func (m *Model) chatComposerKey(key string) bool {
	switch key {
	case "esc":
		m.chatFocus = false
		return true
	case "enter":
		text := strings.TrimSpace(m.chatInput.Value)
		if text != "" {
			m.chatPost(text)
			m.chatInput = kit.Input{}
		}
		return true
	}
	return m.chatInput.HandleKey(key)
}

// chatPost persists the human's message, records the floor hand-off and
// dispatches a turn. The runtime resolves @mentions; in a 1:1 session the
// single participant is invited even without a mention.
func (m *Model) chatPost(text string) {
	sess, ok := m.openSession()
	if !ok || m.bus == nil {
		return
	}
	posted, err := m.bus.Post(bus.Message{
		Channel: sess.Channel,
		Author:  busHuman,
		Text:    text,
	})
	if err != nil {
		m.opErr = "post: " + err.Error()
		return
	}
	m.recordHumanFloor(sess, text)
	m.requestTurn(posted)
	// 1:1: a plain message still reaches the one participant.
	if sess.Mode == ideation.ModeOneOnOne && len(sess.Agents) == 1 {
		mentioned := false
		for _, a := range bus.Mentions(text) {
			if a == sess.Agents[0] {
				mentioned = true
			}
		}
		if !mentioned {
			m.giveFloor(sess.Agents[0])
		}
	}
	m.chatScroll = 0
}

// recordHumanFloor moves the floor to the first mentioned participant, or
// back to the moderator when the human speaks unaddressed.
func (m *Model) recordHumanFloor(sess ideation.Session, text string) {
	next := ""
	for _, a := range bus.Mentions(text) {
		if sess.Participates(a) {
			next = a
			break
		}
	}
	m.grantOnly(next)
}

// chatBody renders the transcript + composer within the cell budget.
func (m *Model) chatBody(w, h int) string {
	sess, ok := m.openSession()
	if !ok {
		return theme.TextDim().Render("(no session open — pick one under SESSIONS)")
	}
	holder := sess.CurrentSpeaker()
	head := theme.Hint().Render("transcript") +
		theme.TextDim().Render(fmt.Sprintf("   mode %s · floor %s · %s",
			sess.Mode, floorName(holder), kit.Count(len(sess.Turns), "turn")))
	out := []string{head}
	if m.bus == nil {
		out = append(out, kit.Notice{Kind: kit.NoticeWarning, What: "No message bus",
			Why:   "the round-table runs over the agent bus, which starts with the agent runtime.",
			Retry: "add agents in Settings › AGENTS, then restart"}.Lines(w, 0)...)
		return strings.Join(out, "\n")
	}
	out = append(out, theme.Brand().Render(sess.Channel))

	// The transcript renders through the shared kit.Transcript (F-026
	// P6): day dividers, stamps, author styles, shared wrap. Each row is
	// tagged with its bus id so the exchange replays in order.
	tr := &kit.Transcript{Width: w - 2, Markdown: kit.ChatMarkdown}
	for _, msg := range m.chatHistory() {
		row := kit.TrnRow{
			Author:   msg.Author,
			Text:     msg.Text,
			Suffix:   theme.TextMuted().Render(fmt.Sprintf("#%d", msg.ID)),
			At:       msg.At,
			Kind:     kit.TrnAgent,
			Markdown: true,
		}
		switch {
		case msg.Author == busHuman:
			row.Kind = kit.TrnHuman
		case msg.Kind == bus.KindAlert:
			row.Kind = kit.TrnAlert
		}
		tr.Rows = append(tr.Rows, row)
	}
	flat := tr.View()
	if len(flat) == 0 {
		flat = append(flat, kit.EmptyInline("No messages yet",
			"Press i to compose, and @mention an invited agent.", w)...)
	}

	body := h - 5 // header, channel, composer hint, input line, blank
	if body < 3 {
		body = 3
	}
	if over := len(flat) - body - m.chatScroll; over > 0 {
		flat = flat[over:]
	} else if m.chatScroll > 0 {
		m.chatScroll = maxInt(0, len(flat)-body)
	}
	for _, fl := range flat {
		out = append(out, "  "+fl)
	}
	out = append(out, "")
	out = append(out, "  "+kit.Composer(m.chatInput, "press i to compose — @mention an invited agent", m.chatFocus))
	return strings.Join(out, "\n")
}
