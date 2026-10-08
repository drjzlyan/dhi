package reviewer

import (
	"fmt"
	"strconv"
	"strings"

	"charm.land/lipgloss/v2"

	"github.com/drjzlyan/dhi/internal/review"
	"github.com/drjzlyan/dhi/internal/tui/branding"
	"github.com/drjzlyan/dhi/internal/tui/kit"
	"github.com/drjzlyan/dhi/internal/tui/theme"
)

// dockMinWidth is the narrowest terminal that still fits rail + pane.
const dockMinWidth = 84

const railWidth = 20

// View renders the reviewer floor: docked rail + active pane on wide
// terminals, centered stack on narrow ones, brand hero when not inside
// a workspace.
func (m *Model) View() string {
	if m.ws == nil {
		lines := strings.Split(branding.HeroBlock(m.version), "\n")
		lines = append(lines, "", theme.Hint().Render("not inside a DHI workspace"))
		return kit.Center(strings.Join(lines, "\n"), maxInt(m.width, 40), maxInt(m.height, 10))
	}
	if m.width < kit.WCompact {
		return kit.Center(m.compactBody(), maxInt(m.width, 40), maxInt(m.height, 10))
	}
	if m.width < kit.WDock {
		return m.compactBody()
	}
	return m.dockedView()
}

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

// Click implements the clickHandler seam (F-041): a click on a rail row
// jumps to that section. Only the docked layout has a rail.
func (m *Model) Click(x, y int) bool {
	if m.width < kit.WDock || x >= railWidth {
		return false
	}
	rail := &kit.Rail{Rows: make([]kit.RailRow, secCount), Active: int(m.sec), Width: railWidth, Height: m.height, Foot: "x"}
	if i, ok := rail.RowAt(y); ok {
		m.sec = sectionID(i)
		return true
	}
	return false
}

func (m *Model) railView(h int) string {
	counts := m.sectionCounts()
	rows := make([]kit.RailRow, 0, secCount)
	for s := sectionID(0); s < secCount; s++ {
		row := kit.RailRow{Label: s.label()}
		if counts[s] > 0 {
			row.Count = strconv.Itoa(counts[s])
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
	c[secReviews] = len(m.reviews())
	c[secFiles] = len(m.files)
	c[secDiff] = len(m.diffRows())
	return c
}

func (m *Model) mainPane(w, h int) string {
	p := kit.NewPanel(strings.ToLower(m.sec.label()), true)
	inner := w - 4
	body := m.activeSectionFor(inner, h-3)
	content := strings.Split(body, "\n")
	for len(content) < h-3 {
		content = append(content, "")
	}
	content = content[:h-3]
	p.SetContent(content...)
	p.SetFooter(kit.HintBar(inner, m.statusFlash(), m.sectionHints()...))
	p.Width, p.Height = w, h
	// Per-pane scrollbar: the diff viewport is the one section with a
	// real scroll window (F-025); the HintBar is a footer row, so the
	// track spans exactly the diff rows.
	if m.sec == secDiff && m.transcriptOpen {
		if n := len(m.transcriptLines); n > 0 {
			p.SetScroll(kit.NewScroller(n, h-3, m.transcriptScroll))
		}
	} else if m.sec == secDiff && !m.threadOpen && len(m.diffRows()) > 0 {
		segs, _ := m.diffSegmentsView(inner-4, m.viewedSet())
		if len(segs) > 0 {
			p.SetScroll(kit.NewScroller(len(segs), h-3, m.scroll))
		}
	} else if m.sec == secFiles && len(m.files) > 0 {
		p.SetScroll(kit.NewScroller(len(m.files), h-3, m.offsets[secFiles]))
	} else if m.sec == secReviews {
		total, off := m.reviewsScroll(inner)
		p.SetScroll(kit.NewScroller(total, h-3, off))
	}
	pane := p.View()
	if m.composer != nil {
		return kit.Overlay(strings.Split(pane, "\n"), composerBox(m.composer).View(), w, h)
	}
	if m.form.kind == fNone {
		return pane
	}
	box := kit.Modal{Title: modalTitle(m.form.kind), Lines: m.modalLines()}
	return kit.Overlay(strings.Split(pane, "\n"), box.View(), w, h)
}

// viewedSet is the open review's viewed-file map, or nil when none is
// open (diff rendering treats nil as "nothing viewed").
func (m *Model) viewedSet() map[string]bool {
	if r, ok := m.openReview(); ok && r.ID != "" {
		return r.Viewed
	}
	return nil
}

// statusFlash renders the outcome segment on the chrome bar.
func (m *Model) statusFlash() string {
	switch {
	case m.form.err != "":
		return theme.ChromeStatus(theme.Current.Danger).Render("✗ " + m.form.err)
	case m.form.flash != "":
		return theme.ChromeStatus(theme.Current.Success).Render("✓ " + m.form.flash)
	}
	return ""
}

// sectionHints is the active section's keymap for the chrome bar.
func (m *Model) sectionHints() []string {
	switch m.sec {
	case secReviews:
		return []string{"n new", "enter open", "S submit review", "F fixer"}
	case secFiles:
		return []string{"enter diff", "v viewed", "A agent review", "S submit"}
	case secDiff:
		return []string{"c comment", "t threads", "S submit", "T transcript", "\\ split", "n/p file"}
	}
	return nil
}

// composerBox renders the comment input as a dialog box.
func composerBox(c *composer) *kit.Modal {
	head := theme.TextDim().Render(c.file)
	if c.line > 0 {
		head += theme.TextDim().Render(" :" + strconv.Itoa(c.line) + " " + string(c.side))
	} else {
		head += theme.TextDim().Render("  (file-level)")
	}
	lines := []string{head, ""}
	if c.editIdx >= 0 {
		lines[0] += theme.WarningText().Render("  editing draft")
	}
	if c.replyTo > 0 && c.editIdx < 0 {
		lines = append(lines, theme.TextDim().Render("replying to #"+strconv.FormatInt(c.replyTo, 10)))
	}
	lines = append(lines,
		theme.SuccessText().Render(string(c.runes))+"▏",
		"",
		theme.Hint().Render("enter save (pending) · esc cancel"))
	return &kit.Modal{Title: "comment", Lines: lines}
}

func (m *Model) activeSectionFor(w, h int) string {
	switch m.sec {
	case secDiff:
		if m.transcriptOpen {
			return m.renderTranscript(w-4, maxInt(h-4, 6))
		}
		if m.threadOpen {
			return m.renderThreads(w-4, maxInt(h-4, 6))
		}
		return m.renderDiff(w-4, maxInt(h-4, 6), m.viewedSet())
	case secFiles:
		return m.filesBody(w-4, h)
	default:
		return m.reviewsBody(w-4, h)
	}
}

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
	switch m.sec {
	case secDiff:
		return m.activeSectionFor(maxInt(m.width-8, 40), maxInt(m.height-8, 12))
	case secFiles:
		return m.filesBody(maxInt(m.width-8, 40), maxInt(m.height-8, 12))
	default:
		return m.reviewsBody(maxInt(m.width-8, 40), maxInt(m.height-8, 12))
	}
}

// ---- section bodies ----

func (m *Model) reviewsBody(w, h int) string {
	rows := m.reviews()
	c := m.cursors[secReviews]
	clampCursor(&c, len(rows))

	out := []string{}
	if m.svc == nil {
		out = append(out, theme.DangerText().Render("(review service unavailable)"))
		return strings.Join(out, "\n")
	}
	if warns := m.svc.Store().Warnings(); len(warns) > 0 {
		out = append(out, theme.DangerText().Render(
			fmt.Sprintf("%d malformed card(s) skipped", len(warns))))
	}
	if len(rows) == 0 {
		out = append(out, kit.EmptyState{
			Title:  "No reviews yet",
			Why:    "A review is a diff of a branch, worktree or pull request with threaded comments you and your agents can resolve.",
			Action: "press n to start one",
		}.Lines(w, h-len(out))...)
		return strings.Join(out, "\n")
	}

	// Scroll window (F-025): a selected row expands one detail line, so
	// window by review (groups carry their rows) with cursor-follow.
	groups := m.reviewsGroups(w)
	avail := h - len(out)
	if avail < 1 {
		avail = 1
	}
	start := clampInt(m.offsets[secReviews], 0, len(rows)-1)
	if c < start {
		start = c
	}
	render := func(from int) (rs []string, last int) {
		last = from - 1
		for i := from; i < len(rows); i++ {
			g := groups[i]
			if len(rs) > 0 && len(rs)+len(g) > avail {
				break
			}
			rs = append(rs, g...)
			last = i
			if len(rs) >= avail {
				break
			}
		}
		return rs, last
	}
	window, last := render(start)
	if c > last { // cursor below the window: restart at it
		start = c
		window, _ = render(start)
	}
	m.offsets[secReviews] = start
	out = append(out, window...)
	return strings.Join(out, "\n")
}

// reviewsGroups renders each review card to its rows (the selected one
// carries its detail line), for the pane scroll window.
func (m *Model) reviewsGroups(w int) [][]string {
	rows := m.reviews()
	c := m.cursors[secReviews]
	clampCursor(&c, len(rows))
	groups := make([][]string, len(rows))
	for i, r := range rows {
		style := theme.TextDim()
		if i == c {
			style = theme.TabActive()
		}
		var chips []string
		switch {
		case r.Done:
			chips = append(chips, theme.TextMuted().Render("discarded"))
		default:
			chips = append(chips, string(r.Status))
		}
		if r.Posted {
			chips = append(chips, theme.SuccessText().Render("posted"))
		}
		if p := r.PendingCount(); p > 0 {
			chips = append(chips, theme.WarningText().Render(itoa(p)+" pending"))
		}
		state := strings.Join(chips, " · ")
		g := []string{cursorGlyph(i == c) +
			style.Render(padTo(crop(r.ID, 26), 28)) +
			theme.Hint().Render(crop(r.Title+"  ["+state+"]", maxInt(w-32, 12)))}
		if i == c {
			detail := fmt.Sprintf("%s %s...%s in member %q",
				r.Target.Kind, r.Target.Base, shortSHA(r.Target.Head), r.Target.Member)
			if r.Target.PRNumber > 0 {
				detail += fmt.Sprintf(" · PR #%d", r.Target.PRNumber)
			}
			g = append(g, "      "+theme.TextDim().Render(detail))
		}
		groups[i] = g
	}
	return groups
}

// reviewsScroll reports the visual-row total and the rows before the
// window's first review, for the pane scrollbar (F-025).
func (m *Model) reviewsScroll(w int) (total, offset int) {
	rows := m.reviews()
	if m.svc == nil || len(rows) == 0 {
		return 1, 0
	}
	groups := m.reviewsGroups(w)
	start := clampInt(m.offsets[secReviews], 0, len(rows)-1)
	for i, g := range groups {
		if i < start {
			offset += len(g)
		}
		total += len(g)
	}
	return total, offset
}

func (m *Model) filesBody(w, h int) string {
	var out []string
	r, ok := m.openReview()
	if !ok {
		out = append(out, theme.TextDim().Render("(no review open — pick one under REVIEWS)"))
		return strings.Join(out, "\n")
	}
	head := fmt.Sprintf("%s  %s...%s", r.ID, r.Target.Base, shortSHA(r.Target.Head))
	if r.Target.PRNumber > 0 {
		head += fmt.Sprintf(" · PR #%d", r.Target.PRNumber)
		if n := remoteCount(r); n > 0 {
			head += fmt.Sprintf(" · %d remote", n)
		}
		if !m.syncedAt.IsZero() {
			head += " · synced " + m.syncedAt.Format("15:04")
		}
	}
	out = append(out, theme.TextDim().Render(head))
	if m.busy && m.diffFor == "" {
		out = append(out, theme.TabActive().Render("loading diff…"))
		return strings.Join(out, "\n")
	}
	if m.opErr != "" && m.diffFor != r.ID {
		out = append(out, theme.DangerText().Render(m.opErr))
	}
	if len(m.files) == 0 {
		out = append(out, theme.TextDim().Render("(no files)"))
		return strings.Join(out, "\n")
	}
	c := m.cursors[secFiles]
	clampCursor(&c, len(m.files))

	var rows []string
	for i := range m.files {
		f := &m.files[i]
		adds, dels := f.Stat()
		mark := " "
		if r.Viewed[f.DisplayPath()] {
			mark = theme.GlyphCheck
		}
		style := theme.TextDim()
		if i == c {
			style = theme.TabActive()
		}
		rows = append(rows,
			cursorGlyph(i == c)+
				theme.SuccessText().Render(mark)+
				style.Render(padTo(crop(f.DisplayPath(), maxInt(w-18, 10)), maxInt(w-16, 12)))+
				theme.SuccessText().Render(fmt.Sprintf("+%d ", adds))+
				theme.DangerText().Render(fmt.Sprintf("-%d", dels)))
	}

	// Scroll window (F-025): header rows stay pinned; the file rows
	// window with cursor-follow. offsets[secFiles] is a file index.
	avail := h - len(out)
	if avail < 1 {
		avail = 1
	}
	off := clampInt(m.offsets[secFiles], 0, len(rows)-1)
	if c < off {
		off = c
	} else if c >= off+avail {
		off = c - avail + 1
	}
	if max := len(rows) - avail; off > max {
		off = max
	}
	if off < 0 {
		off = 0
	}
	m.offsets[secFiles] = off
	end := minInt(off+avail, len(rows))
	if end > off {
		out = append(out, rows[off:end]...)
	}
	return strings.Join(out, "\n")
}

func cursorGlyph(active bool) string {
	if active {
		return theme.GlyphCursor + " "
	}
	return "  "
}

// remoteCount counts comments mirrored from GitHub.
func remoteCount(r review.Review) int {
	n := 0
	for _, t := range r.Threads {
		for _, c := range t.Comments {
			if c.RemoteID != 0 {
				n++
			}
		}
	}
	return n
}

func shortSHA(s string) string {
	if len(s) > 7 {
		return s[:7]
	}
	return s
}

// ---- modals ----

func (m *Model) modalView(body string) string {
	box := kit.Modal{Title: modalTitle(m.form.kind), Lines: m.modalLines()}
	return kit.Overlay(strings.Split(body, "\n"), box.View(),
		maxInt(m.width, 40), maxInt(m.height, 10))
}

func (m *Model) modalLines() []string {
	f := &m.form
	switch f.kind {
	case fSubmit, fSubmitConfirm:
		return m.submitLines()
	case fCreatePR:
		lines := []string{
			theme.TextDim().Render("pushes " + f.orig + " and opens a PR"),
			fieldLine(f.fields[0], f.cur == 0),
			fieldLine(f.fields[1], f.cur == 1),
			"",
		}
		if f.err != "" {
			lines = append(lines, theme.DangerText().Render(f.err), "")
		}
		return append(lines, theme.Hint().Render(
			"dirty worktrees are refused · tab field · enter create"))
	case fAgentReview:
		lines := []string{
			fieldLine(f.fields[0], f.cur == 0),
			fieldLine(f.fields[1], f.cur == 1),
			"",
		}
		if f.err != "" {
			lines = append(lines, theme.DangerText().Render(f.err), "")
		}
		return append(lines, theme.Hint().Render(
			"posts the diff to #review and dispatches a turn · tab field · enter send"))
	case fDiscardConfirm:
		return confirmLines("discard review worktree "+f.target()+"?",
			[]string{"the review worktree under .dhi/reviews/",
				"is removed; comments and history stay."}, f)
	case fRemoveConfirm:
		return confirmLines("remove review card "+f.target()+"?",
			[]string{"the card and its comment threads are",
				"deleted. The worktree stays on disk —",
				"discard it first to clean up."}, f)
	}

	lines := make([]string, 0, len(f.fields)*2+3)
	for i, fl := range f.fields {
		lines = append(lines, fieldLine(fl, i == f.cur && !f.busy))
		if fl.toggle != nil {
			lines = append(lines, theme.TextDim().Render("      ←/→ switches kind"))
		}
	}
	lines = append(lines, "", hintOrErr(f))
	return lines
}

func confirmLines(question string, ls []string, f *formState) []string {
	lines := []string{question}
	for _, l := range ls {
		lines = append(lines, theme.TextDim().Render(l))
	}
	lines = append(lines, "")
	if f.err != "" {
		lines = append(lines, theme.DangerText().Render(f.err), "")
	}
	return append(lines, theme.Hint().Render("enter confirm · esc keep"))
}

func fieldLine(fl field, focused bool) string {
	cursor := " "
	value := fl.text()
	if fl.toggle != nil {
		value = "< " + fl.toggleValue() + " >"
	} else if focused {
		value += "▏"
	}
	style := theme.Hint()
	if focused {
		cursor = theme.GlyphCursor
		style = theme.SuccessText()
	}
	return cursor + " " + style.Render(padTo(fl.label, 8)) + value
}

func hintOrErr(f *formState) string {
	switch {
	case f.busy:
		return theme.TabActive().Render("working…")
	case f.err != "":
		return theme.DangerText().Render(f.err)
	default:
		return theme.Hint().Render("member repo · kind ←/→ · base ref · head branch/sha or PR number")
	}
}

func modalTitle(k modalKind) string {
	switch k {
	case fNewReview:
		return "new review"
	case fDiscardConfirm:
		return "discard worktree"
	case fRemoveConfirm:
		return "remove review"
	case fAgentReview:
		return "agent review"
	case fCreatePR:
		return "create PR"
	case fSubmit, fSubmitConfirm:
		return "submit review"
	}
	return ""
}
