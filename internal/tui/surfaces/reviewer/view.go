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

func (m *Model) railView(h int) string {
	rows := make([]kit.RailRow, 0, secCount)
	for s := sectionID(0); s < secCount; s++ {
		rows = append(rows, kit.RailRow{Label: s.label()})
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
	content = append(content, kit.HintBar(inner, m.statusFlash(), m.sectionHints()...))
	p.SetContent(content...)
	p.Width, p.Height = w, h
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
		return []string{"n new", "enter open", "s submit", "P post", "F fixer"}
	case secFiles:
		return []string{"enter diff", "v viewed", "A agent review"}
	case secDiff:
		return []string{"c comment", "t threads", "\\ split", "n/p file"}
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
		if m.threadOpen {
			return m.renderThreads(w-4, maxInt(h-4, 6))
		}
		r, _ := m.openReview()
		viewed := map[string]bool{}
		if r.ID != "" {
			viewed = r.Viewed
		}
		return m.renderDiff(w-4, maxInt(h-4, 6), viewed)
	case secFiles:
		return m.filesBody(w - 4)
	default:
		return m.reviewsBody(w - 4)
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
		return m.filesBody(maxInt(m.width-8, 40))
	default:
		return m.reviewsBody(maxInt(m.width-8, 40))
	}
}

// ---- section bodies ----

func (m *Model) reviewsBody(w int) string {
	rows := m.reviews()
	c := m.cursors[secReviews]
	clampCursor(&c, len(rows))

	out := []string{}
	if m.svc == nil {
		out = append(out, theme.DangerText().Render("(review service unavailable)"))
		return strings.Join(out, "\n")
	}
	if w := m.svc.Store().Warnings(); len(w) > 0 {
		out = append(out, theme.DangerText().Render(
			fmt.Sprintf("%d malformed card(s) skipped", len(w))))
	}
	if len(rows) == 0 {
		out = append(out, theme.TextDim().Render("(none — press n to start one)"))
	}
	for i, r := range rows {
		style := theme.TextDim()
		if i == c {
			style = theme.TabActive()
		}
		state := string(r.Status)
		if r.Posted {
			state += " · posted"
		}
		if r.Done {
			state += " · discarded"
		}
		if p := r.PendingCount(); p > 0 {
			state += fmt.Sprintf(" · %d pending", p)
		}
		line := cursorGlyph(i == c) +
			style.Render(padTo(crop(r.ID, 26), 28)) +
			theme.Hint().Render(crop(r.Title+"  ["+state+"]", maxInt(w-32, 12)))
		out = append(out, line)
		if i == c {
			detail := fmt.Sprintf("%s %s...%s in member %q",
				r.Target.Kind, r.Target.Base, shortSHA(r.Target.Head), r.Target.Member)
			if r.Target.PRNumber > 0 {
				detail += fmt.Sprintf(" · PR #%d", r.Target.PRNumber)
			}
			out = append(out, "      "+theme.TextDim().Render(detail))
		}
	}
	return strings.Join(out, "\n")
}

func (m *Model) filesBody(w int) string {
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
		out = append(out,
			cursorGlyph(i == c)+
				theme.SuccessText().Render(mark)+
				style.Render(padTo(crop(f.DisplayPath(), maxInt(w-18, 10)), maxInt(w-16, 12)))+
				theme.SuccessText().Render(fmt.Sprintf("+%d ", adds))+
				theme.DangerText().Render(fmt.Sprintf("-%d", dels)))
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
	}
	return ""
}
