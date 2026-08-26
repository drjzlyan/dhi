package reviewer

import (
	"fmt"
	"strconv"
	"strings"

	"charm.land/lipgloss/v2"

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
	if m.width < dockMinWidth {
		return kit.Center(m.compactBody(), maxInt(m.width, 40), maxInt(m.height, 10))
	}
	return m.dockedView()
}

func (m *Model) compactBody() string {
	body := m.sectionStrip() + "\n" + m.activeSection()
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
	lines := make([]string, 0, h)
	for s := sectionID(0); s < secCount; s++ {
		if s == m.sec {
			lines = append(lines, theme.TabActive().Render(
				padTo(theme.GlyphCursor+" "+padTo(s.label(), 8), railWidth-4)))
		} else {
			lines = append(lines, " "+
				theme.TextDim().Render(padTo(s.label(), railWidth-5)))
		}
	}
	for len(lines) < h {
		lines = append(lines, "")
	}
	if m.form.flash != "" && h >= 3 {
		lines[h-3] = theme.SuccessText().Render(crop("✓ "+m.form.flash, railWidth-1))
	}
	if h >= 2 {
		lines[h-2] = theme.Hint().Render(padTo("[ ] sections", railWidth-1))
	}
	lines = lines[:h]
	return strings.Join(lines, "\n")
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
	body := m.activeSectionFor(w, h)
	p.SetContent(strings.Split(body, "\n")...)
	p.Width, p.Height = w, h
	pane := p.View()
	if m.composer != nil {
		return m.overlayCentered(pane, composerPanel(m.composer).View())
	}
	if m.form.kind == fNone {
		return pane
	}
	modal := kit.NewPanel(modalTitle(m.form.kind), true)
	modal.SetContent(m.modalLines()...)
	return m.overlayCentered(pane, modal.View())
}

// composerPanel renders the comment input as its own small panel.
func composerPanel(c *composer) *kit.Panel {
	p := kit.NewPanel("comment", true)
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
	p.SetContent(lines...)
	p.Width = 56
	p.Height = len(lines) + 2
	return p
}

func (m *Model) overlayCentered(pane string, overlay string) string {
	pl := strings.Split(pane, "\n")
	ol := strings.Split(overlay, "\n")
	vOff := (len(pl) - len(ol)) / 2
	if vOff < 0 {
		vOff = 0
	}
	inner := maxInt(m.width-railWidth-4, 10)
	for i, line := range ol {
		y := vOff + i
		if y >= len(pl) {
			break
		}
		indent := (inner - lipgloss.Width(line)) / 2
		if indent < 0 {
			indent = 0
		}
		pl[y] = strings.Repeat(" ", indent) + line
	}
	return strings.Join(pl, "\n")
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

func itoaInt(n int) string {
	return strconv.Itoa(n)
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

	out := []string{theme.Hint().Render("review sessions") +
		theme.TextDim().Render("        n new · enter open · x discard wt · d remove")}
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
	r, ok := m.openReview()
	out := []string{theme.Hint().Render("changed files") +
		theme.TextDim().Render("         enter diff · v mark viewed")}
	if !ok {
		out = append(out, theme.TextDim().Render("(no review open — pick one under REVIEWS)"))
		return strings.Join(out, "\n")
	}
	head := fmt.Sprintf("%s  %s...%s", r.ID, r.Target.Base, shortSHA(r.Target.Head))
	if r.Target.PRNumber > 0 {
		head += fmt.Sprintf(" · PR #%d", r.Target.PRNumber)
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

func shortSHA(s string) string {
	if len(s) > 7 {
		return s[:7]
	}
	return s
}

// ---- modals ----

func (m *Model) modalView(body string) string {
	f := &m.form
	p := kit.NewPanel(modalTitle(f.kind), true)
	p.SetContent(m.modalLines()...)
	return stackOver(strings.Split(body, "\n"), p.View())
}

func (m *Model) modalLines() []string {
	f := &m.form
	switch f.kind {
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
	}
	return ""
}

func stackOver(body []string, overlay string) string {
	bl := body
	ol := strings.Split(overlay, "\n")
	vOffset := (len(bl) - len(ol)) / 2
	if vOffset < 0 {
		vOffset = 0
	}
	for i, line := range ol {
		y := vOffset + i
		if y >= len(bl) {
			break
		}
		bl[y] = line
	}
	return strings.Join(bl, "\n")
}

func dimLines(s string) string {
	var out []string
	for _, l := range strings.Split(s, "\n") {
		out = append(out, theme.TextDim().Render(l))
	}
	return strings.Join(out, "\n")
}
