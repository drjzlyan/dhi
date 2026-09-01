// Package ideator is DHI's ideation floor (F-004): SESSIONS (ideation
// sessions), ARTIFACTS (the open session's read-only artifact tree) and
// PREVIEW (rendered markdown or raw text) — switched with [ ].
// Session chat, agent dispatch and the reject→revision loop layer onto
// this skeleton in later phases. The Ideator never edits files.
package ideator

import (
	"context"
	"strings"
	"time"

	"charm.land/bubbletea/v2"

	"github.com/drjzlyan/dhi/internal/agentkit/bus"
	"github.com/drjzlyan/dhi/internal/ideation"
	"github.com/drjzlyan/dhi/internal/tui/surfaces"
	"github.com/drjzlyan/dhi/internal/workspace"
)

// opTimeout bounds every async service call.
const opTimeout = 5 * time.Minute

// crew is the narrow runtime seam: dispatch turns + roster ids.
// *runtime.Runtime satisfies it; tests use scripted fakes.
type crew interface {
	Handle(ctx context.Context, msg bus.Message)
	AgentIDs() []string
}

// sectionID enumerates the switchable panes.
type sectionID uint8

const (
	secSessions sectionID = iota
	secArtifacts
	secPreview
	secChat
	secCount
)

func (s sectionID) label() string {
	switch s {
	case secSessions:
		return "SESSIONS"
	case secArtifacts:
		return "ARTIFACTS"
	case secPreview:
		return "PREVIEW"
	default:
		return "CHAT"
	}
}

// Model is the Ideator surface.
type Model struct {
	version string
	ws      *workspace.Workspace
	store   *ideation.Store // nil = whole surface degrades visibly
	width   int
	height  int

	sec     sectionID
	cursors [secCount]int

	openID string // session currently loaded ("", none)
	busy   bool   // an async op is running
	opErr  string

	previewTop int // preview viewport top (lines)
	scroll     int

	form formState

	chatFocus  bool
	chatInput  []rune
	chatScroll int

	storeCancel func()
	cancelBus   func()

	bus  *bus.Bus
	crew crew

	events chan ideEvent
}

var _ surfaces.Surface = (*Model)(nil)

type ideEvent struct {
	kind uint8
	err  string
	id   string
	msg  bus.Message
}

const (
	evPing uint8 = iota
	evCreated
	evBus
)

// Deps carries the services this surface operates. A nil Store degrades
// every section to visible "unavailable" rows; nil Bus/Crew disable the
// agent-participation keys with visible messages.
type Deps struct {
	Store *ideation.Store
	Bus   *bus.Bus
	Crew  crew
}

// New returns the ideator model. A nil ws renders the empty state with
// all keys inert.
func New(version string, ws *workspace.Workspace, d Deps) *Model {
	return &Model{
		version: version,
		ws:      ws,
		store:   d.Store,
		bus:     d.Bus,
		crew:    d.Crew,
		events:  make(chan ideEvent, 16),
	}
}

func (m *Model) Meta() surfaces.Meta { return surfaces.Meta{ID: "ideator", Title: "Ideator"} }

// Init starts the store change pump.
func (m *Model) Init() tea.Cmd {
	if m.ws == nil || m.store == nil {
		return nil
	}
	ch, cancel := m.store.Subscribe()
	m.storeCancel = cancel
	go func() {
		for range ch {
			m.send(ideEvent{kind: evPing})
		}
	}()
	return m.listen()
}

func (m *Model) send(ev ideEvent) {
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
	m.clampPreview()
}

// Update resolves async results: pings re-render; bus traffic mirrors
// into the store (artifact authorship claims).
func (m *Model) Update(msg tea.Msg) tea.Cmd {
	switch ev := msg.(type) {
	case ideEvent:
		switch ev.kind {
		case evCreated:
			m.busy = false
			if ev.err != "" {
				m.opErr = ev.err
				m.form.err = ev.err
			} else {
				m.closeFormWithFlash("session " + ev.id + " created")
				for i, s := range m.sessions() {
					if s.ID == ev.id {
						m.cursors[secSessions] = i
						break
					}
				}
				m.open(ev.id)
				m.sec = secArtifacts
			}
		case evBus:
			m.mirrorBus(ev.msg)
		}
		return m.listen()
	}
	return nil
}

// ---- data access ----

func (m *Model) sessions() []ideation.Session {
	if m.ws == nil || m.store == nil {
		return nil
	}
	return m.store.Sessions()
}

func (m *Model) openSession() (ideation.Session, bool) {
	if m.openID == "" {
		return ideation.Session{}, false
	}
	return m.store.Get(m.openID)
}

// open selects a session, rescans its artifact folder and resets views.
func (m *Model) open(id string) {
	m.openID = id
	m.previewTop = 0
	m.scroll = 0
	m.cursors[secArtifacts] = 0
	if m.store != nil {
		if err := m.store.Scan(id); err != nil {
			m.opErr = err.Error()
		}
	}
	m.subscribeBus(id)
}

// subscribeBus pumps the open session's channel into the event loop.
func (m *Model) subscribeBus(id string) {
	if m.cancelBus != nil {
		m.cancelBus()
		m.cancelBus = nil
	}
	if m.bus == nil || m.store == nil {
		return
	}
	sess, ok := m.store.Get(id)
	if !ok {
		return
	}
	ch, cancel := m.bus.Subscribe(sess.Channel)
	m.cancelBus = cancel
	go func() {
		for msg := range ch {
			m.send(ideEvent{kind: evBus, msg: msg})
		}
	}()
}

// artifacts returns the open session's artifact list sorted by path.
func (m *Model) artifacts() []ideation.Artifact {
	s, ok := m.openSession()
	if !ok {
		return nil
	}
	return s.Artifacts
}

// artifactRelAt maps the ARTIFACTS cursor to a recorded artifact path.
func (m *Model) artifactRelAt(i int) (string, bool) {
	arts := m.artifacts()
	if i < 0 || i >= len(arts) {
		return "", false
	}
	return arts[i].Path, true
}

// ---- key routing ----

func (m *Model) HandleKey(key string) bool {
	if m.ws == nil {
		return false
	}
	if m.form.kind != fNone {
		return m.formKey(key)
	}
	if m.sec == secChat && m.chatFocus {
		return m.chatComposerKey(key)
	}
	return m.sectionKey(key)
}

func (m *Model) sectionKey(key string) bool {
	switch key {
	case "[":
		m.sec = (m.sec - 1 + secCount) % secCount
		m.clampPreview()
		return true
	case "]":
		m.sec = (m.sec + 1) % secCount
		m.clampPreview()
		return true
	case "esc":
		if m.sec != secSessions {
			m.sec = secSessions
			return true
		}
		return false
	}

	switch m.sec {
	case secArtifacts:
		return m.artifactsKey(key)
	case secPreview:
		return m.previewKey(key)
	case secChat:
		return m.chatKey(key)
	default:
		return m.sessionsKey(key)
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

func (m *Model) sessionsKey(key string) bool {
	rows := m.sessions()
	c := &m.cursors[secSessions]
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
		if m.store == nil {
			return false
		}
		m.form = formState{kind: fNewSession, fields: []field{
			textField("name   ", ""),
			textField("topic  ", ""),
			textField("agents ", firstAgent(m)+" (comma-separated)"),
		}}
		return true
	case "enter", "v":
		if sel := selSession(rows, *c); sel != nil {
			m.open(sel.ID)
			m.sec = secArtifacts
		}
		return true
	case "x":
		if sel := selSession(rows, *c); sel != nil {
			m.form = formState{kind: fRemoveConfirm, orig: sel.ID}
			return true
		}
	}
	return false
}

func (m *Model) artifactsKey(key string) bool {
	_, ok := m.openSession()
	c := &m.cursors[secArtifacts]
	arts := m.artifacts()
	clampCursor(c, len(arts))
	switch key {
	case "j", "down":
		if *c < len(arts)-1 {
			*c++
		}
		return true
	case "k", "up":
		if *c > 0 {
			*c--
		}
		return true
	case "enter", "\t", "l":
		if ok && *c < len(arts) {
			m.previewTop = 0
			m.sec = secPreview
		}
		return true
	case "s":
		if ok {
			if err := m.store.Scan(m.openID); err != nil {
				m.opErr = err.Error()
			} else {
				m.closeFormWithFlash("scanned " + m.openID)
			}
			return true
		}
	case "v":
		if rel, ok := m.artifactRelAt(*c); ok {
			if err := m.store.MarkReviewed(m.openID, rel); err != nil {
				m.opErr = err.Error()
			}
			return true
		}
	case "a":
		if rel, ok := m.artifactRelAt(*c); ok {
			if err := m.store.Approve(m.openID, rel); err != nil {
				m.opErr = err.Error()
			} else {
				m.closeFormWithFlash("approved " + rel)
			}
			return true
		}
	case "r":
		if rel, ok := m.artifactRelAt(*c); ok {
			m.form = formState{kind: fReject, orig: rel, fields: []field{
				textField("notes  ", ""),
			}}
			return true
		}
	}
	return false
}

func (m *Model) previewKey(key string) bool {
	switch key {
	case "j", "down":
		m.previewTop++
		m.clampPreview()
		return true
	case "k", "up":
		if m.previewTop > 0 {
			m.previewTop--
		}
		return true
	case "g":
		m.previewTop = 0
		return true
	case "G":
		m.previewTop = maxInt(0, len(m.previewLines())-m.previewHeight())
		return true
	case "\t", "h":
		m.sec = secArtifacts
		return true
	}
	return false
}

func (m *Model) createSession(name, topic string, agents []string) {
	if m.store == nil {
		return
	}
	m.busy = true
	go func() {
		sess, err := m.store.Create(name, topic, agents)
		ev := ideEvent{kind: evCreated}
		if err != nil {
			ev.err = err.Error()
		} else {
			ev.id = sess.ID
		}
		m.send(ev)
	}()
}

func (m *Model) removeSession(id string) {
	if m.store == nil {
		return
	}
	if err := m.store.Remove(id); err != nil {
		m.opErr = err.Error()
		return
	}
	if m.openID == id {
		m.openID = ""
	}
	clampCursor(&m.cursors[secSessions], len(m.sessions()))
}

func selSession(rows []ideation.Session, i int) *ideation.Session {
	if i < len(rows) {
		return &rows[i]
	}
	return nil
}

func firstAgent(m *Model) string {
	if m.crew == nil {
		return ""
	}
	if ids := m.crew.AgentIDs(); len(ids) > 0 {
		return ids[0]
	}
	return ""
}

// csvList splits a comma-separated field into trimmed entries.
func csvList(s string) []string {
	var out []string
	for _, part := range strings.Split(s, ",") {
		if p := strings.TrimSpace(strings.TrimPrefix(part, "@")); p != "" {
			out = append(out, p)
		}
	}
	return out
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
