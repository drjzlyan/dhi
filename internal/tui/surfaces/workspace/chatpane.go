package workspace

import (
	"context"
	"sort"
	"strings"

	"github.com/drjzlyan/dhi/internal/agentkit/bus"
	"github.com/drjzlyan/dhi/internal/agentkit/org"
	"github.com/drjzlyan/dhi/internal/ansi"
	"github.com/drjzlyan/dhi/internal/tui/theme"
	"github.com/drjzlyan/dhi/internal/unread"
)

// turnHandler is the slice of the agent runtime the channels floor
// needs: mention-triggered turns. *runtime.Runtime satisfies it; tests
// substitute fakes.
type turnHandler interface {
	Handle(ctx context.Context, msg bus.Message)
}

// Layout constants for the Slack floor (F-022). Below slackCtxMin the
// context pane overlays the transcript (the pre-M11 drill-down).
const (
	slackCtxMin = 100
	slackRailW  = 18
	slackCtxW   = 30
)

// chatPane is the CHANNELS section (F-022): a left channel rail
// (CHANNELS group + DIRECT MESSAGES group), a center transcript with
// the composer, and a right context pane — the open thread beside the
// transcript, or an agent profile. Posting lands in the bus first;
// mentions then route through the turn handler exactly like the editor
// sidebar.
type chatPane struct {
	bus *bus.Bus
	rt  turnHandler // nil → post-only (no crew installed)
	org *org.Org

	// F-017 read-on-open seams: the Model injects both (store-backed).
	// onRead advances a scope's watermark; unreadFor feeds rail markers.
	onRead    func(scope string, upToID int64)
	unreadFor func(ch string) int

	// F-022 agent-profile seam: the Model injects a renderer.
	profile func(id string) []string

	channels   []string
	active     int
	events     chan struct{}
	subCancel  func() // cancels the current channel subscription
	subscribed string // channel currently subscribed

	focus    bool // composer focused
	input    []rune
	cursor   int   // selected message index (blurred navigation)
	threadID int64 // 0 = channel view; else thread filter

	railFocus bool   // tab toggles rail navigation (wide layout)
	railCur   int    // rail highlight (wide layout)
	profileID string // agent profile pane ("" = closed)
	lastWidth int    // updated per render; keys are width-aware
}

func newChatPane(b *bus.Bus, rt turnHandler, o *org.Org) *chatPane {
	return &chatPane{
		bus:    b,
		rt:     rt,
		org:    o,
		events: make(chan struct{}, 16),
	}
}

// buildChannels composes the rail: #general seeded first, then team
// channels from the org registry, then DMs for every rostered agent.
func (p *chatPane) buildChannels(agents []string, teams []org.Team) {
	channels := []string{"#general"}
	seen := map[string]bool{"#general": true}
	for _, t := range teams {
		ch := "#" + t.Name
		if !seen[ch] {
			seen[ch] = true
			channels = append(channels, ch)
		}
	}
	sorted := append([]string(nil), agents...)
	sort.Strings(sorted)
	for _, id := range sorted {
		ch := "dm:" + id
		if !seen[ch] {
			seen[ch] = true
			channels = append(channels, ch)
		}
	}
	active := p.channelName()
	p.channels = channels
	p.active = 0
	for i, ch := range channels {
		if ch == active {
			p.active = i
			break
		}
	}
	p.clampRail()
}

// refreshRoster rebuilds the rail from live sources (called on pings).
func (p *chatPane) refreshRoster(agents []string, teams []org.Team) {
	p.buildChannels(agents, teams)
}

func (p *chatPane) channelName() string {
	if p.active < len(p.channels) {
		return p.channels[p.active]
	}
	return "#general"
}

// hints is the channels section's keymap for the workspace chrome bar —
// the one place channel navigation hints render. Keys track state: the
// composer owns focus while typing, an open thread narrows to reply
// navigation, otherwise the full channel keymap applies.
func (p *chatPane) hints() []string {
	if p.focus {
		return []string{"enter send", "esc blur"}
	}
	if p.profileID != "" {
		return []string{"esc close", "i compose", "j/k select", ",/. channel"}
	}
	if p.threadID != 0 {
		return []string{"i reply in thread", "esc close"}
	}
	return []string{"i compose", "j/k select", "t thread", "v profile", "tab rail", ",/. channel"}
}

func (p *chatPane) switchChannel(dir int) {
	if len(p.channels) == 0 {
		return
	}
	p.openChannel((p.active + dir + len(p.channels)) % len(p.channels))
}

// openChannel selects a rail index: resets the context pane, resubscribes,
// and marks the channel read (F-017 Slack rule).
func (p *chatPane) openChannel(i int) {
	if i < 0 || i >= len(p.channels) {
		return
	}
	p.active = i
	p.threadID = 0
	p.profileID = ""
	p.cursor = 0
	p.railFocus = false // opening returns focus to the transcript
	p.clampRail()
	p.resubscribe()
	p.markChannelRead(p.channelName())
}

func (p *chatPane) clampRail() {
	if p.railCur >= len(p.channels) {
		p.railCur = maxInt(len(p.channels)-1, 0)
	}
	if p.railCur < 0 {
		p.railCur = 0
	}
}

// markChannelRead advances the channel's top-level watermark to the
// current transcript tail (F-017: opening a channel reads it).
func (p *chatPane) markChannelRead(ch string) {
	if p.onRead == nil || ch == "" {
		return
	}
	top := p.bus.History(ch, 0)
	if len(top) == 0 {
		return
	}
	p.onRead(ch, top[len(top)-1].ID)
}

// markThreadRead advances the thread scope watermark for the given
// thread root on the active channel.
func (p *chatPane) markThreadRead(root int64) {
	if p.onRead == nil || root == 0 {
		return
	}
	thread := append([]bus.Message{}, p.bus.History(p.channelName(), root)...)
	if root != 0 {
		// include the root message itself in the read set
		for _, m := range p.bus.History(p.channelName(), 0) {
			if m.ID == root {
				thread = append(thread, m)
				break
			}
		}
	}
	if len(thread) == 0 {
		return
	}
	maxID := thread[0].ID
	for _, m := range thread {
		if m.ID > maxID {
			maxID = m.ID
		}
	}
	p.onRead(unread.ThreadScope(p.channelName(), root), maxID)
}

// openAt jumps the CHANNELS pane to a channel's thread, positioning the
// cursor on the message that triggered the jump. False when the channel
// has fallen out of the rail (reported after aggregation) so callers can
// degrade visibly.
func (p *chatPane) openAt(channel string, threadRoot, msgID int64) bool {
	for i, ch := range p.channels {
		if ch != channel {
			continue
		}
		p.active = i
		p.profileID = ""
		p.threadID = threadRoot
		p.cursor = 0
		p.clampRail()
		p.resubscribe()
		// Position on the triggering message in the navigable list (the
		// top-level channel on wide screens, the thread drill-down on
		// narrow ones).
		for j, m := range p.navMsgs(p.lastWidth >= slackCtxMin) {
			if m.ID == msgID {
				p.cursor = j
				break
			}
		}
		// F-017: jumping into a thread reads that thread (and, per the
		// Scan rule, its root message).
		if threadRoot != 0 {
			p.markThreadRead(threadRoot)
		} else {
			p.markChannelRead(channel)
		}
		return true
	}
	return false
}

// resubscribe points the message pump at the active channel.
func (p *chatPane) resubscribe() {
	if p.subCancel != nil {
		p.subCancel()
		p.subCancel = nil
	}
	if p.bus == nil {
		return
	}
	ch := p.channelName()
	msgs, cancel := p.bus.Subscribe(ch)
	p.subCancel = cancel
	p.subscribed = ch
	go func() {
		for range msgs {
			select {
			case p.events <- struct{}{}:
			default:
			}
		}
	}()
}

// ---- keys ----

// handleKey consumes one key while the CHANNELS section is active.
func (p *chatPane) handleKey(key string) bool {
	if p.bus == nil {
		return false
	}
	if p.focus {
		return p.composerKey(key)
	}

	switch key {
	case ",", ".":
		p.switchChannel(map[string]int{",": -1, ".": 1}[key])
		return true
	case "tab":
		if p.lastWidth >= slackCtxMin {
			p.railFocus = !p.railFocus
			return true
		}
		return false
	}

	// Rail focus: navigate channels like Slack's sidebar.
	if p.railFocus {
		switch key {
		case "j", "down":
			if p.railCur < len(p.channels)-1 {
				p.railCur++
			}
			return true
		case "k", "up":
			if p.railCur > 0 {
				p.railCur--
			}
			return true
		case "enter":
			p.openChannel(p.railCur)
			return true
		case "i":
			p.openChannel(p.railCur)
			p.focus = true
			return true
		case "v":
			if ch := p.channelAt(p.railCur); strings.HasPrefix(ch, "dm:") {
				p.profileID = strings.TrimPrefix(ch, "dm:")
			}
			return true
		}
		return false
	}

	// Transcript focus.
	history := p.navMsgs(p.lastWidth >= slackCtxMin)
	switch key {
	case "j", "down":
		if p.cursor < len(history)-1 {
			p.cursor++
		}
		return true
	case "k", "up":
		if p.cursor > 0 {
			p.cursor--
		}
		return true
	case "t":
		if p.cursor < len(history) {
			root := bus.ThreadOf(history[p.cursor])
			p.threadID = root
			p.profileID = ""
			p.cursor = 0
			// Opening a thread reads it (F-017 Slack rule).
			p.markThreadRead(root)
		}
		return true
	case "c", "0":
		p.threadID = 0
		p.profileID = ""
		p.cursor = 0
		return true
	case "esc":
		// Close the context pane (thread or profile) — the transcript
		// keeps its place.
		if p.threadID != 0 || p.profileID != "" {
			p.threadID = 0
			p.profileID = ""
			return true
		}
		return false
	case "v":
		if p.cursor < len(history) {
			if author := history[p.cursor].Author; author != "" && author != bus.Human {
				p.profileID = author
				return true
			}
		}
		return false
	case "i", "enter":
		p.focus = true
		return true
	}
	return false
}

// channelAt returns the rail label at index i ("" when out of range).
func (p *chatPane) channelAt(i int) string {
	if i >= 0 && i < len(p.channels) {
		return p.channels[i]
	}
	return ""
}

func (p *chatPane) composerKey(key string) bool {
	switch key {
	case "esc":
		p.focus = false
		return true
	case "enter":
		text := strings.TrimSpace(string(p.input))
		if text != "" {
			p.post(text)
			p.input = nil
		}
		return true
	case "backspace":
		if len(p.input) > 0 {
			p.input = p.input[:len(p.input)-1]
		}
		return true
	}
	if r := []rune(key); len(r) == 1 && r[0] >= 32 {
		p.input = append(p.input, r[0])
		return true
	}
	return false
}

// post persists the message and hands it to the turn handler so
// @mentions (or DM addressees) trigger agent turns.
func (p *chatPane) post(text string) {
	posted, err := p.bus.Post(bus.Message{
		Channel: p.channelName(),
		Thread:  p.threadID,
		Author:  bus.Human,
		Text:    text,
	})
	if err != nil {
		return
	}
	if p.threadID != 0 {
		p.markThreadRead(p.threadID)
	} else {
		p.markChannelRead(p.channelName())
	}
	if p.rt != nil {
		p.rt.Handle(context.Background(), posted)
	}
	select {
	case p.events <- struct{}{}:
	default:
	}
}

// ---- data ----

// visibleHistory returns the transcript rows: the whole channel, or —
// when drilled down — the thread root plus its replies. (bus.History
// filters strictly by Thread==id, which excludes the root itself.)
func (p *chatPane) visibleHistory() []bus.Message {
	if p.bus == nil {
		return nil
	}
	if p.threadID == 0 {
		return p.bus.History(p.channelName(), 0)
	}
	var out []bus.Message
	for _, m := range p.bus.History(p.channelName(), 0) {
		if m.ID == p.threadID {
			out = append(out, m)
			break
		}
	}
	return append(out, p.bus.History(p.channelName(), p.threadID)...)
}

// navMsgs is the list the message cursor moves through: on wide screens
// with a thread pane open, the cursor keeps navigating the channel while
// replies render beside it; narrow screens drill inline.
func (p *chatPane) navMsgs(wide bool) []bus.Message {
	if wide && p.threadID != 0 {
		return p.bus.History(p.channelName(), 0)
	}
	return p.visibleHistory()
}

// ---- rendering ----

// render produces the section body within the given cell budget.
func (p *chatPane) render(width, height int) []string {
	p.lastWidth = width
	if p.bus == nil {
		return []string{
			theme.Hint().Render("channels") + theme.TextDim().Render("              (no message bus — install a crew)"),
			theme.TextDim().Render("the bus starts with the agent runtime"),
		}
	}
	if width >= slackCtxMin {
		return p.renderWide(width, height)
	}
	return p.renderNarrow(width, height)
}

// renderWide is the Slack floor: rail | transcript | context pane.
func (p *chatPane) renderWide(width, height int) []string {
	transW := width - slackRailW - slackCtxW - 2 // the two separators
	if transW < 30 {
		transW = 30
	}
	rail := p.railLines(slackRailW, height)
	trans := p.transcriptLines(transW, height, true)
	ctx := p.contextLines(slackCtxW, height)

	rows := make([]string, 0, height)
	for y := 0; y < height; y++ {
		r := railLineAt(rail, y, slackRailW)
		tr := padToANSI(lineAt(trans, y, transW), transW)
		c := lineAt(ctx, y, slackCtxW)
		rows = append(rows, r+" "+tr+" "+c)
	}
	return rows
}

// renderNarrow keeps the pre-M11 single-column floor: horizontal rail,
// inline thread drill-down, composer.
func (p *chatPane) renderNarrow(width, height int) []string {
	var lines []string
	lines = append(lines, p.rail())
	lines = append(lines, theme.TextDim().Render(strings.Repeat("─", minInt(width, 56))))

	history := p.visibleHistory()
	header := p.channelName()
	if p.threadID != 0 {
		header += theme.Hint().Render("  · thread #" + itoa(int(p.threadID)) + " (c back)")
	}
	lines = append(lines, theme.Brand().Render(header))

	body := height - 5 // rail, rule, header, input/hint, blank
	if body < 3 {
		body = 3
	}
	wrap := width - 8
	if wrap < 20 {
		wrap = 20
	}

	type owned struct {
		line  string
		msgIx int
	}
	var flat []owned
	for mi, msg := range history {
		style := theme.TabActive()
		if msg.Author != bus.Human {
			style = theme.Brand()
		}
		tag := ""
		if msg.Thread != 0 {
			tag = theme.Hint().Render(" ↳")
		}
		prefix := style.Render(msg.Author) + tag + " "
		for i, seg := range wrapWords(msg.Text, wrap) {
			l := prefix + seg
			if i > 0 {
				l = "  " + seg
			}
			flat = append(flat, owned{line: l, msgIx: mi})
		}
	}
	if len(flat) == 0 {
		flat = append(flat, owned{line: theme.TextDim().Render(
			"(no messages yet — press i and say hi, @mention an agent)")})
	} else if over := len(flat) - body; over > 0 {
		flat = flat[over:]
	}
	if p.cursor >= len(history) {
		p.cursor = maxInt(len(history)-1, 0)
	}
	var rendered []string
	for _, fl := range flat {
		marker := "  "
		if !p.focus && fl.msgIx == p.cursor {
			marker = theme.GlyphCursor + " "
		}
		rendered = append(rendered, marker+fl.line)
	}
	lines = append(lines, rendered...)

	lines = append(lines, "")
	if p.focus {
		lines = append(lines, theme.TabActive().Render("> "+string(p.input))+"▌")
	}
	return lines
}

// transcriptLines renders the center column: header, messages, composer.
// wide selects the width-aware message list (channel stays visible while
// a thread pane is open).
func (p *chatPane) transcriptLines(width, height int, wide bool) []string {
	history := p.navMsgs(wide)
	wrap := width - 6
	if wrap < 20 {
		wrap = 20
	}

	var lines []string
	composer := 2
	header := p.channelName()
	if p.threadID != 0 {
		header += theme.Hint().Render("  · thread #" + itoa(int(p.threadID)))
	}
	lines = append(lines, theme.Brand().Render(header))

	type owned struct {
		line  string
		msgIx int
	}
	var flat []owned
	for mi, msg := range history {
		style := theme.TabActive()
		if msg.Author != bus.Human {
			style = theme.Brand()
		}
		tag := ""
		if msg.Thread != 0 {
			tag = theme.Hint().Render(" ↳")
		}
		prefix := style.Render(msg.Author) + tag + " "
		for i, seg := range wrapWords(msg.Text, wrap) {
			l := prefix + seg
			if i > 0 {
				l = "  " + seg
			}
			flat = append(flat, owned{line: l, msgIx: mi})
		}
	}
	if len(flat) == 0 {
		flat = append(flat, owned{line: theme.TextDim().Render(
			"(no messages yet — press i and say hi, @mention an agent)")})
	}
	body := height - 1 - composer - 1
	if over := len(flat) - body; over > 0 {
		flat = flat[over:]
	}
	if p.cursor >= len(history) {
		p.cursor = maxInt(len(history)-1, 0)
	}
	for _, fl := range flat {
		marker := "  "
		if !p.focus && fl.msgIx == p.cursor {
			marker = theme.GlyphCursor + " "
		}
		lines = append(lines, marker+fl.line)
	}

	lines = append(lines, "")
	if p.focus {
		lines = append(lines, theme.TabActive().Render("> "+string(p.input))+"▌")
	}
	return lines
}

// railLines renders the vertical channel sidebar: groups, unread
// markers, rail cursor, active channel highlight.
func (p *chatPane) railLines(width, height int) []string {
	bg := theme.InsetBg()
	inset := func(s string) string { return bg.Render(padToANSI(s, width)) }
	var lines []string
	lines = append(lines, inset(" "+theme.TextMuted().Render("CHANNELS")))
	for _, ch := range p.channels {
		if strings.HasPrefix(ch, "dm:") {
			// DM group header goes right before the first DM.
			lines = append(lines, inset(" "+theme.TextMuted().Render("DIRECT MESSAGES")))
			break
		}
	}
	for i, ch := range p.channels {
		label := " " + ch
		if n := p.unreadCount(ch); n > 0 {
			if n == 1 {
				label += " " + theme.DangerText().Render(theme.GlyphDot)
			} else {
				label += " " + theme.DangerText().Render(theme.GlyphDot+itoa(n))
			}
		}
		if i == p.railCur && p.railFocus {
			label = theme.GlyphCursor + label[1:]
		}
		if i == p.active {
			label = theme.TabActive().Render(label)
		} else {
			label = theme.TextDim().Render(label)
		}
		lines = append(lines, inset(label))
	}
	for len(lines) < height {
		lines = append(lines, inset(""))
	}
	return lines[:height]
}

// contextLines renders the right pane: the open thread beside the
// transcript, or an agent profile; blank when closed.
func (p *chatPane) contextLines(width, height int) []string {
	bg := theme.ElevatedBg()
	inset := func(s string) string { return bg.Render(padToANSI(s, width)) }
	blank := func() string { return bg.Render(strings.Repeat(" ", width)) }

	var body []string
	switch {
	case p.profileID != "":
		body = append(body, theme.Brand().Render(" "+p.profileID), "")
		if p.profile != nil {
			body = append(body, p.profile(p.profileID)...)
		} else {
			body = append(body, theme.TextDim().Render(" (profile unavailable)"))
		}
	case p.threadID != 0:
		wrap := width - 12 // room for the author prefix + margins
		body = append(body, theme.Brand().Render(" thread #"+itoa(int(p.threadID))), "")
		for _, msg := range p.visibleHistory() {
			style := theme.TabActive()
			if msg.Author != bus.Human {
				style = theme.Brand()
			}
			tag := ""
			if msg.Thread != 0 {
				tag = theme.Hint().Render(" ↳")
			}
			prefix := style.Render(msg.Author) + tag + " "
			for i, seg := range wrapWords(msg.Text, wrap) {
				if i > 0 {
					seg = "  " + seg
				}
				body = append(body, " "+prefix+seg)
				prefix = "  "
			}
		}
	default:
		out := make([]string, height)
		for i := range out {
			out[i] = blank()
		}
		return out
	}

	out := make([]string, 0, height)
	for y := 0; y < height; y++ {
		if y < len(body) {
			out = append(out, inset(ansi.Clip(body[y], width)))
		} else {
			out = append(out, blank())
		}
	}
	return out
}

func railLineAt(lines []string, y, width int) string {
	if y < len(lines) {
		return lines[y]
	}
	return strings.Repeat(" ", width)
}

func lineAt(lines []string, y, width int) string {
	if y < len(lines) {
		return lines[y]
	}
	return strings.Repeat(" ", width)
}

// padToANSI pads to width counting visible cells (styled strings).
func padToANSI(s string, w int) string {
	if rn := len([]rune(ansi.Strip(s))); rn < w {
		return s + strings.Repeat(" ", w-rn)
	}
	return s
}

func (p *chatPane) rail() string {
	var parts []string
	for i, ch := range p.channels {
		label := ch
		if n := p.unreadCount(ch); n > 0 {
			if n == 1 {
				label += " " + theme.DangerText().Render(theme.GlyphDot)
			} else {
				label += " " + theme.DangerText().Render(theme.GlyphDot+itoa(n))
			}
		}
		if i == p.active {
			parts = append(parts, theme.TabActive().Render("["+label+"]"))
		} else {
			parts = append(parts, theme.TextDim().Render(label))
		}
	}
	if len(parts) == 0 {
		parts = append(parts, theme.TextDim().Render("#general"))
	}
	return strings.Join(parts, " ")
}

// unreadCount reports the channel's unread attention count via the
// Model-injected seam (nil-safe: no store → no markers).
func (p *chatPane) unreadCount(ch string) int {
	if p.unreadFor == nil {
		return 0
	}
	return p.unreadFor(ch)
}

func minInt(a, b int) int {
	if a < b {
		return a
	}
	return b
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
