// Package workspace is DHI's landing view: the company of agents.
// Four sections — inbox, board, channels, repos — switched with [ ];
// each carries its own cursor and contextual keymap (ADR-0014).
// Management (agents, teams, packs, standards, autopilot cards) lives
// in Settings; this surface keeps the operational floor plus the
// autopilot execution engine (launch catch-up + ticks, ADR-0014 §5).
package workspace

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"charm.land/bubbletea/v2"

	"github.com/drjzlyan/dhi/internal/agentkit/bus"
	"github.com/drjzlyan/dhi/internal/agentkit/channelmeta"
	"github.com/drjzlyan/dhi/internal/agentkit/org"
	profiface "github.com/drjzlyan/dhi/internal/agentkit/profile"
	"github.com/drjzlyan/dhi/internal/agentkit/tools"
	"github.com/drjzlyan/dhi/internal/autopilot"
	"github.com/drjzlyan/dhi/internal/gitcore"
	"github.com/drjzlyan/dhi/internal/inbox"
	"github.com/drjzlyan/dhi/internal/review"
	"github.com/drjzlyan/dhi/internal/tasks"
	"github.com/drjzlyan/dhi/internal/tui/kit"
	"github.com/drjzlyan/dhi/internal/tui/surfaces"
	"github.com/drjzlyan/dhi/internal/unread"
	"github.com/drjzlyan/dhi/internal/workspace"
)

// Timeouts for async operations; the UI stays responsive either way.
const (
	cloneTimeout  = 5 * time.Minute
	taskPRTimeout = 5 * time.Minute
)

// sectionID enumerates the switchable panes (rail order, ADR-0014 §1).
type sectionID uint8

const (
	secInbox sectionID = iota
	secBoard
	secChannels
	secRepos
	secCount
)

func (s sectionID) label() string {
	switch s {
	case secInbox:
		return "INBOX"
	case secBoard:
		return "BOARD"
	case secChannels:
		return "CHANNELS"
	case secRepos:
		return "REPOS"
	default:
		return "INBOX"
	}
}

// Model is the Workspace landing surface.
type Model struct {
	version string
	ws      *workspace.Workspace
	width   int
	height  int

	sec     sectionID
	cursors [secCount]int

	// Board state (F-021): active lane + per-lane card cursors. Lanes
	// follow tasks.Statuses order (backlog, active, in-review, done).
	boardActive int
	boardCur    [4]int
	// Board depth (F-035): a free-text filter over title/labels/epic/
	// assignee, and a bulk selection set toggled with space.
	boardFilter     string
	boardFilterEdit bool
	boardMarks      map[string]bool

	org    *org.Org
	orgErr string
	pane   *chatPane
	replay *runReplay // non-nil = run-replay pane open (F-014)

	form formState

	taskStore *tasks.Store
	roster    profiface.Roster
	reviewSvc *review.Service

	autopilots *autopilot.Store // nil = store unavailable (not a workspace)
	bus        *bus.Bus
	rt         turnHandler
	now        func() time.Time
	armSeq     uint64 // autopilot tick-chain guard: exactly one in flight
	cancelAuto func()

	approvals    *tools.Approvals                        // pending-approval queue (F-016 source)
	unreadStore  *unread.Store                           // read-mark store (F-017); nil = no bus
	unreadErr    string                                  // store unavailable: named, never silent
	chatMetaErr  string                                  // reactions/edits/pins store unavailable
	unreadCounts map[string]int                          // per-frame rail counts (syncUnread)
	openChat     func() bool                             // focus editor chat approvals (F-016 jump)
	openReview   func(id string) bool                    // reviewer select (F-016 jump)
	openEditor   func(paths []string) bool               // editor open-in-editor (repos `e`, F-026 P3)
	working      func(channel string, thread int64) bool // live turn indicator (F-035 Part D)
	inboxHint    string                                  // last jump degrade hint (visible, never silent)
	snoozeTarget inbox.Item                              // item parked by the fSnooze form
	snoozeChain  bool                                    // expiry tick chain in flight (F-017)

	chromeSeen string    // last chrome flash content (toast dedupe, F-026 P7)
	chromeAt   time.Time // when that content first appeared

	events       chan wsEvent
	cancelSub    func()
	cancelOrg    func()
	cancelUnread func()
}

var _ surfaces.Surface = (*Model)(nil)

type wsEvent struct {
	kind       uint8 // evPing | evCloneDone | evTaskPRDone
	err        string
	packName   string // task slug for evTaskPRDone
	packAgents []string
	prNum      int
	flash      string
}

const (
	evPing uint8 = iota
	evCloneDone
	evTaskPRDone
	evPingFlash // ping with flash message
)

// errString converts error to string, empty string if nil.
func errString(err error) string {
	if err == nil {
		return ""
	}
	return err.Error()
}

// Deps carries the services this surface operates. Zero fields degrade
// their sections to visible "unavailable" rows rather than errors.
type Deps struct {
	Bus        *bus.Bus
	Runtime    turnHandler
	Tasks      *tasks.Store
	Roster     profiface.Roster
	ReviewSvc  *review.Service           // nil = task PR creation unavailable
	Approvals  *tools.Approvals          // nil = no pending-approval inbox source
	Unread     *unread.Store             // shared read-mark store (F-017); opened here if nil
	Autopilots *autopilot.Store          // shared with Settings (F-023); opened here if nil
	OpenChat   func() bool               // focus editor chat (approval jump)
	OpenReview func(id string) bool      // reviewer select (in_review jump)
	OpenEditor func(paths []string) bool // editor open-in-editor (repos `e`)
	// Working reports an in-flight agent turn for a thread (F-035
	// Part D); nil renders no live indicator.
	Working func(channel string, thread int64) bool
}

// New returns the workspace model. A nil ws renders the not-a-workspace
// empty state (all keys inert).
func New(version string, ws *workspace.Workspace, d Deps) *Model {
	m := &Model{
		version: version,
		ws:      ws,
		events:  make(chan wsEvent, 16),
		sec:     secBoard, // the dashboard is the landing view (F-021)
	}
	if ws != nil {
		if o, err := org.Load(ws.Root); err == nil {
			m.org = o
		} else {
			m.orgErr = err.Error()
		}
		m.taskStore = d.Tasks
		m.roster = d.Roster
		m.reviewSvc = d.ReviewSvc
		m.bus = d.Bus
		m.rt = d.Runtime
		m.now = time.Now
		m.approvals = d.Approvals
		m.openChat = d.OpenChat
		m.openReview = d.OpenReview
		m.openEditor = d.OpenEditor
		m.working = d.Working
		switch {
		case d.Autopilots != nil:
			m.autopilots = d.Autopilots // shared with Settings (F-023)
		default:
			if as, err := autopilot.Open(ws); err == nil {
				m.autopilots = as
			}
		}
		if d.Bus != nil {
			m.pane = newChatPane(d.Bus, d.Runtime, m.org)
			m.pane.profile = m.agentProfileLines
			if cm, err := channelmeta.Open(ws.Root); err != nil {
				m.chatMetaErr = err.Error()
			} else {
				m.pane.meta = cm
			}
			m.wireUnreadSeams()
			switch {
			case d.Unread != nil:
				m.unreadStore = d.Unread // shared with the editor chat
			default:
				if us, err := unread.Open(ws, d.Bus); err != nil {
					m.unreadErr = err.Error()
				} else {
					m.unreadStore = us
				}
			}
		}
	}
	return m
}

func (m *Model) Meta() surfaces.Meta { return surfaces.Meta{ID: "workspace", Title: "Workspace"} }

// StatusContext feeds the app statusline (F-025): the active zone plus
// a mode chip when a modal or the replay pane owns the keys.
func (m *Model) StatusContext() (string, string) {
	zone := strings.ToLower(m.sec.label())
	switch {
	case m.replay != nil:
		return zone, "REPLAY"
	case m.form.kind != fNone:
		return zone, "FORM"
	}
	return zone, ""
}

// Init starts the change pumps for re-render triggers and arms the
// autopilot chain — launch catch-up rides the due-now tick (F-015),
// execution stays on this surface (ADR-0014 §5).
func (m *Model) Init() tea.Cmd {
	if m.ws == nil {
		return nil
	}
	ch, cancel := m.ws.Subscribe()
	m.cancelSub = cancel
	go func() {
		for range ch {
			m.send(wsEvent{kind: evPing})
		}
	}()
	if m.org != nil {
		och, ocancel := m.org.Subscribe()
		m.cancelOrg = ocancel
		go func() {
			for range och {
				m.send(wsEvent{kind: evPing})
			}
		}()
	}
	cmds := []tea.Cmd{m.listen()}
	if m.pane != nil {
		m.refreshPaneRail()
		m.pane.resubscribe()
		cmds = append(cmds, m.listenPane())
	}
	if m.autopilots != nil {
		ach, acancel := m.autopilots.Subscribe()
		m.cancelAuto = acancel
		go func() {
			for range ach {
				m.send(wsEvent{kind: evPing})
			}
		}()
		cmds = append(cmds, m.armAutopilots())
	}
	if m.unreadStore != nil {
		uch, ucancel := m.unreadStore.Subscribe()
		m.cancelUnread = ucancel
		go func() {
			for range uch {
				m.send(wsEvent{kind: evPing})
			}
		}()
		cmds = append(cmds, m.armSnoozeTick())
	}
	return tea.Batch(cmds...)
}

// armAutopilots runs the F-015 re-evaluation chain: an explicit tick to
// the next schedule/date boundary, or an immediate catch-up when a card
// is due right now. Paused/absent stores arm nothing (no tick chain).
// The guard (armSeq) keeps exactly one chain in flight per surface.
func (m *Model) armAutopilots() tea.Cmd {
	if m.autopilots == nil {
		return nil
	}
	if !m.autoArmed() {
		return nil
	}
	next, ok := m.autopilots.NextArm(m.now())
	if !ok {
		m.autoDisarm()
		return nil
	}
	d := time.Until(next)
	if d < 0 {
		d = 0
	}
	return tea.Tick(d, func(time.Time) tea.Msg { return autopilotTickMsg{} })
}

func (m *Model) autoArmed() bool {
	if m.armSeq > 0 {
		return false
	}
	m.armSeq++
	return true
}

func (m *Model) autoDisarm() { m.armSeq = 0 }

// autopilotTickMsg fires when an armed tick boundary arrives.
type autopilotTickMsg struct{}

// onAutopilotTick runs the launch/in-session catch-up and re-arms the
// chain for the next boundary.
func (m *Model) onAutopilotTick() tea.Cmd {
	m.autoDisarm()
	if m.autopilots != nil {
		m.catchUpAutopilots()
	}
	return m.armAutopilots()
}

// refreshPaneRail rebuilds channel sources from live org+roster state.
func (m *Model) refreshPaneRail() {
	if m.pane == nil {
		return
	}
	var teams []org.Team
	if m.org != nil {
		teams = m.org.Teams()
	}
	agents := []string{}
	if roster, err := org.LoadRoster(m.ws); err == nil {
		for _, a := range roster {
			agents = append(agents, a.ID)
		}
	}
	m.pane.buildChannels(agents, teams)
}

type paneMsg struct{}

func (m *Model) listenPane() tea.Cmd {
	ch := m.pane.events
	return func() tea.Msg {
		_, ok := <-ch
		if !ok {
			return nil
		}
		return paneMsg{}
	}
}

func (m *Model) send(ev wsEvent) {
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

func (m *Model) Resize(w, h int) { m.width, m.height = w, h }

// Update handles async events: pings re-render; clone results resolve
// the busy modal or surface the error inline.
func (m *Model) Update(msg tea.Msg) tea.Cmd {
	switch msg := msg.(type) {
	case paneMsg:
		return m.listenPane()
	case wsEvent:
		if msg.kind == evPing && m.pane != nil {
			m.refreshPaneRail()
		}
		switch msg.kind {
		case evCloneDone:
			if m.form.kind == fAdd && m.form.busy {
				m.form.busy = false
				if msg.err != "" {
					m.form.err = msg.err
				} else {
					m.form = formState{}
				}
			}
		case evTaskPRDone:
			if msg.err != "" {
				m.flashErr(msg.err)
			} else {
				m.form = formState{flash: "PR #" + itoa(msg.prNum) +
					" created for " + msg.packName}
			}
		case evPingFlash:
			m.form = formState{flash: msg.flash}
		}
		if msg.kind == evPing && m.autopilots != nil {
			// A card changed (created/armed/removed): the schedule set
			// moved — re-arm the tick chain for the new boundaries so a
			// brand-new interval card starts ticking without a resize.
			m.autoDisarm()
			return tea.Batch(m.armAutopilots(), m.listen())
		}
		return m.listen()
	case autopilotTickMsg:
		return m.onAutopilotTick()
	case snoozeTickMsg:
		return m.onSnoozeTick()
	}
	return nil
}

// ---- forms ----

// modalKind enumerates overlay states.
type modalKind uint8

const (
	fNone modalKind = iota
	fAdd
	fRename
	fRemoveConfirm
	fTaskNew
	fTaskAssign
	fTaskAttach
	fTaskThread
	fTaskRemoveConfirm
	fTaskPR
	fTaskCommit
	fTaskPush
	fTaskMove
	fTaskLabels
	fTaskPriority
	fTaskEpic
	fTaskDue
	fTaskBulkMove
	fSnooze
)

// field is the canonical kit field: in-value cursor (left/right),
// paste, shift+tab, focus-visible rendering ride kit.Form (F-026 P3a).
type field = kit.Field

func textField(label, value string) field {
	return kit.NewTextField(label, value)
}

// toggleField is a cycling single-choice field (F-017 snooze presets).
func toggleField(label string, opts []string) field {
	return kit.NewToggleField(label, opts, 0)
}

// toggleFieldAt opens the choice at a specific index (lane picker).
func toggleFieldAt(label string, opts []string, initial int) field {
	return kit.NewToggleField(label, opts, initial)
}

// formState is the active modal (zero kind = none). Inputs live in the
// canonical kit.Form; orig captures the entity being edited so renames
// of the name buffer cannot detach the target. err stays local (tests
// assert it) and mirrors into the kit form for rendering.
type formState struct {
	kind  modalKind
	orig  string
	f     *kit.Form
	busy  bool
	err   string
	flash string
}

func (fs *formState) fields() []field {
	if fs.f == nil {
		return nil
	}
	return fs.f.Fields
}

func (fs *formState) values() []string {
	if fs.f == nil {
		return nil
	}
	return fs.f.Values()
}

func (fs *formState) curField() int {
	if fs.f == nil {
		return 0
	}
	return fs.f.Cur()
}

func (fs *formState) target() string { return fs.orig }

// openForm opens kind with the orig target and canonical kit fields
// (confirm modals pass no fields).
func openForm(kind modalKind, orig string, fields ...field) formState {
	f := formState{kind: kind, orig: orig}
	f.f = kit.NewForm("", fields...)
	return f
}

// csv splits comma-separated entries and trims blanks.
func csv(s string) []string {
	var out []string
	for _, part := range strings.Split(s, ",") {
		if p := strings.TrimSpace(part); p != "" {
			out = append(out, p)
		}
	}
	return out
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

// Wheel routes wheel events to the focused section (F-026 P2): three
// rows per tick through the same key path as j/k; forms and the
// channels composer never see the wheel.
func (m *Model) Wheel(dy int) bool {
	if dy == 0 || m.ws == nil || m.form.kind != fNone ||
		(m.pane != nil && m.pane.focus) {
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

// HelpSections feeds the shell's contextual help (F-026 P7): the
// active section's live keymap — the same wording as its chrome bar.
func (m *Model) HelpSections() [][2]string {
	hints := m.sectionHints()
	out := make([][2]string, 0, len(hints)+2)
	out = append(out, [2]string{"[ / ]", "switch sections"})
	out = append(out, [2]string{"j / k", "move the cursor"})
	for _, h := range hints {
		parts := strings.SplitN(h, " ", 2)
		if len(parts) != 2 {
			out = append(out, [2]string{h, ""})
			continue
		}
		out = append(out, [2]string{parts[0], parts[1]})
	}
	return out
}

func (m *Model) sectionKey(key string) bool {
	// Run-replay pane (F-014/F-026): modal until esc, or a section
	// switch which CLOSES it first (a hidden replay must not keep
	// owning keys while the board renders beneath it).
	if m.replay != nil {
		switch key {
		case "esc":
			m.replay = nil
			return true
		case "[":
			m.replay = nil
			m.sec = (m.sec - 1 + secCount) % secCount
			return true
		case "]":
			m.replay = nil
			m.sec = (m.sec + 1) % secCount
			return true
		}
		return m.replay.Key(key, m)
	}

	switch key {
	case "[":
		m.sec = (m.sec - 1 + secCount) % secCount
		return true
	case "]":
		m.sec = (m.sec + 1) % secCount
		return true
	}

	switch m.sec {
	case secInbox:
		return m.inboxKey(key)
	case secBoard:
		return m.boardKey(key)
	case secChannels:
		return m.pane.handleKey(key)
	case secRepos:
		return m.reposKey(key)
	}
	return false
}

func clampCursor(c *int, n int) {
	if *c >= n {
		*c = maxInt(n-1, 0)
	}
	if *c < 0 {
		*c = 0
	}
}

func (m *Model) flashErr(msg string) {
	m.form = formState{kind: fNone, err: msg}
}

// ---- REPOS section (member repos) ----

func (m *Model) reposKey(key string) bool {
	members := m.ws.Members()
	c := &m.cursors[secRepos]
	clampCursor(c, len(members))
	switch key {
	case "j", "down":
		if *c < len(members)-1 {
			*c++
		}
		return true
	case "k", "up":
		if *c > 0 {
			*c--
		}
		return true
	case "a", "n":
		m.form = openForm(fAdd, "",
			textField("name ", ""), textField("path ", ""))
		return true
	case "r", "enter":
		if len(members) > 0 {
			mem := members[*c]
			m.form = openForm(fRename, mem.Name, textField("new  ", mem.Name))
		}
		return true
	case "d", "delete":
		if len(members) > 0 {
			m.form = openForm(fRemoveConfirm, members[*c].Name)
		}
		return true
	case "e":
		if len(members) > 0 && m.openEditor != nil {
			mem := members[*c]
			if !m.openEditor([]string{mem.Path}) {
				m.inboxHint = "editor unavailable — open " + mem.Name + " by hand"
			}
			return true
		}
	}
	return false
}

// ---- BOARD section (F-021): the kanban over the task store ----

// boardGroups snapshots the store into the four lanes, in
// tasks.Statuses order. Pure over List(); rendering and keys share it.
func (m *Model) boardGroups() [4][]tasks.Task {
	var g [4][]tasks.Task
	if m.taskStore == nil {
		return g
	}
	ix := map[tasks.Status]int{}
	for i, s := range tasks.Statuses {
		ix[s] = i
	}
	for _, tk := range m.taskRows() {
		if i, ok := ix[tk.Status]; ok {
			g[i] = append(g[i], tk)
		}
	}
	return g
}

// boardSelected returns the card under the cursor.
func (m *Model) boardSelected(g [4][]tasks.Task) (tasks.Task, bool) {
	if m.boardActive < 0 || m.boardActive >= 4 {
		return tasks.Task{}, false
	}
	col := g[m.boardActive]
	if len(col) == 0 {
		return tasks.Task{}, false
	}
	cur := m.boardCur[m.boardActive]
	if cur < 0 || cur >= len(col) {
		return col[len(col)-1], true
	}
	return col[cur], true
}

func (m *Model) boardKey(key string) bool {
	if m.boardFilterEdit {
		switch key {
		case "esc":
			m.boardFilterEdit = false
			m.boardFilter = ""
		case "enter":
			m.boardFilterEdit = false
		case "backspace":
			if len(m.boardFilter) > 0 {
				m.boardFilter = m.boardFilter[:len(m.boardFilter)-1]
			}
		default:
			if r := []rune(key); len(r) == 1 && r[0] >= 32 {
				m.boardFilter += key
			}
		}
		return true
	}
	g := m.boardGroups()
	switch key {
	case "h", "left":
		if m.boardActive > 0 {
			m.boardActive--
		}
		return true
	case "l", "right":
		if m.boardActive < 3 {
			m.boardActive++
		}
		return true
	case "j", "down":
		if len(g[m.boardActive]) > 0 &&
			m.boardCur[m.boardActive] < len(g[m.boardActive])-1 {
			m.boardCur[m.boardActive]++
		}
		return true
	case "k", "up":
		if m.boardCur[m.boardActive] > 0 {
			m.boardCur[m.boardActive]--
		}
		return true
	case "g":
		m.boardCur[m.boardActive] = 0
		return true
	case "G":
		if n := len(g[m.boardActive]); n > 0 {
			m.boardCur[m.boardActive] = n - 1
		}
		return true
	case "/", "f":
		m.boardFilterEdit = true
		return true
	case "C":
		m.clearBoardMarks()
		return true
	}

	tk, ok := m.boardSelected(g)
	if !ok {
		// The board is inert without a selected card, but `n` always
		// works and the board swallows navigation keys above.
		if key == "n" && m.taskStore != nil {
			m.form = openForm(fTaskNew, "",
				textField("slug  ", ""), textField("title ", ""))
			return true
		}
		return false
	}

	switch key {
	case "n":
		if m.taskStore == nil {
			return false
		}
		m.form = openForm(fTaskNew, "",
			textField("slug  ", ""), textField("title ", ""))
	case "s":
		if m.taskStore != nil {
			slug := tk.Slug
			next := nextStatus(tk.Status)
			if err := m.taskStore.SetStatus(slug, next); err != nil {
				m.flashErr(err.Error())
				return true
			}
			// Focus follows the card into its new lane (the board is a
			// kanban, not a list — selection never strands in the old
			// column).
			m.followCardIntoLane(slug)
		}
	case "S":
		if m.taskStore != nil {
			if err := m.taskStore.SetStatus(tk.Slug, prevStatus(tk.Status)); err != nil {
				m.flashErr(err.Error())
				return true
			}
			m.followCardIntoLane(tk.Slug)
		}
	case "m":
		if m.taskStore != nil {
			m.form = openForm(fTaskMove, tk.Slug,
				toggleFieldAt("lane ", statusLabels(), statusIndex(tk.Status)))
		}
	case "a":
		m.form = openForm(fTaskAssign, tk.Slug, textField("assignee ", tk.Assignee))
	case "w":
		m.form = openForm(fTaskAttach, tk.Slug,
			textField("member ", ""),
			textField("branch ", "task/"+tk.Slug))
	case "t":
		m.form = openForm(fTaskThread, tk.Slug,
			textField("channel ", tk.ThreadChannel),
			textField("thread# ", itoa(int(tk.ThreadID))))
	case "x", "d":
		m.form = openForm(fTaskRemoveConfirm, tk.Slug)
	case "p":
		switch {
		case m.reviewSvc == nil:
			m.flashErr("review service unavailable — cannot create PRs")
		case len(tk.ChangeSets) == 0:
			m.flashErr("card has no worktree — attach one first (w)")
		default:
			m.form = openForm(fTaskPR, tk.Slug,
				textField("title ", tk.Title),
				textField("base  ", "main"))
		}
	case "c":
		if m.taskStore == nil || len(tk.ChangeSets) == 0 {
			m.flashErr("card has no worktree — attach one first (w)")
			return true
		}
		m.form = openForm(fTaskCommit, tk.Slug, textField("message ", ""))
	case "u":
		if m.taskStore == nil || len(tk.ChangeSets) == 0 {
			m.flashErr("card has no worktree — attach one first (w)")
			return true
		}
		m.form = openForm(fTaskPush, tk.Slug)
	case "r":
		if run, ok := tk.NewestRun(); ok {
			m.replay = openReplay(run)
			return true
		}
		m.flashErr("card has no recorded runs")
	case "o":
		return m.boardOpenOnFloor(tk)
	case "L":
		m.form = openForm(fTaskLabels, tk.Slug, textField("labels ", strings.Join(tk.Labels, ",")))
	case "P":
		m.form = openForm(fTaskPriority, tk.Slug,
			toggleFieldAt("priority ", priorityLabels(), priorityIndex(tk.Priority)))
	case "E":
		m.form = openForm(fTaskEpic, tk.Slug, textField("epic   ", tk.Epic))
	case "D":
		m.form = openForm(fTaskDue, tk.Slug, textField("due    ", tk.Due))
	case " ":
		m.toggleBoardMark(tk.Slug)
	case "M":
		if len(m.boardMarks) == 0 {
			m.flashErr("no cards marked — space marks a card")
			return true
		}
		m.form = openForm(fTaskBulkMove, "",
			toggleFieldAt("lane ", statusLabels(), statusIndex(tk.Status)))
	default:
		return false
	}
	return true
}

// toggleBoardMark adds/removes a card from the bulk selection.
func (m *Model) toggleBoardMark(slug string) {
	if m.boardMarks == nil {
		m.boardMarks = map[string]bool{}
	}
	if m.boardMarks[slug] {
		delete(m.boardMarks, slug)
	} else {
		m.boardMarks[slug] = true
	}
}

func (m *Model) clearBoardMarks() {
	m.boardMarks = map[string]bool{}
}

// priorityLabels is the priority picker's option order.
func priorityLabels() []string {
	out := make([]string, 0, len(tasks.Priorities)+1)
	out = append(out, "(unset)")
	for _, p := range tasks.Priorities {
		out = append(out, string(p))
	}
	return out
}

// priorityIndex maps a priority to its picker index (0 = unset).
func priorityIndex(p tasks.Priority) int {
	if p == "" {
		return 0
	}
	for i, v := range tasks.Priorities {
		if v == p {
			return i + 1
		}
	}
	return 0
}

// priorityWord returns the picker value at an index.
func priorityWord(i int) tasks.Priority {
	if i <= 0 || i > len(tasks.Priorities) {
		return ""
	}
	return tasks.Priorities[i-1]
}

// boardOpenOnFloor is the board→floor jump (F-021): the bound thread,
// else the assignee's DM; neither → a named flash, never a guess.
func (m *Model) boardOpenOnFloor(tk tasks.Task) bool {
	if tk.ThreadChannel != "" && m.pane != nil {
		if m.pane.openAt(tk.ThreadChannel, tk.ThreadID, 0) {
			m.sec = secChannels
			return true
		}
	}
	if tk.Assignee != "" && tk.Assignee != "you" && m.pane != nil {
		if m.pane.openAt("dm:"+tk.Assignee, 0, 0) {
			m.sec = secChannels
			return true
		}
	}
	m.flashErr("no bound thread or agent assignee to open")
	return true
}

func nextStatus(st tasks.Status) tasks.Status {
	for i, s := range tasks.Statuses {
		if s == st {
			return tasks.Statuses[(i+1)%len(tasks.Statuses)]
		}
	}
	return tasks.Backlog
}

// prevStatus is the backward kanban move (`S`, F-026 P3).
func prevStatus(st tasks.Status) tasks.Status {
	for i, s := range tasks.Statuses {
		if s == st {
			return tasks.Statuses[(i-1+len(tasks.Statuses))%len(tasks.Statuses)]
		}
	}
	return tasks.Backlog
}

// statusLabels is the lane-picker field order (tasks.Statuses).
// splitCSV splits a comma-separated form value into trimmed entries.
func splitCSV(s string) []string {
	var out []string
	for _, part := range strings.Split(s, ",") {
		if p := strings.TrimSpace(part); p != "" {
			out = append(out, p)
		}
	}
	return out
}

// indexOfValue returns the position of v in opts (-1 when absent).
func indexOfValue(opts []string, v string) int {
	for i, o := range opts {
		if o == v {
			return i
		}
	}
	return -1
}

func statusLabels() []string {
	out := make([]string, 0, len(tasks.Statuses))
	for _, s := range tasks.Statuses {
		out = append(out, string(s))
	}
	return out
}

func statusIndex(st tasks.Status) int {
	for i, s := range tasks.Statuses {
		if s == st {
			return i
		}
	}
	return 0
}

// followCardIntoLane puts the board cursor on the card's lane and row.
func (m *Model) followCardIntoLane(slug string) {
	for li, col := range m.boardGroups() {
		for ci, t2 := range col {
			if t2.Slug == slug {
				m.boardActive = li
				m.boardCur[li] = ci
				return
			}
		}
	}
}

// taskRows lists every card in store order.
func (m *Model) taskRows() []tasks.Task {
	if m.taskStore == nil {
		return nil
	}
	all := m.taskStore.List()
	if strings.TrimSpace(m.boardFilter) == "" {
		return all
	}
	out := make([]tasks.Task, 0, len(all))
	for _, tk := range all {
		if boardMatches(tk, m.boardFilter) {
			out = append(out, tk)
		}
	}
	return out
}

// boardMatches reports whether a card matches the free-text board filter
// (F-035): a case-insensitive substring over title, slug, assignee, epic
// and labels.
func boardMatches(tk tasks.Task, query string) bool {
	q := strings.ToLower(strings.TrimSpace(query))
	if q == "" {
		return true
	}
	hay := strings.ToLower(strings.Join(append([]string{
		tk.Title, tk.Slug, tk.Assignee, tk.Epic,
	}, tk.Labels...), " "))
	return strings.Contains(hay, q)
}

// ---- autopilot execution (ADR-0014 §5) ----
// (catchUpAutopilots/autopilotRun/rostered live in autopilots.go)

// ---- form keys & submission ----

func (m *Model) formKey(key string) bool {
	f := &m.form
	f.f.Busy = f.busy // kit focus-trap mirrors the async state
	if f.busy {
		return true // swallow while async work runs
	}
	switch f.kind {
	case fRemoveConfirm, fTaskRemoveConfirm:
		switch key {
		case "enter":
			m.submitConfirm()
			return true
		case "esc", "n":
			m.closeForm()
			return true
		}
		return true // confirm modals swallow everything else
	}

	switch key {
	case "esc":
		m.closeForm()
		return true
	case "enter":
		m.submitForm()
		return true
	}
	f.f.HandleKey(key) // tab/shift+tab/backspace/left/right/runes/paste
	return true        // the modal swallows everything else (focus trap)
}

func (m *Model) closeForm() {
	flash := m.form.flash
	m.form = formState{flash: flash}
}

func (m *Model) submitForm() {
	f := &m.form
	switch f.kind {
	case fAdd:
		name := strings.TrimSpace(f.values()[0])
		loc := strings.TrimSpace(f.values()[1])
		if err := workspace.ValidateName(name); err != nil {
			f.err = err.Error()
			return
		}
		if loc == "" {
			f.err = "path or URL required"
			return
		}
		if isCloneSource(loc) {
			dst := filepath.Join(m.ws.Root, name)
			if _, err := os.Stat(dst); err == nil {
				f.err = dst + " already exists"
				return
			}
			f.busy = true
			f.err = ""
			go m.cloneAndRegister(name, loc, dst)
			return
		}
		if err := m.ws.AddMember(name, loc); err != nil {
			f.err = err.Error()
			return
		}
		m.closeForm()
	case fRename:
		newName := strings.TrimSpace(f.values()[0])
		if newName == f.target() {
			m.closeForm()
			return
		}
		if err := m.ws.RenameMember(f.target(), newName); err != nil {
			f.err = err.Error()
			return
		}
		m.closeForm()
	case fTaskNew:
		slug := strings.TrimSpace(f.values()[0])
		title := strings.TrimSpace(f.values()[1])
		if m.taskStore == nil {
			f.err = "task store unavailable"
			return
		}
		if err := m.taskStore.Create(slug, title, "", ""); err != nil {
			f.err = err.Error()
			return
		}
		m.closeForm()
	case fTaskAssign:
		if m.taskStore == nil {
			f.err = "task store unavailable"
			return
		}
		if err := m.taskStore.Assign(f.orig, strings.TrimSpace(f.values()[0])); err != nil {
			f.err = err.Error()
			return
		}
		m.closeForm()
	case fTaskAttach:
		if m.taskStore == nil {
			f.err = "task store unavailable"
			return
		}
		member := strings.TrimSpace(f.values()[0])
		branch := strings.TrimSpace(f.values()[1])
		if err := m.taskStore.Attach(f.orig, member, branch, ""); err != nil {
			f.err = err.Error()
			return
		}
		// Cross-project propagation (F-032): changing this member may
		// affect declared dependents — seed visible, accept/decline
		// proposals; nothing is auto-created.
		if m.ws != nil {
			var seeds []tasks.Propagation
			for _, d := range m.ws.DependentsOf(member) {
				seeds = append(seeds, tasks.Propagation{FromMember: d.From, ToMember: d.To, Kind: d.Kind})
			}
			if err := m.taskStore.SeedPropagations(f.orig, seeds); err != nil {
				f.err = err.Error()
				return
			}
		}
		m.closeForm()
	case fTaskThread:
		if m.taskStore == nil {
			f.err = "task store unavailable"
			return
		}
		channel := strings.TrimSpace(f.values()[0])
		tid := int64(0)
		_, _ = fmt.Sscanf(strings.TrimSpace(f.values()[1]), "%d", &tid)
		if err := m.taskStore.BindThread(f.orig, channel, tid); err != nil {
			f.err = err.Error()
			return
		}
		m.closeForm()
	case fTaskPR:
		title := strings.TrimSpace(f.values()[0])
		base := strings.TrimSpace(f.values()[1])
		if title == "" || base == "" {
			f.err = "title and base required"
			return
		}
		slug := f.orig
		tk, ok := m.taskStore.Get(slug)
		if !ok || len(tk.ChangeSets) == 0 {
			f.err = "card lost its worktree"
			return
		}
		m.closeForm()
		m.form = formState{kind: fNone, flash: "creating PR…"}
		go func() {
			ctx, cancel := context.WithTimeout(context.Background(), taskPRTimeout)
			defer cancel()
			cs := tk.ChangeSets[0]
			meta, err := m.reviewSvc.CreatePRForBranch(ctx,
				cs.Member, cs.Branch, title, base)
			ev := wsEvent{kind: evTaskPRDone, packName: slug}
			if err != nil {
				ev.err = err.Error()
			} else {
				ev.prNum = meta.Number
				_ = m.taskStore.SetPR(slug, meta.Number, meta.URL)
			}
			m.send(ev)
		}()
		return
	case fTaskCommit:
		if m.taskStore == nil {
			f.err = "task store unavailable"
			return
		}
		message := strings.TrimSpace(f.values()[0])
		if message == "" {
			f.err = "commit message required"
			return
		}
		slug := f.orig
		tk, ok := m.taskStore.Get(slug)
		if !ok || len(tk.ChangeSets) == 0 {
			f.err = "card has no worktree"
			return
		}
		m.closeForm()
		m.form = formState{kind: fNone, flash: "committing..."}
		go func() {
			err := m.taskStore.Commit(slug, message)
			ev := wsEvent{kind: evPingFlash, err: errString(err)}
			if err == nil {
				ev.flash = "committed"
			}
			m.send(ev)
		}()
		return
	case fTaskPush:
		if m.taskStore == nil {
			f.err = "task store unavailable"
			return
		}
		slug := f.orig
		m.closeForm()
		m.form = formState{kind: fNone, flash: "pushing..."}
		go func() {
			err := m.taskStore.PushBranch(slug)
			ev := wsEvent{kind: evPingFlash, err: errString(err)}
			if err == nil {
				ev.flash = "pushed"
			}
			m.send(ev)
		}()
		return
	case fTaskMove:
		if m.taskStore == nil {
			f.err = "task store unavailable"
			return
		}
		next := tasks.Status(f.values()[0])
		if err := m.taskStore.SetStatus(f.orig, next); err != nil {
			f.err = err.Error()
			return
		}
		m.followCardIntoLane(f.orig)
		m.closeForm()
	case fTaskLabels:
		if m.taskStore == nil {
			f.err = "task store unavailable"
			return
		}
		if err := m.taskStore.SetLabels(f.orig, splitCSV(f.values()[0])); err != nil {
			f.err = err.Error()
			return
		}
		m.closeForm()
	case fTaskPriority:
		if m.taskStore == nil {
			f.err = "task store unavailable"
			return
		}
		if err := m.taskStore.SetPriority(f.orig, priorityWord(indexOfValue(priorityLabels(), f.values()[0]))); err != nil {
			f.err = err.Error()
			return
		}
		m.closeForm()
	case fTaskEpic:
		if m.taskStore == nil {
			f.err = "task store unavailable"
			return
		}
		if err := m.taskStore.SetEpic(f.orig, f.values()[0]); err != nil {
			f.err = err.Error()
			return
		}
		m.closeForm()
	case fTaskDue:
		if m.taskStore == nil {
			f.err = "task store unavailable"
			return
		}
		if err := m.taskStore.SetDue(f.orig, f.values()[0]); err != nil {
			f.err = err.Error()
			return
		}
		m.closeForm()
	case fTaskBulkMove:
		if m.taskStore == nil {
			f.err = "task store unavailable"
			return
		}
		next := tasks.Status(f.values()[0])
		slugs := make([]string, 0, len(m.boardMarks))
		for slug := range m.boardMarks {
			slugs = append(slugs, slug)
		}
		sort.Strings(slugs)
		m.closeForm()
		for _, slug := range slugs {
			if err := m.taskStore.SetStatus(slug, next); err != nil {
				m.flashErr(err.Error())
				return
			}
		}
		m.clearBoardMarks()
		m.form = formState{kind: fNone, flash: "moved " + itoa(len(slugs)) + " card(s) to " + string(next)}
	case fSnooze:
		if m.snoozeTarget.Kind != inbox.AgentMessage {
			f.err = "snooze target lost — reopen with z"
			return
		}
		m.snoozeSelected(m.snoozeTarget, f.values()[0])
		m.closeForm()
	}
}

func (m *Model) submitConfirm() {
	f := &m.form
	switch f.kind {
	case fRemoveConfirm:
		if err := m.ws.RemoveMember(f.target()); err != nil {
			f.err = err.Error()
			return
		}
		c := &m.cursors[secRepos]
		clampCursor(c, len(m.ws.Members()))
		m.closeForm()
	case fTaskRemoveConfirm:
		if m.taskStore == nil {
			f.err = "task store unavailable"
			return
		}
		if err := m.taskStore.Remove(f.target()); err != nil {
			f.err = err.Error()
			return
		}
		rows := m.taskRows()
		clampCursor(&m.boardCur[m.boardActive], len(m.boardGroups()[m.boardActive]))
		_ = rows
		m.closeForm()
	}
}

// async workers ----

func (m *Model) cloneAndRegister(name, url, dst string) {
	ctx, cancel := context.WithTimeout(context.Background(), cloneTimeout)
	defer cancel()
	if _, err := gitcore.Clone(ctx, url, dst); err != nil {
		_ = os.RemoveAll(dst)
		m.send(wsEvent{kind: evCloneDone, err: err.Error()})
		return
	}
	if err := m.ws.AddMember(name, dst); err != nil {
		m.send(wsEvent{kind: evCloneDone, err: err.Error()})
		return
	}
	m.send(wsEvent{kind: evCloneDone})
}

func isCloneSource(loc string) bool {
	for _, p := range []string{"http://", "https://", "git://", "ssh://", "git@"} {
		if strings.HasPrefix(loc, p) {
			return true
		}
	}
	return false
}
