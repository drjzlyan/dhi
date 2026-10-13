package workspace

import (
	"charm.land/lipgloss/v2"
	"context"
	"sort"
	"strconv"
	"strings"

	"github.com/drjzlyan/dhi/internal/agentkit/bus"
	"github.com/drjzlyan/dhi/internal/agentkit/channelmeta"
	"github.com/drjzlyan/dhi/internal/agentkit/org"
	"github.com/drjzlyan/dhi/internal/ansi"
	"github.com/drjzlyan/dhi/internal/tui/kit"
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
	// markAllRead bulk-marks a whole channel (top-level + threads) read
	// (F-017 bulk action, key R). nil = the action degrades by name.
	markAllRead func(ch string) error

	// F-022 agent-profile seam: the Model injects a renderer.
	profile func(id string) []string
	// agentWorking feeds presence dots and the live status line (F-072);
	// nil renders neither.
	agentWorking func(id string) bool

	channels   []string
	active     int
	events     chan struct{}
	subCancel  func() // cancels the current channel subscription
	subscribed string // channel currently subscribed

	focus    bool   // composer focused
	flash    string // post-failure notice for the chrome bar (F-026 P3)
	input    []rune
	cursor   int   // selected message index (blurred navigation)
	threadID int64 // 0 = channel view; else thread filter

	railFocus bool   // tab toggles rail navigation (wide layout)
	railCur   int    // rail highlight (wide layout)
	profileID string // agent profile pane ("" = closed)
	lastWidth int    // updated per render; keys are width-aware

	// F-035 Slack depth: reactions/edits/pins over the immutable bus.
	meta      *channelmeta.Store
	searching bool
	search    string
	reactPick bool
	editID    int64 // message being edited (0 = compose new)

	// F-072 find by name: the rail filter (rail focus, "/") and the
	// quick switcher (ctrl+g) over every channel and DM.
	railSearching bool
	railQuery     string
	switcher      *switcherState
}

// reactionTokens is the fixed, width-safe reaction set (no emoji-width
// surprises in a terminal).
var reactionTokens = []string{"+1", "ok", "?", "!"}

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
// capturing reports whether plain keys are text for the pane: the
// composer, the message search, or the reaction picker ("?" is a token).
func (p *chatPane) capturing() bool {
	return p.focus || p.searching || p.reactPick || p.railSearching || p.switcher != nil
}

func (p *chatPane) hints() []string {
	if p.switcher != nil {
		return []string{"type to filter", "↑/↓ select", "enter open", "esc close"}
	}
	if p.railSearching {
		return []string{"type to filter", "enter open", "esc clear"}
	}
	if p.focus {
		return []string{"enter send", "esc blur"}
	}
	if p.railFocus {
		return []string{"j/k select", "enter open", "/ find", "ctrl+g jump", "v profile", "tab transcript"}
	}
	if p.profileID != "" {
		return []string{"esc close", "i compose", "j/k select", ",/. channel"}
	}
	if p.threadID != 0 {
		return []string{"i reply in thread", "esc close"}
	}
	// tab is width-gated (the rail is a column only on wide floors) —
	// never advertise an inert key (F-026 P3).
	base := []string{"i compose", "j/k select", "t thread", "v profile",
		"/ search", "+ react", "e edit", "p pin", "R all read", ",/. channel", "ctrl+g jump"}
	if p.lastWidth >= slackCtxMin {
		return append([]string{"tab rail"}, base...)
	}
	return base
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
	p.railCur = p.railIndexOf(p.channels[i])
	p.resubscribe()
	p.markChannelRead(p.channelName())
}

func (p *chatPane) clampRail() {
	if n := len(p.railList()); p.railCur >= n {
		p.railCur = maxInt(n-1, 0)
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

// markAllReadNow bulk-marks the active channel (top-level + every thread)
// fully read via the injected store seam (F-017 bulk action, key R).
func (p *chatPane) markAllReadNow() {
	ch := p.channelName()
	if ch == "" {
		return
	}
	if p.markAllRead == nil {
		p.flash = "read marks unavailable"
		return
	}
	if err := p.markAllRead(ch); err != nil {
		p.flash = err.Error()
		return
	}
	p.flash = "marked " + ch + " read"
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
	if p.switcher != nil {
		p.switcherKey(key)
		return true
	}
	if key == "ctrl+g" {
		p.openSwitcher()
		return true
	}
	if p.railSearching {
		p.railSearchKey(key)
		return true
	}
	if p.searching {
		switch key {
		case "esc":
			p.searching = false
			p.search = ""
		case "enter":
			p.searching = false
		case "backspace":
			if len(p.search) > 0 {
				p.search = p.search[:len(p.search)-1]
			}
		default:
			if r := []rune(key); len(r) == 1 && r[0] >= 32 {
				p.search += key
			}
		}
		p.cursor = 0
		return true
	}
	if p.reactPick {
		if key == "esc" {
			p.reactPick = false
			return true
		}
		if n, err := strconv.Atoi(key); err == nil && n >= 1 && n <= len(reactionTokens) {
			history := p.navMsgs(p.lastWidth >= slackCtxMin)
			if p.cursor < len(history) && p.meta != nil {
				if err := p.meta.ToggleReaction(p.channelName(), history[p.cursor].ID, reactionTokens[n-1]); err != nil {
					p.flash = err.Error()
				}
			}
			p.reactPick = false
			return true
		}
		return true
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
			if p.railCur < len(p.railList())-1 {
				p.railCur++
			}
			return true
		case "k", "up":
			if p.railCur > 0 {
				p.railCur--
			}
			return true
		case "enter":
			p.openNamed(p.channelAt(p.railCur))
			return true
		case "i":
			p.openNamed(p.channelAt(p.railCur))
			p.focus = true
			return true
		case "/":
			p.railSearching = true
			p.railQuery = ""
			p.railCur = 0
			return true
		case "esc":
			if p.railQuery != "" {
				p.railQuery = ""
				p.clampRail()
				return true
			}
			p.railFocus = false
			return true
		case "v":
			if ch := p.channelAt(p.railCur); bus.IsDM(ch) {
				p.profileID = bus.ChannelLabel(ch)
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
	case "/":
		p.searching = true
		return true
	case "+":
		if p.meta == nil {
			p.flash = "channel metadata unavailable"
			return true
		}
		if p.cursor < len(history) {
			p.reactPick = true
		}
		return true
	case "p":
		if p.meta == nil {
			p.flash = "channel metadata unavailable"
			return true
		}
		if p.cursor < len(history) {
			if err := p.meta.TogglePin(p.channelName(), history[p.cursor].ID); err != nil {
				p.flash = err.Error()
			}
		}
		return true
	case "e":
		if p.meta == nil {
			p.flash = "channel metadata unavailable"
			return true
		}
		if p.cursor < len(history) {
			m := history[p.cursor]
			if m.Author == bus.Human {
				p.editID = m.ID
				p.input = []rune(p.displayText(m))
				p.focus = true
			} else {
				p.flash = "only your own messages can be edited"
			}
		}
		return true
	case "R":
		p.markAllReadNow()
		return true
	case "i", "enter":
		p.focus = true
		return true
	}
	return false
}

// channelAt returns the channel at rail row i of the (filtered) rail
// ("" when out of range).
func (p *chatPane) channelAt(i int) string {
	if list := p.railList(); i >= 0 && i < len(list) {
		return list[i]
	}
	return ""
}

// openNamed opens a channel by id (no-op when it left the rail).
func (p *chatPane) openNamed(ch string) {
	for i, c := range p.channels {
		if c == ch {
			p.railQuery = ""
			p.openChannel(i)
			return
		}
	}
}

func (p *chatPane) composerKey(key string) bool {
	switch key {
	case "esc":
		p.focus = false
		if p.editID != 0 {
			p.editID = 0
			p.input = nil
		}
		return true
	case "enter":
		text := strings.TrimSpace(string(p.input))
		if p.editID != 0 {
			if text != "" && p.meta != nil {
				if err := p.meta.Edit(p.channelName(), p.editID, text); err != nil {
					p.flash = err.Error()
				}
			}
			p.editID = 0
			p.input = nil
			p.focus = false
			return true
		}
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
// @mentions (or DM addressees) trigger agent turns. Failures surface on
// the chrome bar (F-011: a dropped post is never silent — F-026 P3).
func (p *chatPane) post(text string) {
	posted, err := p.bus.Post(bus.Message{
		Channel: p.channelName(),
		Thread:  p.threadID,
		Author:  bus.Human,
		Text:    text,
	})
	if err != nil {
		p.flash = "post failed: " + err.Error()
		return
	}
	p.flash = ""
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
		return p.filterSearch(conversational(p.bus.History(p.channelName(), 0)))
	}
	var out []bus.Message
	for _, m := range p.bus.History(p.channelName(), 0) {
		if m.ID == p.threadID {
			out = append(out, m)
			break
		}
	}
	out = append(out, p.bus.History(p.channelName(), p.threadID)...)
	return p.filterSearch(conversational(out))
}

// filterSearch narrows history to messages matching the search text
// (case-insensitive over author + display text).
func (p *chatPane) filterSearch(msgs []bus.Message) []bus.Message {
	q := strings.ToLower(strings.TrimSpace(p.search))
	if q == "" {
		return msgs
	}
	out := make([]bus.Message, 0, len(msgs))
	for _, m := range msgs {
		if strings.Contains(strings.ToLower(m.Author+" "+p.displayText(m)), q) {
			out = append(out, m)
		}
	}
	return out
}

// displayText applies a human edit over the immutable bus text.
func (p *chatPane) displayText(m bus.Message) string {
	if p.meta == nil {
		return m.Text
	}
	if t, ok := p.meta.EditedText(m.Channel, m.ID); ok {
		return t
	}
	return m.Text
}

// navMsgs is the list the message cursor moves through: on wide screens
// with a thread pane open, the cursor keeps navigating the channel while
// replies render beside it; narrow screens drill inline.
func (p *chatPane) navMsgs(wide bool) []bus.Message {
	if wide && p.threadID != 0 {
		return p.filterSearch(conversational(p.bus.History(p.channelName(), 0)))
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
	ctxW := p.ctxWidth(width)
	transW := width - slackRailW - ctxW - 2 // the two separators
	if transW < 30 {
		transW = 30
	}
	rail := p.railLines(slackRailW, height)
	trans := p.transcriptLines(transW, height, true)
	ctx := p.contextLines(ctxW, height)

	rows := make([]string, 0, height)
	for y := 0; y < height; y++ {
		r := railLineAt(rail, y, slackRailW)
		tr := padToANSI(lineAt(trans, y, transW), transW)
		c := lineAt(ctx, y, ctxW)
		rows = append(rows, r+" "+tr+" "+c)
	}
	return rows
}

// ctxWidth sizes the right pane: an open thread or profile gets a third
// of the floor (code blocks need room), the closed pane stays slim.
func (p *chatPane) ctxWidth(width int) int {
	if p.threadID == 0 && p.profileID == "" {
		return slackCtxW
	}
	return min(max(width/3, slackCtxW), 64)
}

// renderNarrow keeps the pre-M11 single-column floor: horizontal rail,
// inline thread drill-down, composer.
func (p *chatPane) renderNarrow(width, height int) []string {
	var lines []string
	lines = append(lines, p.rail())
	lines = append(lines, theme.TextDim().Render(strings.Repeat("─", minInt(width, 56))))

	history := p.visibleHistory()
	header := p.header()
	if p.threadID != 0 {
		header += theme.Hint().Render("  · thread (c back)")
	}
	lines = append(lines, header)
	if p.switcher != nil {
		return append(lines, p.switcherLines(width, height-len(lines))...)
	}

	body := height - 5 // rail, rule, header, input/hint, blank
	if body < 3 {
		body = 3
	}
	wrap := width - 8
	if wrap < 20 {
		wrap = 20
	}

	if bar := p.searchBar(); bar != "" {
		lines = append(lines, bar)
		body--
	}
	if bar := p.reactBar(); bar != "" {
		lines = append(lines, bar)
		body--
	}
	if body < 3 {
		body = 3
	}

	lines = append(lines, p.transcriptRender(history, wrap, body)...)

	lines = append(lines, p.statusLine())
	lines = append(lines, p.composerBar(width))
	return lines
}

// transcriptLines renders the center column: header, messages, composer.
// wide selects the width-aware message list (channel stays visible while
// a thread pane is open).
func (p *chatPane) transcriptLines(width, height int, wide bool) []string {
	history := p.navMsgs(wide)
	var lines []string
	header := p.header()
	if n := p.pinnedCount(); n > 0 {
		header += theme.TextDim().Render("  · " + itoa(n) + " pinned")
	}
	lines = append(lines, header)
	if p.switcher != nil {
		return append(lines, p.switcherLines(width, height-1)...)
	}
	extra := 0
	if bar := p.searchBar(); bar != "" {
		lines = append(lines, bar)
		extra++
	}
	if bar := p.reactBar(); bar != "" {
		lines = append(lines, bar)
		extra++
	}
	lines = append(lines, p.transcriptRender(history, width, height-3-extra)...)
	lines = append(lines, p.statusLine())
	lines = append(lines, p.composerBar(width))
	return lines
}

// pinnedCount is the number of pinned messages in the active channel.
func (p *chatPane) pinnedCount() int {
	if p.meta == nil {
		return 0
	}
	return len(p.meta.Pinned(p.channelName()))
}

// composerRow is always rendered (F-026 P3): focused it owns the caret;
// blurred it stays as the quiet typing affordance instead of vanishing.
func (p *chatPane) composerRow() string {
	if p.focus {
		return theme.TabActive().Render("> "+string(p.input)) + "▌"
	}
	return theme.TextMuted().Render("> " + string(p.input) + "  (i to type)")
}

// composerBar paints the composer as an input bar across width (F-064):
// the header shade while idle, the selection shade with an accent edge
// while typing.
func (p *chatPane) composerBar(width int) string {
	if p.focus {
		return kit.PaintRow(theme.AccentText().Render(theme.GlyphCursor)+p.composerRow(), width,
			lipgloss.NewStyle().Background(theme.Current.BgSelection))
	}
	return kit.PaintRow(" "+p.composerRow(), width, theme.HeaderBg())
}

// transcriptRender renders the navigable message list through the
// shared kit.Transcript (F-026 P3): day dividers, HH:MM stamps, thread
// tags, word wrap, and a tail window that follows the message cursor.
// Empty history renders the named empty state (caller's header aside).
func (p *chatPane) transcriptRender(history []bus.Message, width, body int) []string {
	if len(history) == 0 {
		if ch := p.channelName(); bus.IsDM(ch) {
			return []string{theme.TextDim().Render(
				"(no messages yet — press i to message " + bus.ChannelLabel(ch) + " directly)")}
		}
		return []string{theme.TextDim().Render(
			"(no messages yet — press i and say hi, @mention an agent)")}
	}
	if p.cursor >= len(history) {
		p.cursor = maxInt(len(history)-1, 0)
	}
	tr := &kit.Transcript{Width: width, Tail: body, Markdown: kit.ChatMarkdown}
	if !p.focus {
		tr.CursorAt = p.cursor
	}
	for _, msg := range history {
		row := kit.TrnRow{
			Author:   msg.Author,
			Text:     p.displayText(msg),
			Suffix:   p.rowSuffix(msg),
			Thread:   msg.Thread != 0,
			At:       msg.At,
			Kind:     kit.TrnAgent,
			Markdown: true,
		}
		switch {
		case msg.Author == bus.Human:
			row.Kind = kit.TrnHuman
		case msg.Kind == bus.KindAlert:
			row.Kind = kit.TrnAlert
		}
		tr.Rows = append(tr.Rows, row)
	}
	return tr.View()
}

// rowSuffix is the F-035 metadata under a message: a pin marker, an
// edit note and reaction chips (kept out of the body so markdown never
// parses it), plus a reply count on a top-level message with a thread.
func (p *chatPane) rowSuffix(msg bus.Message) string {
	var parts []string
	if p.meta != nil {
		if p.meta.IsPinned(msg.Channel, msg.ID) {
			parts = append(parts, theme.WarningText().Render("pinned"))
		}
		if _, edited := p.meta.EditedText(msg.Channel, msg.ID); edited {
			parts = append(parts, theme.TextDim().Render("(edited)"))
		}
		if rx := p.meta.Reactions(msg.Channel, msg.ID); len(rx) > 0 {
			parts = append(parts, theme.Hint().Render("["+strings.Join(rx, " ")+"]"))
		}
	}
	if msg.Thread == 0 && p.bus != nil {
		if n := p.bus.ReplyCount(msg.Channel, msg.ID); n > 0 {
			word := " replies"
			if n == 1 {
				word = " reply"
			}
			parts = append(parts, theme.AccentText().Render("↳ "+itoa(n)+word))
		}
	}
	return strings.Join(parts, " ")
}

// searchBar renders the active search/filter state ("" when idle).
func (p *chatPane) searchBar() string {
	if p.searching {
		return theme.TabActive().Render("/ "+p.search) + "▏" +
			theme.Hint().Render("  enter keep · esc clear")
	}
	if p.search != "" {
		return theme.Hint().Render("search \""+p.search+"\"") +
			theme.TextDim().Render("  (/ to change · esc clears)")
	}
	return ""
}

// reactBar renders the reaction picker ("" when idle).
func (p *chatPane) reactBar() string {
	if !p.reactPick {
		return ""
	}
	var parts []string
	for i, tok := range reactionTokens {
		parts = append(parts, theme.Keycap().Render(itoa(i+1)+" "+tok))
	}
	return theme.Hint().Render("react: ") + strings.Join(parts, " ")
}

// contextLines renders the right pane: the open thread beside the
// transcript, or an agent profile; blank when closed.
func (p *chatPane) contextLines(width, height int) []string {
	bg := theme.ElevatedBg()
	inset := func(s string) string { return kit.PaintRow(s, width, bg) }
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
		body = append(body, p.threadLines(width)...)
	default:
		// The closed side pane is not a dead zone (F-026 P3): it names
		// what opens it, then quiet blank rows on the elevated shade.
		out := []string{
			blank(),
			blank(),
			inset(theme.TextMuted().Render(" " + theme.GlyphSpark + " no thread open")),
			inset(theme.TextMuted().Render("   t thread · v profile")),
		}
		for len(out) < height {
			out = append(out, blank())
		}
		return out[:height]
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
