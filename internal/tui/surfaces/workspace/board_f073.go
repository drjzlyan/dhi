package workspace

import (
	"fmt"
	"path/filepath"
	"strings"
	"time"

	"charm.land/lipgloss/v2"

	"github.com/drjzlyan/dhi/internal/agentkit/bus"
	"github.com/drjzlyan/dhi/internal/agentkit/org"
	"github.com/drjzlyan/dhi/internal/tasks"
	"github.com/drjzlyan/dhi/internal/tui/kit"
	"github.com/drjzlyan/dhi/internal/tui/theme"
)

// F-073: board cards say who is working, a card opens as a page with
// every comment formatted, and work moves between people and agents by
// picking an assignee — or by taking the card yourself.

// threadCanceller is the runtime's optional turn-stop seam
// (*runtime.Runtime satisfies it).
type threadCanceller interface {
	CancelThread(channel string, thread int64) int
}

// cardWorking reports a live turn on the card's thread.
func (m *Model) cardWorking(tk tasks.Task) bool {
	return m.working != nil && tk.ThreadChannel != "" && m.working(tk.ThreadChannel, tk.ThreadID)
}

// assigneeChip names who owns a card: a working agent with a filled
// dot, an idle one with a ring, you in the accent, unassigned muted.
func assigneeChip(who string, working bool) string {
	switch who {
	case "":
		return theme.TextMuted().Render("unassigned")
	case bus.Human:
		return theme.AccentText().Bold(true).Render("you")
	}
	name := lipgloss.NewStyle().Foreground(theme.AuthorColor(who)).Bold(true).Render(who)
	if working {
		return theme.SuccessText().Render("●") + " " + name
	}
	return theme.TextMuted().Render("○") + " " + name
}

// cardBadges are the quiet state glyphs after the assignee: tests,
// PR, comment count.
func cardBadges(tk tasks.Task) string {
	var parts []string
	if tk.TestsPass {
		parts = append(parts, theme.SuccessText().Render(theme.GlyphCheck))
	}
	if tk.PRNumber > 0 {
		parts = append(parts, theme.InfoText().Render(fmt.Sprintf("#%d", tk.PRNumber)))
	}
	if n := len(tk.Comments); n > 0 {
		parts = append(parts, theme.TextDim().Render(fmt.Sprintf("↳%d", n)))
	}
	return strings.Join(parts, " ")
}

// boardCard renders one lane row (two lines, F-064): the title with
// mark/priority/working glyphs, then who owns it, its state badges and
// the slug — the owner first, because "who is on this" is the question
// a board answers.
func boardCard(tk tasks.Task, laneW int, marked, working bool) string {
	title := tk.Title
	if title == "" {
		title = tk.Slug
	}
	prefix, prefixW := "", 0
	if laneW >= 14 {
		prefix, prefixW = boardMarkPrefix(tk.Priority, marked, working), 3
	}
	avail := laneW - prefixW - 1 // one cell of air before the next lane
	top := prefix + kit.ClipEllipsis(title, maxInt(avail, 4))

	meta := assigneeChip(tk.Assignee, working)
	if b := cardBadges(tk); b != "" {
		meta += "  " + b
	}
	meta += theme.TextMuted().Render("  " + tk.Slug)
	bottom := strings.Repeat(" ", prefixW) + kit.ClipEllipsis(meta, maxInt(avail, 4))
	return top + "\n" + bottom
}

// ---- card page ----

// cardPage is the open card (enter on the board): one scrollable page.
type cardPage struct {
	slug   string
	scroll int
}

func (m *Model) openCardPage(tk tasks.Task) {
	m.card = &cardPage{slug: tk.Slug}
}

// cardPageKey handles keys while a card page is open; keys it does not
// own fall through to the board's card actions (N, a, T, o, r, s, …).
func (m *Model) cardPageKey(key string) bool {
	switch key {
	case "esc", "q":
		m.card = nil
		return true
	case "j", "down":
		m.card.scroll++
		return true
	case "k", "up":
		m.card.scroll = maxInt(m.card.scroll-1, 0)
		return true
	case "g":
		m.card.scroll = 0
		return true
	case "G", "end":
		m.card.scroll = 1 << 20 // clamped at render
		return true
	case "h", "l", "left", "right", "enter", " ", "M", "/", "f":
		return true // lane navigation and bulk keys belong to the board
	}
	return m.boardKey(key)
}

// cardPageBody renders the open card as sections, scrolled to fit h.
func (m *Model) cardPageBody(w, h int) string {
	tk, ok := m.taskStore.Get(m.card.slug)
	if !ok {
		m.card = nil
		return m.boardBody(w, h)
	}
	lines := m.cardPageLines(tk, w)
	maxScroll := maxInt(len(lines)-h, 0)
	m.card.scroll = clampInt(m.card.scroll, 0, maxScroll)
	lines = lines[m.card.scroll:]
	if len(lines) > h {
		lines = lines[:h]
	}
	return strings.Join(lines, "\n")
}

func (m *Model) cardPageLines(tk tasks.Task, w int) []string {
	textW := clampInt(w-4, 20, 100)
	section := func(title string) []string {
		return []string{"", theme.SectionHeader().Render(title)}
	}
	var out []string
	title := tk.Title
	if title == "" {
		title = tk.Slug
	}
	for _, l := range kit.WrapWords(title, textW) {
		out = append(out, theme.TextStyle().Bold(true).Render(l))
	}
	head := theme.Chip().Render(string(tk.Status)) + "  " + theme.TextMuted().Render(tk.Slug)
	if meta := boardMetaLine(tk); meta != "" {
		head += theme.TextDim().Render("  · " + meta)
	}
	out = append(out, head)

	working := m.cardWorking(tk)
	out = append(out, section("ASSIGNEE")...)
	who := "  " + assigneeChip(tk.Assignee, working)
	switch {
	case tk.Assignee == "":
		who += theme.Hint().Render("  a assign · T take it")
	case working:
		who += theme.SuccessText().Render("  working on it now")
	case tk.Assignee != bus.Human:
		who += theme.TextDim().Render("  idle")
	}
	if tk.Team != "" {
		who += theme.TextDim().Render("  · team " + tk.Team)
	}
	out = append(out, who)
	for _, a := range assignHistory(tk, 3) {
		out = append(out, "  "+theme.TextDim().Render(activityText(a)+" · "+timeAgo(a.At, m.now())))
	}

	out = append(out, section("WORK")...)
	if len(tk.ChangeSets) == 0 {
		out = append(out, "  "+theme.TextDim().Render("no worktree yet")+theme.Hint().Render("  w attach · T take it"))
	}
	for _, cs := range tk.ChangeSets {
		out = append(out, "  "+kit.Pill(cs.Branch, theme.Current.TextDim)+" "+theme.TextDim().Render(cs.Member+" · "+cs.Path))
	}
	if tk.PRNumber > 0 {
		pr := fmt.Sprintf("PR #%d", tk.PRNumber)
		if tk.PRURL != "" {
			pr += " " + tk.PRURL
		}
		out = append(out, "  "+theme.InfoText().Render(pr))
	}
	tests := theme.TextDim().Render("tests not passed yet")
	if tk.TestsPass {
		tests = theme.SuccessText().Render(theme.GlyphCheck + " tests pass")
	}
	runs := ""
	if len(tk.Runs) > 0 {
		runs = theme.TextDim().Render(fmt.Sprintf(" · %d runs · %s", len(tk.Runs), tasks.RollupRuns(tk.Runs).CostText())) +
			theme.Hint().Render("  r runs")
	}
	out = append(out, "  "+tests+runs)
	if ep := m.episodeLines(tk, textW); len(ep) > 0 {
		out = append(out, "")
		out = append(out, ep...)
	}

	out = append(out, section(fmt.Sprintf("COMMENTS (%d)", len(tk.Comments)))...)
	if len(tk.Comments) == 0 {
		out = append(out, "  "+theme.TextDim().Render("No comments yet.")+theme.Hint().Render("  N add one"))
	}
	for i, c := range tk.Comments {
		if i > 0 {
			out = append(out, "")
		}
		out = append(out, commentLines(c, textW, m.now())...)
	}

	if n := len(tk.Activity); n > 0 {
		out = append(out, section("ACTIVITY")...)
		for _, a := range tk.Activity[maxInt(n-10, 0):] {
			out = append(out, "  "+theme.TextDim().Render(activityText(a)+" · "+timeAgo(a.At, m.now())))
		}
	}
	return out
}

// commentLines renders one comment: author and age, then the body with
// markdown formatting (code blocks, lists, inline code) indented under.
func commentLines(c tasks.Comment, textW int, now time.Time) []string {
	st := theme.AccentText().Bold(true)
	if c.Author != bus.Human {
		st = lipgloss.NewStyle().Foreground(theme.AuthorColor(c.Author)).Bold(true)
	}
	out := []string{"  " + st.Render(c.Author) + "  " + theme.TextMuted().Render(timeAgo(c.At, now))}
	body := kit.ChatMarkdown(c.Text, textW-2)
	if body == "" {
		var ls []string
		for _, l := range kit.WrapWords(c.Text, textW-2) {
			ls = append(ls, theme.TextStyle().Render(l))
		}
		body = strings.Join(ls, "\n")
	}
	for _, l := range strings.Split(body, "\n") {
		out = append(out, "    "+l)
	}
	return out
}

// assignHistory is the card's newest assignee changes, oldest first.
func assignHistory(tk tasks.Task, n int) []tasks.Activity {
	var out []tasks.Activity
	for _, a := range tk.Activity {
		if a.Kind == tasks.ActAssignee {
			out = append(out, a)
		}
	}
	return out[maxInt(len(out)-n, 0):]
}

// ---- assignee picker ----

type assignPicker struct {
	slug string
	pal  *kit.Palette
}

// openAssignPicker lists you, every rostered agent (role, busy) and
// unassigned; typing filters.
func (m *Model) openAssignPicker(tk tasks.Task) {
	items := []kit.PaletteItem{{Title: bus.Human, Hint: "take it yourself", Data: bus.Human}}
	if roster, err := org.LoadRoster(m.ws); err == nil {
		for _, a := range roster {
			hint := a.Role
			if m.agentWorking != nil && m.agentWorking(a.ID) {
				hint = strings.TrimSpace(hint + " · busy")
			}
			if a.ID == tk.Assignee {
				hint = strings.TrimSpace(hint + " · current")
			}
			items = append(items, kit.PaletteItem{Group: "agent", Title: a.ID, Hint: hint, Data: a.ID})
		}
	}
	items = append(items, kit.PaletteItem{Title: "unassigned", Hint: "nobody", Data: ""})
	p := kit.NewPalette(items)
	p.Title, p.Noun = "assign "+tk.Slug, "people"
	m.assign = &assignPicker{slug: tk.Slug, pal: p}
}

func (m *Model) assignPickerKey(key string) bool {
	slug := m.assign.slug
	// A name nobody on the roster has is still an assignee (a teammate
	// outside DHI): enter with no match assigns the typed name.
	if q := strings.TrimSpace(m.assign.pal.Query()); key == "enter" && q != "" && len(m.assign.pal.Matches()) == 0 {
		m.assign = nil
		m.reassign(slug, q)
		return true
	}
	picked, closed := m.assign.pal.HandleKey(key)
	if closed {
		m.assign = nil
	}
	if picked != nil {
		who, _ := picked.Data.(string)
		m.reassign(slug, who)
	}
	return true
}

// reassign moves a card to who. Moving it off a busy agent stops that
// agent's turn first; moving it to an agent hands the work off with the
// card's worktree named, so the new owner continues where it stands.
func (m *Model) reassign(slug, who string) {
	if m.taskStore == nil {
		m.flashErr("task store unavailable")
		return
	}
	tk, ok := m.taskStore.Get(slug)
	if !ok {
		m.flashErr("card " + slug + " is gone")
		return
	}
	if tk.Assignee == who {
		return
	}
	prev := tk.Assignee
	if prev != "" && prev != bus.Human && tk.ThreadChannel != "" {
		if c, ok := m.rt.(threadCanceller); ok && c.CancelThread(tk.ThreadChannel, tk.ThreadID) > 0 {
			_ = m.taskStore.AddComment(slug, bus.Human, fmt.Sprintf("Stopped %s's turn: the card moved to %s.", prev, orWord(who, "nobody")))
		}
	}
	if err := m.taskStore.AssignAs(slug, who, bus.Human); err != nil {
		m.flashErr(err.Error())
		return
	}
	if who == bus.Human {
		m.flashOK("assigned " + slug + " to you · T opens it in the editor")
		return
	}
	if tk, ok := m.taskStore.Get(slug); ok {
		m.handoffFrom(tk, prev)
	}
}

func orWord(s, alt string) string {
	if s == "" {
		return alt
	}
	return s
}

// continuationBrief tells a new owner where the earlier work is.
func continuationBrief(tk tasks.Task, prev string) string {
	if prev == "" && len(tk.ChangeSets) == 0 {
		return ""
	}
	var b strings.Builder
	if prev != "" {
		fmt.Fprintf(&b, " You are picking this up from %s.", prev)
	}
	for _, cs := range tk.ChangeSets {
		fmt.Fprintf(&b, " Continue in the existing worktree %s on branch %s: its commits and uncommitted changes are the work so far.", cs.Path, cs.Branch)
	}
	if len(tk.Comments) > 0 {
		b.WriteString(" Read the card's comments first.")
	}
	return b.String()
}

// ---- take it ----

// takeCard assigns the card to you, gives it a worktree when it has
// none, moves it to active and opens the worktree in the editor.
func (m *Model) takeCard(tk tasks.Task) {
	if m.taskStore == nil {
		m.flashErr("task store unavailable")
		return
	}
	if tk.Assignee != bus.Human {
		m.reassign(tk.Slug, bus.Human)
	}
	if tk.Status == tasks.Backlog {
		_ = m.taskStore.SetStatusAs(tk.Slug, tasks.Active, bus.Human)
		m.followCardIntoLane(tk.Slug)
	}
	if len(tk.ChangeSets) > 0 {
		m.openTakenWorktree(tk.Slug)
		return
	}
	members := m.ws.Members()
	branch := m.branchFor(tk.Slug)
	if len(members) == 1 {
		if err := m.taskStore.Attach(tk.Slug, members[0].Name, branch, ""); err != nil {
			m.flashErr("take: " + err.Error())
			return
		}
		m.openTakenWorktree(tk.Slug)
		return
	}
	// Several members: ask which one, then open it (formSubmit follows up).
	m.takeAfterAttach = tk.Slug
	m.form = openForm(fTaskAttach, tk.Slug, textField("member ", ""), textField("branch ", branch))
}

// openTakenWorktree opens the card's first worktree in the editor.
func (m *Model) openTakenWorktree(slug string) {
	tk, ok := m.taskStore.Get(slug)
	if !ok || len(tk.ChangeSets) == 0 {
		m.flashErr("card has no worktree")
		return
	}
	cs := tk.ChangeSets[0]
	dir := filepath.Join(m.wsRoot(), cs.Path)
	if m.openWorktree == nil || !m.openWorktree(slug+" ("+cs.Branch+")", dir) {
		m.flashOK("worktree ready at " + cs.Path + " (open it from the editor)")
		return
	}
	m.flashOK("you took " + slug + " · c commit · u push · p PR")
}

// branchFor is the conventional task branch for slug.
func (m *Model) branchFor(slug string) string {
	if m.branchForFn != nil {
		if b := m.branchForFn(slug); b != "" {
			return b
		}
	}
	return "task/" + slug
}

// flashOK shows a success note on the chrome bar.
func (m *Model) flashOK(msg string) { m.form = formState{kind: fNone, flash: msg} }
