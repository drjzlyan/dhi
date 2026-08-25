// Package reviewer is DHI's code-review floor (F-005): three sections —
// REVIEWS (session list), FILES (changed paths of the open review) and
// DIFF (unified or side-by-side patch view) — switched with [ ].
// Comments, agent invites and completion flows layer onto this skeleton
// in later phases.
package reviewer

import (
	"context"
	"time"

	"charm.land/bubbletea/v2"

	"github.com/drjzlyan/dhi/internal/gitdiff"
	"github.com/drjzlyan/dhi/internal/review"
	"github.com/drjzlyan/dhi/internal/tui/surfaces"
	"github.com/drjzlyan/dhi/internal/workspace"
)

// opTimeout bounds every async service call.
const opTimeout = 5 * time.Minute

// sectionID enumerates the switchable panes.
type sectionID uint8

const (
	secReviews sectionID = iota
	secFiles
	secDiff
	secCount
)

func (s sectionID) label() string {
	switch s {
	case secReviews:
		return "REVIEWS"
	case secFiles:
		return "FILES"
	default:
		return "DIFF"
	}
}

// layoutMode picks the diff rendering.
type layoutMode uint8

const (
	layoutUnified layoutMode = iota
	layoutSideBySide
)

// Model is the Reviewer surface.
type Model struct {
	version string
	ws      *workspace.Workspace
	svc     *review.Service // nil = whole surface degrades visibly
	width   int
	height  int

	sec     sectionID
	cursors [secCount]int

	openID  string // review currently loaded ("", none)
	files   []gitdiff.FileDiff
	diffFor string // review id the cached diff belongs to
	busy    bool   // an async op is running
	opErr   string
	layout  layoutMode
	scroll  int // diff viewport top (visual rows)
	cursor  int // diff cursor (logical rows)
	fileCur int // FILES cursor mirrored into DIFF jumps

	form formState

	events chan revEvent
}

var _ surfaces.Surface = (*Model)(nil)

type revEvent struct {
	kind uint8 // evPing | evStartDone | evDiffDone | evDiscardDone
	err  string
	id   string
}

const (
	evPing uint8 = iota
	evStartDone
	evDiffDone
	evDiscardDone
)

// Deps carries the services this surface operates. A nil Service degrades
// every section to visible "unavailable" rows rather than errors.
type Deps struct {
	Service *review.Service
}

// New returns the reviewer model. A nil ws renders the empty state with
// all keys inert.
func New(version string, ws *workspace.Workspace, d Deps) *Model {
	return &Model{
		version: version,
		ws:      ws,
		svc:     d.Service,
		events:  make(chan revEvent, 16),
	}
}

func (m *Model) Meta() surfaces.Meta { return surfaces.Meta{ID: "reviewer", Title: "Reviewer"} }

// Init starts the store change pump.
func (m *Model) Init() tea.Cmd {
	if m.ws == nil || m.svc == nil {
		return nil
	}
	ch, cancel := m.svc.Store().Subscribe()
	go func() {
		defer cancel()
		for range ch {
			m.send(revEvent{kind: evPing})
		}
	}()
	return m.listen()
}

func (m *Model) send(ev revEvent) {
	select {
	case m.events <- ev:
	default:
	}
}

func (m *Model) listen() tea.Cmd {
	ch := m.events
	return func() tea.Msg {
		ev, ok := <-ch
		if !ok {
			return nil
		}
		return ev
	}
}

func (m *Model) Resize(w, h int) {
	m.width, m.height = w, h
	m.clampScroll()
}

// Update resolves async results: pings re-render; start/diff completion
// swap the busy state and refresh caches.
func (m *Model) Update(msg tea.Msg) tea.Cmd {
	switch ev := msg.(type) {
	case revEvent:
		switch ev.kind {
		case evStartDone:
			m.busy = false
			if ev.err != "" {
				m.opErr = ev.err
				m.form.err = ev.err
			} else {
				m.closeForm()
				for i, r := range m.reviews() {
					if r.ID == ev.id {
						m.cursors[secReviews] = i
						break
					}
				}
				m.open(ev.id)
				m.sec = secFiles
			}
		case evDiffDone:
			m.busy = false
			if ev.err != "" {
				m.opErr = ev.err
			} else {
				m.opErr = ""
				m.cursor, m.scroll = 0, 0
				m.fileCur = 0
			}
		case evDiscardDone:
			m.busy = false
			if ev.err != "" {
				m.opErr = ev.err
			} else if ev.id == m.openID {
				m.openID = ""
				m.files = nil
				m.diffFor = ""
			}
		}
		return m.listen()
	}
	return nil
}

// ---- data access ----

func (m *Model) reviews() []review.Review {
	if m.ws == nil || m.svc == nil {
		return nil
	}
	return m.svc.Store().List()
}

func (m *Model) openReview() (review.Review, bool) {
	if m.openID == "" {
		return review.Review{}, false
	}
	return m.svc.Store().Get(m.openID)
}

// open selects a review and (re)loads its diff asynchronously.
func (m *Model) open(id string) {
	m.openID = id
	m.fileCur = 0
	m.cursor, m.scroll = 0, 0
	m.loadDiff()
}

// loadDiff fetches and parses the review patch asynchronously.
func (m *Model) loadDiff() {
	r, ok := m.openReview()
	if !ok || m.svc == nil || !m.svc.CanDiff() {
		return
	}
	if m.diffFor == r.ID && !r.Done {
		return // cached
	}
	m.busy = true
	m.diffFor = ""
	go func(id string) {
		ctx, cancel := context.WithTimeout(context.Background(), opTimeout)
		defer cancel()
		files, err := m.svc.Diff(ctx, r)
		ev := revEvent{kind: evDiffDone, id: id}
		if err != nil {
			ev.err = err.Error()
		} else {
			m.files = files
			m.diffFor = id
		}
		m.send(ev)
	}(r.ID)
}

// ---- key routing ----

func (m *Model) HandleKey(key string) bool {
	if m.ws == nil {
		return false
	}
	if m.form.kind != fNone {
		return m.formKey(key)
	}
	return m.sectionKey(key)
}

func (m *Model) sectionKey(key string) bool {
	switch key {
	case "[":
		m.sec = (m.sec - 1 + secCount) % secCount
		return true
	case "]":
		m.sec = (m.sec + 1) % secCount
		return true
	case "esc":
		if m.sec != secReviews {
			m.sec = secReviews
			return true
		}
		return false
	}

	switch m.sec {
	case secFiles:
		return m.filesKey(key)
	case secDiff:
		return m.diffKey(key)
	default:
		return m.reviewsKey(key)
	}
}

func clampCursor(c *int, n int) {
	if *c >= n {
		*c = maxInt(n-1, 0)
	}
	if *c < 0 {
		*c = 0
	}
}

func (m *Model) reviewsKey(key string) bool {
	rows := m.reviews()
	c := &m.cursors[secReviews]
	clampCursor(c, len(rows))
	switch key {
	case "j", "down":
		if *c < len(rows)-1 {
			*c++
		}
		return true
	case "k", "up":
		if *c > 0 {
			*c--
		}
		return true
	case "n":
		if m.svc == nil {
			return false
		}
		m.form = formState{kind: fNewReview, fields: []field{
			textField("member ", firstMember(m)),
			kField(),
			textField("base   ", "main"),
			textField("head/# ", ""),
		}}
		return true
	case "enter", "v":
		if sel := selReview(rows, *c); sel != nil {
			m.open(sel.ID)
			m.sec = secFiles
		}
		return true
	case "x":
		if sel := selReview(rows, *c); sel != nil && !sel.Done {
			m.form = formState{kind: fDiscardConfirm, orig: sel.ID,
				fields: nil}
			return true
		}
	case "d":
		if sel := selReview(rows, *c); sel != nil {
			m.form = formState{kind: fRemoveConfirm, orig: sel.ID}
			return true
		}
	}
	return false
}

func selReview(rows []review.Review, i int) *review.Review {
	if i < len(rows) {
		return &rows[i]
	}
	return nil
}

func firstMember(m *Model) string {
	if mems := m.ws.Members(); len(mems) > 0 {
		return mems[0].Name
	}
	return ""
}

func (m *Model) filesKey(key string) bool {
	r, ok := m.openReview()
	c := &m.cursors[secFiles]
	clampCursor(c, len(m.files))
	switch key {
	case "j", "down":
		if *c < len(m.files)-1 {
			*c++
		}
		return true
	case "k", "up":
		if *c > 0 {
			*c--
		}
		return true
	case "enter", "\t", "l":
		if ok && len(m.files) > 0 {
			m.fileCur = *c
			m.jumpToFile(*c)
			m.sec = secDiff
		}
		return true
	case "v":
		if ok && *c < len(m.files) && m.svc != nil {
			if err := m.svc.Store().ToggleViewed(r.ID, m.files[*c].DisplayPath()); err != nil {
				m.opErr = err.Error()
			}
			return true
		}
	}
	return false
}

func (m *Model) diffKey(key string) bool {
	rows := m.diffRows()
	switch key {
	case "j", "down":
		if m.cursor < len(rows)-1 {
			m.cursor++
		}
		return true
	case "k", "up":
		if m.cursor > 0 {
			m.cursor--
		}
		return true
	case "g":
		m.cursor, m.scroll = 0, 0
		return true
	case "G":
		m.cursor = len(rows) - 1
		return true
	case "\\":
		m.layout = (m.layout + 1) % 2
		m.cursor, m.scroll = 0, 0
		return true
	case "n":
		return m.jumpFileDelta(+1)
	case "p":
		return m.jumpFileDelta(-1)
	case "v":
		if r, ok := m.openReview(); ok {
			if path := m.pathAtRow(m.cursor); path != "" {
				if err := m.svc.Store().ToggleViewed(r.ID, path); err != nil {
					m.opErr = err.Error()
				}
				return true
			}
		}
	case "\t", "h":
		m.sec = secFiles
		return true
	}
	return false
}

// jumpFileDelta moves the cursor to the next/prev file header row.
func (m *Model) jumpFileDelta(delta int) bool {
	rows := m.diffRows()
	for i := m.cursor + delta; i >= 0 && i < len(rows); i += delta {
		if rows[i].kind == vrFileHeader {
			m.cursor = i
			m.syncFileCur()
			return true
		}
	}
	return false
}

func (m *Model) jumpToFile(fileIdx int) {
	rows := m.diffRows()
	for i, row := range rows {
		if row.kind == vrFileHeader && row.file == fileIdx {
			m.cursor = i
			return
		}
	}
}

func (m *Model) syncFileCur() {
	if row := m.rowAt(m.cursor); row != nil {
		m.fileCur = row.file
	}
}

// ---- async operations ----

func (m *Model) startReview(member string, kind review.Kind, base, head string, pr int) {
	if m.svc == nil {
		return
	}
	m.busy = true
	m.form.busy = true
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), opTimeout)
		defer cancel()
		r, err := m.svc.Start(ctx, member, kind, base, head, pr)
		ev := revEvent{kind: evStartDone}
		if err != nil {
			ev.err = err.Error()
		} else {
			ev.id = r.ID
		}
		m.send(ev)
	}()
}

func (m *Model) discardReview(id string) {
	if m.svc == nil {
		return
	}
	m.busy = true
	go func() {
		err := m.svc.Discard(id)
		m.send(revEvent{kind: evDiscardDone, id: id, err: errString(err)})
	}()
}

func (m *Model) removeReview(id string) {
	if m.svc == nil {
		return
	}
	if err := m.svc.Store().Remove(id); err != nil {
		m.opErr = err.Error()
	}
}

func errString(err error) string {
	if err == nil {
		return ""
	}
	return err.Error()
}

func maxInt(a, b int) int {
	if a > b {
		return a
	}
	return b
}
