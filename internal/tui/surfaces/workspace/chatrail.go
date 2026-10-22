package workspace

import (
	"charm.land/lipgloss/v2"
	"sort"
	"strings"

	"github.com/drjzlyan/dhi/internal/agentkit/bus"
	"github.com/drjzlyan/dhi/internal/ansi"
	"github.com/drjzlyan/dhi/internal/fuzzy"
	"github.com/drjzlyan/dhi/internal/tui/kit"
	"github.com/drjzlyan/dhi/internal/tui/theme"
)

// F-072: the channel rail reads like Slack's sidebar — CHANNELS, then
// DIRECT MESSAGES by agent name with a presence dot — and every channel
// is reachable by name (the rail filter and the ctrl+g switcher).

// conversational drops stream lines (tool calls, CLI progress, routine
// notices): chat shows what people and agents said, the trace the rest.
func conversational(msgs []bus.Message) []bus.Message {
	out := make([]bus.Message, 0, len(msgs))
	for _, m := range msgs {
		if bus.Conversational(m) {
			out = append(out, m)
		}
	}
	return out
}

// railList is the rail in display order: every channel, or the ones the
// rail filter matches (group order kept so headers stay meaningful).
func (p *chatPane) railList() []string {
	q := strings.TrimSpace(p.railQuery)
	if q == "" {
		return p.channels
	}
	var out []string
	for _, ch := range p.channels {
		if _, ok := fuzzy.Match(strings.TrimPrefix(q, "#"), matchName(ch)); ok {
			out = append(out, ch)
		}
	}
	return out
}

// matchName is what a name search sees: the label without its "#".
func matchName(ch string) string { return strings.TrimPrefix(bus.ChannelLabel(ch), "#") }

func (p *chatPane) railIndexOf(ch string) int {
	for i, c := range p.railList() {
		if c == ch {
			return i
		}
	}
	return 0
}

func (p *chatPane) railSearchKey(key string) {
	switch key {
	case "esc":
		p.railSearching = false
		p.railQuery = ""
	case "enter":
		p.railSearching = false
		if ch := p.channelAt(p.railCur); ch != "" {
			p.openNamed(ch)
			p.railFocus = true // stay in the sidebar after a jump
		}
		return
	case "down", "ctrl+n":
		if p.railCur < len(p.railList())-1 {
			p.railCur++
		}
		return
	case "up", "ctrl+k":
		if p.railCur > 0 {
			p.railCur--
		}
		return
	case "backspace":
		if r := []rune(p.railQuery); len(r) > 0 {
			p.railQuery = string(r[:len(r)-1])
		}
	default:
		if r := []rune(key); len(r) == 1 && r[0] >= 32 {
			p.railQuery += key
		}
	}
	p.railCur = 0
}

// presence is the DM row's dot: a filled success dot while the agent is
// working, a quiet ring otherwise ("" when presence is unknown).
func (p *chatPane) presence(ch string) string {
	if !bus.IsDM(ch) || p.agentWorking == nil {
		return ""
	}
	if p.agentWorking(bus.ChannelLabel(ch)) {
		return theme.SuccessText().Render("●")
	}
	return theme.TextMuted().Render("○")
}

func (p *chatPane) unreadBadge(ch string) string {
	switch n := p.unreadCount(ch); {
	case n == 1:
		return " " + theme.DangerText().Render(theme.GlyphDot)
	case n > 1:
		return " " + theme.DangerText().Render(theme.GlyphDot+itoa(n))
	}
	return ""
}

// railLines renders the vertical sidebar: the filter row when active,
// the CHANNELS and DIRECT MESSAGES groups, presence and unread markers,
// the rail cursor and the open channel's highlight.
func (p *chatPane) railLines(width, height int) []string {
	bg := theme.InsetBg()
	inset := func(s string) string { return kit.PaintRow(s, width, bg) }
	var lines []string
	if p.railSearching || p.railQuery != "" {
		q := theme.TabActive().Render("/" + p.railQuery)
		if p.railSearching {
			q += "▏"
		}
		lines = append(lines, inset(" "+q))
	}
	list := p.railList()
	group := ""
	for i, ch := range list {
		g := "CHANNELS"
		if bus.IsDM(ch) {
			g = "DIRECT MESSAGES"
		}
		if g != group {
			if group != "" {
				lines = append(lines, inset(""))
			}
			lines = append(lines, inset(" "+theme.SectionHeader().Render(g)))
			group = g
		}
		lines = append(lines, inset(p.railRow(i, ch, width)))
	}
	if len(list) == 0 {
		lines = append(lines, inset(theme.TextMuted().Render("  no match")))
	}
	for len(lines) < height {
		lines = append(lines, inset(""))
	}
	return lines[:height]
}

func (p *chatPane) railRow(i int, ch string, width int) string {
	lead := "  "
	if i == p.railCur && (p.railFocus || p.railSearching) {
		lead = theme.AccentText().Render(theme.GlyphCursor) + " "
	}
	dot := p.presence(ch)
	if dot != "" {
		dot += " "
	}
	badge := p.unreadBadge(ch)
	room := width - ansi.Width(lead) - ansi.Width(dot) - ansi.Width(badge) - 1
	name := kit.ClipEllipsis(bus.ChannelLabel(ch), max(room, 2))
	switch {
	case ch == p.channelName():
		name = theme.TabActive().Render(name)
	case p.unreadCount(ch) > 0:
		name = theme.TextStyle().Bold(true).Render(name)
	default:
		name = theme.TextDim().Render(name)
	}
	return lead + dot + name + badge
}

// rail is the narrow floor's one-line channel strip.
func (p *chatPane) rail() string {
	var parts []string
	for i, ch := range p.channels {
		label := bus.ChannelLabel(ch) + p.unreadBadge(ch)
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

// header names the open conversation: "#team" or the agent's name with
// its presence ("scout · working").
func (p *chatPane) header() string {
	ch := p.channelName()
	h := theme.Brand().Render(bus.ChannelLabel(ch))
	if bus.IsDM(ch) && p.agentWorking != nil && p.agentWorking(bus.ChannelLabel(ch)) {
		h += theme.SuccessText().Render("  ● working")
	}
	return h
}

// ---- quick switcher (ctrl+g) ----

type switcherState struct {
	query string
	cur   int
}

func (p *chatPane) openSwitcher() {
	p.switcher = &switcherState{}
	p.focus = false
}

// switcherMatches ranks every channel and DM by the query; an empty
// query lists unread conversations first, then the rail order.
func (p *chatPane) switcherMatches() []string {
	if p.switcher == nil {
		return nil
	}
	q := strings.TrimPrefix(strings.TrimSpace(p.switcher.query), "#")
	if q == "" {
		out := append([]string(nil), p.channels...)
		sort.SliceStable(out, func(i, j int) bool {
			return p.unreadCount(out[i]) > 0 && p.unreadCount(out[j]) == 0
		})
		return out
	}
	names := make([]string, len(p.channels))
	for i, ch := range p.channels {
		names[i] = matchName(ch)
	}
	var out []string
	for _, r := range fuzzy.Rank(q, names) {
		out = append(out, p.channels[r.Index])
	}
	return out
}

func (p *chatPane) switcherKey(key string) {
	s := p.switcher
	matches := p.switcherMatches()
	switch key {
	case "esc", "ctrl+g":
		p.switcher = nil
		return
	case "enter":
		p.switcher = nil
		if s.cur < len(matches) {
			p.openNamed(matches[s.cur])
		}
		return
	case "down", "ctrl+n", "tab":
		if s.cur < len(matches)-1 {
			s.cur++
		}
		return
	case "up", "ctrl+k", "shift+tab":
		if s.cur > 0 {
			s.cur--
		}
		return
	case "backspace":
		if r := []rune(s.query); len(r) > 0 {
			s.query = string(r[:len(r)-1])
		}
	default:
		if r := []rune(key); len(r) == 1 && r[0] >= 32 {
			s.query += key
		}
	}
	s.cur = 0
}

// switcherLines renders the switcher in place of the transcript body:
// a query row, then ranked rows with kind, presence and unread.
func (p *chatPane) switcherLines(width, height int) []string {
	s := p.switcher
	lines := []string{
		theme.SectionHeader().Render("JUMP TO A CHANNEL OR DM"),
		kit.PaintRow(" "+theme.AccentText().Render("›")+" "+theme.TextStyle().Render(s.query)+"▏", width, theme.HeaderBg()),
	}
	matches := p.switcherMatches()
	if len(matches) == 0 {
		return append(lines, theme.TextMuted().Render("  no channel or DM matches \""+s.query+"\""))
	}
	room := max(height-len(lines), 1)
	from := 0
	if s.cur >= room {
		from = s.cur - room + 1
	}
	for i := from; i < len(matches) && i < from+room; i++ {
		ch := matches[i]
		kind := "channel"
		if bus.IsDM(ch) {
			kind = "direct message"
		}
		dot := p.presence(ch)
		if dot != "" {
			dot += " "
		}
		row := dot + bus.ChannelLabel(ch) + p.unreadBadge(ch) + "  " + theme.TextMuted().Render(kind)
		if i == s.cur {
			lines = append(lines, kit.PaintRow(theme.AccentText().Render(theme.GlyphCursor)+" "+row, width, theme.SelectionBg()))
		} else {
			lines = append(lines, "  "+row)
		}
	}
	return lines
}

// ---- live status line ----

// statusLine is the typing indicator above the composer (F-072): which
// agents are working on the open conversation and what they are doing,
// from each one's newest stream line. "" when nobody is.
func (p *chatPane) statusLine() string {
	if p.agentWorking == nil || p.bus == nil {
		return ""
	}
	var msgs []bus.Message
	if p.threadID != 0 {
		msgs = p.bus.History(p.channelName(), p.threadID)
	} else {
		msgs = p.bus.History(p.channelName(), 0)
	}
	latest := map[string]string{} // author → newest stream line since its last message
	var order []string
	for _, m := range msgs {
		if m.Author == bus.Human {
			continue
		}
		if bus.Conversational(m) {
			delete(latest, m.Author)
			continue
		}
		if _, seen := latest[m.Author]; !seen {
			order = append(order, m.Author)
		}
		latest[m.Author] = m.Text
	}
	var parts []string
	for _, a := range order {
		line, ok := latest[a]
		if !ok || !p.agentWorking(a) {
			continue
		}
		parts = append(parts, a+" is "+bus.StatusPhrase(line))
	}
	if len(parts) == 0 {
		// A turn that has not streamed yet still shows in a DM.
		if ch := p.channelName(); bus.IsDM(ch) && p.agentWorking(bus.ChannelLabel(ch)) {
			parts = append(parts, bus.ChannelLabel(ch)+" is thinking")
		} else {
			return ""
		}
	}
	return theme.TextMuted().Italic(true).Render(" ◌ " + strings.Join(parts, " · ") + "…")
}

// threadLines renders the open thread in the side pane: each message as
// an author/time header over its formatted body (markdown, code blocks),
// with a blank row between messages so replies read as a conversation.
func (p *chatPane) threadLines(width int) []string {
	out := []string{" " + theme.Brand().Render("Thread"), ""}
	textW := max(width-3, 8)
	for i, msg := range p.visibleHistory() {
		if i > 0 {
			out = append(out, "")
		}
		st := theme.AccentText().Bold(true)
		if msg.Author != bus.Human {
			st = lipgloss.NewStyle().Foreground(theme.AuthorColor(msg.Author)).Bold(true)
		}
		head := " " + st.Render(msg.Author)
		if !msg.At.IsZero() {
			head += theme.TextMuted().Render("  " + msg.At.Format("15:04"))
		}
		out = append(out, head)
		text := p.displayText(msg)
		var lines []string
		if md := kit.ChatMarkdown(text, textW); md != "" {
			lines = strings.Split(md, "\n")
		} else {
			body := theme.TextStyle()
			if msg.Kind == bus.KindAlert {
				body = theme.WarningText()
			}
			for _, l := range kit.WrapWords(text, textW) {
				lines = append(lines, body.Render(l))
			}
		}
		for _, l := range lines {
			out = append(out, "  "+l)
		}
		if sfx := p.rowSuffix(msg); sfx != "" && msg.Thread != 0 {
			out = append(out, "  "+sfx)
		}
	}
	return out
}
