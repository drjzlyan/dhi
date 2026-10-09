package reviewer

import (
	"fmt"
	"strconv"
	"strings"

	"charm.land/lipgloss/v2"

	"github.com/drjzlyan/dhi/internal/tui/kit"
	"github.com/drjzlyan/dhi/internal/tui/theme"
)

// The review screen (F-049 R-E): on a wide terminal an open review shows
// FILES | DIFF | CONVERSATION side by side, like a pull request page. It
// is a different arrangement of the same state — the DIFF column is the
// ordinary diff section, so every key, composer and dialog keeps working —
// and narrower terminals keep the section view.

const (
	screenFilesW = 30
	screenConvW  = 38
)

// screenMode reports whether the three-column review screen is showing.
func (m *Model) screenMode() bool {
	if m.width < kit.WWide || m.sec != secDiff || m.transcriptOpen {
		return false
	}
	_, ok := m.openReview()
	return ok && len(m.files) > 0
}

func (m *Model) screenView() string {
	h := m.height
	mid := m.mainPane(m.width-screenFilesW-screenConvW, h)
	return lipgloss.JoinHorizontal(lipgloss.Top,
		m.screenFiles(screenFilesW, h), mid, m.screenConversation(screenConvW, h))
}

// screenFiles is the FILES column: every changed file with its ± stats and
// a tick when viewed; the file under the diff cursor is marked.
func (m *Model) screenFiles(w, h int) string {
	r, _ := m.openReview()
	m.syncFileCur()
	inner := w - 4
	body := h - 3
	rows := make([]string, 0, len(m.files))
	for i := range m.files {
		f := &m.files[i]
		adds, dels := f.Stat()
		mark := " "
		if r.Viewed[f.DisplayPath()] {
			mark = theme.GlyphCheck
		}
		style := theme.TextDim()
		if i == m.fileCur {
			style = theme.TabActive()
		}
		glyph, kc := theme.FileKind(f.DisplayPath())
		nameW := maxInt(inner-6-1-len(fmt.Sprintf("+%d -%d", adds, dels)), 6)
		name := padTo(crop(baseDir(f.DisplayPath()), nameW), nameW)
		row := cursorGlyph(i == m.fileCur) + theme.SuccessText().Render(mark) + " " +
			lipgloss.NewStyle().Foreground(kc).Render(glyph) + " " +
			style.Render(name) + " " +
			theme.SuccessText().Render(fmt.Sprintf("+%d", adds)) + " " + theme.DangerText().Render(fmt.Sprintf("-%d", dels))
		if i == m.fileCur { // the file in view reads as a band (F-064)
			row = kit.PaintRow(row, inner, lipgloss.NewStyle().Background(theme.Current.BgSelection))
		}
		rows = append(rows, row)
	}
	head := []string{theme.TextStyle().Bold(true).Render(crop(r.ID, inner))}
	if r.Target.PRNumber > 0 {
		head = append(head, kit.Pill("PR #"+strconv.Itoa(r.Target.PRNumber), theme.Current.Info))
	}
	viewed := 0
	for i := range m.files {
		if r.Viewed[m.files[i].DisplayPath()] {
			viewed++
		}
	}
	head = append(head, viewedMeter(viewed, len(m.files), minInt(inner-12, 12))+
		theme.TextMuted().Render(fmt.Sprintf(" %d/%d viewed", viewed, len(m.files))), "")
	avail := maxInt(body-len(head), 1)
	start := 0
	if m.fileCur >= avail {
		start = m.fileCur - avail + 1
	}
	end := minInt(start+avail, len(rows))
	m.screenRows, m.screenFirst = len(head), start
	content := append(head, rows[start:end]...)
	p := kit.NewPanel("files", false)
	p.Width, p.Height = w, h
	p.SetContent(padLines(content, body)...)
	if len(rows) > avail {
		p.SetScroll(kit.NewScroller(len(rows), avail, start))
	}
	p.SetFooter(kit.HintBar(inner, "", "n/p file", "v viewed"))
	return p.View()
}

// viewedMeter is a w-cell progress bar of reviewed files (F-064).
func viewedMeter(done, total, w int) string {
	if w < 3 || total == 0 {
		return ""
	}
	fill := done * w / total
	return theme.SuccessText().Render(strings.Repeat("━", fill)) +
		theme.RuleText().Render(strings.Repeat("━", w-fill))
}

// baseDir shows a path as "dir/file" so long trees stay recognisable.
func baseDir(p string) string {
	parts := strings.Split(p, "/")
	if len(parts) <= 2 {
		return p
	}
	return "…/" + strings.Join(parts[len(parts)-2:], "/")
}

// screenConversation is the CONVERSATION column: where the review stands
// and the discussion on the file in view. Reading only — the diff column's
// t / a / y / e / x / S keys act on it.
func (m *Model) screenConversation(w, h int) string {
	r, _ := m.openReview()
	inner := w - 4
	body := h - 3
	var out []string
	state := string(r.Status)
	if r.Posted {
		state = "sent"
		if r.Verdict != "" {
			state += " · " + r.Verdict
		}
	}
	stateC := theme.Current.Warning
	if r.Posted {
		stateC = theme.Current.Success
	}
	out = append(out, theme.SectionHeader().Render("REVIEW")+" "+kit.Pill(crop(state, inner-9), stateC))
	if p := r.PendingCount(); p > 0 {
		out = append(out, theme.WarningText().Render(fmt.Sprintf("● %d draft(s) · S sends one review", p)))
	}
	if n := r.OpenSuggestions(); n > 0 {
		out = append(out, theme.AccentText().Render(fmt.Sprintf("◇ %d suggestion(s) to decide", n)))
	}
	out = append(out, "")
	path := ""
	if m.fileCur < len(m.files) {
		path = m.files[m.fileCur].DisplayPath()
	}
	out = append(out, theme.SectionHeader().Render("ON THIS FILE"), theme.TextDim().Render(crop(path, inner)))
	rows, order := flatThreads(r, path)
	if len(rows) == 0 {
		out = append(out, theme.TextDim().Render("no discussion on this file"), theme.Hint().Render("c comments on the line"))
	}
	for _, tr := range rows {
		t := order[tr.thread]
		if tr.comment < 0 {
			loc := "file"
			if t.Line > 0 {
				loc = "line " + strconv.Itoa(t.Line)
			}
			tag := ""
			if t.Resolved {
				tag = theme.SuccessText().Render(" ✓")
			}
			out = append(out, "", theme.TabActive().Render(loc)+tag)
			continue
		}
		c := t.Comments[tr.comment]
		who, mark := c.Author, " "
		switch {
		case c.Suggested:
			who, mark = c.Author+" suggests", "◇"
		case c.Pending:
			mark = "●"
		}
		out = append(out, theme.TextMuted().Render(mark+" ")+
			lipgloss.NewStyle().Foreground(theme.AuthorColor(c.Author)).Bold(true).Render(crop(who, inner-2)))
		for _, seg := range kit.WrapWords(c.Text, maxInt(inner-2, 8)) {
			out = append(out, "  "+theme.TextStyle().Render(seg))
		}
	}
	p := kit.NewPanel("conversation", false)
	p.Width, p.Height = w, h
	p.SetContent(padLines(clipLines(out, body), body)...)
	p.SetFooter(kit.HintBar(inner, "", "t threads", "S submit"))
	return p.View()
}

func padLines(l []string, n int) []string {
	for len(l) < n {
		l = append(l, "")
	}
	return l
}

func clipLines(l []string, n int) []string {
	if len(l) > n {
		return l[:n]
	}
	return l
}

// clickScreen selects the file under a click in the FILES column and jumps
// the diff to it; clicks elsewhere are left to the diff.
func (m *Model) clickScreen(x, y int) bool {
	if x >= screenFilesW {
		return false
	}
	fi := m.screenFirst + y - 1 - m.screenRows // 1 = the panel's top edge
	if y-1-m.screenRows < 0 || fi < 0 || fi >= len(m.files) {
		return false
	}
	m.fileCur = fi
	m.jumpToFile(fi)
	return true
}
