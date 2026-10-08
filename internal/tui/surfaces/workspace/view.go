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
	m.hits.Reset()
	if m.ws == nil {
		return branding.NoWorkspace(m.width, m.height, m.version)
	}
	m.syncUnread()
	if m.width < kit.WDock {
		return m.compactBody()
	}
	return m.dockedView()
}

const railWidth = 26

func (m *Model) compactBody() string {
	// Below the dock width the rail folds into a one-line section strip
	// and the same panel (body + hint bar) takes the full width (F-054):
	// narrow terminals get the real UI, never a stripped-down stack.
	strip := kit.ClipEllipsis(m.sectionStrip(), m.width)
	m.hits.SetOrigin(2, 2) // panel border + padding, under the strip
	return strip + "\n" + m.mainPane(m.width, m.height-1)
}

func (m *Model) dockedView() string {
	paneW := m.width - railWidth
	if paneW < 40 {
		paneW = 40
	}
	rail := m.railView(m.height)
	m.hits.SetOrigin(railWidth+2, 1) // panel border + padding, right of the rail
	pane := m.mainPane(paneW, m.height)
	return lipgloss.JoinHorizontal(lipgloss.Top, rail, pane)
}

// Click implements the clickHandler seam (F-041): a click on a rail row
// jumps to that section. Only the docked layout has a rail.
func (m *Model) Click(x, y int) bool {
	if m.modalOpen() {
		return false // a dialog owns the screen; keys close it
	}
	if m.width >= kit.WDock && x < railWidth {
		rail := &kit.Rail{Rows: make([]kit.RailRow, secCount), Active: int(m.sec), Width: railWidth, Height: m.height}
		if i, ok := rail.RowAt(y); ok {
			m.selectSection(sectionID(i))
			return true
		}
		return false
	}
	// Section strip, list rows and lanes: zones recorded by the last View.
	return m.hits.Click(x, y)
}

// modalOpen reports a dialog over the pane (clicks then do nothing).
func (m *Model) modalOpen() bool { return m.form.kind != fNone }

// selectSection switches the active section (rail or strip click).
func (m *Model) selectSection(s sectionID) {
	m.replay = nil
	m.sec = s
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
	// The first frame can arrive before any resize (0x0) and terminals
	// can be tiny; render a minimal panel and let the shell clip it.
	w, h = maxInt(w, 12), maxInt(h, 4)
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
	// The keymap row is clickable (F-055): a hint acts like its key.
	m.hits.Add(0, h-3, inner, 1, func(dx, _ int) {
		if k, ok := kit.HintKeyAt(inner, m.statusFlash(), dx, m.sectionHints()...); ok {
			m.HandleKey(k)
		}
	})
	p.Width, p.Height = w, h
	// Per-pane scrollbar (F-025): REPOS windows by row (members lead the
	// dependency lines) and owns offsets[secRepos]; paint the thumb when
	// it overflows the body budget.
	if m.sec == secRepos && m.replay == nil {
		p.SetScroll(kit.NewScroller(m.reposRowCount(inner), h-3, m.offsets[secRepos]))
	}
	if m.sec == secInbox && m.replay == nil {
		total, off := m.inboxScroll(inner)
		p.SetScroll(kit.NewScroller(total, h-3, off))
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
			"/ filter", "L/P/E/D meta", "N comment", "space mark", "M bulk"}
	case secChannels:
		if m.pane == nil {
			return nil
		}
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
		if m.pane == nil {
			// The bus failed to open (named at launch); never a crash.
			return strings.Join(kit.Notice{What: "Channels are unavailable",
				Why:   "the message bus could not open; the cause was named at launch.",
				Retry: "run dhi doctor for the cause"}.Lines(w, maxInt(h, 3)), "\n")
		}
		return strings.Join(m.pane.render(w, maxInt(h, 12)), "\n")
	case secInbox:
		return m.inboxBody(w, maxInt(h, 6))
	case secRepos:
		return m.reposBody(w, maxInt(h, 6))
	default:
		return m.boardBody(w, maxInt(h, 6))
	}
}

// sectionStrip renders the switchable pane tabs with the active one lit.
func (m *Model) sectionStrip() string {
	labels := make([]string, secCount)
	for s := sectionID(0); s < secCount; s++ {
		labels[s] = s.label()
	}
	line, spans := kit.SectionStrip(labels, int(m.sec))
	for i, sp := range spans {
		sec := sectionID(i)
		m.hits.Add(sp[0], 0, sp[1]-sp[0], 1, func(int, int) { m.selectSection(sec) })
	}
	return line
}

// ---- BOARD (F-021) ----

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
		out = append(out, theme.DangerText().Render(theme.GlyphCross+" task store unavailable")+
			theme.TextDim().Render(" — run dhi doctor for the cause"))
		// The lanes still render (empty) so the board reads as a board.
		g = [4][]tasks.Task{}
	} else if warn := m.taskStore.Warnings(); len(warn) > 0 {
		out = append(out, theme.DangerText().Render(
			fmt.Sprintf("%d malformed card(s) skipped", len(warn))))
	}
	if line := m.boardFilterLine(); line != "" {
		out = append(out, line)
	}

	// The detail pane docks right on wide panes only while a card is
	// selected; an empty board gives the lanes the whole width (F-054).
	selTask, hasSel := m.boardSelected(g)
	detailW := 0
	if w >= kit.WWide && hasSel {
		detailW = clampInt(w/4, 32, 48)
	}
	wrapW := w - 4
	if detailW > 0 {
		wrapW = detailW - 1
	}
	lanesH := h - len(out) // the warning/unavailable rows, if any
	var detailLines []string
	if hasSel {
		tk := selTask
		root := ""
		if m.ws != nil {
			root = m.ws.Root
		}
		detailLines = boardDetailLines(tk, wrapW, root)
		detailLines = append(detailLines, boardNotesLines(tk, wrapW, detailW > 0, m.now)...)
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
	// Click zones (F-055): a lane header focuses the lane, a card row
	// selects that card; clicking the selected card again opens it.
	headY := len(out)
	for i := range cols {
		lane, laneW := i, board.LaneWidth(i)
		start, end := board.Window(i)
		m.hits.Add(i*laneW, headY, laneW, 1, func(int, int) { m.boardActive = lane })
		m.hits.Add(i*laneW, headY+1, laneW, end-start, func(_, dy int) {
			row := start + dy
			if m.boardActive == lane && m.boardCur[lane] == row {
				m.boardKey("o") // open the card on its floor (thread)
				return
			}
			m.boardActive, m.boardCur[lane] = lane, row
		})
	}
	if total := len(g[0]) + len(g[1]) + len(g[2]) + len(g[3]); total == 0 && m.taskStore != nil && m.boardFilter == "" {
		// An empty board teaches instead of showing four bare dashes.
		head := strings.SplitN(lanes, "\n", 2)[0]
		empty := kit.EmptyState{
			Title:  "No tasks yet",
			Why:    "Tasks are cards you and your agents pick up: assign one to an agent and it works in its own worktree, then hands it back for review.",
			Action: "press n to create a task",
		}.Lines(w, lanesH)
		lanes = head + "\n" + strings.Join(empty, "\n")
	}

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
	// The title is what a person scans a lane for, so it always gets the
	// room (F-055); the assignee and slug join only when the lane is wide
	// enough to show them whole-ish. "unassigned" is not printed — the
	// detail pane says it — so narrow lanes stay readable.
	title := tk.Title
	if title == "" {
		title = tk.Slug
	}
	prefix, prefixW := "", 0
	if laneW >= 14 {
		prefix, prefixW = boardMarkPrefix(tk.Priority, marked, working), 3
	}
	avail := laneW - prefixW - 1 // one cell of air before the next lane
	whoW, slugW := 0, 0
	if tk.Assignee != "" && avail >= 30 {
		whoW = minInt(ansi.Width(tk.Assignee), 10) + 1
	}
	if avail >= 48 {
		slugW = minInt(ansi.Width(tk.Slug), 16) + 2
	}
	titleW := maxInt(avail-whoW-slugW, 4)
	row := prefix
	if slugW > 0 {
		row += padTo(theme.TextDim().Render(kit.ClipEllipsis(tk.Slug, slugW-2)), slugW)
	}
	row += padTo(kit.ClipEllipsis(title, titleW), titleW)
	if whoW > 0 {
		row += padTo(theme.Hint().Render(kit.ClipEllipsis(tk.Assignee, whoW-1)), whoW)
	}
	return padTo(row, laneW)
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
	lines = append(lines, kit.WrapWords(tk.Title, clampInt(wrapW, 20, 72))...)
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

// boardNotesLines renders the card's comments and activity trail
// (F-037). The wide side pane has the height for the newest comments and
// activity; below it a single summary line keeps the lanes tall.
func boardNotesLines(tk tasks.Task, wrapW int, wide bool, now func() time.Time) []string {
	if now == nil {
		now = time.Now
	}
	if len(tk.Comments) == 0 && len(tk.Activity) == 0 {
		return nil
	}
	if !wide {
		if n := len(tk.Comments); n > 0 {
			c := tk.Comments[n-1]
			return []string{theme.TextDim().Render(fmt.Sprintf("%d comments · %s: %s", n, c.Author, firstLineOf(c.Text)))}
		}
		return nil
	}
	w := clampInt(wrapW, 20, 72)
	var out []string
	if n := len(tk.Comments); n > 0 {
		out = append(out, "", theme.TextDim().Render(fmt.Sprintf("COMMENTS (%d)", n)))
		from := n - 3
		if from < 0 {
			from = 0
		}
		for _, c := range tk.Comments[from:] {
			out = append(out, theme.AccentText().Render(c.Author)+" "+theme.Hint().Render(timeAgo(c.At, now())))
			for _, l := range kit.WrapWords(c.Text, w-2) {
				out = append(out, "  "+l)
			}
		}
	}
	if n := len(tk.Activity); n > 0 {
		out = append(out, "", theme.TextDim().Render("ACTIVITY"))
		from := n - 5
		if from < 0 {
			from = 0
		}
		for _, a := range tk.Activity[from:] {
			out = append(out, theme.Hint().Render(activityText(a)+" · "+timeAgo(a.At, now())))
		}
	}
	return out
}

// activityText is one human line for an activity entry.
func activityText(a tasks.Activity) string {
	who := a.Actor
	if who == "" {
		who = "someone"
	}
	switch a.Kind {
	case tasks.ActStatus:
		return who + " moved " + a.From + " → " + a.To
	case tasks.ActAssignee:
		switch {
		case a.From == "":
			return who + " assigned it to " + a.To
		case a.To == "":
			return who + " unassigned " + a.From
		}
		return who + " reassigned " + a.From + " → " + a.To
	case tasks.ActPriority:
		if a.From == "" || a.From == "-" {
			return who + " set priority " + orDash(a.To)
		}
		return who + " changed priority " + a.From + " → " + orDash(a.To)
	case tasks.ActLabels:
		return who + " labels " + orDash(a.To)
	case tasks.ActRun:
		return who + " run " + a.To
	case tasks.ActComment:
		return who + " commented"
	}
	return who + " " + a.Kind
}

func firstLineOf(s string) string {
	if i := strings.IndexByte(s, '\n'); i >= 0 {
		s = s[:i]
	}
	return s
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

func (m *Model) inboxBody(w, h int) string {
	items := m.inboxItems()
	c := &m.cursors[secInbox]
	clampCursor(c, len(items))

	var out []string
	if m.unreadErr != "" {
		out = append(out, theme.DangerText().Render("unread unavailable: "+m.unreadErr))
	}
	if len(items) == 0 {
		out = append(out, kit.EmptyState{
			Glyph: theme.GlyphCheck, Title: "You're all caught up",
			Why: "Approvals, mentions, failed runs and tasks waiting for review land here, newest-urgent first.",
		}.Lines(w, h-len(out))...)
		return strings.Join(out, "\n")
	}

	// Wide panes dock the selected item's preview on the right (F-055).
	previewW := inboxPreviewWidth(w)
	if previewW > 0 {
		w -= previewW + 1
	}
	groups := m.inboxGroups(w)
	start := clampInt(m.offsets[secInbox], 0, len(items)-1)
	if *c < start {
		start = *c
	}
	render := func(from int) (rows []string, last int) {
		last = from - 1
		for i := from; i < len(items); i++ {
			g := groups[i]
			if h > 0 && len(rows) > 0 && len(rows)+len(g) > h {
				break
			}
			rows = append(rows, g...)
			last = i
			if h > 0 && len(rows) >= h {
				break
			}
		}
		return rows, last
	}
	rows, last := render(start)
	if *c > last { // cursor below the window: restart at the cursor
		start = *c
		rows, _ = render(start)
	}
	m.offsets[secInbox] = start
	// Click zones (F-055): select an item; click it again to jump.
	y := len(out)
	for i := start; i < len(items) && y-len(out) < len(rows); i++ {
		item := i
		m.hits.Add(0, y, w, len(groups[i]), func(int, int) {
			if *c == item {
				m.inboxKey("enter")
				return
			}
			*c = item
		})
		y += len(groups[i])
	}
	out = append(out, rows...)
	if previewW > 0 {
		out = sideBySide(out, inboxPreview(items[*c], previewW-2), w, previewW, h)
	}
	return strings.Join(out, "\n")
}

// inboxPreviewWidth is the docked preview's width for an inbox pane w
// wide (0 = too narrow, list only).
func inboxPreviewWidth(w int) int {
	if w < inboxPreviewMinWidth {
		return 0
	}
	return clampInt(w*2/5, 40, 64)
}

// sideBySide joins a left block (padded to leftW) with a right detail
// block on the elevated shade (rightW wide, one cell of padding), for h rows.
func sideBySide(left, right []string, leftW, rightW, h int) []string {
	bg := theme.ElevatedBg()
	out := make([]string, 0, h)
	for y := 0; y < h; y++ {
		l, r := "", ""
		if y < len(left) {
			l = left[y]
		}
		if y < len(right) {
			r = ansi.Clip(right[y], rightW-2)
		}
		out = append(out, padToANSI(l, leftW)+" "+bg.Render(" "+padToANSI(r, rightW-1)))
	}
	return out
}

// inboxGroups renders each inbox item to its wrapped visual rows, so the
// pane can window by item while the scrollbar counts rows (F-025).
func (m *Model) inboxGroups(w int) [][]string {
	items := m.inboxItems()
	c := m.cursors[secInbox]
	clampCursor(&c, len(items))
	lines := maxInt(w-4, 30)
	gl := len([]rune(theme.GlyphCursor))
	groups := make([][]string, len(items))
	for i, it := range items {
		snoozed := !it.Snoozed.IsZero()
		// Snoozed rows stay visible but dim — even under the cursor —
		// and carry the parked bullet glyph so the affordance is more
		// than color (F-017; F-026 P3).
		style := theme.TextDim()
		if i == c && !snoozed {
			style = theme.TabActive()
		}
		glyph := inboxGlyph(it.Kind)
		if snoozed {
			glyph = theme.GlyphBullet
		}
		row := it.Row
		if snoozed {
			row += "  — snoozed until " + snoozeUntilText(it.Snoozed, m.now())
		}
		// Relative stamps ride rows whose At is a real instant;
		// approvals sort on a synthetic epoch and stay unstamped
		// (ADR-0011: never guess). The stamp sits right-aligned on the
		// row's FIRST line (F-041) — wrapping it in with the text parked
		// "now" alone on its own line at narrow widths.
		stamp, stampW := "", 0
		if it.Kind != inbox.Approval && !it.At.IsZero() {
			plain := timeAgo(it.At, m.now())
			stamp, stampW = theme.Hint().Render(plain), len([]rune(plain))
		}
		wrapW := lines
		if stampW > 0 {
			wrapW = maxInt(lines-stampW-2, 16)
		}
		var g []string
		first := true
		for _, ln := range kit.WrapWords(row, wrapW) {
			if first {
				prefix := strings.Repeat(" ", gl)
				if i == c {
					prefix = theme.GlyphCursor + " "
				}
				text := glyph + " " + ln
				line := prefix + style.Render(text)
				if stampW > 0 {
					pad := (lines + 2) - len([]rune(text)) - stampW
					line += strings.Repeat(" ", maxInt(pad, 2)) + stamp
				}
				g = append(g, line)
				first = false
			} else {
				g = append(g, strings.Repeat(" ", gl+1)+style.Render(ln))
			}
		}
		groups[i] = g
	}
	return groups
}

// inboxScroll reports the visual-row total and the rows before the
// window's first item, for the pane scrollbar (F-025). Falls to (0,0)
// for the informational single-line states (which never overflow).
func (m *Model) inboxScroll(w int) (total, offset int) {
	items := m.inboxItems()
	if m.unreadErr != "" || len(items) == 0 {
		return 1, 0
	}
	if pw := inboxPreviewWidth(w); pw > 0 {
		w -= pw + 1 // the list beside the preview (same split as inboxBody)
	}
	groups := m.inboxGroups(w)
	start := clampInt(m.offsets[secInbox], 0, len(items)-1)
	for i, g := range groups {
		if i < start {
			offset += len(g)
		}
		total += len(g)
	}
	return total, offset
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

func threadRef(channel string, id int64) string {
	if id == 0 {
		return channel
	}
	return fmt.Sprintf("%s#%d", channel, id)
}

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

func (m *Model) modalLines() []string {
	f := &m.form
	switch f.kind {
	case fTaskRemoveConfirm:
		return confirmLines("remove task "+f.target()+"?",
			"the card is deleted; recorded worktrees",
			"stay on disk — detach them first to clean up.", f)
	case fTaskAttach:
		where := m.worktreeHint
		if where == "" {
			where = ".dhi/tasks/<slug>/<member>"
		}
		hint := "creates " + where + " via hermetic git"
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
	case fTaskComment:
		return "comment on task"
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
