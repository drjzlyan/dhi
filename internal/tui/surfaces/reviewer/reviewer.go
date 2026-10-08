// Package reviewer is DHI's code-review floor (F-005): three sections —
// REVIEWS (session list), FILES (changed paths of the open review) and
// DIFF (unified or side-by-side patch view) — switched with [ ].
// Comments, agent invites and completion flows layer onto this skeleton
// in later phases.
package reviewer

import (
	"context"
	"github.com/drjzlyan/dhi/internal/tutorial"
	"strconv"
	"strings"
	"time"

	"charm.land/bubbletea/v2"

	"github.com/drjzlyan/dhi/internal/agentkit/bus"
	"github.com/drjzlyan/dhi/internal/gitdiff"
	"github.com/drjzlyan/dhi/internal/review"
	"github.com/drjzlyan/dhi/internal/tasks"
	"github.com/drjzlyan/dhi/internal/tui/surfaces"
	"github.com/drjzlyan/dhi/internal/workspace"
)

// opTimeout bounds every async service call.
const opTimeout = 5 * time.Minute

// maxPromptPatch caps the diff excerpt sent to reviewing agents.
const maxPromptPatch = 48_000

// crew is the narrow runtime seam: dispatch turns + roster ids.
// *runtime.Runtime satisfies it; tests use scripted fakes.
type crew interface {
	Handle(ctx context.Context, msg bus.Message)
	AgentIDs() []string
}

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
	emit    func(event string) // tutorial action events (F-051); nil = not wired
	version string
	ws      *workspace.Workspace
	svc     *review.Service // nil = whole surface degrades visibly
	width   int
	height  int

	sec     sectionID
	cursors [secCount]int
	// offsets is the first visible rendered row per section for panes
	// with a real scroll window (F-025: FILES).
	offsets [secCount]int

	openID  string // review currently loaded ("", none)
	files   []gitdiff.FileDiff
	diffFor string // review id the cached diff belongs to
	busy    bool   // an async op is running
	opErr   string
	layout  layoutMode
	scroll  int // diff viewport top (visual rows)
	cursor  int // diff cursor (logical rows)
	fileCur int // FILES cursor mirrored into DIFF jumps

	// screenRows/screenFirst: header rows and first file shown in the
	// three-column FILES column, for click mapping (F-049 R-E).
	screenRows, screenFirst int

	form formState

	composer *composer    // active comment input (nil = none)
	draft    *submitDraft // the review being composed in the submit dialog
	// styleCache holds per-file syntax and changed-word styles (F-049);
	// it is dropped whenever the diff rows are rebuilt.
	styleCache map[int]map[gitdiff.MarkKey]lineStyle
	// Collapsed-context state (F-049): the new-side source of each file,
	// the lines of expanded regions, and which regions are open.
	srcCache map[int][]string
	gapCache map[gapKey][]gitdiff.Line
	expanded map[gapKey]bool

	rowsCache  []viewRow // diffRows flatten cache (F-026 P6)
	rowsFP     string
	threadOpen bool // DIFF replaced by the thread view
	threadFile string
	threadCur  int

	transcriptOpen   bool     // DIFF replaced by the run-transcript view
	transcriptTitle  string   // header of the loaded transcript
	transcriptLines  []string // rendered transcript lines
	transcriptScroll int
	syncedAt         time.Time // last successful remote-comment import

	bus       *bus.Bus
	crew      crew
	cancelBus func()

	taskStore    *tasks.Store
	openInEditor func(paths []string) bool

	events chan revEvent
}

var _ surfaces.Surface = (*Model)(nil)

type revEvent struct {
	kind uint8 // ...|evImported
	err  string
	id   string
	msg  bus.Message
	n    int // PR number for evPosted, added-comment count for evImported, comments sent for evSubmitted
	url  string
}

func itoa(n int) string { return strconv.Itoa(n) }

const (
	evPing uint8 = iota
	evStartDone
	evDiffDone
	evDiscardDone
	evBus
	evAgentDone
	evSubmitted // a whole review was sent (F-049)
	evPRCreated
	evImported
)

// Deps carries the services this surface operates. A nil Service degrades
// every section to visible "unavailable" rows; nil Bus/Crew disable the
// agent-participation keys with visible messages; nil Tasks/OpenInEditor
// disable the matching completion flows.
type Deps struct {
	Service      *review.Service
	Bus          *bus.Bus
	Crew         crew
	Tasks        *tasks.Store
	OpenInEditor func(paths []string) bool
}

// New returns the reviewer model. A nil ws renders the empty state with
// all keys inert.
func New(version string, ws *workspace.Workspace, d Deps) *Model {
	return &Model{
		version:      version,
		ws:           ws,
		svc:          d.Service,
		bus:          d.Bus,
		crew:         d.Crew,
		taskStore:    d.Tasks,
		openInEditor: d.OpenInEditor,
		events:       make(chan revEvent, 16),
	}
}

func (m *Model) Meta() surfaces.Meta { return surfaces.Meta{ID: "reviewer", Title: "Reviewer"} }

// StatusContext feeds the app statusline (F-025).
func (m *Model) StatusContext() (string, string) {
	zone := strings.ToLower(m.sec.label())
	switch {
	case m.composer != nil:
		return zone, "COMPOSE"
	case m.form.kind != fNone:
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
		out = append(out, surfaces.Command{Group: "Reviewer", Title: "Go to " + s.label(), Hint: "[ ]", Run: func() tea.Cmd {
			m.sec = s
			return nil
		}})
	}
	return out
}

// CapturesInput implements surfaces.InputCapturer: the comment composer
// and forms take plain keys as text.
func (m *Model) CapturesInput() bool {
	return m.composer != nil || m.form.kind != fNone
}

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
				m.resetGapState() // a new diff: forget the old file contents and expansions
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
		case evBus:
			m.mirrorBus(ev.msg)
		case evAgentDone:
			m.busy = false
			m.form = formState{flash: "agent review requested"}
			if ev.err != "" {
				m.opErr = ev.err
			}
		case evSubmitted:
			m.busy = false
			if ev.err != "" {
				m.opErr = ev.err
			} else {
				msg := "review sent as you"
				if ev.n > 0 {
					msg += " (" + plural(ev.n, "comment") + ")"
				}
				if ev.url != "" {
					msg += " — " + ev.url
				}
				m.closeFormWithFlash(msg)
				m.act(tutorial.EvReviewSubmitted)
			}
		case evPRCreated:
			m.busy = false
			if ev.err != "" {
				m.opErr = ev.err
			} else {
				m.closeFormWithFlash("PR #" + itoa(ev.n) + " created")
				if ev.id == m.openID {
					m.loadDiff() // target flipped to PR: refresh view model
					m.refreshRemote()
				}
			}
		case evImported:
			if ev.err != "" {
				m.opErr = ev.err
			} else if ev.n > 0 && ev.id == m.openID {
				m.closeFormWithFlash("+" + itoa(ev.n) + " remote comment(s)")
				m.syncedAt = time.Now()
			} else if ev.id == m.openID {
				m.syncedAt = time.Now()
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

// SetEmitter implements surfaces.Emitter: the shell learns what the user
// just did so a running tutorial can advance on the real action (F-051).
func (m *Model) SetEmitter(fn func(event string)) { m.emit = fn }

// act reports one user action (UI goroutine only).
func (m *Model) act(event string) {
	if m.emit != nil {
		m.emit(event)
	}
}

func (m *Model) openReview() (review.Review, bool) {
	if m.openID == "" {
		return review.Review{}, false
	}
	return m.svc.Store().Get(m.openID)
}

// SelectReview is the F-016 in-review jump seam: opens the named review
// card for a task. Returns false when the reviewer is unavailable or the
// review is unknown (the inbox then degrades to a named hint).
func (m *Model) SelectReview(id string) bool {
	if m.ws == nil || m.svc == nil {
		return false
	}
	if _, ok := m.svc.Store().Get(id); !ok {
		return false
	}
	m.open(id)
	return true
}

// open selects a review and (re)loads its diff asynchronously, then
// subscribes to its review channel so agent replies mirror live. PR-backed
// reviews also pull remote GitHub comments in the background.
func (m *Model) open(id string) {
	m.act(tutorial.EvReviewOpened)
	m.openID = id
	m.fileCur = 0
	m.cursor, m.scroll = 0, 0
	m.loadDiff()
	m.subscribeBus(id)
	m.refreshRemote()
}

// refreshRemote re-imports PR comments through gh asynchronously.
func (m *Model) refreshRemote() {
	r, ok := m.openReview()
	if !ok || m.svc == nil || !m.svc.HasGH() || r.Target.PRNumber <= 0 {
		return
	}
	go func(id string) {
		ctx, cancel := context.WithTimeout(context.Background(), opTimeout)
		defer cancel()
		n, err := m.svc.ImportComments(ctx, r)
		m.send(revEvent{kind: evImported, id: id, n: n, err: errString(err)})
	}(r.ID)
}

// subscribeBus pumps the review's bus channel into the event loop.
func (m *Model) subscribeBus(id string) {
	if m.cancelBus != nil {
		m.cancelBus()
		m.cancelBus = nil
	}
	if m.bus == nil || m.svc == nil {
		return
	}
	r, ok := m.svc.Store().Get(id)
	if !ok || r.Channel == "" {
		return
	}
	ch, cancel := m.bus.Subscribe(r.Channel)
	m.cancelBus = cancel
	go func() {
		for msg := range ch {
			m.send(revEvent{kind: evBus, msg: msg})
		}
	}()
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
	if m.composer != nil {
		return m.composerKey(key)
	}
	if m.form.kind != fNone {
		return m.formKey(key)
	}
	if m.threadOpen && m.sec == secDiff {
		return m.threadsKey(key)
	}
	if m.transcriptOpen && m.sec == secDiff {
		return m.transcriptKey(key)
	}
	return m.sectionKey(key)
}

// Wheel routes wheel events to the focused section (F-026 P2): three
// rows per tick through the same key path as j/k; input surfaces
// (composer, forms) never see the wheel.
func (m *Model) Wheel(dy int) bool {
	if dy == 0 || m.ws == nil || m.composer != nil || m.form.kind != fNone {
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
		return true
	case "]":
		m.sec = (m.sec + 1) % secCount
		return true
	case "S":
		m.openSubmit()
		return true
	case "R":
		if r, ok := m.openReview(); ok && r.Target.PRNumber > 0 {
			m.refreshRemote()
			return true
		}
		return false
	case "esc":
		if m.sec != secReviews {
			m.sec = secReviews
			m.threadOpen = false
			m.transcriptOpen = false
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

// HelpSections feeds the shell's contextual help (F-026 P7).
func (m *Model) HelpSections() [][2]string {
	out := [][2]string{
		{"[ / ]", "switch sections"},
		{"j / k", "move the cursor"},
	}
	for _, h := range m.sectionHints() {
		parts := strings.SplitN(h, " ", 2)
		if len(parts) != 2 {
			out = append(out, [2]string{h, ""})
			continue
		}
		out = append(out, [2]string{parts[0], parts[1]})
	}
	return out
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
	case "s":
		m.openSubmit()
		return true
	case "C":
		if sel := selReview(rows, *c); sel != nil {
			switch {
			case m.svc == nil:
				m.opErr = "review service unavailable"
			case sel.Target.PRNumber > 0:
				m.opErr = "already backs PR #" + itoa(sel.Target.PRNumber)
			default:
				base := sel.Target.Base
				if base == "" {
					base = "main"
				}
				m.form = formState{kind: fCreatePR, orig: sel.ID,
					fields: []field{
						textField("title ", sel.Title),
						textField("base  ", base),
					}}
			}
			return true
		}
	case "F":
		if sel := selReview(rows, *c); sel != nil {
			m.dispatchFixer()
			return true
		}
	case "e":
		if sel := selReview(rows, *c); sel != nil && sel.ID == m.openID {
			m.handoffToEditor()
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

func firstAgent(m *Model) string {
	if m.crew == nil {
		return ""
	}
	if ids := m.crew.AgentIDs(); len(ids) > 0 {
		return ids[0]
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
	case "A":
		if ok && len(m.files) > 0 {
			if !m.canAgent() {
				m.opErr = "agent crew unavailable — no bus or runtime"
				return true
			}
			m.form = formState{kind: fAgentReview, fields: []field{
				textField("agent ", firstAgent(m)),
				textField("files ", ". (comma-separated, . = all)"),
			}}
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
	case "c":
		file, line, side, ok := m.anchorAtCursor()
		if !ok {
			return false
		}
		m.threadFile = file
		_ = line
		_ = side
		m.openComposer(0, 0, 0)
		return true
	case "t":
		if path := m.pathAtRow(m.cursor); path != "" {
			m.threadFile = path
			m.threadCur = 0
			m.threadOpen = true
			return true
		}
	case "e":
		if row := m.rowAt(m.cursor); row != nil && row.kind == vrGap {
			m.expandGap(row.gap)
			return true
		}
	case "T":
		m.toggleTranscript()
		return true
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

// clampInt clamps v into [lo, hi]. Callers pass lo <= hi.
func clampInt(v, lo, hi int) int {
	if v < lo {
		return lo
	}
	if v > hi {
		return hi
	}
	return v
}
