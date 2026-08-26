package reviewer

import (
	"strconv"
	"strings"

	"github.com/drjzlyan/dhi/internal/review"
	"github.com/drjzlyan/dhi/internal/tui/theme"
)

// composer is the active comment input. replyTo==0 opens a new thread at
// the anchor; otherwise the text appends to that thread.
type composer struct {
	file    string
	line    int
	side    review.Side
	replyTo int64
	editIdx int // >=0 edits that pending comment instead of appending
	runes   []rune
}

func (c *composer) text() string { return string(c.runes) }

// anchorAtCursor derives the comment anchor from the diff cursor row.
func (m *Model) anchorAtCursor() (string, int, review.Side, bool) {
	row := m.rowAt(m.cursor)
	if row == nil {
		return "", 0, review.SideNew, false
	}
	path := m.pathAtRow(m.cursor)
	if path == "" {
		return "", 0, review.SideNew, false
	}
	switch row.kind {
	case vrFileHeader, vrBinary, vrHunkHeader:
		return path, 0, review.SideNew, true // file-level / hunk-level
	case vrLineUnified:
		if l := row.left; l != nil {
			if l.NewNo > 0 {
				return path, l.NewNo, review.SideNew, true
			}
			return path, l.OldNo, review.SideOld, true
		}
	case vrSideBySide:
		if r := row.right; r != nil && r.NewNo > 0 {
			return path, r.NewNo, review.SideNew, true
		}
		if l := row.left; l != nil && l.OldNo > 0 {
			return path, l.OldNo, review.SideOld, true
		}
	}
	return path, 0, review.SideNew, false
}

// openComposer starts a new thread (replyTo=0), a reply, or an edit.
func (m *Model) openComposer(replyTo int64, editThread int64, editIdx int) {
	r, ok := m.openReview()
	if !ok || m.svc == nil {
		return
	}
	c := &composer{replyTo: replyTo, editIdx: -1}
	if replyTo == 0 && editThread == 0 {
		file, line, side, ok := m.anchorAtCursor()
		if !ok {
			m.opErr = "cursor is not on a commentable line"
			return
		}
		c.file, c.line, c.side = file, line, side
	} else if editThread > 0 {
		c.editIdx = editIdx
		for _, t := range r.Threads {
			if t.ID == editThread {
				c.file, c.line, c.side = t.File, t.Line, t.Side
				if editIdx < len(t.Comments) {
					c.runes = []rune(t.Comments[editIdx].Text)
				}
				break
			}
		}
		c.replyTo = editThread
	} else {
		for _, t := range r.Threads {
			if t.ID == replyTo {
				c.file, c.line, c.side = t.File, t.Line, t.Side
				break
			}
		}
	}
	m.composer = c
}

func (m *Model) composerKey(key string) bool {
	c := m.composer
	switch key {
	case "esc":
		m.composer = nil
		return true
	case "enter":
		txt := strings.TrimSpace(c.text())
		if txt == "" {
			return true // empty draft: ignore
		}
		st := m.svc.Store()
		var err error
		var threadID int64
		switch {
		case c.editIdx >= 0:
			err = st.EditComment(m.openID, c.replyTo, c.editIdx, txt)
			threadID = c.replyTo
		case c.replyTo > 0:
			err = st.AppendComment(m.openID, c.replyTo,
				review.Comment{Author: busHuman(), Text: txt, Pending: true})
			threadID = c.replyTo
		default:
			threadID, err = st.AddThread(m.openID, review.Thread{
				File: c.file, Line: c.line, Side: c.side,
				Comments: []review.Comment{{Author: busHuman(), Text: txt, Pending: true}},
			})
		}
		if err != nil {
			m.opErr = err.Error()
		}
		m.composer = nil
		// @mentions dispatch the comment to the agent crew in-thread.
		if err == nil && m.mentionedAgent(txt) != "" {
			m.invite(threadID, txt)
		}
		return true
	case "backspace":
		if len(c.runes) > 0 {
			c.runes = c.runes[:len(c.runes)-1]
		}
		return true
	}
	if r := []rune(key); len(r) == 1 && r[0] >= 32 {
		c.runes = append(c.runes, r[0])
		return true
	}
	return false
}

func busHuman() string { return "you" }

// ---- thread view ----

// threadRow flattens threads+comments for cursor navigation.
type threadRow struct {
	thread   int64
	comment  int // -1 = the thread header itself
	resolved bool
}

// flatThreads lists visible rows of the thread view for one file.
func flatThreads(r review.Review, file string) ([]threadRow, map[int64]*review.Thread) {
	order := map[int64]*review.Thread{}
	var rows []threadRow
	for i := range r.Threads {
		t := &r.Threads[i]
		if t.File != file {
			continue
		}
		order[t.ID] = t
		rows = append(rows, threadRow{thread: t.ID, comment: -1, resolved: t.Resolved})
		for ci := range t.Comments {
			rows = append(rows, threadRow{thread: t.ID, comment: ci, resolved: t.Resolved})
		}
	}
	return rows, order
}

func (m *Model) threadsKey(key string) bool {
	r, ok := m.openReview()
	if !ok {
		m.threadOpen = false
		return false
	}
	rows, order := flatThreads(r, m.threadFile)
	clampCursor(&m.threadCur, len(rows))
	cur := func() *threadRow {
		if m.threadCur < len(rows) {
			return &rows[m.threadCur]
		}
		return nil
	}
	switch key {
	case "j", "down":
		if m.threadCur < len(rows)-1 {
			m.threadCur++
		}
		return true
	case "k", "up":
		if m.threadCur > 0 {
			m.threadCur--
		}
		return true
	case "a":
		if tr := cur(); tr != nil {
			m.openComposer(tr.thread, 0, 0)
			return true
		}
	case "e":
		if tr := cur(); tr != nil && tr.comment >= 0 {
			if t, okT := order[tr.thread]; okT && t.Comments[tr.comment].Pending &&
				t.Comments[tr.comment].Author == busHuman() {
				m.openComposer(0, tr.thread, tr.comment)
				return true
			}
		}
	case "x", "d":
		if tr := cur(); tr != nil && tr.comment >= 0 {
			if t, okT := order[tr.thread]; okT && t.Comments[tr.comment].Pending &&
				t.Comments[tr.comment].Author == busHuman() {
				if err := m.svc.Store().DeleteComment(r.ID, tr.thread, tr.comment); err != nil {
					m.opErr = err.Error()
				}
				fresh, _ := m.openReview()
				rows2, _ := flatThreads(fresh, m.threadFile)
				clampCursor(&m.threadCur, len(rows2))
				return true
			}
		}
	case "r":
		if tr := cur(); tr != nil {
			if err := m.svc.Store().SetResolved(r.ID, tr.thread, !tr.resolved); err != nil {
				m.opErr = err.Error()
			}
			return true
		}
	case "esc", "t", "q":
		m.threadOpen = false
		return true
	}
	return false
}

// flatThreadsRows re-counts rows after mutations.
func flatThreadsRows(r review.Review, file string) int {
	n, _ := flatThreads(r, file)
	return len(n)
}

// renderThreads paints the thread view for the anchored file.
func (m *Model) renderThreads(w, h int) string {
	r, ok := m.openReview()
	if !ok {
		m.threadOpen = false
		return theme.TextDim().Render("(no review open)")
	}
	out := []string{theme.Hint().Render("threads — "+m.threadFile) +
		theme.TextDim().Render("     a reply · e edit · x delete · r resolve · esc back")}
	rows, order := flatThreads(r, m.threadFile)
	if len(rows) == 0 {
		out = append(out, theme.TextDim().Render("(none — press c on a diff line to start one)"))
	}
	pending := r.PendingCount()
	if pending > 0 {
		out = append(out, theme.WarningText().Render(
			strconv.Itoa(pending)+" pending — submit from REVIEWS with s"))
	}
	for i, tr := range rows {
		t := order[tr.thread]
		if tr.comment < 0 {
			state := ""
			if t.Resolved {
				state = theme.SuccessText().Render(" ✓resolved")
			}
			loc := ""
			if t.Line > 0 {
				loc = ":" + strconv.Itoa(t.Line) + string(t.Side[0])
			}
			style := theme.TabActive()
			if i != m.threadCur {
				style = theme.TextDim()
			}
			out = append(out, cursorGlyph(i == m.threadCur)+
				style.Render("#"+strconv.FormatInt(t.ID, 10)+" "+crop(t.File+loc, maxInt(w-12, 8)))+state)
			continue
		}
		c := t.Comments[tr.comment]
		mark := " "
		if c.Pending {
			mark = "●"
		}
		style := theme.TextDim
		if i == m.threadCur {
			style = theme.TabActive
		}
		out = append(out, cursorGlyph(i == m.threadCur)+
			theme.TextDim().Render(mark+" "+c.Author+" ")+
			style().Render(crop(c.Text, maxInt(w-14, 8))))
	}
	return strings.Join(out[:minInt(len(out), h)], "\n")
}
