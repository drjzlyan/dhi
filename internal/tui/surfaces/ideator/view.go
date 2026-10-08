package ideator

import (
	"fmt"
	"sort"
	"strings"

	"charm.land/lipgloss/v2"

	"github.com/drjzlyan/dhi/internal/agentkit/bus"
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
			row.Count = itoa(counts[s])
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

// sectionCounts feeds the rail: sessions, participants, artifacts and
// the transcript's agent-message count.
func (m *Model) sectionCounts() [secCount]int {
	var c [secCount]int
	if m.store != nil {
		c[secSessions] = len(m.store.Sessions()) + len(m.store.PendingProposals())
		if s, ok := m.openSession(); ok {
			c[secParticipants] = len(s.Agents)
			c[secCanvas] = len(s.Artifacts)
			if m.bus != nil {
				for _, msg := range m.bus.History(s.Channel, 0) {
					if msg.Author != bus.Human {
						c[secTranscript]++
					}
				}
			}
		}
	}
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
	case m.opErr != "":
		return theme.ChromeStatus(theme.Current.Danger).Render("✗ " + m.opErr)
	}
	return ""
}

// sectionHints is the active section's keymap for the chrome bar.
func (m *Model) sectionHints() []string {
	switch m.sec {
	case secSessions:
		return []string{"n new", "b breakout", "enter open", "a/x proposals", "x remove"}
	case secParticipants:
		return []string{"f floor", "m moderator", "a invite", "x remove"}
	case secCanvas:
		return []string{"j/k select", "J/K scroll", "s scan", "v/a/r review", "e edit"}
	case secTranscript:
		return []string{"i compose", "enter send"}
	}
	return nil
}

func (m *Model) activeSectionFor(w, h int) string {
	switch m.sec {
	case secParticipants:
		return m.participantsBody(w - 4)
	case secCanvas:
		return m.canvasBody(w-4, maxInt(h-4, 8))
	case secTranscript:
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
	case secParticipants:
		return m.participantsBody(maxInt(m.width-8, 40))
	case secCanvas:
		return m.canvasBody(maxInt(m.width-8, 40), maxInt(m.height-8, 8))
	case secTranscript:
		return m.chatBody(maxInt(m.width-8, 40), maxInt(m.height-8, 12))
	default:
		return m.sessionsBody(maxInt(m.width-8, 40))
	}
}

// ---- SESSIONS ----

func (m *Model) sessionsBody(w int) string {
	if m.store == nil {
		return theme.DangerText().Render("(session store unavailable)")
	}
	var out []string
	if warn := m.store.Warnings(); len(warn) > 0 {
		out = append(out, theme.DangerText().Render(
			fmt.Sprintf("%d malformed card(s) skipped", len(warn))))
	}
	rows := m.sessionRows()
	c := m.cursors[secSessions]
	clampCursor(&c, len(rows))
	if len(rows) == 0 {
		out = append(out, kit.EmptyState{
			Title:  "No sessions yet",
			Why:    "A session is a round-table: invite agents, pass the floor, and shape ideas on a shared canvas.",
			Action: "press n to start one",
		}.Lines(w, 0)...)
	}
	for i, row := range rows {
		active := i == c
		style := theme.TextDim()
		if active {
			style = theme.TabActive()
		}
		indent := strings.Repeat("  ", row.depth)
		if row.proposal != nil {
			p := row.proposal
			line := indent + cursorGlyph(active) +
				theme.WarningText().Render("propose ") +
				style.Render(padTo(crop(p.Name, 24), 26)) +
				theme.Hint().Render(crop("["+string(p.Mode)+"] by "+p.Caller, maxInt(w-40, 12)))
			out = append(out, line)
			if active {
				out = append(out, indent+"      "+theme.TextDim().Render(
					"a accept · x decline · does not open until you accept"))
			}
			continue
		}
		s := row.sess
		line := indent + cursorGlyph(active) +
			style.Render(padTo(crop(s.ID, 24), 26)) +
			modeChip(s) + " " +
			theme.Hint().Render(crop(s.Topic+"  ["+itoa(len(s.Agents))+"]", maxInt(w-44, 8)))
		out = append(out, line)
		if active {
			detail := s.Channel
			if s.Moderator != "" {
				detail += " · moderator " + s.Moderator
			}
			if h := s.CurrentSpeaker(); h != "" {
				detail += " · floor " + h
			} else {
				detail += " · floor you"
			}
			if n := len(m.store.Breakouts(s.ID)); n > 0 {
				detail += fmt.Sprintf(" · %d breakout(s)", n)
			}
			out = append(out, indent+"      "+theme.TextDim().Render(crop(detail, maxInt(w-8, 12))))
		}
	}
	return strings.Join(out, "\n")
}

// modeChip renders the session mode as a colored tag.
func modeChip(s *ideation.Session) string {
	switch s.Mode {
	case ideation.ModeOneOnOne:
		return theme.InfoText().Render("[1:1]")
	case ideation.ModeBreakout:
		return theme.AccentText().Render("[breakout]")
	default:
		return theme.TextDim().Render("[group]")
	}
}

// ---- PARTICIPANTS ----

func (m *Model) participantsBody(w int) string {
	sess, ok := m.openSession()
	if !ok {
		return theme.TextDim().Render("(no session open — pick one under SESSIONS)")
	}
	rows := m.participantRows()
	c := m.cursors[secParticipants]
	clampCursor(&c, len(rows))
	holder := sess.CurrentSpeaker()
	out := []string{theme.TextDim().Render(
		"moderator: " + moderatorName(sess) + " · floor: " + floorName(holder))}
	for i, name := range rows {
		active := i == c
		style := theme.TextDim()
		if active {
			style = theme.TabActive()
		}
		marker := "  "
		switch {
		case i == 0 && holder == "":
			marker = theme.SuccessText().Render("● ")
		case i == 0 && sess.Moderator != "" && holder == sess.Moderator:
			marker = theme.SuccessText().Render("● ")
		case i > 0 && name == holder:
			marker = theme.SuccessText().Render("● ")
		}
		role := ""
		switch {
		case i == 0 && sess.Moderator == "":
			role = " (you · moderator)"
		case i == 0:
			role = " (moderator)"
		case name == sess.Moderator:
			role = " (moderator)"
		}
		line := cursorGlyph(active) + marker + style.Render(crop(name, maxInt(w-16, 8))) +
			theme.Hint().Render(role)
		if !active {
			line = "  " + marker + style.Render(crop(name, maxInt(w-16, 8))) + theme.Hint().Render(role)
		}
		out = append(out, line)
	}
	return strings.Join(out, "\n") // the key hints live on the bottom bar only (F-025)
}

func moderatorName(sess ideation.Session) string {
	if sess.Moderator == "" {
		return "you"
	}
	return sess.Moderator
}

func floorName(holder string) string {
	if holder == "" {
		return "you"
	}
	return holder
}

// ---- CANVAS ----

// sortedArtifacts orders a session's artifacts: draft (newest work)
// → reviewed → approved → rejected last, path order within a status.
func sortedArtifacts(arts []ideation.Artifact) []ideation.Artifact {
	rank := map[ideation.ArtifactStatus]int{
		ideation.StatusDraft: 0, ideation.StatusReviewed: 1,
		ideation.StatusApproved: 2, ideation.StatusRejected: 3,
	}
	out := append([]ideation.Artifact{}, arts...)
	sort.SliceStable(out, func(i, j int) bool {
		ri, rj := rank[out[i].Status], rank[out[j].Status]
		if ri != rj {
			return ri < rj
		}
		return out[i].Path < out[j].Path
	})
	return out
}

// canvasBody renders the artifact list and the selected artifact's live
// preview (markdown/mermaid/raw) inside one pane (F-033 Part B/C).
func (m *Model) canvasBody(w, h int) string {
	sess, ok := m.openSession()
	if !ok {
		return theme.TextDim().Render("(no session open — pick one under SESSIONS)")
	}
	out := []string{theme.Hint().Render(sess.ID) +
		theme.TextDim().Render("  canvas · live preview · e edit in editor")}
	arts := sortedArtifacts(sess.Artifacts)
	if len(arts) == 0 {
		out = append(out, theme.TextDim().Render(
			"(none yet — agents write with artifact_create under .dhi/sessions/"+sess.ID+"/)"))
		return strings.Join(out, "\n")
	}
	c := m.cursors[secCanvas]
	clampCursor(&c, len(arts))
	for i, a := range arts {
		active := i == c
		style := theme.TextDim()
		if active {
			style = theme.TabActive()
		}
		line := cursorGlyph(active) +
			style.Render(padTo(crop(a.Path, maxInt(w-24, 10)), maxInt(w-22, 12))) +
			statusChip(a)
		out = append(out, line)
		if active && (a.Author != "" || a.Notes != "") {
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
	out = append(out, "")
	// Live preview of the selected artifact, windowed by the remaining rows.
	avail := h - len(out)
	if avail < 3 {
		avail = 3
	}
	rel, _ := m.artifactRelAt(c)
	e := m.loadPreview(w)
	total := len(e.lines)
	top := m.previewTop
	if top > total {
		top = 0
	}
	for i := top; i < total && len(out) < h; i++ {
		out = append(out, e.lines[i])
	}
	if top+avail < total {
		out = append(out, theme.Hint().Render(
			fmt.Sprintf("… %d more (J/K scroll)", total-top-avail)))
	}
	_ = rel
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
			fieldLine(f.fields[3], f.cur == 3),
			"",
		}
		if f.err != "" {
			lines = append(lines, theme.DangerText().Render(f.err), "")
		}
		return append(lines, theme.Hint().Render(
			"tab field · ←/→ mode · enter create"))
	case fNewBreakout:
		lines := []string{
			theme.TextDim().Render("nests under " + f.parent),
			fieldLine(f.fields[0], f.cur == 0),
			fieldLine(f.fields[1], f.cur == 1),
			fieldLine(f.fields[2], f.cur == 2),
			"",
		}
		if f.err != "" {
			lines = append(lines, theme.DangerText().Render(f.err), "")
		}
		return append(lines, theme.Hint().Render("enter open breakout"))
	case fRemoveConfirm:
		return confirmLines("remove session "+f.target()+"?",
			[]string{"the card is deleted. Artifacts under",
				".dhi/sessions/ stay on disk. A session",
				"with breakouts refuses until they go."}, f)
	case fRemoveParticipant:
		return confirmLines("remove participant "+f.target()+"?",
			[]string{"they can be invited again at any time."}, f)
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
	case fAddParticipant:
		lines := []string{
			theme.TextDim().Render("invite agents to this session"),
			fieldLine(f.fields[0], f.cur == 0),
			"",
		}
		if f.err != "" {
			lines = append(lines, theme.DangerText().Render(f.err), "")
		}
		return append(lines, theme.Hint().Render("comma-separated agent ids · enter invite"))
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
		return theme.Hint().Render("enter submit · esc cancel")
	}
}

func modalTitle(k modalKind) string {
	switch k {
	case fNewSession:
		return "new session"
	case fNewBreakout:
		return "new breakout"
	case fRemoveConfirm:
		return "remove session"
	case fRemoveParticipant:
		return "remove participant"
	case fReject:
		return "reject artifact"
	case fAddParticipant:
		return "invite participants"
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
