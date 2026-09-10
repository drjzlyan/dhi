package workspace

import (
	"fmt"
	"image/color"
	"strings"

	"charm.land/lipgloss/v2"

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
		rows = append(rows, kit.RailRow{
			Label: s.label(),
			Count: fmt.Sprintf("%d", counts[s]),
		})
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
	content = append(content, kit.HintBar(inner, m.statusFlash(), m.sectionHints()...))
	p.SetContent(content...)
	p.Width, p.Height = w, h
	pane := p.View()

	if m.form.kind == fNone {
		return pane
	}
	box := kit.Modal{Title: modalTitle(m.form.kind), Lines: m.modalLines()}
	return kit.Overlay(strings.Split(pane, "\n"), box.View(), w, h)
}

// statusFlash renders the outcome segment on the chrome bar: error >
// warning hint > success flash (F-025 Part A).
func (m *Model) statusFlash() string {
	switch {
	case m.form.err != "":
		return theme.ChromeStatus(theme.Current.Danger).Render("✗ " + m.form.err)
	case m.inboxHint != "":
		return theme.ChromeStatus(theme.Current.Warning).Render(m.inboxHint)
	case m.form.flash != "":
		return theme.ChromeStatus(theme.Current.Success).Render("✓ " + m.form.flash)
	}
	return ""
}

// sectionHints is the active section's keymap for the chrome bar.
func (m *Model) sectionHints() []string {
	switch m.sec {
	case secInbox:
		return []string{"enter/o jump", "z snooze", "u unsnooze"}
	case secBoard:
		return []string{"h/l lane", "n new", "s status", "a assign", "o thread"}
	case secChannels:
		return []string{"i compose", "t thread", "v profile", ",/. channel"}
	case secRepos:
		return []string{"a add", "r rename", "d remove"}
	}
	return nil
}

// activeSectionFor renders the active section body with pane-aware
// geometry (board and channels need real width/height). The run-replay
// pane is modal: it replaces whatever section is active.
func (m *Model) activeSectionFor(w, h int) string {
	if m.replay != nil {
		return m.replayBody()
	}
	switch m.sec {
	case secBoard:
		return m.boardBody(w, maxInt(h, 6))
	case secChannels:
		return strings.Join(m.pane.render(w, maxInt(h, 12)), "\n")
	default:
		return m.activeSection()
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
	line := strings.Join(parts, theme.TextDim().Render(" · "))
	flash := ""
	if m.form.flash != "" {
		flash = "   " + theme.SuccessText().Render(m.form.flash)
	}
	return line + flash
}

func (m *Model) activeSection() string {
	if m.replay != nil {
		return m.replayBody()
	}
	w := maxInt(m.width, 40)
	switch m.sec {
	case secInbox:
		return m.inboxBody()
	case secRepos:
		return m.reposBody()
	default:
		return m.boardBody(w-6, maxInt(m.height-8, 8))
	}
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

	detailW := 0
	if w >= kit.WWide {
		detailW = boardDetailWidth
	}
	lanesH := h - len(out) // the warning/unavailable rows, if any
	var detailLines []string
	if tk, ok := m.boardSelected(g); ok {
		detailLines = boardDetailLines(tk)
	}
	if detailW == 0 && len(detailLines) > 0 {
		// -1: the lane header row above the Height body rows.
		lanesH = h - len(out) - len(detailLines) - 1
	}
	if lanesH < 3 {
		lanesH = 3
	}

	cols := make([]kit.Column, 4)
	for i, st := range tasks.Statuses {
		rows := make([]string, 0, len(g[i]))
		for _, tk := range g[i] {
			rows = append(rows, boardCard(tk))
		}
		cols[i] = kit.Column{
			Title: string(st), Cursor: m.boardCur[i], Rows: rows,
			Accent: boardStatusColor(i),
		}
	}
	board := &kit.Columns{Cols: cols, Active: m.boardActive, Width: w - detailW, Height: lanesH}
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

// boardCard renders one lane row: slug + title, assignee chip.
func boardCard(tk tasks.Task) string {
	title := tk.Title
	if title == "" {
		title = "-"
	}
	if len(title) > 18 {
		title = title[:17] + "…"
	}
	who := tk.Assignee
	if who == "" {
		who = "unassigned"
	}
	return padTo(tk.Slug, 14) + padTo(title, 20) + theme.Hint().Render(who)
}

// boardDetailLines is the JIRA-issue fact block for the selected card.
func boardDetailLines(tk tasks.Task) []string {
	who := tk.Assignee
	if who == "" {
		who = "unassigned"
	}
	lines := []string{
		theme.Brand().Render(tk.Slug) + "  " + theme.Chip().Render(string(tk.Status)),
		tk.Title,
		theme.TextDim().Render("assignee " + who + " · team " + orDash(tk.Team)),
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
	return lines
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

func (m *Model) inboxBody() string {
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
	lines := maxInt(m.width-railWidth-12, 30)
	gl := len([]rune(theme.GlyphCursor))
	for i, it := range items {
		snoozed := !it.Snoozed.IsZero()
		// Snoozed rows stay visible but dim — even under the cursor
		// (F-017: parked, not urgent).
		style := theme.TextDim()
		if i == *c && !snoozed {
			style = theme.TabActive()
		}
		row := it.Row
		if snoozed {
			row += "  — snoozed until " + snoozeUntilText(it.Snoozed, m.now())
		}
		first := true
		for _, ln := range wordWrap(row, lines) {
			if first {
				prefix := strings.Repeat(" ", gl)
				if i == *c {
					prefix = theme.GlyphCursor + " "
				}
				out = append(out, prefix+style.Render(inboxGlyph(it.Kind)+" "+ln))
				first = false
			} else {
				out = append(out, strings.Repeat(" ", gl+1)+style.Render(ln))
			}
		}
	}
	return strings.Join(out, "\n")
}

// wordWrap breaks s at word boundaries into width-or-less lines (F-016
// inbox rows); a single word wider than width hard-breaks at width.
func wordWrap(s string, width int) []string {
	if width < 1 {
		width = 1
	}
	var out []string
	for _, para := range strings.Split(s, "\n") {
		line := ""
		for _, f := range strings.Fields(para) {
			cand := f
			if line != "" {
				cand = line + " " + f
			}
			if len([]rune(cand)) <= width {
				line = cand
				continue
			}
			if line != "" {
				out = append(out, line)
				line = ""
			}
			for len([]rune(f)) > width { // hard break
				out = append(out, string([]rune(f)[:width]))
				f = string([]rune(f)[width:])
			}
			line = f
		}
		out = append(out, line)
	}
	return out
}

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

func cursorGlyph(active bool) string {
	if active {
		return theme.GlyphCursor + " "
	}
	return "  "
}

// ---- REPOS (member repos) ----

func (m *Model) reposBody() string {
	members := m.ws.Members()
	c := m.cursors[secRepos]
	clampCursor(&c, len(members))

	var rows []string
	if len(members) == 0 {
		rows = append(rows, theme.TextDim().Render("(none — press a to add one)"))
	}
	for i, mem := range members {
		style := theme.TextDim()
		if i == c {
			style = theme.TabActive()
		}
		rows = append(rows, cursorGlyph(i == c)+
			style.Render(padTo(mem.Name, nameCol))+
			theme.Hint().Render(shorten(mem.Path, 46)))
	}
	return strings.Join(rows, "\n")
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
		for i, fl := range f.fields {
			lines = append(lines, m.fieldLine(fl, i == f.cur && !f.busy))
		}
		lines = append(lines, "", hintOrErr(f, hint))
		return lines
	case fTaskCommit:
		lines := []string{}
		for i, fl := range f.fields {
			lines = append(lines, m.fieldLine(fl, i == f.cur && !f.busy))
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

	lines := make([]string, 0, len(f.fields)*2)
	if f.orig != "" && f.kind != fAdd {
		lines = append(lines, theme.TextDim().Render("editing: "+f.target()))
	}
	for i, fl := range f.fields {
		lines = append(lines, m.fieldLine(fl, i == f.cur && !f.busy))
		if fl.isToggle() {
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
	value := fl.text()
	if fl.isToggle() {
		value = "< " + fl.toggleValue() + " >"
	} else if focused {
		value += "▏"
	}
	style := theme.Hint()
	if focused {
		cursor = theme.GlyphCursor
		style = theme.SuccessText()
	}
	return cursor + " " + style.Render(padTo(fl.label, 15)) + value
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
	if rn := len([]rune(s)); rn < w {
		return s + strings.Repeat(" ", w-rn)
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
