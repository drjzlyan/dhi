// Package ideator is DHI's ideation floor (F-004, F-033): SESSIONS (the
// round-table roster, nested breakouts and pending agent proposals),
// PARTICIPANTS (the invited set, the moderator and the floor),
// CANVAS (the artifact list plus the live markdown/mermaid preview) and
// TRANSCRIPT (the ordered, replayable session channel) — switched with
// [ ]. Sessions carry a mode (1:1 | group | breakout), an optional
// moderator and a recorded turn order; agents may only propose a session,
// never open one. The Ideator never edits repo files: canvas edits ride
// the artifact tools or the injected editor seam.
package ideator

import (
	"context"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/drjzlyan/dhi/internal/agentkit/bus"
	"github.com/drjzlyan/dhi/internal/ideation"
	"github.com/drjzlyan/dhi/internal/tui/kit"
	"github.com/drjzlyan/dhi/internal/tui/surfaces"
	"github.com/drjzlyan/dhi/internal/workspace"
)

// opTimeout bounds every async service call.
const opTimeout = 5 * time.Minute

// roundTableMaxTurns caps automatic floor hand-offs so a pair of agents
// addressing each other can never loop forever (F-033 Part A).
const roundTableMaxTurns = 24

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
	secParticipants
	secCanvas
	secTranscript
	secCount
)

func (s sectionID) label() string {
	switch s {
	case secSessions:
		return "SESSIONS"
	case secParticipants:
		return "PARTICIPANTS"
	case secCanvas:
		return "CANVAS"
	default:
		return "TRANSCRIPT"
	}
}

// Model is the Ideator surface.
type Model struct {
	hits    kit.HitMap // click zones recorded by the last View (F-055)
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

	previewTop int // canvas preview viewport top (lines)

	form formState

	chatFocus  bool
	chatInput  []rune
	chatScroll int

	storeCancel func()
	cancelBus   func()

	bus          *bus.Bus
	crew         crew
	openInEditor func(paths []string) bool

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
// agent-participation keys with visible messages; a nil OpenInEditor
// degrades the canvas "open" key to a named hint.
type Deps struct {
	Store        *ideation.Store
	Bus          *bus.Bus
	Crew         crew
	OpenInEditor func(paths []string) bool
}

// New returns the ideator model. A nil ws renders the empty state with
// all keys inert.
func New(version string, ws *workspace.Workspace, d Deps) *Model {
	return &Model{
		version:      version,
		ws:           ws,
		store:        d.Store,
		bus:          d.Bus,
		crew:         d.Crew,
		openInEditor: d.OpenInEditor,
		events:       make(chan ideEvent, 16),
	}
}

func (m *Model) Meta() surfaces.Meta { return surfaces.Meta{ID: "ideator", Title: "Ideator"} }

// StatusContext feeds the app statusline (F-025).
func (m *Model) StatusContext() (string, string) {
	zone := strings.ToLower(m.sec.label())
	if m.form.kind != fNone {
		return zone, "FORM"
	}
	return zone, ""
}

// Commands implements surfaces.CommandProvider (F-041): a jump to every
// section.
func (m *Model) Commands() []surfaces.Command {
	var out []surfaces.Command
	for s := sectionID(0); s < secCount; s++ {
		s := s
		out = append(out, surfaces.Command{Group: "Ideator", Title: "Go to " + s.label(), Hint: "[ ]", Run: func() tea.Cmd {
			m.sec = s
			return nil
		}})
	}
	return out
}

// CapturesInput implements surfaces.InputCapturer: forms and the
// transcript composer take plain keys as text.
func (m *Model) CapturesInput() bool {
	return m.form.kind != fNone || (m.sec == secTranscript && m.chatFocus)
}

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
// into the store (authorship claims + the floor protocol).
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
				for i, row := range m.sessionRows() {
					if row.sess != nil && row.sess.ID == ev.id {
						m.cursors[secSessions] = i
						break
					}
				}
				m.open(ev.id)
				m.sec = secCanvas
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
	if m.openID == "" || m.store == nil {
		return ideation.Session{}, false
	}
	return m.store.Get(m.openID)
}

// open selects a session, rescans its artifact folder and resets views.
func (m *Model) open(id string) {
	m.openID = id
	m.previewTop = 0
	m.cursors[secCanvas] = 0
	m.cursors[secParticipants] = 0
	m.chatScroll = 0
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

// artifactRelAt maps the CANVAS cursor to a recorded artifact path.
func (m *Model) artifactRelAt(i int) (string, bool) {
	arts := m.artifacts()
	if i < 0 || i >= len(arts) {
		return "", false
	}
	return arts[i].Path, true
}

// sessionRow is one SESSIONS row: a pending proposal or a session (a
// breakout carries depth 1 and renders nested under its parent).
type sessionRow struct {
	proposal *ideation.Proposal
	sess     *ideation.Session
	depth    int
}

// sessionRows builds the flattened SESSIONS list: pending proposals
// first, then top-level sessions each followed by their breakouts.
func (m *Model) sessionRows() []sessionRow {
	var rows []sessionRow
	if m.store == nil {
		return rows
	}
	pend := m.store.PendingProposals()
	for i := range pend {
		p := pend[i]
		rows = append(rows, sessionRow{proposal: &p})
	}
	for _, s := range m.sessions() {
		if s.Parent != "" {
			continue // rendered under its parent
		}
		sess := s
		rows = append(rows, sessionRow{sess: &sess})
		for _, b := range m.store.Breakouts(s.ID) {
			br := b
			rows = append(rows, sessionRow{sess: &br, depth: 1})
		}
	}
	return rows
}

// ---- key routing ----

// SelectProposal focuses the SESSIONS pane on a pending proposal (the
// F-016 inbox jump). Returns false when the proposal is gone (already
// decided) so the caller can degrade with a named hint.
func (m *Model) SelectProposal(id int) bool {
	if m.ws == nil || m.store == nil {
		return false
	}
	for i, r := range m.sessionRows() {
		if r.proposal != nil && r.proposal.ID == id {
			m.sec = secSessions
			m.cursors[secSessions] = i
			return true
		}
	}
	return false
}

func (m *Model) HandleKey(key string) bool {
	if m.ws == nil {
		return false
	}
	if m.form.kind != fNone {
		return m.formKey(key)
	}
	if m.sec == secTranscript && m.chatFocus {
		return m.chatComposerKey(key)
	}
	return m.sectionKey(key)
}

// Wheel routes wheel events to the focused section (F-026 P2): three
// rows per tick through the same key path as j/k; the composer and
// forms never see the wheel.
func (m *Model) Wheel(dy int) bool {
	if dy == 0 || m.ws == nil || m.form.kind != fNone ||
		(m.sec == secTranscript && m.chatFocus) {
		return false
	}
	key := "k"
	if dy > 0 {
		key = "j"
	}
	scrolled := false
	for i := 0; i < 3; i++ {
		if m.HandleKey(key) {
			scrolled = true
		}
	}
	return scrolled
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
	case secParticipants:
		return m.participantsKey(key)
	case secCanvas:
		return m.canvasKey(key)
	case secTranscript:
		return m.chatKey(key)
	default:
		return m.sessionsKey(key)
	}
}

// HelpSections feeds the shell's contextual help (F-026 P7).
func (m *Model) HelpSections() [][2]string {
	out := [][2]string{
		{"[ / ]", "switch sections"},
		{"j / k", "move the cursor"},
	}
	out = append(out, kit.HintRows(m.sectionHints()...)...)
	return kit.DedupeHelpRows(out)
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
	rows := m.sessionRows()
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
			toggleField("mode   ", sessionModes),
		}}
		return true
	case "b":
		if m.store == nil {
			return false
		}
		if sel := selSession(rows, *c); sel == nil || sel.ID == "" {
			return false
		} else {
			m.form = formState{kind: fNewBreakout, parent: sel.ID, fields: []field{
				textField("name   ", ""),
				textField("topic  ", ""),
				textField("agents ", "(blank = inherit)"),
			}}
		}
		return true
	case "enter", "v":
		if sel := selSession(rows, *c); sel != nil {
			m.open(sel.ID)
			m.sec = secCanvas
		}
		return true
	case "a", "x":
		if *c < len(rows) && rows[*c].proposal != nil {
			if key == "a" {
				m.acceptProposal(*rows[*c].proposal)
			} else {
				m.declineProposal(*rows[*c].proposal)
			}
			return true
		}
		if key == "x" {
			if sel := selSession(rows, *c); sel != nil {
				m.form = formState{kind: fRemoveConfirm, orig: sel.ID}
				return true
			}
		}
	}
	return false
}

// sessionModes is the create-form mode toggle order.
var sessionModes = []string{string(ideation.ModeGroup), string(ideation.ModeOneOnOne)}

func (m *Model) participantsKey(key string) bool {
	if _, ok := m.openSession(); !ok {
		return false
	}
	n := len(m.participantRows())
	c := &m.cursors[secParticipants]
	clampCursor(c, n)
	switch key {
	case "j", "down":
		if *c < n-1 {
			*c++
		}
		return true
	case "k", "up":
		if *c > 0 {
			*c--
		}
		return true
	case "f":
		if agent, ok := m.participantAt(*c); ok {
			if agent == "" {
				m.grantOnly("")
			} else {
				m.giveFloor(agent)
			}
		}
		return true
	case "m":
		if m.store == nil {
			return false
		}
		target := ""
		if *c > 0 {
			agent, ok := m.participantAt(*c)
			if !ok {
				return false
			}
			target = agent
		}
		if err := m.store.SetModerator(m.openID, target); err != nil {
			m.opErr = err.Error()
		} else if target == "" {
			m.closeFormWithFlash("the user moderates " + m.openID)
		} else {
			m.closeFormWithFlash(target + " moderates " + m.openID)
		}
		return true
	case "a":
		if m.store == nil {
			return false
		}
		m.form = formState{kind: fAddParticipant, fields: []field{
			textField("agent  ", "scout (comma-separated)"),
		}}
		return true
	case "x":
		if *c == 0 {
			return true // the moderator slot is not removable
		}
		if agent, ok := m.participantAt(*c); ok {
			m.form = formState{kind: fRemoveParticipant, orig: agent}
			return true
		}
	}
	return false
}

// participantRows is [moderator-slot] + participants, excluding the
// moderator from the agent rows so a moderator-agent is not listed twice.
// Index 0 is the moderator: "you" when the user moderates, else the
// moderator's id.
func (m *Model) participantRows() []string {
	sess, ok := m.openSession()
	if !ok {
		return nil
	}
	rows := []string{moderatorName(sess)}
	for _, a := range sess.Agents {
		if a == sess.Moderator {
			continue
		}
		rows = append(rows, a)
	}
	return rows
}

// participantAt maps a PARTICIPANTS cursor to an agent id ("" = the
// user/moderator slot).
func (m *Model) participantAt(i int) (string, bool) {
	sess, ok := m.openSession()
	if !ok || i < 0 {
		return "", false
	}
	if i == 0 {
		return sess.Moderator, true
	}
	agents := make([]string, 0, len(sess.Agents))
	for _, a := range sess.Agents {
		if a == sess.Moderator {
			continue
		}
		agents = append(agents, a)
	}
	if i-1 < len(agents) {
		return agents[i-1], true
	}
	return "", false
}

func (m *Model) canvasKey(key string) bool {
	arts := m.artifacts()
	c := &m.cursors[secCanvas]
	clampCursor(c, len(arts))
	var rel string
	var hasRel bool
	switch key {
	case "j", "down":
		if *c < len(arts)-1 {
			*c++
			m.previewTop = 0
			m.clampPreview()
		}
		return true
	case "k", "up":
		if *c > 0 {
			*c--
			m.previewTop = 0
			m.clampPreview()
		}
		return true
	case "J":
		m.previewTop++
		m.clampPreview()
		return true
	case "K":
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
	case "s":
		if m.store != nil {
			if err := m.store.Scan(m.openID); err != nil {
				m.opErr = err.Error()
			} else {
				m.closeFormWithFlash("scanned " + m.openID)
			}
		}
		return true
	case "e":
		rel, hasRel = m.artifactRelAt(*c)
		if !hasRel {
			return true
		}
		if m.openInEditor == nil {
			m.opErr = "open in editor unavailable"
			return true
		}
		abs := m.store.ArtifactPath(m.openID, rel)
		if !m.openInEditor([]string{abs}) {
			m.opErr = "open in editor refused"
		}
		return true
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

func (m *Model) createSession(name, topic string, agents []string, mode ideation.SessionMode, parent string) {
	if m.store == nil {
		return
	}
	m.busy = true
	go func() {
		var sess ideation.Session
		var err error
		if parent != "" {
			sess, err = m.store.CreateBreakout(parent, name, topic, agents)
		} else {
			sess, err = m.store.CreateSession(ideation.CreateOptions{
				Name: name, Topic: topic, Mode: mode, Agents: agents,
			})
		}
		ev := ideEvent{kind: evCreated}
		if err != nil {
			ev.err = err.Error()
		} else {
			ev.id = sess.ID
		}
		m.send(ev)
	}()
}

// acceptProposal opens the proposed session/breakout and records the
// slug on the proposal. Only the human can do this (F-033 acceptance 3).
func (m *Model) acceptProposal(p ideation.Proposal) {
	if m.store == nil {
		return
	}
	var sess ideation.Session
	var err error
	if p.Mode == ideation.ModeBreakout {
		sess, err = m.store.CreateBreakout(p.Parent, p.Name, p.Topic, p.Participants)
	} else {
		sess, err = m.store.CreateSession(ideation.CreateOptions{
			Name: p.Name, Topic: p.Topic, Mode: p.Mode, Agents: p.Participants,
		})
	}
	if err != nil {
		m.opErr = err.Error()
		return
	}
	if err := m.store.Decision(p.ID, ideation.ProposalAccepted, sess.ID); err != nil {
		m.opErr = err.Error()
		return
	}
	m.open(sess.ID)
	m.sec = secCanvas
	m.closeFormWithFlash("opened " + sess.ID + " (proposal #" + itoa(p.ID) + ")")
}

// declineProposal creates nothing and marks the proposal decided.
func (m *Model) declineProposal(p ideation.Proposal) {
	if m.store == nil {
		return
	}
	if err := m.store.Decision(p.ID, ideation.ProposalDeclined, ""); err != nil {
		m.opErr = err.Error()
		return
	}
	m.closeFormWithFlash("declined proposal #" + itoa(p.ID))
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
	clampCursor(&m.cursors[secSessions], len(m.sessionRows()))
}

// grantOnly records a floor grant (or release) without dispatching.
func (m *Model) grantOnly(speaker string) {
	sess, ok := m.openSession()
	if !ok || m.store == nil {
		return
	}
	if sess.CurrentSpeaker() == speaker {
		return
	}
	var err error
	if speaker == "" {
		err = m.store.ReleaseFloor(sess.ID)
	} else {
		err = m.store.GrantFloor(sess.ID, speaker)
	}
	if err != nil {
		m.opErr = err.Error()
	}
}

// giveFloor records a grant and dispatches one turn to the speaker. The
// floor grant is not written to the transcript (the turn order is on the
// session card); the synthetic trigger carries the @mention the runtime
// needs to route the turn.
func (m *Model) giveFloor(speaker string) {
	m.opErr = ""
	if speaker == "" {
		m.grantOnly("")
		return
	}
	sess, ok := m.openSession()
	if !ok || m.store == nil {
		return
	}
	if sess.CurrentSpeaker() == speaker {
		return
	}
	m.grantOnly(speaker)
	if m.opErr != "" {
		return
	}
	prompt := "the moderator grants you the floor"
	if sess.Topic != "" {
		prompt += " — " + sess.Topic
	}
	m.requestTurn(bus.Message{Channel: sess.Channel, Author: busHuman, Text: "@" + speaker + " " + prompt})
}

func selSession(rows []sessionRow, i int) *ideation.Session {
	if i >= 0 && i < len(rows) {
		return rows[i].sess
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

func maxInt(a, b int) int {
	if a > b {
		return a
	}
	return b
}
