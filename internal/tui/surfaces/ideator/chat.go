package ideator

import (
	"strings"

	"github.com/drjzlyan/dhi/internal/agentkit/bus"
	"github.com/drjzlyan/dhi/internal/tui/kit"
	"github.com/drjzlyan/dhi/internal/tui/theme"
)

// chat state lives on the model: composer focus/input plus a scroll
// offset over the transcript. The session channel is the chat — no rail,
// no thread drill-down; agent replies land top-level so the runtime's
// History(ch, 0) context window sees the whole ideation conversation.
func (m *Model) chatHistory() []bus.Message {
	sess, ok := m.openSession()
	if !ok || m.bus == nil {
		return nil
	}
	return m.bus.History(sess.Channel, 0)
}

// chatKey handles keys while CHAT is active and the composer is blurred.
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
		text := strings.TrimSpace(string(m.chatInput))
		if text != "" {
			m.chatPost(text)
			m.chatInput = nil
		}
		return true
	case "backspace":
		if len(m.chatInput) > 0 {
			m.chatInput = m.chatInput[:len(m.chatInput)-1]
		}
		return true
	}
	if r := []rune(key); len(r) == 1 && r[0] >= 32 {
		m.chatInput = append(m.chatInput, r[0])
		return true
	}
	return false
}

// chatPost persists the message and dispatches a turn; the runtime
// resolves @mentions (or invites the whole session via dispatchRevision).
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
	m.requestTurn(posted)
	m.chatScroll = 0
}

// chatBody renders the transcript + composer within the cell budget.
func (m *Model) chatBody(w, h int) string {
	out := []string{theme.Hint().Render("session chat") +
		theme.TextDim().Render("        i compose · j/k scroll · G latest")}
	sess, ok := m.openSession()
	if !ok {
		out = append(out, theme.TextDim().Render("(no session open — pick one under SESSIONS)"))
		return strings.Join(out, "\n")
	}
	if m.bus == nil {
		out = append(out, theme.DangerText().Render("(no message bus — install a crew)"))
		return strings.Join(out, "\n")
	}
	out = append(out, theme.Brand().Render(sess.Channel))

	// The transcript renders through the shared kit.Transcript (F-026
	// P6): day dividers, stamps, author styles, shared wrap.
	tr := &kit.Transcript{Width: w - 2}
	for _, msg := range m.chatHistory() {
		row := kit.TrnRow{Author: msg.Author, Text: msg.Text, At: msg.At, Kind: kit.TrnAgent}
		if msg.Author == busHuman {
			row.Kind = kit.TrnHuman
		}
		tr.Rows = append(tr.Rows, row)
	}
	flat := tr.View()
	if len(flat) == 0 {
		flat = append(flat, theme.TextDim().Render(
			"(no messages yet — i compose, @mention an invited agent)"))
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
	if m.chatFocus {
		out = append(out, theme.TabActive().Render("> "+string(m.chatInput))+"▏")
		out = append(out, theme.Hint().Render(
			"⏎ send · @mention triggers agents · esc blur"))
	} else {
		out = append(out, theme.Hint().Render("i compose"))
	}
	return strings.Join(out, "\n")
}
