package editor

import (
	"strings"

	"charm.land/bubbletea/v2"

	"github.com/drjzlyan/dhi/internal/agentkit/bus"
	"github.com/drjzlyan/dhi/internal/agentkit/runtime"
	"github.com/drjzlyan/dhi/internal/agentkit/tools"
	"github.com/drjzlyan/dhi/internal/tui/theme"
)

const (
	chatWidth       = 46
	chatTranscriptN = 200 // messages kept rendered per channel
)

// chatEvent is one async sidebar update: a bus message, an approval-
// queue ping, or a roster-change ping (P2 reloads).
type chatEvent struct {
	msg    bus.Message
	ping   bool
	roster bool
}

// chatModel is the crew sidebar (F-007 component 7): transcript of the
// active channel, mention input, roster switching, approval prompts, and
// apply-suggestion into the focused buffer.
type chatModel struct {
	rt      *runtime.Runtime
	bus     *bus.Bus
	apprs   *tools.Approvals
	agents  []string
	events  chan chatEvent
	cancel  func()
	onApply func(string)

	open     bool
	focus    bool
	channels []string // "#general", "dm:<id>", …
	active   int
	input    []rune
}

func newChat(rt *runtime.Runtime) *chatModel {
	c := &chatModel{
		rt:     rt,
		bus:    rt.Bus(),
		apprs:  rt.Approvals(),
		agents: rt.AgentIDs(),
		events: make(chan chatEvent, 64),
	}
	c.channels = []string{"#general"}
	for _, id := range c.agents {
		c.channels = append(c.channels, "dm:"+id)
	}
	return c
}

// start pumps bus + approval + roster events into c.events; safe to call
// once.
func (c *chatModel) start() tea.Cmd {
	if c.cancel == nil {
		go c.pumpApprovals()
		go c.pumpRoster()
	}
	return c.listen()
}

func (c *chatModel) listen() tea.Cmd {
	ch := c.events
	return func() tea.Msg {
		ev, ok := <-ch
		if !ok {
			return nil
		}
		return ev
	}
}

// resubscribe points the message pump at the active channel.
func (c *chatModel) resubscribe() {
	if c.cancel != nil {
		c.cancel()
	}
	sub, cancel := c.bus.Subscribe(c.channels[c.active])
	c.cancel = cancel
	go func() {
		for m := range sub {
			select {
			case c.events <- chatEvent{msg: m}:
			default:
			}
		}
	}()
}

func (c *chatModel) pumpApprovals() {
	for range c.apprs.Changes() {
		select {
		case c.events <- chatEvent{ping: true}:
		default:
		}
	}
}

// pumpRoster forwards runtime reload pings so the channel rail tracks
// crew changes without reopening the sidebar.
func (c *chatModel) pumpRoster() {
	if c.rt == nil {
		return
	}
	for range c.rt.Changes() {
		select {
		case c.events <- chatEvent{roster: true}:
		default:
		}
	}
}

// refreshRoster rebuilds agents + channels from the runtime, keeping the
// active channel selected when it still exists.
func (c *chatModel) refreshRoster() {
	if c.rt == nil {
		return
	}
	c.agents = c.rt.AgentIDs()
	channels := []string{"#general"}
	for _, id := range c.agents {
		channels = append(channels, "dm:"+id)
	}
	active := c.channelName()
	c.channels = channels
	c.active = 0
	for i, ch := range channels {
		if ch == active {
			c.active = i
			break
		}
	}
}

// Toggle opens/closes the sidebar; opening focuses it and (re)subscribes.
func (c *chatModel) Toggle() {
	if !c.open {
		c.open = true
		c.focus = true
		c.resubscribe()
		return
	}
	if c.focus { // first toggle from focused: blur, second closes
		c.focus = false
		return
	}
	c.open = false
	c.focus = false
}

// closed reports whether the events channel is drained for good.
func (c *chatModel) closed() bool { return c == nil || c.events == nil }

func (c *chatModel) channelName() string { return c.channels[c.active] }

// handleKey processes input while the sidebar is open. Returns true when
// the key was consumed.
func (c *chatModel) handleKey(key string, apply func(string)) bool {
	if !c.open {
		return false
	}
	if !c.focus {
		if key == "enter" || key == "i" {
			c.focus = true
			return true
		}
		return false
	}
	switch key {
	case "esc":
		c.focus = false
		return true
	case "[":
		c.active = (c.active - 1 + len(c.channels)) % len(c.channels)
		c.resubscribe()
		return true
	case "]":
		c.active = (c.active + 1) % len(c.channels)
		c.resubscribe()
		return true
	case "ctrl+f":
		if apply != nil {
			if block := c.lastSuggestion(); block != "" {
				apply(block)
			}
		}
		return true
	case "y":
		if list := c.apprs.List(); len(list) > 0 {
			c.apprs.Resolve(list[0].ID, true)
		}
		return true
	case "n":
		if list := c.apprs.List(); len(list) > 0 {
			c.apprs.Resolve(list[0].ID, false)
		}
		return true
	case "enter":
		text := strings.TrimSpace(string(c.input))
		if text != "" {
			_, _ = c.bus.Post(bus.Message{Channel: c.channelName(), Author: bus.Human, Text: text})
			c.input = nil
		}
		return true
	case "backspace":
		if len(c.input) > 0 {
			c.input = c.input[:len(c.input)-1]
		}
		return true
	}
	if r := []rune(key); len(r) == 1 && r[0] >= 32 {
		c.input = append(c.input, r[0])
		return true
	}
	return false
}

// lastSuggestion extracts the final fenced code block from the latest
// agent message in the channel ("" when none).
func (c *chatModel) lastSuggestion() string {
	h := c.bus.History(c.channelName(), 0)
	for i := len(h) - 1; i >= 0; i-- {
		m := h[i]
		if m.Author == bus.Human || !strings.Contains(m.Text, "```") {
			continue
		}
		parts := strings.Split(m.Text, "```")
		if len(parts) < 2 {
			continue
		}
		block := parts[len(parts)-2] // last opened fence
		block = strings.TrimPrefix(strings.TrimPrefix(block, "go\n"), "\n")
		return strings.Trim(block, "\n")
	}
	return ""
}

// view renders the panel body at full height h.
func (c *chatModel) view(h int) string {
	name := theme.Brand().Render(channelLabel(c.channelName()))
	head := name + theme.Hint().Render("  [/] switch · ^f apply · esc blur")

	var lines []string
	lines = append(lines, head, "")

	transcriptH := h - 8 // head, blank, approvals, input, hint, padding
	if list := c.apprs.List(); len(list) > 0 {
		transcriptH -= len(list) + 2
	}
	if transcriptH < 3 {
		transcriptH = 3
	}
	lines = append(lines, c.transcript(transcriptH)...)

	if list := c.apprs.List(); len(list) > 0 {
		lines = append(lines, "", theme.DangerText().Render("approvals — y allow · n deny"))
		for i, a := range list {
			if i >= 3 {
				lines = append(lines, theme.TextDim().Render(itoa(len(list)-i)+" more…"))
				break
			}
			lines = append(lines, theme.Hint().Render("#"+itoa(a.ID)+" "+string(a.Op))+" "+a.Target)
		}
	}

	if c.focus {
		lines = append(lines, "", theme.TabActive().Render("> "+string(c.input))+"▌")
		lines = append(lines, theme.Hint().Render("⏎ send · @mention triggers agents · ^f apply"))
	} else {
		lines = append(lines, "", theme.Hint().Render("⏎ focus input to chat"))
	}
	return strings.Join(lines, "\n")
}

func channelLabel(ch string) string {
	if ch == "#general" {
		return "#general"
	}
	return ch
}

// transcript renders the most recent rows that fit maxRows.
func (c *chatModel) transcript(maxRows int) []string {
	history := c.bus.History(c.channelName(), 0)
	if n := len(history); n > chatTranscriptN {
		history = history[n-chatTranscriptN:]
	}
	var lines []string
	for _, m := range history {
		style := theme.TabActive()
		if m.Author != bus.Human {
			style = theme.Brand()
		}
		prefix := style.Render(m.Author)
		for i, seg := range wrapWords(m.Text, chatWidth-6) {
			if i == 0 {
				lines = append(lines, prefix+" "+seg)
				continue
			}
			lines = append(lines, "  "+seg)
		}
	}
	if len(lines) == 0 {
		return []string{theme.TextDim().Render("(no messages yet — say hi or @mention an agent)")}
	}
	if over := len(lines) - maxRows; over > 0 {
		lines = lines[over:]
	}
	return lines
}

// wrapWords wraps text to width on spaces (long words hard-split).
func wrapWords(text string, width int) []string {
	if width < 12 {
		width = 12
	}
	var out []string
	for _, para := range strings.Split(text, "\n") {
		words := strings.Fields(para)
		if len(words) == 0 {
			out = append(out, "")
			continue
		}
		cur := ""
		flush := func() {
			out = append(out, cur)
			cur = ""
		}
		for _, w := range words {
			switch {
			case cur == "":
				cur = w
			case len([]rune(cur))+1+len([]rune(w)) <= width:
				cur += " " + w
			default:
				flush()
				cur = w
			}
			for len([]rune(cur)) > width { // hard-split oversized word
				r := []rune(cur)
				out = append(out, string(r[:width]))
				cur = string(r[width:])
			}
		}
		if cur != "" || len(out) == 0 {
			flush()
		}
	}
	return out
}
