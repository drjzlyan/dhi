package settings

import (
	"fmt"
	"strings"
	"time"

	"github.com/drjzlyan/dhi/internal/agentkit/lessons"
	"github.com/drjzlyan/dhi/internal/tui/kit"
	"github.com/drjzlyan/dhi/internal/tui/theme"
)

// BRAIN (F-068): everything the crew has learned, reviewable. Lessons
// apply automatically; this is where the human sees why an agent behaves
// as it does and pins, retires or shares what it learned.

// brainRow is one listed lesson (or a group header when lesson is nil).
type brainRow struct {
	header string
	lesson *lessons.Lesson
}

// brainRows lists the human's preferences first, then each rostered
// agent's private lessons; live before retired, filtered by m.brainFilter.
func (m *Model) brainRows() []brainRow {
	if m.d.Lessons == nil {
		return nil
	}
	var rows []brainRow
	add := func(header string, ls []lessons.Lesson) {
		var keep []lessons.Lesson
		for _, l := range ls {
			if m.brainFilter == "" || strings.Contains(strings.ToLower(l.Text+" "+strings.Join(l.Tags, " ")), strings.ToLower(m.brainFilter)) {
				keep = append(keep, l)
			}
		}
		if len(keep) == 0 {
			return
		}
		rows = append(rows, brainRow{header: header})
		// Pinned first, then active, retired last: the order they matter.
		for _, status := range []string{lessons.StatusPinned, lessons.StatusActive, lessons.StatusRetired} {
			for i := range keep {
				if keep[i].Status == status {
					rows = append(rows, brainRow{lesson: &keep[i]})
				}
			}
		}
	}
	if user, err := m.d.Lessons.List(""); err == nil {
		add("YOUR PREFERENCES", user)
	}
	agents, _ := m.agentRows()
	for _, a := range agents {
		all, err := m.d.Lessons.List(a.id)
		if err != nil {
			continue
		}
		var private []lessons.Lesson
		for _, l := range all {
			if l.Scope == lessons.ScopePrivate {
				private = append(private, l)
			}
		}
		add(strings.ToUpper(a.id), private)
	}
	return rows
}

// brainSelected is the lesson under the cursor (nil on none).
func (m *Model) brainSelected() *lessons.Lesson {
	n := 0
	for _, r := range m.brainRows() {
		if r.lesson == nil {
			continue
		}
		if n == m.brainCur {
			return r.lesson
		}
		n++
	}
	return nil
}

func (m *Model) brainCount() int {
	n := 0
	for _, r := range m.brainRows() {
		if r.lesson != nil {
			n++
		}
	}
	return n
}

func (m *Model) brainKey(key string) bool {
	if m.brainTyping {
		switch key {
		case "esc", "enter":
			m.brainTyping = false
		case "backspace":
			if r := []rune(m.brainFilter); len(r) > 0 {
				m.brainFilter = string(r[:len(r)-1])
			}
		default:
			if len([]rune(key)) == 1 {
				m.brainFilter += key
			} else if key == "space" {
				m.brainFilter += " "
			}
		}
		m.brainCur = min(m.brainCur, max(m.brainCount()-1, 0))
		return true
	}
	switch key {
	case "j", "down":
		if m.brainCur < m.brainCount()-1 {
			m.brainCur++
		}
		return true
	case "k", "up":
		if m.brainCur > 0 {
			m.brainCur--
		}
		return true
	case "/":
		m.brainTyping = true
		return true
	case "p", "x", "s":
		l := m.brainSelected()
		if l == nil {
			return true
		}
		m.brainAct(key, *l)
		return true
	case "m":
		l := m.brainSelected()
		if l == nil || l.Agent == "" {
			m.flash = "select one of an agent's lessons to see that agent's memory"
			return true
		}
		m.openDisplayDialog("memory · "+l.Agent, m.memoryLines(l.Agent))
		return true
	}
	return false
}

// memoryLines is an agent's memory at a glance (F-078): the rolled-up
// digest, the newest journal entries and the head of its notes.
func (m *Model) memoryLines(agent string) []string {
	if m.d.Memory == nil {
		return []string{theme.TextDim().Render("memory unavailable")}
	}
	var out []string
	sect := func(title string) { out = append(out, "", theme.SectionHeader().Render(title)) }
	sect("DIGEST (older journal, newest last)")
	if d, err := m.d.Memory.Digest(agent); err != nil {
		out = append(out, theme.DangerText().Render(err.Error()))
	} else if strings.TrimSpace(d) == "" {
		out = append(out, theme.TextDim().Render("nothing rolled up yet"))
	} else {
		ls := strings.Split(strings.TrimSpace(d), "\n")
		out = append(out, ls[max(len(ls)-20, 0):]...)
	}
	sect("JOURNAL (newest)")
	if js, err := m.d.Memory.Journal(agent, 15); err != nil {
		out = append(out, theme.DangerText().Render(err.Error()))
	} else if len(js) == 0 {
		out = append(out, theme.TextDim().Render("empty"))
	} else {
		for _, e := range js {
			out = append(out, theme.TextDim().Render(e.At.Format("2006-01-02 15:04")+" "+e.Kind)+" "+e.Text)
		}
	}
	sect("NOTES")
	if n, err := m.d.Memory.ReadNotes(agent); err != nil {
		out = append(out, theme.DangerText().Render(err.Error()))
	} else if strings.TrimSpace(n) == "" {
		out = append(out, theme.TextDim().Render("empty"))
	} else {
		ls := strings.Split(strings.TrimSpace(n), "\n")
		out = append(out, ls[:min(len(ls), 30)]...)
		if len(ls) > 30 {
			out = append(out, theme.TextDim().Render(fmt.Sprintf("… %d more lines", len(ls)-30)))
		}
	}
	return out[1:]
}

// brainAct pins/unpins (p), retires/restores (x) or shares (s) a lesson.
func (m *Model) brainAct(key string, l lessons.Lesson) {
	set := func(status, done string) {
		if err := m.d.Lessons.Set(l.Scope, l.Agent, l.ID, func(x *lessons.Lesson) { x.Status = status }); err != nil {
			m.flash = "failed: " + err.Error()
			return
		}
		m.flash = done
	}
	switch key {
	case "p":
		if l.Status == lessons.StatusPinned {
			set(lessons.StatusActive, "unpinned — it now competes on relevance")
		} else {
			if cap := m.cfg.Memory.PinnedMax; cap > 0 && m.d.Lessons.PinnedCount(l.Agent) >= cap {
				m.flash = fmt.Sprintf("failed: %d lessons are pinned already (Settings → CONFIG → pinned lessons) — unpin one first", cap)
				return
			}
			set(lessons.StatusPinned, "pinned — it rides every prompt and never decays")
		}
	case "x":
		if l.Status == lessons.StatusRetired {
			set(lessons.StatusActive, "restored")
		} else {
			set(lessons.StatusRetired, "retired — never injected again (relearning brings it back)")
		}
	case "s":
		if m.d.ShareLesson == nil {
			m.flash = "sharing unavailable (no knowledge base)"
			return
		}
		status, err := m.d.ShareLesson(l)
		if err != nil {
			m.flash = "failed: " + err.Error()
			return
		}
		m.flash = "shared to the knowledge base (" + status + ")"
	}
}

// brainView renders the section body.
func (m *Model) brainView(w int) []string {
	if m.d.Lessons == nil {
		return []string{theme.TextDim().Render("(learning unavailable — not inside a workspace)")}
	}
	rows := m.brainRows()
	filter := theme.Hint().Render("/ to filter")
	if m.brainTyping || m.brainFilter != "" {
		cursor := ""
		if m.brainTyping {
			cursor = theme.GlyphCursor
		}
		filter = theme.TextStyle().Render("filter: "+m.brainFilter) + theme.AccentText().Render(cursor)
	}
	out := []string{theme.TextDim().Render("what your crew has learned · "+kit.Count(m.brainCount(), "lesson")) + "  " + filter, ""}
	if len(rows) == 0 {
		if m.brainFilter != "" {
			return append(out, theme.Hint().Render("  nothing matches"))
		}
		return append(out,
			theme.Hint().Render("  nothing yet — agents learn when a check fails, a retry was needed,"),
			theme.Hint().Render("  or you correct them (\"no, …\", \"don't …\"), and from lesson_record"))
	}
	// Columns: cursor+glyph+meter (10) · text · meta (24) · badge (10).
	textW := max(w-10-1-24-10, 20)
	n := 0
	for _, r := range rows {
		if r.lesson == nil {
			if len(out) > 2 {
				out = append(out, "")
			}
			out = append(out, " "+theme.SectionHeader().Render(r.header)+" "+theme.Rule(max(w-len(r.header)-4, 0)))
			continue
		}
		out = append(out, m.brainLine(*r.lesson, n == m.brainCur, textW))
		n++
	}
	return out
}

// brainLine: cursor, status glyph, confidence meter, text, meta.
func (m *Model) brainLine(l lessons.Lesson, selected bool, textW int) string {
	glyph := theme.SuccessText().Render(theme.GlyphDot)
	textStyle := theme.TextStyle()
	switch l.Status {
	case lessons.StatusPinned:
		glyph = theme.AccentText().Render(theme.GlyphDiamond)
	case lessons.StatusRetired:
		glyph = theme.TextMuted().Render(theme.GlyphCross)
		textStyle = theme.TextMuted()
	}
	lead := "  "
	if selected {
		lead = theme.GlyphCursor + " "
		if l.Status != lessons.StatusRetired {
			textStyle = theme.TabActive()
		}
	}
	text := l.Text
	if r := []rune(text); len(r) > textW {
		text = string(r[:textW-1]) + "…"
	}
	meta := fmt.Sprintf("×%d · %s · %s", l.Uses, l.Source, ago(m.now(), l.Updated))
	badge := ""
	if l.Uses >= 5 && l.Confidence >= 0.8 && l.Status != lessons.StatusRetired {
		badge = " " + theme.AccentText().Render("standard?")
	}
	return lead + glyph + " " + meter(l.Confidence) + " " + textStyle.Render(padTo(text, textW)) + " " + theme.Hint().Render(padTo(meta, 24)) + badge
}

// meter renders confidence as five cells.
func meter(c float64) string {
	full := int(c*5 + 0.5)
	return theme.SuccessText().Render(strings.Repeat("▮", full)) + theme.TextMuted().Render(strings.Repeat("▯", 5-full))
}

// ago renders an age compactly ("now", "5m", "3h", "2d").
func ago(now, t time.Time) string {
	d := now.Sub(t)
	switch {
	case d < time.Minute:
		return "now"
	case d < time.Hour:
		return fmt.Sprintf("%dm", int(d.Minutes()))
	case d < 24*time.Hour:
		return fmt.Sprintf("%dh", int(d.Hours()))
	}
	return fmt.Sprintf("%dd", int(d.Hours()/24))
}
