package workspace

import (
	"fmt"
	"image/color"
	"strings"
	"time"

	"charm.land/lipgloss/v2"

	"github.com/drjzlyan/dhi/internal/agentkit/workflow"
	"github.com/drjzlyan/dhi/internal/ansi"
	"github.com/drjzlyan/dhi/internal/inbox"
	"github.com/drjzlyan/dhi/internal/tasks"
	"github.com/drjzlyan/dhi/internal/tui/branding"
	"github.com/drjzlyan/dhi/internal/tui/kit"
	"github.com/drjzlyan/dhi/internal/tui/theme"
)

// View renders the operations floor. Wide terminals get a docked
// layout — persistent section rail on the left, active section filling
// the remaining width and full height. Middle widths render a
// full-width vertical stack (no centered dead margins); only truly
// tiny terminals center (F-025 Part D). Not-inside-a-workspace keeps
// the brand hero.
func (m *Model) View() string {
	if m.ws == nil {
		lines := strings.Split(branding.HeroBlock(m.version), "\n")
		lines = append(lines, "", theme.Hint().Render("not inside a DHI workspace"))
		return kit.Center(strings.Join(lines, "\n"), maxInt(m.width, 40), maxInt(m.height, 10))
	}
	m.syncUnread()
	if m.width < kit.WCompact {
		return kit.Center(m.compactBody(), maxInt(m.width, 40), maxInt(m.height, 10))
	}
	if m.width < kit.WDock {
		return m.compactBody()
	}
	return m.dockedView()
}

// dockMinWidth is the narrowest terminal that still fits rail + a
// usable pane.
const dockMinWidth = 84

const railWidth = 26

func (m *Model) compactBody() string {
	body := m.sectionStrip() + "\n" + m.activeSection() + "\n" +
		kit.HintBar(maxInt(m.width, 40), m.statusFlash(), m.sectionHints()...)
	if m.form.kind != fNone {
		body = m.modalView(body)
	}
	return body
}

func (m *Model) dockedView() string {
	paneW := m.width - railWidth
	if paneW < 40 {
		paneW = 40
	}
	rail := m.railView(m.height)
	pane := m.mainPane(paneW, m.height)
	return lipgloss.JoinHorizontal(lipgloss.Top, rail, pane)
}

// railView renders the always-visible section switcher with live item
// counts, padded to the full body height — the kit.Rail primitive with
// the inset background shade (F-025). Status messages live on the
// pane's HintBar, not here.
func (m *Model) railView(h int) string {
	counts := m.sectionCounts()
	rows := make([]kit.RailRow, 0, secCount)
	for s := sectionID(0); s < secCount; s++ {
		row := kit.RailRow{Label: s.label()}
		if counts[s] > 0 {
			// The attention count carries the tint (F-026 P3): INBOX is
			// the urgent channel — danger fg; the rest stay quiet.
			if s == secInbox {
				row.Badge = theme.DangerText().Render(itoa(counts[s]))
			} else {
				row.Count = itoa(counts[s])
			}
		}
		rows = append(rows, row)
	}
	return (&kit.Rail{
		Rows:   rows,
		Active: int(m.sec),
		Width:  railWidth,
		Height: h,
		Foot:   "[ ] sections",
	}).View()
}

func (m *Model) sectionCounts() [secCount]int {
	var c [secCount]int
	c[secInbox] = m.AttentionCount()
	for _, tk := range m.taskRows() {
		if tk.Status != tasks.Done {
			c[secBoard]++
		}
	}
	if m.pane != nil {
		c[secChannels] = len(m.pane.channels)
	}
	c[secRepos] = len(m.ws.Members())
	return c
}

// mainPane wraps the active section in a full-height panel; dialogs
// ride kit.Modal over a dimmed backdrop while the rail stays put.
// modalLines renders busy/error rows itself, so the box carries only
// title + lines. The last pane row is the chrome HintBar (F-025):
// status/flash left, the section's keymap right.
func (m *Model) mainPane(w, h int) string {
	p := kit.NewPanel(strings.ToLower(m.sec.label()), true)
	inner := w - 4 // panel edges + horizontal padding
	body := m.activeSectionFor(inner, h-3)
	content := strings.Split(body, "\n")
	for len(content) < h-3 {
		content = append(content, "")
	}
	content = content[:h-3]
	p.SetContent(content...)
	p.SetFooter(kit.HintBar(inner, m.statusFlash(), m.sectionHints()...))
	p.Width, p.Height = w, h
	// Per-pane scrollbar (F-025): REPOS windows by row (members lead the
	// dependency lines) and owns offsets[secRepos]; paint the thumb when
	// it overflows the body budget.
	if m.sec == secRepos && m.replay == nil {
		p.SetScroll(kit.NewScroller(m.reposRowCount(inner), h-3, m.offsets[secRepos]))
	}
	pane := p.View()

	if m.form.kind == fNone {
		return pane
	}
	box := kit.Modal{Title: modalTitle(m.form.kind), Lines: m.modalLines()}
	return kit.Overlay(strings.Split(pane, "\n"), box.View(), w, h)
}

// flashTTL is how long a chrome flash stays visible (F-026 P7 toast
// semantics: outcomes announce, then leave — no permanent furniture).
const flashTTL = 4 * time.Second

// statusFlash renders the outcome segment on the chrome bar: error >
// warning hint > success flash (F-025 Part A). Messages expire
// flashTTL after they first appear; identical content keeps its
// original stamp (no restart), so a stable state clears itself.
func (m *Model) statusFlash() string {
	msg := ""
	var fg color.Color
	switch {
	case m.form.err != "":
		msg, fg = "✗ "+m.form.err, theme.Current.Danger
	case m.pane != nil && m.pane.flash != "":
		msg, fg = "✗ "+m.pane.flash, theme.Current.Danger
	case m.inboxHint != "":
		msg, fg = m.inboxHint, theme.Current.Warning
	case m.form.flash != "":
		msg, fg = "✓ "+m.form.flash, theme.Current.Success
	}
	if msg == "" {
		m.chromeSeen = ""
		return ""
	}
	now := m.now()
	if msg != m.chromeSeen {
		m.chromeSeen = msg
		m.chromeAt = now
	}
	if now.Sub(m.chromeAt) > flashTTL {
		return ""
	}
	return theme.ChromeStatus(fg).Render(msg)
}

// sectionHints is the active section's keymap for the chrome bar.
func (m *Model) sectionHints() []string {
	switch m.sec {
	case secInbox:
		return []string{"enter/o jump", "z snooze", "u unsnooze"}
	case secBoard:
		if m.replay != nil {
			return []string{"esc close", "j/k scroll", "g/G top/bottom"}
		}
		return []string{"h/l lane", "n new", "s/S status", "m move",
			"/ filter", "L/P/E/D meta", "space mark", "M bulk"}
	case secChannels:
		return m.pane.hints()
	case secRepos:
		return []string{"a add", "r rename", "e editor", "d remove"}
	}
	return nil
}

// activeSectionFor renders the active section body with pane-aware
// geometry (every section takes the real pane width — F-026 P3 ends
// the m.width-based underfill). The run-replay pane is modal: it
// replaces whatever section is active.
func (m *Model) activeSectionFor(w, h int) string {
	if m.replay != nil {
		return m.replayBody()
	}
	switch m.sec {
	case secBoard:
		return m.boardBody(w, maxInt(h, 6))
	case secChannels:
		return strings.Join(m.pane.render(w, maxInt(h, 12)), "\n")
	case secInbox:
		return m.inboxBody(w)
	case secRepos:
		return m.reposBody(w, maxInt(h, 6))
	default:
		return m.activeSection()
	}
}

// activeSection renders the compact-stack fallbacks (below WDock the
// sections fill the full width; inbox/repos here stay width-aware).
func (m *Model) activeSection() string {
	if m.replay != nil {
		return m.replayBody()
	}
	w := maxInt(m.width, 40)
	switch m.sec {
	case secInbox:
		return m.inboxBody(w - 6)
	case secRepos:
		return m.reposBody(w-6, maxInt(m.height-8, 8))
	default:
		return m.boardBody(w-6, maxInt(m.height-8, 8))
	}
}

// sectionStrip renders the switchable pane tabs with the active one lit.
func (m *Model) sectionStrip() string {
	var parts []string
	for s := sectionID(0); s < secCount; s++ {
		label := s.label()
		if s == m.sec {
			parts = append(parts, theme.TabActive().Render("["+label+"]"))
		} else {
			parts = append(parts, theme.TextDim().Render(label))
		}
	}
	// Flash outcomes announce ONCE, on the chrome HintBar (F-026 P3 —
	// the strip's second announcement is deleted).
	return strings.Join(parts, theme.TextDim().Render(" · "))
}

// ---- BOARD (F-021) ----

const boardDetailWidth = 36

// boardStatusColor maps a task lane to its status color (F-025 lane
// dots: backlog quiet, active accent, in-review warning, done success).
func boardStatusColor(i int) color.Color {
	switch tasks.Statuses[i] {
	case tasks.Active:
		return theme.Current.Accent
	case tasks.InReview:
		return theme.Current.Warning
	case tasks.Done:
		return theme.Current.Success
	}
	return theme.Current.TextMuted
}

// boardBody renders the four kanban lanes plus the selected card's
// detail pane (right on wide panes, below on narrow, ElevatedBg).
func (m *Model) boardBody(w, h int) string {
	g := m.boardGroups()

	var out []string
	if m.taskStore == nil {
		out = append(out, theme.TextDim().Render("(task store unavailable)"))
		// The lanes still render (empty) so the board reads as a board.
		g = [4][]tasks.Task{}
	} else if warn := m.taskStore.Warnings(); len(warn) > 0 {
		out = append(out, theme.DangerText().Render(
			fmt.Sprintf("%d malformed card(s) skipped", len(warn))))
	}
	if line := m.boardFilterLine(); line != "" {
		out = append(out, line)
	}

	detailW := 0
	if w >= kit.WWide {
		detailW = boardDetailWidth
	}
	wrapW := w - 4
	if detailW > 0 {
		wrapW = detailW - 1
	}
	lanesH := h - len(out) // the warning/unavailable rows, if any
	var detailLines []string
	if tk, ok := m.boardSelected(g); ok {
		root := ""
		if m.ws != nil {
			root = m.ws.Root
		}
		detailLines = boardDetailLines(tk, wrapW, root)
		if m.working != nil && tk.ThreadChannel != "" && m.working(tk.ThreadChannel, tk.ThreadID) {
			detailLines = append(detailLines, theme.SuccessText().Render("● agent working — live in the thread"))
		}
	}
	if detailW == 0 && len(detailLines) > 0 {
		// -1: the lane header row above the Height body rows.
		lanesH = h - len(out) - len(detailLines) - 1
	}
	if lanesH < 3 {
		lanesH = 3
	}

	cols := make([]kit.Column, 4)
	board := &kit.Columns{Cols: cols, Active: m.boardActive, Width: w - detailW, Height: lanesH}
	for i, st := range tasks.Statuses {
		laneW := board.LaneWidth(i)
		rows := make([]string, 0, len(g[i]))
		for _, tk := range g[i] {
			working := m.working != nil && tk.ThreadChannel != "" && m.working(tk.ThreadChannel, tk.ThreadID)
			rows = append(rows, boardCard(tk, laneW, m.boardMarks[tk.Slug], working))
		}
		cols[i] = kit.Column{
			Title: string(st), Cursor: m.boardCur[i], Rows: rows,
			Accent: boardStatusColor(i),
		}
	}
	lanes := board.View()

	if detailW > 0 && len(detailLines) > 0 {
		laneLines := strings.Split(lanes, "\n")
		for len(laneLines) < lanesH+1 {
			laneLines = append(laneLines, "")
		}
		bg := theme.ElevatedBg()
		block := make([]string, 0, len(laneLines))
		for y, ln := range laneLines {
			d := ""
			if y < len(detailLines) {
				d = ansi.Clip(detailLines[y], detailW-1)
				d = bg.Render(padToANSI(d, detailW))
			} else {
				d = bg.Render(strings.Repeat(" ", detailW))
			}
			block = append(block, padToANSI(ln, w-detailW)+d)
		}
		out = append(out, block...)
	} else {
		out = append(out, lanes)
		out = append(out, detailLines...)
	}
	return strings.Join(out, "\n")
}

// boardFilterLine renders the active filter / bulk-selection state
// (F-035); empty when neither is active.
func (m *Model) boardFilterLine() string {
	switch {
	case m.boardFilterEdit:
		return theme.TabActive().Render("/ "+m.boardFilter) + "▏" +
			theme.Hint().Render("  type to filter · enter keep · esc clear")
	case m.boardFilter != "":
		return theme.Hint().Render("filter \""+m.boardFilter+"\"") +
			theme.TextDim().Render("  (esc in edit, or / to change)")
	case len(m.boardMarks) > 0:
		return theme.WarningText().Render(itoa(len(m.boardMarks))+" marked") +
			theme.Hint().Render("  · M move · C clear")
	}
	return ""
}

// boardCard renders one lane row proportional to the lane budget
// (F-026 P3): a mark/priority prefix, slug, ellipsized title, assignee
// chip — no fixed pads.
func boardCard(tk tasks.Task, laneW int, marked, working bool) string {
	title := tk.Title
	if title == "" {
		title = "-"
	}
	who := tk.Assignee
	if who == "" {
		who = "unassigned"
	}
	whoPart := ""
	whoW := 0
	if laneW >= 16 {
		whoW = minInt(minInt(ansi.Width(who), 12), laneW/3)
	}
	prefix := ""
	prefixW := 0
	if laneW >= 14 {
		prefix = boardMarkPrefix(tk.Priority, marked, working)
		prefixW = 3
	}
	slugW := clampInt(laneW-whoW-prefixW-6, 4, 14)
	titleW := clampInt(laneW-slugW-whoW-prefixW-1, 4, 40)
	if whoW > 0 {
		gap := maxInt(laneW-slugW-titleW-whoW-prefixW, 0)
		whoPart = padTo(theme.Hint().Render(kit.ClipEllipsis(who, whoW)), whoW+gap)
	}
	row := prefix +
		padTo(kit.ClipEllipsis(tk.Slug, slugW-1), slugW) +
		padTo(kit.ClipEllipsis(title, titleW-1), titleW)
	if whoW == 0 {
		row = padTo(row, laneW)
	}
	return row + whoPart
}

// boardMarkPrefix is the 3-cell mark+priority indicator.
func boardMarkPrefix(p tasks.Priority, marked, working bool) string {
	mark := " "
	if marked {
		mark = theme.SuccessText().Render("◆")
	}
	glyph := " "
	switch p {
	case tasks.PriorityUrgent:
		glyph = theme.DangerText().Render("▲")
	case tasks.PriorityHigh:
		glyph = theme.WarningText().Render("△")
	case tasks.PriorityLow:
		glyph = theme.TextDim().Render("▽")
	case tasks.PriorityNormal:
		glyph = theme.TextDim().Render("·")
	}
	tail := " "
	if working {
		tail = theme.SuccessText().Render("●")
	}
	return mark + glyph + tail
}

func clampInt(v, lo, hi int) int {
	if v < lo {
		return lo
	}
	if v > hi {
		return hi
	}
	return v
}

// boardDetailLines is the JIRA-issue fact block for the selected card;
// wrapW word-wraps the title (F-026 P3 — long titles wrapped, never
// silently clipped at the pane edge).
func boardDetailLines(tk tasks.Task, wrapW int, wsRoot string) []string {
	who := tk.Assignee
	if who == "" {
		who = "unassigned"
	}
	lines := []string{
		theme.Brand().Render(tk.Slug) + "  " + theme.Chip().Render(string(tk.Status)),
	}
	for _, l := range kit.WrapWords(tk.Title, clampInt(wrapW, 20, 72)) {
		lines = append(lines, l)
	}
	if tk.Title == "" {
		lines = append(lines, "-")
	}
	lines = append(lines,
		theme.TextDim().Render("assignee "+who+" · team "+orDash(tk.Team)))
	if meta := boardMetaLine(tk); meta != "" {
		lines = append(lines, theme.TextDim().Render(meta))
	}
	if tk.ThreadChannel != "" {
		lines = append(lines, theme.Hint().Render("thread "+threadRef(tk.ThreadChannel, tk.ThreadID)))
	}
	if tk.PRNumber > 0 {
		pr := fmt.Sprintf("PR #%d", tk.PRNumber)
		if tk.PRURL != "" {
			pr += " " + tk.PRURL
		}
		lines = append(lines, theme.SuccessText().Render(pr))
	}
	if len(tk.ChangeSets) > 0 {
		var cs []string
		for _, c := range tk.ChangeSets {
			cs = append(cs, c.Member+"@"+c.Branch)
		}
		lines = append(lines, theme.TextDim().Render("worktrees "+strings.Join(cs, ", ")))
	}
	if len(tk.Runs) > 0 {
		rl := tasks.RollupRuns(tk.Runs)
		lines = append(lines, theme.TextDim().Render(
			fmt.Sprintf("%d runs · %s", len(tk.Runs), rl.CostText())))
	}
	if line, ok := boardWorkflowLine(tk, wsRoot); ok {
		lines = append(lines, line)
	}
	return lines
}

// boardWorkflowLine shows a task's active feature workflow and where it
// stands (F-031): the next enforced step, or "complete".
func boardWorkflowLine(tk tasks.Task, wsRoot string) (string, bool) {
	if tk.Workflow == "" || wsRoot == "" {
		return "", false
	}
	def, err := workflow.Load(wsRoot, tk.Workflow)
	if err != nil {
		return theme.Hint().Render("workflow " + tk.Workflow + " (missing)"), true
	}
	p := workflow.Progress{
		Worktree:  len(tk.ChangeSets) > 0,
		TestsPass: tk.TestsPass,
		Reviewed:  len(tk.Bypasses) > 0,
	}
	if s, ok := workflow.NextStep(def, p); ok {
		return theme.Hint().Render(fmt.Sprintf("workflow %s · next: %s (%s)", tk.Workflow, s.ID, s.Gate)), true
	}
	return theme.SuccessText().Render("workflow " + tk.Workflow + " · complete"), true
}

// boardMetaLine renders a card's F-035 metadata (labels, priority, epic,
// due); empty when the card carries none.
func boardMetaLine(tk tasks.Task) string {
	var parts []string
	if len(tk.Labels) > 0 {
		parts = append(parts, "labels "+strings.Join(tk.Labels, ","))
	}
	if tk.Priority != "" {
		parts = append(parts, "priority "+string(tk.Priority))
	}
	if tk.Epic != "" {
		parts = append(parts, "epic "+tk.Epic)
	}
	if tk.Due != "" {
		parts = append(parts, "due "+tk.Due)
	}
	return strings.Join(parts, " · ")
}

func orDash(s string) string {
	if s == "" {
		return "-"
	}
	return s
}

// inboxGlyph marks one item kind with a shape: warning diamond (approval),
// cross (run failed), check (in review), @ (mention).
func inboxGlyph(k inbox.ItemKind) string {
	switch k {
	case inbox.Approval:
		return theme.GlyphDiamond
	case inbox.RunFailed:
		return theme.GlyphCross
	case inbox.InReview:
		return theme.GlyphCheck
	default:
		return theme.GlyphAt
	}
}

func (m *Model) inboxBody(w int) string {
	items := m.inboxItems()
	c := &m.cursors[secInbox]
	clampCursor(c, len(items))

	var out []string
	if m.unreadErr != "" {
		out = append(out, theme.DangerText().Render("unread unavailable: "+m.unreadErr))
	}
	if len(items) == 0 {
		out = append(out, theme.TextDim().Render("(nothing needs attention)"))
		return strings.Join(out, "\n")
	}
	lines := maxInt(w-4, 30)
	gl := len([]rune(theme.GlyphCursor))
	for i, it := range items {
		snoozed := !it.Snoozed.IsZero()
		// Snoozed rows stay visible but dim — even under the cursor —
		// and carry the parked bullet glyph so the affordance is more
		// than color (F-017; F-026 P3).
		style := theme.TextDim()
		if i == *c && !snoozed {
			style = theme.TabActive()
		}
		glyph := inboxGlyph(it.Kind)
		if snoozed {
			glyph = theme.GlyphBullet
		}
		row := it.Row
		// Relative stamps ride rows whose At is a real instant;
		// approvals sort on a synthetic epoch and stay unstamped
		// (ADR-0011: never guess).
		if it.Kind != inbox.Approval && !it.At.IsZero() {
			row += "  " + theme.Hint().Render("· "+timeAgo(it.At, m.now()))
		}
		if snoozed {
			row += "  — snoozed until " + snoozeUntilText(it.Snoozed, m.now())
		}
		first := true
		for _, ln := range kit.WrapWords(row, lines) {
			if first {
				prefix := strings.Repeat(" ", gl)
				if i == *c {
					prefix = theme.GlyphCursor + " "
				}
				out = append(out, prefix+style.Render(glyph+" "+ln))
				first = false
			} else {
				out = append(out, strings.Repeat(" ", gl+1)+style.Render(ln))
			}
		}
	}
	return strings.Join(out, "\n")
}

// timeAgo is the relative stamp used across rows (deterministic from
// the injected now — table-tested).
func timeAgo(at, now time.Time) string {
	d := now.Sub(at)
	if d < 0 {
		d = 0
	}
	switch {
	case d < time.Minute:
		return "now"
	case d < time.Hour:
		return itoa(int(d/time.Minute)) + "m"
	case d < 24*time.Hour:
		return itoa(int(d/time.Hour)) + "h"
	default:
		return itoa(int(d/(24*time.Hour))) + "d"
	}
}

// wordWrap was the duplicated F-016 wrap; the shared kit.WrapWords
// (word-boundary + hard-break contract) replaced it.

func taskDetail(tk tasks.Task) string {
	var parts []string
	for _, cs := range tk.ChangeSets {
		parts = append(parts, cs.Member+"@"+cs.Branch)
	}
	thread := ""
	if tk.ThreadChannel != "" {
		thread = fmt.Sprintf("thread %s", threadRef(tk.ThreadChannel, tk.ThreadID))
	}
	if tk.PRNumber > 0 {
		pr := fmt.Sprintf("PR #%d", tk.PRNumber)
		if thread != "" {
			pr += " "
		}
		thread = strings.TrimSpace(pr + thread)
	}
	if len(parts) > 0 {
		thread = strings.TrimSpace(strings.Join([]string{thread, "·"}, " "))
	}
	all := parts
	if thread != "" {
		all = append(all, thread)
	}
	// Run accounting (F-014 §Part B): a runs suffix only when the card
	// has history — "cost partial" marks a cost-less run in the set.
	if len(tk.Runs) > 0 {
		rl := tasks.RollupRuns(tk.Runs)
		all = append(all, fmt.Sprintf("%d runs · %s", len(tk.Runs), rl.CostText()))
	}
	return strings.Join(all, "  ")
}

func threadRef(channel string, id int64) string {
	if id == 0 {
		return channel
	}
	return fmt.Sprintf("%s#%d", channel, id)
}

const nameCol = 14

// ---- REPOS (member repos) ----

const reposNameCol = 14

// reposBody renders member rows on the inset shade (the section zone
// reads like every other shaded block, F-026 P3): cursor, name, and a
// path clipped into the real pane budget — not a fixed 46 cells.
func (m *Model) reposBody(w, h int) string {
	members := m.ws.Members()
	c := m.cursors[secRepos]
	clampCursor(&c, len(members))

	inset := theme.InsetBg()
	pathBudget := maxInt(w-reposNameCol-6, 12)
	var rows []string
	if len(members) == 0 {
		rows = append(rows, inset.Render(theme.TextMuted().Render(
			padTo("(none — press a to add one)", maxInt(w-4, 12)))))
	}
	for i, mem := range members {
		mark := "  "
		style := theme.TextDim()
		if i == c {
			mark = theme.GlyphCursor + " "
			style = theme.TabActive()
		}
		rows = append(rows, inset.Render(
			mark+style.Render(padTo(mem.Name, reposNameCol))+
				theme.Hint().Render(kit.ClipEllipsis(shorten(mem.Path, pathBudget), pathBudget))))
	}
	rows = append(rows, m.dependencyLines(w, inset)...)

	// Scroll window (F-025): members are one row each and lead the
	// dependency lines, so the cursor's member index is its row index.
	off := clampOffset(m.offsets[secRepos], len(rows), h)
	if len(members) > 0 {
		if c < off {
			off = c
		} else if c >= off+h {
			off = c - h + 1
		}
		off = clampOffset(off, len(rows), h)
	}
	m.offsets[secRepos] = off
	if h <= 0 {
		return strings.Join(rows, "\n")
	}
	end := minInt(off+h, len(rows))
	if end <= off {
		return ""
	}
	return strings.Join(rows[off:end], "\n")
}

// reposRowCount is the rendered row total for REPOS (member rows +
// the dependency block), used to size its scrollbar without a second
// full render.
func (m *Model) reposRowCount(w int) int {
	n := len(m.ws.Members())
	if n == 0 {
		n = 1
	}
	return n + len(m.dependencyLines(w, theme.InsetBg()))
}

// clampOffset clamps a scroll offset so a window of h rows stays inside
// total rows (0 total ⇒ 0).
func clampOffset(off, total, h int) int {
	if h <= 0 {
		return 0
	}
	max := total - h
	if max < 0 {
		max = 0
	}
	if off < 0 {
		return 0
	}
	if off > max {
		return max
	}
	return off
}

// dependencyLines renders the declared cross-project graph (F-032): each
// edge as "from → to (kind)", with a dangling endpoint flagged by name.
// No edges = one line saying so (the view is never invisible).
func (m *Model) dependencyLines(w int, inset lipgloss.Style) []string {
	deps := m.ws.Dependencies()
	lines := []string{"", inset.Render(theme.TextDim().Render(padTo("dependencies", reposNameCol)))}
	if len(deps) == 0 {
		return append(lines, inset.Render(theme.Hint().Render(
			"none declared — add [[dependency]] in .dhi/workspace.toml")))
	}
	dangling := map[string]bool{}
	for _, d := range m.ws.DanglingDependencies() {
		dangling[d.From+"→"+d.To] = true
	}
	for _, d := range deps {
		edge := d.From + " → " + d.To + " (" + d.Kind + ")"
		if dangling[d.From+"→"+d.To] {
			edge += " — member missing"
			lines = append(lines, inset.Render(theme.DangerText().Render(
				padTo("  "+d.From, reposNameCol)+kit.ClipEllipsis(edge, maxInt(w-reposNameCol-4, 12)))))
			continue
		}
		lines = append(lines, inset.Render(
			theme.Hint().Render(padTo("  "+d.From, reposNameCol))+
				theme.TextDim().Render(kit.ClipEllipsis("→ "+d.To+" ("+d.Kind+")", maxInt(w-reposNameCol-4, 12)))))
	}
	return lines
}

// ---- modals ----

// modalView overlays the dialog box on the compact body (narrow path).
func (m *Model) modalView(body string) string {
	box := kit.Modal{Title: modalTitle(m.form.kind), Lines: m.modalLines()}
	return kit.Overlay(strings.Split(body, "\n"), box.View(),
		maxInt(m.width, 40), maxInt(m.height, 10))
}

func (m *Model) modalLines() []string {
	f := &m.form
	switch f.kind {
	case fTaskRemoveConfirm:
		return confirmLines("remove task "+f.target()+"?",
			"the card is deleted; recorded worktrees",
			"stay on disk — detach them first to clean up.", f)
	case fTaskAttach:
		hint := "creates .dhi/tasks/<slug>/<member> via hermetic git"
		lines := []string{}
		for i, fl := range f.fields() {
			lines = append(lines, m.fieldLine(fl, i == f.curField() && !f.busy))
		}
		lines = append(lines, "", hintOrErr(f, hint))
		return lines
	case fTaskCommit:
		lines := []string{}
		for i, fl := range f.fields() {
			lines = append(lines, m.fieldLine(fl, i == f.curField() && !f.busy))
		}
		lines = append(lines, "", hintOrErr(f, "commit message · enter save"))
		return lines
	case fTaskPush:
		return confirmLines("push branch for "+f.target()+"?",
			"pushes the worktree branch to origin",
			"and updates the remote.", f)
	case fRemoveConfirm:
		return confirmLines("remove member "+f.target()+"?",
			"unregisters the repo; the working tree",
			"on disk is never deleted.", f)
	}

	lines := make([]string, 0, len(f.fields())*2)
	if f.orig != "" && f.kind != fAdd {
		lines = append(lines, theme.TextDim().Render("editing: "+f.target()))
	}
	for i, fl := range f.fields() {
		lines = append(lines, m.fieldLine(fl, i == f.curField() && !f.busy))
		if fl.IsToggle() {
			lines = append(lines, theme.TextDim().Render("      ←/→ switches mode"))
		}
	}
	lines = append(lines, "", hintOrErr(f, defaultHint(f.kind)))
	return lines
}

func defaultHint(k modalKind) string {
	switch k {
	case fTaskPR:
		return "pushes the card's branch and opens a PR · enter create"
	case fTaskCommit:
		return "commit message · enter save"
	case fTaskPush:
		return "enter confirms push · esc cancel"
	}
	return "tab next field · enter save · esc cancel"
}

func confirmLines(question, l1, l2 string, f *formState) []string {
	lines := []string{question,
		theme.TextDim().Render(l1),
		theme.TextDim().Render(l2), ""}
	if f.err != "" {
		lines = append(lines, theme.DangerText().Render(f.err), "")
	}
	return append(lines, theme.Hint().Render("enter confirm · esc keep"))
}

func (m *Model) fieldLine(fl field, focused bool) string {
	cursor := " "
	var value string
	if fl.IsToggle() {
		value = "< " + fl.Selected() + " >"
	} else if focused {
		// Canonical cursor rendering (kit.Form contract): the block
		// sits at the in-value position.
		value = fl.CursorValue()
	} else {
		value = " " + fl.Value
	}
	style := theme.Hint()
	if focused {
		cursor = theme.GlyphCursor
		style = theme.SuccessText()
	}
	return cursor + " " + style.Render(padTo(fl.Label, 15)) + value
}

func hintOrErr(f *formState, hint string) string {
	switch {
	case f.busy:
		return theme.TabActive().Render("working…")
	case f.err != "":
		return theme.DangerText().Render(f.err)
	default:
		return theme.Hint().Render(hint)
	}
}

func modalTitle(k modalKind) string {
	switch k {
	case fAdd:
		return "add member"
	case fRename:
		return "rename member"
	case fRemoveConfirm:
		return "remove member"
	case fTaskNew:
		return "new task"
	case fTaskAssign:
		return "assign task"
	case fTaskAttach:
		return "attach worktree"
	case fTaskThread:
		return "bind thread"
	case fTaskRemoveConfirm:
		return "remove task"
	case fTaskPR:
		return "create PR"
	case fTaskCommit:
		return "commit changes"
	case fTaskPush:
		return "push branch"
	case fTaskMove:
		return "move task"
	}
	return ""
}

func shorten(p string, n int) string {
	if len(p) <= n {
		return p
	}
	return "…" + p[len(p)-n+1:]
}

func padTo(s string, w int) string {
	if vis := ansi.Width(s); vis < w {
		return s + strings.Repeat(" ", w-vis)
	}
	return s
}

func maxInt(a, b int) int {
	if a > b {
		return a
	}
	return b
}

func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	var digits []byte
	for n > 0 {
		digits = append([]byte{byte('0' + n%10)}, digits...)
		n /= 10
	}
	return string(digits)
}
