package editor

import (
	"context"
	"strings"
	"time"

	"charm.land/bubbletea/v2"

	"github.com/drjzlyan/dhi/internal/agentkit/bus"
	"github.com/drjzlyan/dhi/internal/agentkit/runtime"
	"github.com/drjzlyan/dhi/internal/agentkit/tools"
	"github.com/drjzlyan/dhi/internal/tui/kit"
	"github.com/drjzlyan/dhi/internal/tui/theme"
	"github.com/drjzlyan/dhi/internal/unread"
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
	rt     *runtime.Runtime
	bus    *bus.Bus
	apprs  *tools.Approvals
	unread *unread.Store // F-017 read-mark store (nil = no markers)
	agents []string
	events chan chatEvent
	cancel func()

	open     bool
	focus    bool
	channels []string // "#general", "dm:<id>", …
	active   int
	input    kit.Input
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

// SelectChannel switches the sidebar to channel ch (false when unknown).
func (c *chatModel) SelectChannel(ch string) bool {
	for i, name := range c.channels {
		if name == ch {
			c.active = i
			c.resubscribe()
			c.markChannelRead()
			return true
		}
	}
	return false
}

// postHuman posts text as the human on the active channel and dispatches
// it to the crew.
func (c *chatModel) postHuman(text string) {
	if c.bus == nil {
		return
	}
	if posted, err := c.bus.Post(bus.Message{Channel: c.channelName(), Author: bus.Human, Text: text}); err == nil && c.rt != nil {
		c.rt.Handle(context.Background(), posted)
	}
}

// Focus opens the sidebar if closed and moves input focus onto it,
// without the toggle-off case (F-016 approval-jump seam).
func (c *chatModel) Focus() {
	if !c.open {
		c.open = true
		c.resubscribe()
	}
	c.focus = true
	c.markChannelRead() // opening reads the channel (F-017)
}

// markChannelRead advances the active channel's watermark to the
// transcript tail (F-017 Slack rule; nil store = no-op).
func (c *chatModel) markChannelRead() {
	if c.unread == nil {
		return
	}
	top := c.bus.History(c.channelName(), 0)
	if len(top) == 0 {
		return
	}
	_ = c.unread.MarkRead(c.channelName(), top[len(top)-1].ID)
}

// unreadCount reports the active channel's unread count for the badge.
func (c *chatModel) unreadCount() int {
	if c.unread == nil || c.bus == nil {
		return 0
	}
	counts := c.unread.Counts(c.bus, time.Now())
	return counts[c.channelName()]
}

// Toggle opens/closes the sidebar; opening focuses it and (re)subscribes.
func (c *chatModel) Toggle() {
	if !c.open {
		c.open = true
		c.focus = true
		c.resubscribe()
		c.markChannelRead()
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
	case "[", "]":
		if c.input.Value != "" { // mid-message they are text (F-102)
			return c.input.HandleKey(key)
		}
		c.stepChannel(key)
		return true
	}
	return c.composerKey(key, apply)
}

// stepChannel moves to the previous ("[") or next channel.
func (c *chatModel) stepChannel(key string) {
	switch key {
	case "[":
		c.active = (c.active - 1 + len(c.channels)) % len(c.channels)
		c.resubscribe()
		c.markChannelRead()
	case "]":
		c.active = (c.active + 1) % len(c.channels)
		c.resubscribe()
		c.markChannelRead()
	}
}

// composerKey handles the focused composer's keys after channel steps.
func (c *chatModel) composerKey(key string, apply func(string)) bool {
	switch key {
	case "ctrl+f":
		if apply != nil {
			if block := c.lastSuggestion(); block != "" {
				apply(block)
			}
		}
		return true
	case "y", "n", "a", "o", "g":
		// Approval answers only when one is pending (tools.DecideKeys);
		// otherwise these are ordinary composer letters.
		list := c.apprs.List()
		if len(list) == 0 || c.input.Value != "" { // mid-message: text
			return c.input.HandleKey(key)
		}
		// A secret still to be typed is answered in the workspace chat
		// or inbox, which have the masked field (F-089); n still denies.
		if ap := list[0]; !ap.IsSecret() || !ap.Missing || key == "n" {
			c.apprs.Decide(ap, tools.DecideKeys[key])
		}
		return true
	case "enter":
		text := strings.TrimSpace(c.input.Value)
		if text != "" {
			// Dispatch the post (F-036 follow-up): without Handle the
			// sidebar only wrote to the bus and no agent ever answered.
			if posted, err := c.bus.Post(bus.Message{Channel: c.channelName(), Author: bus.Human, Text: text}); err == nil && c.rt != nil {
				c.rt.Handle(context.Background(), posted)
			}
			c.input = kit.Input{}
			c.markChannelRead() // posting reads the channel (F-017)
		}
		return true
	}
	return c.input.HandleKey(key)
}

// lastSuggestion extracts the final fenced code block from the latest
// agent message in the channel ("" when none).
func (c *chatModel) lastSuggestion() string {
	h := c.bus.History(c.channelName(), 0)
	for i := len(h) - 1; i >= 0; i-- {
		m := h[i]
		if m.Author == bus.Human || !bus.Conversational(m) || !strings.Contains(m.Text, "```") {
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
	if n := c.unreadCount(); n > 0 {
		marker := theme.GlyphDot
		if n > 1 {
			marker = theme.GlyphDot + itoa(n)
		}
		name += " " + theme.DangerText().Render(marker)
	}
	head := name

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
		lines = append(lines, "", theme.DangerText().Render("approvals — "+tools.DecideHint))
		for i, a := range list {
			if i >= 3 {
				lines = append(lines, theme.TextDim().Render(itoa(len(list)-i)+" more…"))
				break
			}
			lines = append(lines, theme.Hint().Render("#"+itoa(a.ID)+" "+a.Agent)+" "+a.Describe())
		}
	}

	lines = append(lines, "", kit.Composer(c.input, "message the crew — @mention to wake an agent", c.focus))
	return strings.Join(lines, "\n")
}

func channelLabel(ch string) string { return bus.ChannelLabel(ch) }

// transcript renders the most recent rows that fit maxRows through the
// shared kit.Transcript (F-026 P4): day dividers, HH:MM stamps, author
// styles, and code fences via the injected markdown seam (agents post
// diffs and code — raw fences never render well).
func (c *chatModel) transcript(maxRows int) []string {
	var history []bus.Message
	for _, m := range c.bus.History(c.channelName(), 0) {
		if bus.Conversational(m) { // tool lines live in the trace (F-072)
			history = append(history, m)
		}
	}
	if n := len(history); n > chatTranscriptN {
		history = history[n-chatTranscriptN:]
	}
	if len(history) == 0 {
		return kit.EmptyInline("No messages yet", "Say hi, or @mention an agent to put it to work.", chatWidth-4)
	}
	tr := &kit.Transcript{
		// The panel's inner width (edges + padding) — the transcript
		// wraps to what actually renders (F-026 P4).
		Width:    chatWidth - 4,
		Markdown: kit.ChatMarkdown,
	}
	for _, m := range history {
		row := kit.TrnRow{Author: m.Author, Text: m.Text, At: m.At, Kind: kit.TrnAgent, Markdown: true}
		switch {
		case m.Author == bus.Human:
			row.Kind = kit.TrnHuman
		case m.Kind == bus.KindAlert:
			row.Kind = kit.TrnAlert
		}
		tr.Rows = append(tr.Rows, row)
	}
	out := tr.View()
	if over := len(out) - maxRows; over > 0 {
		out = out[over:]
	}
	return out
}
