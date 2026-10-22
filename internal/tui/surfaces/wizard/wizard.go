// Package wizard is DHI's first-run setup (F-043): a Gate that walks a
// list of Steps with an animated header, persists progress, and asks the
// program to relaunch when it changed anything the running services read
// at launch. Later phases register their own Steps; the core knows none.
package wizard

import (
	"context"
	"strings"
	"time"

	"charm.land/bubbletea/v2"

	"github.com/drjzlyan/dhi/internal/agentkit/brief"
	"github.com/drjzlyan/dhi/internal/agentkit/starter"
	"github.com/drjzlyan/dhi/internal/conventions"
	"github.com/drjzlyan/dhi/internal/gitcore"
	"github.com/drjzlyan/dhi/internal/settings"
	"github.com/drjzlyan/dhi/internal/setup"
	"github.com/drjzlyan/dhi/internal/tui/kit"
	"github.com/drjzlyan/dhi/internal/tui/theme"
)

// Action is a step's answer to a key.
type Action uint8

const (
	Stay Action = iota // keep the step open
	Next               // apply, then advance
	Back               // previous step
	Skip               // advance without applying
)

// Step is one screen of the wizard. Steps are created with the shared
// *Env and read it lazily, so a step's Applies can depend on what an
// earlier step did (e.g. the workspace now exists).
type Step interface {
	ID() string
	Title() string
	Applies() bool
	Enter() tea.Cmd
	HandleKey(key string) Action
	Update(msg tea.Msg) tea.Cmd
	// View renders the body; w is the content width, f the number of
	// animation frames since the step was entered (0 under reduced motion
	// the clock never runs, so f stays 0 and every step must render
	// complete there).
	View(w, f int) []string
	// Apply persists the step. A non-nil error keeps the step open and is
	// shown to the user.
	Apply() error
}

// Env carries the injected services and shared results. Funcs left nil
// disable the corresponding capability (the step explains why).
type Env struct {
	Version string
	CWD     string
	Root    string // workspace root; "" until one exists

	Discover      func(root string) []setup.MemberSpec
	InitWorkspace func(root string, members []setup.MemberSpec) ([]setup.MemberSpec, error)

	Identity    func(ctx context.Context) (gitcore.Identity, error) // nil: git unavailable
	SetIdentity func(ctx context.Context, id gitcore.Identity) error
	// SuggestIdentity finds candidates when none is set (F-083): the
	// repo's config and history, GitHub, the computer account.
	SuggestIdentity func(ctx context.Context) []gitcore.Suggestion

	Conventions     conventions.Config
	SaveConventions func(root string, c conventions.Config) error // root "" = user scope
	// LearnConventions infers commit/PR style from the repo (F-082); it
	// seeds the step only while the conventions are uncustomised.
	LearnConventions func() conventions.Inference

	// Team step (F-045). RosterCount nil = unknown (offer the step);
	// ApplyTeam nil hides it. DetectCLIs maps a CLI name to its version
	// ("" = not installed); SetEngine stores the workspace default engine.
	Engine string // the configured default engine ("cli:<name>" or "")

	// CLI step (F-046). CLIStatus lists the registered coding CLIs with
	// their detected state; InstallCLI installs one through the toolchain
	// seam after the user confirmed the exact command (nil = guided only).
	CLIStatus  func() []CLIRow
	InstallCLI func(ctx context.Context, name string) error
	CLIPrefix  func(name string) string // where a managed install lands

	// Integrations step (ADR-0043). SetupIntegration installs a vendor's
	// official plugin from the marketplace and gives it to who ("the bots
	// it fits" / "every bot" / "nobody yet"); it returns a short note of
	// what happened.
	IntegrationRows  func() []IntegrationRow
	SetupIntegration func(ctx context.Context, plugin, who string) (string, error)
	RosterCount      func() int
	DetectCLIs       func() map[string]string
	ApplyTeam        func(templateSlug string) (starter.Result, error)
	// You step (F-081): the user's profile, saved to the user config.
	// WhoName is the git name, for the preview only.
	Profile     settings.Profile
	SaveProfile func(p settings.Profile) error
	// ProfileSaved reports your config already has a profile: it is
	// yours in every workspace, so a new workspace does not ask (F-090).
	ProfileSaved bool
	WhoName      string

	// Project step (F-080): DetectBrief drafts the brief from the repo,
	// SaveBrief writes it, HasBrief reports one exists (nil = unknown).
	DetectBrief func() brief.Brief
	SaveBrief   func(b brief.Brief) error
	HasBrief    func() bool
	// LoadBrief reads the saved brief, so a re-run starts from it.
	LoadBrief func() (brief.Brief, bool)
	// ComposeTeam creates teams from role counts (F-079); nil hides
	// the custom choice.
	ComposeTeam func(c starter.Counts) ([]starter.Result, error)
	SetEngine   func(engine string) error

	// Instructions step (F-090): what every employee should always do.
	// Instructions loads (yours, this workspace's); SaveInstructions
	// writes them for every workspace (everywhere) or this one. Nil hides
	// the step.
	Instructions     func() (mine, here []string)
	SaveInstructions func(lines []string, everywhere bool) error

	// Persist stores progress (user scope always, workspace scope when
	// root != ""). nil = progress is not remembered.
	Persist func(root string, s setup.State) error
	Now     func() time.Time

	// Tour (F-048): the done step offers a 2-minute walkthrough. StartTour
	// begins it in this process; QueueTour leaves a marker so the next
	// launch (after a relaunch) begins it. Both nil = no offer.
	StartTour     func()
	QueueTour     func() error
	TourRequested bool

	// Results, filled by steps.
	Changed bool     // something launch-time services read was modified
	Applied []string // one human line per applied change, for the summary
	Skipped []string // titles of the steps skipped this run, for the summary
}

// noteSkipped records (skip) or clears (applied) a step for the summary.
func (e *Env) noteSkipped(title string, skip bool) {
	out := e.Skipped[:0:0]
	for _, t := range e.Skipped {
		if t != title {
			out = append(out, t)
		}
	}
	if skip {
		out = append(out, title)
	}
	e.Skipped = out
}

func (e *Env) now() time.Time {
	if e.Now != nil {
		return e.Now()
	}
	return time.Now()
}

// tickInterval matches the bootstrap spinner clock.
const tickInterval = 120 * time.Millisecond

type tickMsg struct{}

// Model is the wizard gate.
type Model struct {
	env   *Env
	steps []Step
	state setup.State

	idx        int
	skipped    map[string]bool
	frame      int
	enterFrame int
	clockArmed bool

	width, height int
	finished      bool
	relaunch      bool
	pending       tea.Cmd

	// single runs one step from the checklist (Single): it records only
	// that step and never marks setup finished. onDone runs on close.
	single bool
	onDone func()
}

// New builds the wizard over the given steps (in order) with prior state.
func New(env *Env, state setup.State, steps ...Step) *Model {
	m := &Model{env: env, steps: steps, state: state, skipped: map[string]bool{}}
	m.idx = m.firstApplicable(0)
	if m.idx >= len(m.steps) {
		m.finished = true
	}
	return m
}

// catchUpIDs are steps a finished setup can still owe: ones added after
// people first ran setup (M31), and the team, which waits on a workspace
// that may only appear later. Each is offered once, while it applies and
// was never applied or skipped (F-085).
var catchUpIDs = map[string]bool{"project": true, "you": true, "instructions": true, "team": true}

// CatchUpSteps returns the steps a finished setup still owes the user,
// with the closing step; nil when there are none.
func CatchUpSteps(env *Env, version string, st setup.State) []Step {
	var out []Step
	var done Step
	for _, s := range DefaultSteps(env, version) {
		if s.ID() == "done" {
			done = s
			continue
		}
		if catchUpIDs[s.ID()] && s.Applies() && !st.Settled(s.ID()) {
			out = append(out, s)
		}
	}
	if len(out) == 0 {
		return nil
	}
	return append(out, done)
}

// DefaultSteps is the core P2 step list; later phases pass more to New.
func DefaultSteps(env *Env, version string) []Step {
	return []Step{
		&welcomeStep{env: env, version: version},
		&workspaceStep{env: env},
		&projectStep{env: env},
		&identityStep{env: env},
		&styleStep{env: env},
		&instructionsStep{env: env},
		&conventionsStep{env: env},
		&cliStep{env: env},
		&teamStep{env: env},
		&integrationsStep{env: env},
		&doneStep{env: env},
	}
}

// NeedsRelaunch reports that the wizard changed launch-time state and the
// program should be restarted rather than continue in this process.
func (m *Model) NeedsRelaunch() bool { return m.relaunch }

func (m *Model) Finished() bool { return m.finished }

func (m *Model) Resize(w, h int) { m.width, m.height = w, h }

// TakeCmd drains a command queued by HandleKey (the shell calls it).
func (m *Model) TakeCmd() tea.Cmd {
	c := m.pending
	m.pending = nil
	return c
}

func (m *Model) Init() tea.Cmd {
	if m.finished {
		return nil
	}
	return tea.Batch(m.cur().Enter(), m.tick())
}

func (m *Model) cur() Step { return m.steps[m.idx] }

func (m *Model) firstApplicable(from int) int {
	for i := from; i < len(m.steps); i++ {
		if m.steps[i].Applies() {
			return i
		}
	}
	return len(m.steps)
}

func (m *Model) prevApplicable(from int) int {
	for i := from; i >= 0; i-- {
		if m.steps[i].Applies() {
			return i
		}
	}
	return -1
}

// tick arms the one animation clock; none runs under reduced motion.
func (m *Model) tick() tea.Cmd {
	if m.clockArmed || !theme.Motion || m.finished {
		return nil
	}
	m.clockArmed = true
	return tea.Tick(tickInterval, func(time.Time) tea.Msg { return tickMsg{} })
}

func (m *Model) Update(msg tea.Msg) tea.Cmd {
	if m.finished {
		return nil
	}
	if _, ok := msg.(tickMsg); ok {
		m.clockArmed = false
		m.frame++
		return m.tick()
	}
	return m.cur().Update(msg)
}

func (m *Model) HandleKey(key string) bool {
	if m.finished {
		return false
	}
	// A step running something it cannot cancel (an install) owns the
	// screen: navigating away would orphan the result.
	busy := false
	if b, ok := m.cur().(interface{ Busy() bool }); ok {
		busy = b.Busy()
	}
	if busy {
		return true
	}
	// Option+← (alt+left) goes back too where no text is being typed:
	// ctrl+b is tmux's prefix (F-101).
	typing := false
	if c, ok := m.cur().(interface{ CapturesText() bool }); ok {
		typing = c.CapturesText()
	}
	if key == "alt+left" && !typing {
		key = "ctrl+b"
	}
	switch key {
	case "ctrl+b":
		if !m.single {
			m.act(Back)
		}
		return true
	case "ctrl+x":
		if m.single { // one step: skipping setup is skipping this step
			m.act(Skip)
			return true
		}
		// Skipping setup skips every step still ahead, so a catch-up
		// run never comes back for them (F-085). Only steps that could run
		// now count: one waiting on a workspace (project, team) stays owed
		// and is offered once there is one.
		for i := m.idx; i < len(m.steps); i++ {
			if id := m.steps[i].ID(); id != "done" && !m.state.Has(id) && m.steps[i].Applies() {
				m.state.MarkSkipped(id, m.env.now())
			}
		}
		m.finish()
		return true
	}
	step := m.cur()
	m.act(step.HandleKey(key))
	// A step may start async work from a key (the CLI install); like the
	// gates themselves it queues the command for the shell to drain.
	if t, ok := step.(interface{ TakeCmd() tea.Cmd }); ok {
		m.pending = tea.Batch(m.pending, t.TakeCmd())
	}
	return true
}

// go_ applies a step's Action.
func (m *Model) act(a Action) {
	switch a {
	case Next:
		if err := m.cur().Apply(); err != nil {
			return // the step shows its own error and stays open
		}
		m.markApplied(m.cur().ID())
		m.env.noteSkipped(m.cur().Title(), false)
		m.advance(false)
	case Skip:
		m.advance(true)
	case Back:
		if p := m.prevApplicable(m.idx - 1); p >= 0 {
			m.idx = p
			m.enter()
		}
	}
}

func (m *Model) advance(skip bool) {
	// F-108: leaving a re-run of a step that was already done keeps its
	// record — esc means "leave it as it was", not "skip it".
	if skip && m.single && m.state.Has(m.cur().ID()) {
		m.close()
		return
	}
	if skip {
		m.skipped[m.cur().ID()] = true
		m.env.noteSkipped(m.cur().Title(), true)
		m.state.MarkSkipped(m.cur().ID(), m.env.now())
		m.persist()
	}
	if m.single {
		m.close()
		return
	}
	n := m.firstApplicable(m.idx + 1)
	if n >= len(m.steps) {
		m.finish()
		return
	}
	m.idx = n
	m.enter()
}

func (m *Model) enter() {
	m.enterFrame = m.frame
	delete(m.skipped, m.cur().ID())
	m.pending = tea.Batch(m.pending, m.cur().Enter(), m.tick())
}

func (m *Model) markApplied(id string) {
	m.state.Mark(id, m.env.now())
	m.persist()
}

func (m *Model) persist() {
	if m.env.Persist != nil {
		_ = m.env.Persist(m.env.Root, m.state)
	}
}

// finish ends the run. Changes that launch-time services read force a
// relaunch (no hot-swapping); otherwise the shell is released.
func (m *Model) finish() {
	m.state.Finished = true
	m.persist()
	if m.env.Changed {
		if m.env.TourRequested && m.env.QueueTour != nil {
			_ = m.env.QueueTour() // best effort: the tour is a courtesy
		}
		m.relaunch = true
		m.pending = tea.Quit
		return
	}
	if m.env.TourRequested && m.env.StartTour != nil {
		m.env.StartTour()
	}
	m.finished = true
}

// close ends a single-step run: the step is already recorded; a change
// launch-time services read relaunches, anything else hands back.
func (m *Model) close() {
	if m.env.Changed {
		m.relaunch = true
		m.pending = tea.Quit
		return
	}
	m.finished = true
	if m.onDone != nil {
		m.onDone()
	}
}

// View renders header (brand + stepper), the active step, and the key line.
func (m *Model) View() string {
	if m.finished || m.idx >= len(m.steps) {
		return ""
	}
	w := m.width - 8
	if w > 76 {
		w = 76
	}
	if w < 30 {
		w = 30
	}
	f := m.frame - m.enterFrame
	body := m.cur().View(w, f)

	// One card (F-093): the stepper heads it, the step fills it; the
	// keys live on the shell status row (Hints).
	lines := []string{kit.Stepper(m.stepItems(), w, m.frame), ""}
	title := "dhi setup"
	if m.single {
		// One step: no stepper, the task's own title and why head it.
		id := m.cur().ID()
		c := checklistCopy[id]
		lines = []string{theme.AccentText().Render(c.title) +
			theme.TextMuted().Render("  "+c.why+" · about "+c.est), ""}
		title = "set up"
	}
	lines = append(lines, body...)
	card := kit.NewPanel(title, true)
	card.Boxed = true
	card.Width = w + 4
	card.SetContent(append(lines, "")...)
	if len(lines)+3 > m.height { // a short terminal: no frame, no clip
		return kit.Center(strings.Join(lines, "\n"), m.width, m.height)
	}
	return kit.Center(card.View(), m.width, m.height)
}

// Hints implements surfaces.HintProvider (ADR-0040): the wizard's keys
// on the shell status row, most important first.
func (m *Model) Hints() (string, []string) {
	return "", m.keys()
}

func (m *Model) keys() []string {
	var parts []string
	if m.finished || m.idx >= len(m.steps) {
		return nil
	}
	if k, ok := m.cur().(interface{ Keys() []string }); ok {
		parts = k.Keys() // the step's own keys first
	}
	if m.single {
		if m.state.Has(m.cur().ID()) { // a re-run (F-108)
			return append(parts, "enter save", "esc leave as is")
		}
		return append(parts, "enter save", "esc skip")
	}
	parts = append(parts, "enter continue", "esc skip step")
	if m.prevApplicable(m.idx-1) >= 0 {
		parts = append(parts, "^b/"+kit.AltLabel("alt+←")+" back")
	}
	return append(parts, "^x skip setup")
}

func (m *Model) stepItems() []kit.StepItem {
	var items []kit.StepItem
	for i, s := range m.steps {
		// A step that no longer applies (the workspace now exists) stays
		// visible if it was done or skipped on the way here.
		doneBefore := i < m.idx && (m.state.Has(s.ID()) || m.skipped[s.ID()])
		if !s.Applies() && i != m.idx && !doneBefore {
			continue
		}
		st := kit.StepPending
		switch {
		case i == m.idx:
			st = kit.StepActive
		case m.skipped[s.ID()]:
			st = kit.StepSkipped
		case i < m.idx:
			st = kit.StepDone
		}
		items = append(items, kit.StepItem{Label: s.Title(), State: st})
	}
	return items
}
