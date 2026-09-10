package ideator

import (
	"fmt"
	"strings"

	"charm.land/lipgloss/v2"

	"github.com/drjzlyan/dhi/internal/ideation"
	"github.com/drjzlyan/dhi/internal/tui/branding"
	"github.com/drjzlyan/dhi/internal/tui/kit"
	"github.com/drjzlyan/dhi/internal/tui/theme"
)

const railWidth = 20

// View renders the ideator floor: docked rail + active pane on wide
// terminals, full-width stack on middle widths, centered fallback on
// truly tiny ones, brand hero when not inside a workspace (F-025).
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
	case secSessions:
		return []string{"n new", "enter open", "x remove"}
	case secArtifacts:
		return []string{"enter preview", "s scan", "v reviewed", "a approve", "r reject"}
	case secPreview:
		return []string{"j/k scroll", "esc back"}
	case secChat:
		return []string{"i compose", "enter send"}
	}
	return nil
}

func (m *Model) activeSectionFor(w, h int) string {
	switch m.sec {
	case secArtifacts:
		return m.artifactsBody(w - 4)
	case secPreview:
		return m.previewBody(w-4, maxInt(h-4, 6))
	case secChat:
		return m.chatBody(w-4, maxInt(h-4, 6))
	default:
		return m.sessionsBody(w - 4)
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
	case secArtifacts:
		return m.artifactsBody(maxInt(m.width-8, 40))
	case secPreview:
		return m.previewBody(maxInt(m.width-8, 40), maxInt(m.height-8, 12))
	case secChat:
		return m.chatBody(maxInt(m.width-8, 40), maxInt(m.height-8, 12))
	default:
		return m.sessionsBody(maxInt(m.width-8, 40))
	}
}

// ---- section bodies ----

func (m *Model) sessionsBody(w int) string {
	rows := m.sessions()
	c := m.cursors[secSessions]
	clampCursor(&c, len(rows))

	var out []string
	if m.store == nil {
		out = append(out, theme.DangerText().Render("(session store unavailable)"))
		return strings.Join(out, "\n")
	}
	if warn := m.store.Warnings(); len(warn) > 0 {
		out = append(out, theme.DangerText().Render(
			fmt.Sprintf("%d malformed card(s) skipped", len(warn))))
	}
	if len(rows) == 0 {
		out = append(out, theme.TextDim().Render("(none — press n to start one)"))
	}
	for i, s := range rows {
		style := theme.TextDim()
		if i == c {
			style = theme.TabActive()
		}
		line := cursorGlyph(i == c) +
			style.Render(padTo(crop(s.ID, 26), 28)) +
			theme.Hint().Render(crop(s.Topic+"  ["+itoa(len(s.Agents))+" invited]",
				maxInt(w-32, 12)))
		out = append(out, line)
		if i == c {
			detail := s.Channel
			if len(s.Agents) > 0 {
				detail += " · " + strings.Join(s.Agents, ", ")
			}
			if n := len(s.Artifacts); n > 0 {
				detail += fmt.Sprintf(" · %d artifact(s)", n)
			}
			out = append(out, "      "+theme.TextDim().Render(detail))
		}
	}
	return strings.Join(out, "\n")
}

func (m *Model) artifactsBody(w int) string {
	var out []string
	sess, ok := m.openSession()
	if !ok {
		out = append(out, theme.TextDim().Render("(no session open — pick one under SESSIONS)"))
		return strings.Join(out, "\n")
	}
	arts := sess.Artifacts
	if len(arts) == 0 {
		out = append(out, theme.TextDim().Render("(none yet — agents write into .dhi/sessions/"+sess.ID+"/)"))
		return strings.Join(out, "\n")
	}
	c := m.cursors[secArtifacts]
	clampCursor(&c, len(arts))
	for i, a := range arts {
		style := theme.TextDim()
		if i == c {
			style = theme.TabActive()
		}
		line := cursorGlyph(i == c) +
			style.Render(padTo(crop(a.Path, maxInt(w-24, 10)), maxInt(w-22, 12))) +
			statusChip(a)
		out = append(out, line)
		if i == c && (a.Author != "" || a.Notes != "") {
			detail := ""
			if a.Author != "" {
				detail += "by " + a.Author
			}
			if a.Notes != "" {
				if detail != "" {
					detail += " · "
				}
				detail += "notes: " + a.Notes
			}
			out = append(out, "      "+theme.TextDim().Render(crop(detail, maxInt(w-8, 12))))
		}
	}
	return strings.Join(out, "\n")
}

// statusChip renders the artifact state with per-state coloring.
func statusChip(a ideation.Artifact) string {
	switch a.Status {
	case ideation.StatusApproved:
		return theme.SuccessText().Render(padTo("✓ approved", 11))
	case ideation.StatusRejected:
		return theme.DangerText().Render(padTo("✗ rejected", 11))
	case ideation.StatusReviewed:
		return theme.WarningText().Render(padTo("● reviewed", 11))
	default:
		return theme.Hint().Render(padTo("○ draft", 11))
	}
}

func cursorGlyph(active bool) string {
	if active {
		return theme.GlyphCursor + " "
	}
	return "  "
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
	case fNewSession:
		lines := []string{
			theme.TextDim().Render("opens a channel and an artifact folder"),
			fieldLine(f.fields[0], f.cur == 0),
			fieldLine(f.fields[1], f.cur == 1),
			fieldLine(f.fields[2], f.cur == 2),
			"",
		}
		if f.err != "" {
			lines = append(lines, theme.DangerText().Render(f.err), "")
		}
		return append(lines, theme.Hint().Render(
			"agents are mentioned by id · tab field · enter create"))
	case fRemoveConfirm:
		return confirmLines("remove session "+f.target()+"?",
			[]string{"the card is deleted. Artifacts under",
				".dhi/sessions/ stay on disk — nothing",
				"you ideated is ever garbage-collected."}, f)
	case fReject:
		lines := []string{
			theme.TextDim().Render("routes back to the authoring agent"),
			fieldLine(f.fields[0], f.cur == 0),
			"",
		}
		if f.err != "" {
			lines = append(lines, theme.DangerText().Render(f.err), "")
		}
		return append(lines, theme.Hint().Render(
			"notes post to the session channel · enter reject"))
	}

	lines := make([]string, 0, len(f.fields)*2+3)
	for i, fl := range f.fields {
		lines = append(lines, fieldLine(fl, i == f.cur && !f.busy))
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
		return theme.Hint().Render("name · topic · invited agents")
	}
}

func modalTitle(k modalKind) string {
	switch k {
	case fNewSession:
		return "new session"
	case fRemoveConfirm:
		return "remove session"
	case fReject:
		return "reject artifact"
	}
	return ""
}

func padTo(s string, n int) string {
	if len(s) >= n {
		return s
	}
	return s + strings.Repeat(" ", n-len(s))
}

func crop(s string, n int) string {
	r := []rune(s)
	if len(r) <= n {
		return s
	}
	if n <= 1 {
		return string(r[:1])
	}
	return string(r[:n-1]) + "…"
}

func itoa(n int) string {
	return fmt.Sprintf("%d", n)
}
