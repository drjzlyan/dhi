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
	m.hits.Reset()
	if m.ws == nil {
		return branding.NoWorkspace(m.width, m.height, m.version)
	}
	if m.width < kit.WDock {
		return m.compactBody()
	}
	return m.dockedView()
}

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

// clickRow selects row i of a list section; clicking the selected row
// again does what enter does there.
func (m *Model) clickRow(sec sectionID, i int) {
	if m.cursors[sec] == i {
		m.HandleKey("enter")
		return
	}
	m.cursors[sec] = i
}

// modalOpen reports a dialog over the pane (clicks then do nothing).
func (m *Model) modalOpen() bool { return m.form.kind != fNone }

// selectSection switches the active section (rail or strip click).
func (m *Model) selectSection(s sectionID) {
	m.sec = s
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
	// The first frame can arrive before any resize (0x0) and terminals
	// can be tiny; render a minimal panel and let the shell clip it.
	w, h = maxInt(w, 12), maxInt(h, 4)
	p := kit.NewPanel(strings.ToLower(m.sec.label()), true)
	inner := w - 4
	body := m.activeSectionFor(inner, h-3)
	content := strings.Split(body, "\n")
	for len(content) < h-3 {
		content = append(content, "")
	}
	content = content[:h-3]
	content = append(content, kit.HintBar(inner, m.statusFlash(), m.sectionHints()...))
	// The keymap row is clickable (F-055): a hint acts like its key.
	m.hits.Add(0, h-3, inner, 1, func(dx, _ int) {
		if k, ok := kit.HintKeyAt(inner, m.statusFlash(), dx, m.sectionHints()...); ok {
			m.HandleKey(k)
		}
	})
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
		return []string{"j/k select", "J/K scroll", "s scan", "v/a/r review", "e edit", "X export"}
	case secTranscript:
		return []string{"i compose", "enter send"}
	}
	return nil
}

func (m *Model) activeSectionFor(w, h int) string {
	switch m.sec {
	case secParticipants:
		return m.participantsBody(w)
	case secCanvas:
		return m.canvasBody(w, maxInt(h, 8))
	case secTranscript:
		return m.chatBody(w, maxInt(h, 6))
	default:
		return m.sessionsBody(w, h)
	}
}

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
	if m.form.flash != "" {
		line += "   " + theme.SuccessText().Render(m.form.flash)
	}
	return line
}

// ---- SESSIONS ----

func (m *Model) sessionsBody(w, h int) string {
	if m.store == nil {
		return kit.Notice{What: "Sessions are unavailable",
			Why:   "the session store under .dhi/ideation could not open.",
			Retry: "run dhi doctor for the cause"}.String(w)
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
		}.Lines(w, h-len(out))...)
	}
	for i, row := range rows {
		// Click zone (F-055) over this row's lines, registered once it
		// knows its height: select; click again to open.
		y0, item := len(out), i
		zone := func() {
			m.hits.Add(0, y0, w, len(out)-y0, func(int, int) { m.clickRow(secSessions, item) })
		}
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
			zone()
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
		zone()
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
	case fExport:
		lines := []string{
			theme.TextDim().Render(f.orig),
			fieldLine(f.fields[0], f.cur == 0),
			theme.Hint().Render("        ←/→ repo file · task card · tracker (via an agent's MCP tools)"),
			fieldLine(f.fields[1], f.cur == 1),
			"",
		}
		if f.err != "" {
			lines = append(lines, theme.DangerText().Render(f.err), "")
		}
		return append(lines, theme.Hint().Render("tab field · enter export · esc cancel"))
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
	case fExport:
		return "export artifact"
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
