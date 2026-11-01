package editor

import (
	"context"
	"slices"
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
	any    bool // from the all-channels watch (reply notifications, F-117)
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

	// A secret request answered here opens a masked form (F-104): the
	// value goes straight to secrets, never the bus or the agent.
	secrets    SecretSaver
	root       string // workspace root for "this workspace" secrets
	secretForm *kit.Form
	secretAsk  *tools.Approval
	secretAns  string
	note       string // the last secret outcome, shown under approvals

	toast *kit.Toast // an agent replied where you are not looking (F-117)

	// F-120 context mentions, supplied by the editor.
	expand  func(text string, agents []string) (string, []string)
	suggest func(prefix string, agents []string) []string
}

var secretScopes = []string{"this workspace", "every workspace"}

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
		go c.pumpAll()
	}
	return c.listen()
}

// pumpAll watches every channel so a reply elsewhere can be announced.
func (c *chatModel) pumpAll() {
	if c.bus == nil {
		return
	}
	sub, _ := c.bus.Subscribe(bus.AllChannels)
	for m := range sub {
		select {
		case c.events <- chatEvent{msg: m, any: true}:
		default:
		}
	}
}

// noteReply raises a toast for an agent's answer the user is not looking
// at: another channel, or the sidebar closed (F-117).
func (c *chatModel) noteReply(m bus.Message) {
	if m.Author == bus.Human || !bus.Conversational(m) || c.rt == nil || !slices.Contains(c.agents, m.Author) {
		return
	}
	if c.open && m.Channel == c.channelName() {
		if strings.HasPrefix(c.note, "@"+m.Author+" is on it") {
			c.note = "" // answered: the routing note has done its job
		}
		return
	}
	first, _, _ := strings.Cut(strings.TrimSpace(m.Text), "\n")
	c.toast = &kit.Toast{Kind: kit.ToastInfo, Title: "@" + m.Author + " replied in " + channelLabel(m.Channel),
		Detail: kit.ClipEllipsis(first, 60) + "  · ctrl+a to read"}
}

// stop cancels the turns running in the active channel (ctrl+x).
func (c *chatModel) stop() {
	if c.rt == nil {
		return
	}
	if n := c.rt.CancelChannel(c.channelName()); n > 0 {
		c.note = "stopped " + kit.Count(n, "turn")
	} else {
		c.note = "nothing is running here"
	}
}

// workingLine is the live status of the channel's turns: the latest
// progress the agent relayed, and how many wait behind it (F-117).
func (c *chatModel) workingLine() string {
	if c.rt == nil {
		return ""
	}
	running, queued := c.rt.Busy(c.channelName())
	if running == 0 && queued == 0 {
		return ""
	}
	what := "working…"
	h := c.bus.All(c.channelName())
	for i := len(h) - 1; i >= 0 && i >= len(h)-20; i-- {
		if bus.Conversational(h[i]) {
			break
		}
		if h[i].Author != bus.Human && strings.TrimSpace(h[i].Text) != "" {
			what = "@" + h[i].Author + ": " + strings.Join(strings.Fields(h[i].Text), " ")
			break
		}
	}
	if queued > 0 {
		what += " · " + itoa(queued) + " queued"
	}
	return theme.AccentText().Render(theme.GlyphBusy+" ") + theme.TextDim().Render(kit.ClipEllipsis(what, chatWidth-14)) +
		theme.Hint().Render("  ^x stop")
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
	top := c.bus.All(c.channelName())
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
	if c.secretForm != nil {
		return c.secretKey(key)
	}
	switch key {
	case "esc":
		c.focus = false
		return true
	case "ctrl+x":
		c.stop()
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
	case "enter":
		text := strings.TrimSpace(c.input.Value)
		if choice, ok := tools.SlashDecide(text); ok && c.answer(choice) {
			c.input = kit.Input{}
			return true
		}
		if text != "" {
			if c.expand != nil { // @file, @selection, @problems, @path (F-120)
				text, _ = c.expand(text, c.agents)
			}
			// Dispatch the post (F-036 follow-up): without Handle the
			// sidebar only wrote to the bus and no agent ever answered.
			posted, err := c.bus.Post(bus.Message{Channel: c.channelName(), Author: bus.Human, Text: text})
			if err != nil { // F-117: keep the text and say why
				c.note = "not sent: " + err.Error()
				return true
			}
			if c.rt != nil {
				c.rt.Handle(context.Background(), posted)
			}
			c.input, c.note = kit.Input{}, ""
			if c.channelName() == runtime.GeneralChannel && len(bus.Mentions(text)) == 0 && c.rt != nil {
				if id := c.rt.DefaultAgent(); id != "" { // F-117: say who picked it up
					c.note = "@" + id + " is on it · @name asks someone else"
				}
			}
			c.markChannelRead() // posting reads the channel (F-017)
		}
		return true
	}
	if key == "tab" { // complete the @word being typed (F-120)
		if word := c.mentionPrefix(); word != "" && c.suggest != nil {
			if s := c.suggest(word, c.agents); len(s) > 0 {
				v := c.input.Value
				c.input.SetValue(v[:len(v)-len(word)] + s[0] + " ")
			}
		}
		return true
	}
	// F-117: letters always type. alt+y/o/a/g/n (or ⌥'s ¥ ø å © in an
	// empty box) answer the pending approval; "no, try…" stays a message.
	if choice, ok := tools.ComposerDecide(key, c.input.Value == ""); ok && c.answer(choice) {
		return true
	}
	return c.input.HandleKey(key)
}

// pendingAsk is the approval this channel answers: in a DM, that agent's;
// in a shared channel, the oldest.
func (c *chatModel) pendingAsk() *tools.Approval {
	if c.apprs == nil {
		return nil
	}
	dm, isDM := strings.CutPrefix(c.channelName(), "dm:")
	for _, ap := range c.apprs.List() {
		if !isDM || ap.Agent == dm {
			return ap
		}
	}
	return nil
}

// answer decides the pending approval (false when none is pending).
func (c *chatModel) answer(choice string) bool {
	ap := c.pendingAsk()
	if ap == nil {
		return false
	}
	if ap.IsSecret() && ap.Missing && choice != "no" {
		c.openSecretForm(ap, choice)
		return true
	}
	c.apprs.Decide(ap, choice)
	c.note = choice + ": " + ap.Agent + " " + ap.Describe()
	return true
}

// lastSuggestion extracts the final fenced code block from the latest
// agent message in the channel ("" when none).
func (c *chatModel) lastSuggestion() string {
	h := c.bus.All(c.channelName())
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
		// F-117: the fence's first line is its info string (```python,
		// ```ts title=x, or nothing); it used to land in the buffer as code
		// for anything but go.
		if nl := strings.IndexByte(block, '\n'); nl >= 0 {
			block = block[nl+1:]
		}
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
		lines = append(lines, "", theme.DangerText().Render("approvals — "+tools.ComposerDecideHint))
		for i, a := range list {
			if i >= 3 {
				lines = append(lines, theme.TextDim().Render(itoa(len(list)-i)+" more…"))
				break
			}
			lines = append(lines, theme.Hint().Render("#"+itoa(a.ID)+" "+a.Agent)+" "+a.Describe())
		}
	}

	if wl := c.workingLine(); wl != "" {
		lines = append(lines, wl)
	}
	if c.note != "" {
		lines = append(lines, theme.TextDim().Render(c.note))
	}
	if c.secretForm != nil {
		lines = append(lines, "", theme.SectionHeader().Render(c.secretForm.Title))
		lines = append(lines, c.secretForm.ViewWidth(chatWidth-4)...)
		return strings.Join(lines, "\n")
	}
	lines = append(lines, "", kit.Composer(c.input, "ask the crew · @name picks who", c.focus))
	if extra := c.composerExtra(); extra != "" {
		lines = append(lines, extra)
	}
	return strings.Join(lines, "\n")
}

func channelLabel(ch string) string { return bus.ChannelLabel(ch) }

// transcript renders the most recent rows that fit maxRows through the
// shared kit.Transcript (F-026 P4): day dividers, HH:MM stamps, author
// styles, and code fences via the injected markdown seam (agents post
// diffs and code — raw fences never render well).
func (c *chatModel) transcript(maxRows int) []string {
	var history []bus.Message
	for _, m := range c.bus.All(c.channelName()) {
		if bus.Conversational(m) { // tool lines live in the trace (F-072)
			history = append(history, m)
		}
	}
	if n := len(history); n > chatTranscriptN {
		history = history[n-chatTranscriptN:]
	}
	if len(history) == 0 {
		return kit.EmptyInline("No messages yet", "Ask anything — the default agent answers; @name picks someone else.", chatWidth-4)
	}
	tr := &kit.Transcript{
		// The panel's inner width (edges + padding) — the transcript
		// wraps to what actually renders (F-026 P4).
		Width:    chatWidth - 4,
		Markdown: kit.ChatMarkdown,
	}
	for _, m := range history {
		// Thread replies show inline, marked ↳: an agent that moves to a
		// task worktree answers in a thread, and the sidebar is flat.
		row := kit.TrnRow{Author: m.Author, Text: m.Text, At: m.At, Kind: kit.TrnAgent, Markdown: true, Thread: m.Thread != 0}
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

// openSecretForm asks for a secret an agent requested whose value is not
// stored yet (F-089/F-104), masked.
func (c *chatModel) openSecretForm(ap *tools.Approval, choice string) {
	if c.secrets == nil {
		c.note = "secrets can't be stored here (no user config dir)"
		return
	}
	at := 0
	if choice == "everywhere" {
		at = 1
	}
	c.secretAsk, c.secretAns = ap, choice
	c.secretForm = kit.NewForm("type "+ap.SecretName()+" (masked)",
		kit.NewSecretField(ap.SecretName()),
		kit.NewToggleField("save for", secretScopes, at))
}

// secretKey drives the masked form: enter saves then answers, esc drops
// it (the request stays pending).
func (c *chatModel) secretKey(key string) bool {
	switch c.secretForm.HandleKey(key) {
	case kit.FormCancel:
		c.secretForm, c.secretAsk = nil, nil
	case kit.FormSubmit:
		vals := c.secretForm.Values()
		value := strings.TrimSpace(vals[0])
		if value == "" {
			c.secretForm.SetError("type the value, or esc")
			return true
		}
		ap := c.secretAsk
		if _, ok := c.apprs.Get(ap.ID); !ok {
			c.secretForm.SetError("the request is gone (the agent stopped waiting)")
			return true
		}
		root := c.root
		if vals[1] == secretScopes[1] {
			root = ""
		}
		if err := c.secrets.SetIn(root, ap.SecretName(), value); err != nil {
			c.secretForm.SetError(err.Error())
			return true
		}
		c.apprs.Decide(ap, c.secretAns)
		c.note = "saved " + ap.SecretName() + " for " + vals[1] + "; " + ap.Agent + " can use it"
		c.secretForm, c.secretAsk = nil, nil
	}
	return true
}

// TakeToast implements the shell's toaster seam (F-117 reply notices).
func (m *Model) TakeToast() (kit.Toast, bool) {
	if m.toast != nil {
		t := *m.toast
		m.toast = nil
		return t, true
	}
	if m.chat == nil || m.chat.toast == nil {
		return kit.Toast{}, false
	}
	t := *m.chat.toast
	m.chat.toast = nil
	return t, true
}

// mentionPrefix is the "@word" being typed at the end of the input
// (without the @), or "".
func (c *chatModel) mentionPrefix() string {
	v := c.input.Value
	i := strings.LastIndexAny(v, " \n")
	word := v[i+1:]
	if !strings.HasPrefix(word, "@") {
		return ""
	}
	return word[1:]
}

// composerExtra is the line under the composer: completions for a half
// typed @word, else what the message will carry (F-120).
func (c *chatModel) composerExtra() string {
	if !c.focus {
		return ""
	}
	if w := c.mentionPrefix(); c.input.Value != "" && strings.HasSuffix(c.input.Value, "@"+w) && c.suggest != nil {
		if s := c.suggest(w, c.agents); len(s) > 0 {
			return theme.Hint().Render("tab ") + theme.TextDim().Render(kit.ClipEllipsis("@"+strings.Join(s, "  @"), chatWidth-8))
		}
	}
	if c.expand != nil && strings.Contains(c.input.Value, "@") {
		_, chips := c.expand(c.input.Value, c.agents)
		return kit.ClipEllipsis(chipLine(chips), chatWidth-4)
	}
	return ""
}
