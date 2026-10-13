// Package settings is DHI's Settings view: keyboard-navigable sections
// over the typed config schema (CONFIG) and the agent roster (AGENTS —
// F-018 full CRUD) with live application (theme swap), automatic
// persistence, and roster changes that go live through the reload seam.
package settings

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"

	"github.com/drjzlyan/dhi/internal/agentkit/bus"
	"github.com/drjzlyan/dhi/internal/agentkit/forge"
	"github.com/drjzlyan/dhi/internal/agentkit/lessons"
	"github.com/drjzlyan/dhi/internal/agentkit/library"
	"github.com/drjzlyan/dhi/internal/agentkit/manifest"
	"github.com/drjzlyan/dhi/internal/agentkit/memory"
	"github.com/drjzlyan/dhi/internal/agentkit/org"
	"github.com/drjzlyan/dhi/internal/agentkit/pack"
	"github.com/drjzlyan/dhi/internal/agentkit/principal"
	"github.com/drjzlyan/dhi/internal/agentkit/registry"
	"github.com/drjzlyan/dhi/internal/agentkit/setprop"
	"github.com/drjzlyan/dhi/internal/agentkit/tools"
	"github.com/drjzlyan/dhi/internal/autopilot"
	"github.com/drjzlyan/dhi/internal/conventions"
	"github.com/drjzlyan/dhi/internal/gitcore"
	"github.com/drjzlyan/dhi/internal/settings"
	"github.com/drjzlyan/dhi/internal/tasks"
	"github.com/drjzlyan/dhi/internal/tui/kit"
	"github.com/drjzlyan/dhi/internal/tui/surfaces"
	"github.com/drjzlyan/dhi/internal/tui/theme"
	"github.com/drjzlyan/dhi/internal/workspace"
)

// Model is the Settings surface.
type Model struct {
	cfg      settings.Config
	savePath string
	d        Deps

	sec      sectionID
	cursor   int   // CONFIG rows
	cfgRowAt []int // CONFIG: visible line → setting row (-1 = header), for clicks
	agentCur int   // AGENTS roster rows
	// PROPOSALS (F-069/F-070): selected row.
	propCur int
	// BRAIN (F-068): lesson cursor, filter text, and whether / is typing.
	brainCur    int
	brainFilter string
	brainTyping bool
	libCur      int // LIBRARY listing rows
	teamCur     int // TEAMS rows
	packCur     int // PACKS rows
	stdCur      int // STANDARDS rows
	wfCur       int // WORKFLOWS rows
	convCur     int // CONVENTIONS rows (F-075)
	autoCur     int // AUTOPILOTS rows
	mktCur      int // MARKETPLACE rows
	mktQuery    string
	mktEdit     bool // search input focused
	mktBusy     bool

	detected map[string]string // runtime name → version ("" = not installed)
	lib      *library.Store    // lazy snapshot of the behaviour library

	form    agentForm // legacy agent modal (create/edit/source) — P5 folds into kit dialogs
	dlg     *kit.Modal
	dform   *kit.Form
	learned *conventions.Inference // pending learned conventions (F-082)
	ident   *gitcore.Identity      // cached git identity (F-083); nil = not read yet
	identOK bool
	wantID  gitcore.Identity // the identity awaiting confirmation
	// valueHint explains the value a CONFIG editor dialog wants (F-084).
	valueHint string
	// cfgKeys are the settings keys CONFIG rendered, in order (F-084:
	// the coverage guard reads them; rows show plain labels).
	cfgKeys []string
	dkind   dialogKind
	dtarget string

	flash  string
	events chan settingsEvent
	width  int
	height int
	now    func() time.Time // injectable clock (deterministic due math)
}

var _ surfaces.Surface = (*Model)(nil)

// settingsEvent carries one async crew/import outcome to the loop.
type settingsEvent struct {
	msg   string
	err   string
	title string   // when lines is set: a display dialog title
	lines []string // when set: open a display dialog with these lines
}

type sectionID uint8

const (
	secConfig sectionID = iota
	secProject
	secAgents
	secBrain
	secProposals
	secLibrary
	secTeams
	secPacks
	secStandards
	secConventions
	secWorkflows
	secAutopilots
	secMarketplace
	secCount
)

func (s sectionID) label() string {
	switch s {
	case secConfig:
		return "CONFIG"
	case secProject:
		return "PROJECT"
	case secAgents:
		return "AGENTS"
	case secBrain:
		return "BRAIN"
	case secProposals:
		return "PROPOSALS"
	case secLibrary:
		return "LIBRARY"
	case secTeams:
		return "TEAMS"
	case secPacks:
		return "PACKS"
	case secStandards:
		return "STANDARDS"
	case secConventions:
		return "CONVENTIONS"
	case secWorkflows:
		return "WORKFLOWS"
	case secAutopilots:
		return "AUTOPILOTS"
	case secMarketplace:
		return "MARKETPLACE"
	default:
		return "CONFIG"
	}
}

// TurnHandler is the narrow runtime seam autopilot run-now dispatches
// through (satisfied by *runtime.Runtime).
type TurnHandler interface {
	Handle(ctx context.Context, msg bus.Message)
}

// runner executes one already-vetted argv under the sandbox
// (dhitools.CommandRunner). Nil disables the LIBRARY skill-run action.
type runner interface {
	Run(ctx context.Context, dir string, argv []string, allowNetwork bool) (string, error)
}

// Deps wires workspace-scoped services. Zero fields degrade the managed
// sections to visible "unavailable" rows rather than errors (F-011).
type Deps struct {
	WS   *workspace.Workspace
	Org  *org.Org
	CLIs []string // registered runtime names (form toggle options)
	// Detect probes the registered CLIs on PATH (name → version, ""
	// = not installed). The AGENTS runtime picker is detection-driven
	// (F-027 P2): detected first, undetected last; nil = all dimmed.
	Detect func() map[string]string
	// Library is the behaviour library (F-027): roles + skills for
	// the LIBRARY section and the agent form pickers. Nil degrades to
	// a named empty section.
	Library *library.Store
	// Identity / SetIdentity / SuggestIdentity back the git identity row
	// (F-083); nil Identity shows it as unavailable.
	Identity        gitcore.IdentityFunc
	SetIdentity     func(ctx context.Context, id gitcore.Identity) error
	SuggestIdentity func(ctx context.Context) []gitcore.Suggestion
	// LearnConventions infers the team's commit and PR style from git
	// history and the repo's PR template (F-082); nil hides g.
	LearnConventions func() conventions.Inference
	// Reload swaps the live runtime roster after a successful crew
	// write; nil means changes apply on next launch (named in flash).
	Reload func() error
	// Management-section seams (F-023). Autopilots must be the SAME
	// store instance the workspace uses so schedules and marks share
	// one view of the cards.
	Autopilots *autopilot.Store
	Bus        *bus.Bus
	Runtime    TurnHandler
	Tasks      *tasks.Store
	// Registry is the cached signed pack index (F-034 part B). Nil
	// degrades the MARKETPLACE section to a named unavailable row.
	Registry *registry.Registry
	// RunScript executes a local skill's declared script (sandboxed,
	// network denied). Nil disables the LIBRARY run action.
	RunScript runner
	// SetProps are the agents' settings proposals (F-070); applying one
	// writes a single key into WorkspaceConfig or UserConfig. Nil hides
	// them.
	SetProps        *setprop.Store
	UserConfig      string
	WorkspaceConfig string
	// Forge holds agent-made tools (F-069): PROPOSALS approves, rejects
	// and archives them. Nil shows only settings proposals.
	Forge *forge.Store
	// Lessons are what the crew has learned (F-068): the BRAIN section.
	// Nil renders a named unavailable section.
	Lessons *lessons.Store
	// Memory backs BRAIN's memory view (F-078); nil = unavailable.
	Memory *memory.Store
	// ShareLesson contributes a lesson to the workspace KB (review
	// policy); it returns the KB status. Nil disables s in BRAIN.
	ShareLesson func(lessons.Lesson) (string, error)
	// Grants are the remembered "allow always" decisions (F-067); the
	// AGENTS section lists them and r revokes the selected agent's. Nil
	// hides the list.
	Grants GrantStore
}

// GrantStore is the approvals queue's remembered-grant surface.
type GrantStore interface {
	Grants() []tools.Grant
	Revoke(agent, scope string) error
}

// New wires the surface to a loaded config, its persistence target,
// and (optionally) the workspace services behind agent management.
func New(cfg settings.Config, savePath string, d Deps) *Model {
	return &Model{cfg: cfg, savePath: savePath, d: d,
		events: make(chan settingsEvent, 4),
		now:    time.Now,
	}
}

func (m *Model) Meta() surfaces.Meta { return surfaces.Meta{ID: "settings", Title: "Settings"} }

// StatusContext feeds the app statusline (F-025).
func (m *Model) StatusContext() (string, string) {
	zone := strings.ToLower(m.sec.label())
	if m.form.open || m.dlg != nil {
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
		out = append(out, surfaces.Command{Group: "Settings", Title: "Go to " + s.label(), Hint: "[ ]", Run: func() tea.Cmd {
			m.sec = s
			return nil
		}})
	}
	return out
}

// CapturesInput implements surfaces.InputCapturer: forms and dialogs
// take plain keys as text.
func (m *Model) CapturesInput() bool { return m.form.open || m.dlg != nil }

// Init starts the async-outcome listener (imports/clones can take
// seconds; the form stays busy until the event lands).
func (m *Model) Init() tea.Cmd { return m.listen() }

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

func (m *Model) Update(msg tea.Msg) tea.Cmd {
	if ev, ok := msg.(settingsEvent); ok {
		m.form.busy = false
		m.mktBusy = false
		if m.form.kind == formSource {
			m.form.open = false
		}
		if m.dlg != nil && (m.dkind == dlgPackInstall || m.dkind == dlgRegistrySource) {
			m.closeDialog()
		}
		if ev.err != "" {
			m.flash = "failed: " + ev.err
		} else {
			m.flash = ev.msg
		}
		if len(ev.lines) > 0 {
			m.openDisplayDialog(ev.title, ev.lines)
		}
		rows, _ := m.agentRows()
		clampAgentCursor(&m.agentCur, len(rows))
		return m.listen()
	}
	return nil
}

func (m *Model) Resize(w, h int) {
	m.width, m.height = w, h
}

func (m *Model) HandleKey(key string) bool {
	if m.dlg != nil {
		m.dialogKey(key)
		return true // the modal swallows everything (focus trap)
	}
	if m.form.open {
		return m.formKey(key)
	}
	switch key {
	case "[":
		m.sec = (m.sec - 1 + secCount) % secCount
		return true
	case "]":
		m.sec = (m.sec + 1) % secCount
		return true
	case "ctrl+s":
		m.applyAndPersist()
		return true
	}
	switch m.sec {
	case secAgents:
		return m.agentsKey(key)
	case secBrain:
		return m.brainKey(key)
	case secProposals:
		return m.proposalsKey(key)
	case secLibrary:
		return m.libraryKey(key)
	case secTeams:
		return m.teamsKey(key)
	case secProject:
		return m.projectKey(key)
	case secPacks:
		return m.packsKey(key)
	case secStandards:
		return m.standardsKey(key)
	case secConventions:
		return m.conventionsKey(key)
	case secWorkflows:
		return m.workflowsKey(key)
	case secAutopilots:
		return m.autopilotsKey(key)
	case secMarketplace:
		return m.marketplaceKey(key)
	default:
		return m.configKey(key)
	}
}

// Wheel routes wheel events to the focused section (F-026 P2): three
// rows per tick through the same key path as j/k; open dialogs and
// forms never see the wheel.
func (m *Model) Wheel(dy int) bool {
	if dy == 0 || m.dlg != nil || m.form.open {
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

func (m *Model) configKey(key string) bool {
	switch key {
	case "j", "down":
		if m.cursor < rowCount-1 {
			m.cursor++
			m.flash = ""
		}
		return true
	case "k", "up":
		if m.cursor > 0 {
			m.cursor--
			m.flash = ""
		}
		return true
	case "enter", "right", "l":
		if m.cursor == rowIdentity {
			m.openIdentityForm()
			return true
		}
		m.cycle(1)
		return true
	case "left", "h":
		if m.cursor == rowIdentity {
			return true
		}
		m.cycle(-1)
		return true
	case "e":
		if m.cursor == rowIdentity {
			m.openIdentityForm()
			return true
		}
		m.openValueEditor()
		return true
	}
	return false
}

// CONFIG rows, in display order (j/k walks them by index).
const (
	// You (F-081, F-083): your git identity, who the crew works for and
	// how you write and read.
	rowIdentity = iota
	rowRole
	rowTone
	rowLength
	rowEmoji
	rowLanguage
	rowOrder
	rowFormat
	rowDetail
	// Appearance, editor and terminal.
	rowTheme
	rowReducedMotion
	rowTabWidth
	rowLineNumbers
	rowScrollback
	rowLanguages // editor.languages on/off (F-084)
	// Seven capability-scope rows (F-030 P2), then the sandbox.
	rowScopesBase
	_
	_
	_
	_
	_
	_
	rowSandbox
	// The personal worktree location (F-053), then the conventions rows
	// (F-042), which save to their own tracked file.
	rowWorktrees
	rowBranchTask
	rowCommitFormat
	rowCommitCoAuthor
	rowCopyright
	rowLicense
	rowMaxSubject
	// Agents & budgets (F-066, F-067).
	rowEngine
	rowResume
	rowAutonomy
	rowMaxTurns
	rowMaxIter
	rowStall
	rowReflect
	rowReflectModel
	rowTracePrompts
	rowTaskUSD
	rowTaskTokens
	rowAgentDayUSD
	rowWorkDayUSD
	// Memory (F-078).
	rowMemPrompt
	rowMemSession
	rowMemJournal
	rowMemLessons
	rowMemPinned
	rowMemDecay
	// Key remaps (F-058).
	rowKeys
	rowCount
)

// Preset cycles for the agents & budgets rows (0 = the CLI default / no
// limit); e types any other value (F-084).
var (
	maxTurnPresets  = []int{0, 10, 20, 30, 50, 100}
	maxIterPresets  = []int{1, 2, 3, 5, 8}
	stallPresets    = []int{0, 300, 600, 900, 1800, 3600}
	taskUSDPresets  = []float64{0, 0.5, 1, 2, 5, 10}
	taskTokPresets  = []int{0, 250_000, 500_000, 1_000_000, 2_000_000}
	agentDayPresets = []float64{0, 2, 5, 10, 20, 50}
	workDayPresets  = []float64{0, 5, 10, 25, 50, 100}
	// F-078 memory presets.
	memPromptPresets  = []int{12_000, 24_000, 48_000, 96_000}
	memSessionPresets = []int{80_000, 160_000, 300_000, 600_000}
	memJournalPresets = []int{200, 400, 1000, 2500}
	memLessonPresets  = []int{100, 200, 500, 1000}
	memPinnedPresets  = []int{0, 5, 10, 20}
	memDecayPresets   = []int{14, 30, 60, 90}
)

func cycleNum[T int | float64](cur T, opts []T, dir int) T {
	for i, o := range opts {
		if o == cur {
			return opts[(i+dir+len(opts))%len(opts)]
		}
	}
	return opts[0]
}

// worktreePresets is the cycle for worktrees.root; e types any other
// path. "" keeps worktrees inside .dhi/.
var worktreePresets = []string{"", "../.dhi-worktrees", "~/dhi-worktrees"}

// branchPresets is the cycle for conventions.branch.task; e types any
// other pattern.
var branchPresets = []string{"task/{slug}", "feature/{slug}", "{user}/{slug}", "{user}/{date}-{slug}"}

func cycleString(cur string, opts []string, dir int) string {
	for i, o := range opts {
		if o == cur {
			return opts[(i+dir+len(opts))%len(opts)]
		}
	}
	return opts[0]
}

// languagePresets is the cycle for profile.writing.language.
var languagePresets = []string{"English", "Spanish", "German", "French", "Portuguese", "Hindi", "Japanese", "Chinese"}

func orStr(v, def string) string {
	if v == "" {
		return def
	}
	return v
}

// scopeRowNames orders the settings scope rows.
var scopeRowNames = []string{"read", "write", "exec", "network", "git", "push", "admin"}

// scopeEffects is the cycle order for a scope row.
var scopeEffects = []string{"auto", "ask", "deny"}

// cycle applies one modification step and persists + applies live.
func (m *Model) cycle(dir int) {
	pr := &m.cfg.Profile
	switch m.cursor {
	case rowRole:
		pr.Role = cycleString(orStr(pr.Role, principal.Roles[0]), principal.Roles, dir)
	case rowTone:
		pr.Writing.Tone = cycleString(orStr(pr.Writing.Tone, principal.Tones[0]), principal.Tones, dir)
	case rowLength:
		pr.Writing.Length = cycleString(orStr(pr.Writing.Length, principal.Lengths[1]), principal.Lengths, dir)
	case rowEmoji:
		pr.Writing.Emoji = !pr.Writing.Emoji
	case rowLanguage:
		pr.Writing.Language = cycleString(orStr(pr.Writing.Language, principal.Language), languagePresets, dir)
	case rowOrder:
		pr.Reading.Order = cycleString(orStr(pr.Reading.Order, principal.Orders[0]), principal.Orders, dir)
	case rowFormat:
		pr.Reading.Format = cycleString(orStr(pr.Reading.Format, principal.Formats[0]), principal.Formats, dir)
	case rowDetail:
		pr.Reading.Detail = cycleString(orStr(pr.Reading.Detail, principal.Details[0]), principal.Details, dir)
	case rowTheme:
		names := settings.ThemeNames()
		for i, n := range names {
			if n == m.cfg.Theme {
				m.cfg.Theme = names[(i+dir+len(names))%len(names)]
				m.cfg.ThemeSet = true // a choice: saved, and no longer follows the terminal
				break
			}
		}
	case rowReducedMotion:
		m.cfg.ReducedMotion = !m.cfg.ReducedMotion
	case rowTabWidth:
		widths := []int{2, 4, 8}
		for i, w := range widths {
			if w == m.cfg.Editor.TabWidth {
				m.cfg.Editor.TabWidth = widths[(i+dir+len(widths))%len(widths)]
				break
			}
		}
	case rowLineNumbers:
		m.cfg.Editor.LineNumbers = !m.cfg.Editor.LineNumbers
	case rowScrollback:
		step := 500 * dir
		if m.cfg.Terminal.Scrollback+step >= 100 {
			m.cfg.Terminal.Scrollback += step
		}
	case rowWorktrees:
		m.cfg.Worktrees.Root = cycleString(m.cfg.Worktrees.Root, worktreePresets, dir)
	case rowBranchTask:
		m.cfg.Conventions.Branch.Task = cycleString(m.cfg.Conventions.Branch.Task, branchPresets, dir)
	case rowCommitFormat:
		m.cfg.Conventions.Commit.Format = cycleString(m.cfg.Conventions.Commit.Format,
			[]string{conventions.FormatFree, conventions.FormatConventional, conventions.FormatTicket}, dir)
	case rowCommitCoAuthor:
		cm := &m.cfg.Conventions.Commit
		if !cm.CoAuthorEnabled && strings.TrimSpace(cm.CoAuthor) == "" {
			m.openValueEditor() // nothing to switch on yet: ask for the name first
			return
		}
		cm.CoAuthorEnabled = !cm.CoAuthorEnabled
	case rowCopyright:
		if !m.cfg.Conventions.Copyright.Enabled && strings.TrimSpace(m.cfg.Conventions.Copyright.Holder) == "" {
			m.openValueEditor()
			return
		}
		m.cfg.Conventions.Copyright.Enabled = !m.cfg.Conventions.Copyright.Enabled
	case rowLanguages, rowKeys, rowReflectModel, rowLicense:
		m.openValueEditor()
		return
	case rowSandbox:
		m.cfg.Security.Sandbox = cycleString(m.cfg.Security.Sandbox, []string{settings.SandboxAuto, settings.SandboxOff}, dir)
	case rowMaxSubject:
		m.cfg.Conventions.Commit.MaxSubject = cycleNum(m.cfg.Conventions.Commit.MaxSubject, []int{50, 72, 100, 120}, dir)
	case rowEngine:
		m.cfg.Engine = cycleString(m.cfg.Engine, m.engineChoices(), dir)
	case rowResume:
		m.cfg.Agents.Resume = !m.cfg.Agents.Resume
	case rowAutonomy:
		m.cfg.Agents.Autonomy = cycleString(m.cfg.Agents.Autonomy,
			[]string{settings.AutonomyEnvelope, settings.AutonomyAsk}, dir)
	case rowMaxTurns:
		m.cfg.Agents.MaxTurns = cycleNum(m.cfg.Agents.MaxTurns, maxTurnPresets, dir)
	case rowMaxIter:
		m.cfg.Agents.MaxIterations = cycleNum(m.cfg.Agents.MaxIterations, maxIterPresets, dir)
	case rowStall:
		m.cfg.Agents.StallSeconds = cycleNum(m.cfg.Agents.StallSeconds, stallPresets, dir)
	case rowReflect:
		m.cfg.Agents.Reflect = !m.cfg.Agents.Reflect
	case rowTracePrompts:
		m.cfg.Agents.TracePrompts = !m.cfg.Agents.TracePrompts
	case rowTaskUSD:
		m.cfg.Budgets.TaskUSD = cycleNum(m.cfg.Budgets.TaskUSD, taskUSDPresets, dir)
	case rowTaskTokens:
		m.cfg.Budgets.TaskTokens = cycleNum(m.cfg.Budgets.TaskTokens, taskTokPresets, dir)
	case rowAgentDayUSD:
		m.cfg.Budgets.AgentDayUSD = cycleNum(m.cfg.Budgets.AgentDayUSD, agentDayPresets, dir)
	case rowWorkDayUSD:
		m.cfg.Budgets.WorkspaceDayUSD = cycleNum(m.cfg.Budgets.WorkspaceDayUSD, workDayPresets, dir)
	case rowMemPrompt:
		m.cfg.Memory.PromptTokens = cycleNum(m.cfg.Memory.PromptTokens, memPromptPresets, dir)
	case rowMemSession:
		m.cfg.Memory.SessionTokens = cycleNum(m.cfg.Memory.SessionTokens, memSessionPresets, dir)
	case rowMemJournal:
		m.cfg.Memory.JournalMax = cycleNum(m.cfg.Memory.JournalMax, memJournalPresets, dir)
	case rowMemLessons:
		m.cfg.Memory.LessonsMax = cycleNum(m.cfg.Memory.LessonsMax, memLessonPresets, dir)
	case rowMemPinned:
		m.cfg.Memory.PinnedMax = cycleNum(m.cfg.Memory.PinnedMax, memPinnedPresets, dir)
	case rowMemDecay:
		m.cfg.Memory.DecayDays = cycleNum(m.cfg.Memory.DecayDays, memDecayPresets, dir)
	default:
		if m.cursor >= rowScopesBase && m.cursor < rowScopesBase+7 {
			name := scopeRowNames[m.cursor-rowScopesBase]
			if m.cfg.Scopes == nil {
				m.cfg.Scopes = map[string]string{}
			}
			cur := m.cfg.Scopes[name]
			idx := -1
			for i, e := range scopeEffects {
				if e == cur {
					idx = i
					break
				}
			}
			m.cfg.Scopes[name] = scopeEffects[(idx+dir+2*len(scopeEffects))%len(scopeEffects)]
		}
	}
	m.applyAndPersist()
}

func (m *Model) applyAndPersist() {
	m.cfg.Apply()
	if m.cursor >= rowBranchTask && m.cursor <= rowMaxSubject {
		// One row, one key, into the same file the CONVENTIONS section
		// writes (F-075): never the whole merged table.
		c := m.cfg.Conventions
		if m.cursor == rowCommitCoAuthor {
			// The co-author is personal ("Name <email>"): it goes to the
			// user's conventions file, never the tracked team file.
			if m.savePath == "" {
				m.flash = "(no config path — change is session-only)"
				return
			}
			if m.patchConventionsAt(settings.ConventionsPath(m.savePath), c,
				map[string]any{"commit.co_author_enabled": c.Commit.CoAuthorEnabled, "commit.co_author": c.Commit.CoAuthor}, nil, "saved") {
				m.flash += " — agents use it from their next action"
			}
			return
		}
		key := map[int]map[string]any{
			rowBranchTask:   {"branch.task": c.Branch.Task},
			rowCommitFormat: {"commit.format": c.Commit.Format},
			rowCopyright:    {"copyright.enabled": c.Copyright.Enabled, "copyright.holder": c.Copyright.Holder},
			rowLicense:      {"copyright.license": c.Copyright.License},
			rowMaxSubject:   {"commit.max_subject": c.Commit.MaxSubject},
		}[m.cursor]
		if m.patchConventions(c, key, nil, "saved") {
			m.flash += " — agents use it from their next action"
		}
		return
	}
	if m.savePath == "" {
		m.flash = "(no config path — change is session-only)"
		return
	}
	if err := m.cfg.Save(m.savePath); err != nil {
		m.flash = "save failed: " + err.Error()
		return
	}
	// Conventions are a separate, tracked file shared with the team and
	// are re-read while DHI runs (F-052), so say so.
	switch {
	case m.cursor == rowSandbox:
		m.flash = "saved — the sandbox changes after a restart"
		return
	case m.cursor == rowEngine:
		m.flash = "saved — employees without their own CLI use it from their next turn"
		return
	case m.cursor >= rowResume && m.cursor < rowKeys, m.cursor < rowTheme:
		m.flash = "saved (live — agents use it from their next spawn)"
		return
	case m.cursor == rowKeys, m.cursor == rowLanguages:
		m.flash = "saved — applies after a restart"
		return
	}
	if m.cursor == rowWorktrees {
		m.flash = "saved — new worktrees use it after a restart; existing ones stay where they are"
		return
	}
	m.flash = "saved"
}

// ---- AGENTS section (F-018) ----

// agentRow is one roster snapshot row for rendering and selection.
type agentRow struct {
	id       string
	model    string
	runtime  string
	tools    int
	archived bool
}

// agentRows snapshots the roster from disk (the single identity store);
// a read failure degrades to nil with the error in m.flash — never a
// fake empty roster.
func (m *Model) agentRows() ([]agentRow, error) {
	if m.d.WS == nil || m.d.Org == nil {
		return nil, nil // section renders its unavailable hint
	}
	roster, err := org.LoadRoster(m.d.WS)
	if err != nil {
		return nil, err
	}
	archived := map[string]bool{}
	for _, id := range m.d.Org.Archived(m.d.WS) {
		archived[id] = true
	}
	rows := make([]agentRow, 0, len(roster)+len(archived))
	for _, a := range roster {
		rows = append(rows, agentRow{
			id: a.ID, model: a.Model, runtime: a.Runtime,
			tools: len(a.Tools), archived: archived[a.ID],
		})
	}
	for _, id := range m.d.Org.Archived(m.d.WS) {
		if !archived[id] {
			continue
		}
		rows = append(rows, agentRow{id: id, archived: true})
	}
	return rows, nil
}

// agentsKey handles AGENTS navigation and crew CRUD (F-018 §Part B).
func (m *Model) agentsKey(key string) bool {
	rows, _ := m.agentRows()
	switch key {
	case "j", "down":
		if m.agentCur < len(rows)-1 {
			m.agentCur++
			m.flash = ""
		}
		return true
	case "k", "up":
		if m.agentCur > 0 {
			m.agentCur--
			m.flash = ""
		}
		return true
	case "n":
		m.form = newAgentForm(m.d.CLIs, m.detectedFirst(m.d.CLIs))
		return true
	case "g":
		m.form = newSourceForm() // F-019: add from source (one flow)
		return true
	case "e":
		if m.agentCur < len(rows) && m.d.WS != nil {
			// Prefill from the full manifest, not the summary row.
			if roster, err := org.LoadRoster(m.d.WS); err == nil {
				for _, a := range roster {
					if a.ID == rows[m.agentCur].id {
						m.form = m.editAgentForm(a, m.d.CLIs)
						return true
					}
				}
			}
		}
	case "enter", "v":
		if m.agentCur < len(rows) && m.d.WS != nil {
			m.openProfile(rows[m.agentCur].id)
			return true
		}
	case "a":
		if m.agentCur < len(rows) && m.d.Org != nil {
			r := rows[m.agentCur]
			var err error
			if r.archived {
				err = m.d.Org.RestoreAgent(m.d.WS, r.id)
			} else {
				err = m.d.Org.ArchiveAgent(m.d.WS, r.id)
			}
			m.afterCrewWrite(err, r.id+" archived", r.id+" restored")
			return true
		}
	case "r":
		if m.agentCur < len(rows) {
			m.revokeGrants(rows[m.agentCur].id)
			return true
		}
	case "x":
		if m.agentCur < len(rows) && m.d.Org != nil {
			r := rows[m.agentCur]
			if r.archived {
				err := m.d.Org.DeleteAgent(m.d.WS, r.id)
				m.afterCrewWrite(err, r.id+" deleted", "")
				return true
			}
			// Active agents archive first (the soft path is the
			// default); deleting an active manifest needs the archived
			// route so a mistake is recoverable.
			err := m.d.Org.ArchiveAgent(m.d.WS, r.id)
			m.afterCrewWrite(err, r.id+" archived (x again to delete)", "")
			return true
		}
	}
	return false
}

// afterCrewWrite lands the named flash, drives the reload seam, and
// names every failure (F-011: visible, never silent).
func (m *Model) afterCrewWrite(err error, okMsg, altMsg string) {
	if err != nil {
		m.flash = "failed: " + err.Error()
		return
	}
	if m.d.Reload != nil {
		if rerr := m.d.Reload(); rerr != nil {
			m.flash = "saved, but reload failed: " + rerr.Error()
			return
		}
	}
	m.flash = okMsg
	if altMsg != "" {
		m.flash = altMsg
	}
}

// ---- agent form ----

// agentForm is the create/edit modal over the canonical kit.Form
// (F-026 P6 — the last legacy field system folds in: in-value cursor,
// paste, shift+tab ride kit; err stays local for the tests).
type agentForm struct {
	open bool
	kind formKind // formAgent | formSource
	orig string   // formAgent: "" = create; else the id being edited
	f    *kit.Form
	busy bool
	err  string
}

type formKind uint8

const (
	formAgent formKind = iota
	formSource
)

func (f *agentForm) values() []string {
	if f.f == nil {
		return nil
	}
	return f.f.Values()
}

func (f *agentForm) cur() int {
	if f.f == nil {
		return 0
	}
	return f.f.Cur()
}

// setErr records the error locally (tests) and mirrors it into the
// canonical form so the dialog renders it (F-026 P6).
func (f *agentForm) setErr(s string) {
	f.err = s
	if f.f != nil {
		f.f.SetError(s)
	}
}

func newAgentForm(clis []string, runtimeIdx int) agentForm {
	if len(clis) == 0 {
		clis = []string{"claude"}
	}
	f := agentForm{open: true, kind: formAgent}
	f.f = kit.NewForm("",
		kit.NewTextField("id     ", ""),
		kit.NewTextField("name   ", ""),
		kit.NewTextField("model  ", ""),
		kit.NewTextField("system ", ""),
		kit.NewToggleField("runtime", clis, runtimeIdx),
		kit.NewTextField("tools  ", ""),
		kit.NewTextField("role   ", ""),
		kit.NewTextField("skills ", ""),
		kit.NewTextField("persona", ""),
	)
	return f
}

// detectedFirst orders the runtime picker detection-driven (F-027 P2):
// the initial index preselects the first DETECTED runtime (registry
// order), 0 when none are detected. The probe result caches on the
// model so one form session costs one detection pass.
func (m *Model) detectedFirst(clis []string) int {
	if m.detected == nil && m.d.Detect != nil {
		m.detected = m.d.Detect()
	}
	for i, c := range clis {
		if m.detected != nil && m.detected[c] != "" {
			return i
		}
	}
	return 0
}

// newSourceForm is the F-019 one-flow input: a local path or a git URL,
// optionally with a #sub/path fragment scoping the import.
func newSourceForm() agentForm {
	f := agentForm{open: true, kind: formSource}
	f.f = kit.NewForm("", kit.NewTextField("source ", ""))
	return f
}

func (m *Model) editAgentForm(a *manifest.Agent, clis []string) agentForm {
	f := newAgentForm(clis, m.detectedFirst(clis))
	f.orig = a.ID
	// Reconstruct prefilled fields so the in-value cursor starts at the
	// end of the prefill (direct Value writes leave cur at 0).
	f.f.Fields[0] = kit.NewTextField("id     ", a.ID)
	f.f.Fields[1] = kit.NewTextField("name   ", a.Name)
	f.f.Fields[2] = kit.NewTextField("model  ", a.Model)
	f.f.Fields[3] = kit.NewTextField("system ", a.System)
	f.f.Fields[5] = kit.NewTextField("tools  ", strings.Join(a.Tools, ", "))
	f.f.Fields[6] = kit.NewTextField("role   ", a.Role)
	f.f.Fields[7] = kit.NewTextField("skills ", strings.Join(a.Skills, ", "))
	f.f.Fields[8] = kit.NewTextField("persona", a.Persona)
	for i, c := range clis {
		if c == a.Runtime {
			f.f.Fields[4] = kit.NewToggleField("runtime", clis, i)
			break
		}
	}
	return f
}

func (m *Model) formKey(key string) bool {
	f := &m.form
	f.f.Busy = f.busy
	if f.busy {
		return true // swallow while the import/clone runs
	}
	switch key {
	case "esc":
		f.open = false
		return true
	case "enter":
		switch f.kind {
		case formSource:
			m.submitSource()
		default:
			m.submitAgent()
		}
		return true
	}
	f.f.HandleKey(key) // tab/shift+tab/left/right/backspace/runes/paste
	return true        // the modal swallows everything else
}

// submitAgent validates via the manifest round-trip (the strict parse
// names every problem) then writes through the org crew seam.
func (m *Model) submitAgent() {
	f := &m.form
	if m.d.WS == nil || m.d.Org == nil {
		f.setErr("agent management unavailable: not inside a workspace")
		return
	}
	vals := f.values()
	id := strings.TrimSpace(vals[0])
	name := strings.TrimSpace(vals[1])
	model := strings.TrimSpace(vals[2])
	system := strings.TrimSpace(vals[3])
	runtime := vals[4]
	var tools []string
	for _, t := range strings.Split(vals[5], ",") {
		if t = strings.TrimSpace(t); t != "" {
			tools = append(tools, t)
		}
	}
	role := strings.TrimSpace(vals[6])
	var skills []string
	for _, s := range strings.Split(vals[7], ",") {
		if s = strings.TrimSpace(s); s != "" {
			skills = append(skills, s)
		}
	}
	persona := strings.TrimSpace(vals[8])
	if f.orig != "" && id != f.orig {
		f.setErr("id is immutable — archive this agent and create a new one")
		return
	}
	a := &manifest.Agent{
		ID: id, Name: name, Model: model, System: system,
		Runtime: runtime, Tools: tools,
		Role: role, Persona: persona, Skills: skills,
	}
	// An edit changes only what the form shows. Everything else the
	// manifest carries (sandbox policy, scopes, workflow, engine,
	// timeout, retries) must survive — rebuilding from the form alone
	// silently dropped them.
	if f.orig != "" {
		if roster, err := loadDiskRoster(m.d.WS); err == nil {
			for _, cur := range roster.agents {
				if cur.ID == f.orig {
					keep := *cur
					keep.Name, keep.Model, keep.System = name, model, system
					keep.Runtime, keep.Engine = runtime, ""
					keep.Tools, keep.Role, keep.Persona, keep.Skills = tools, role, persona, skills
					a = &keep
					break
				}
			}
		}
	}
	// Strict round-trip: Marshal → Parse names every validation error
	// before anything touches disk.
	data, err := manifest.Marshal(a)
	if err == nil {
		_, err = manifest.Parse(a.ID, data)
	}
	if err != nil {
		f.setErr(err.Error())
		return
	}
	if f.orig == "" {
		err = m.d.Org.CreateAgent(m.d.WS, a)
	} else {
		err = m.d.Org.UpdateAgent(m.d.WS, a)
	}
	if err != nil {
		f.setErr(err.Error())
		return
	}
	f.open = false
	rows, _ := m.agentRows()
	clampAgentCursor(&m.agentCur, len(rows))
	m.afterCrewWrite(nil, "agent "+id+" saved", "")
}

// HelpSections feeds the shell's contextual help (F-026 P7).
func (m *Model) HelpSections() [][2]string {
	out := [][2]string{
		{"[ / ]", "switch sections"},
		{"j / k", "move the cursor"},
		{"ctrl+s", "write settings"},
	}
	out = append(out, kit.HintRows(m.sectionHints()...)...)
	return kit.DedupeHelpRows(out)
}

func clampAgentCursor(c *int, n int) {
	if *c >= n {
		*c = n - 1
	}
	if *c < 0 {
		*c = 0
	}
}

// ---- add from source (F-019) ----

// submitSource kicks the one-flow import asynchronously: the form stays
// busy until the outcome event lands (clones can take seconds).
func (m *Model) submitSource() {
	f := &m.form
	if m.d.WS == nil || m.d.Org == nil {
		f.setErr("agent import unavailable: not inside a workspace")
		return
	}
	src := strings.TrimSpace(f.values()[0])
	if src == "" {
		f.setErr("path or git URL required")
		return
	}
	f.busy = true
	f.setErr("")
	go func(src string) {
		summary, err := m.addFromSource(src)
		ev := settingsEvent{msg: summary}
		if err != nil {
			ev = settingsEvent{err: err.Error()}
		}
		select {
		case m.events <- ev:
		default:
		}
	}(src)
}

// addFromSource is the F-019 one flow: a git URL clones through the
// hermetic shim; then pack.toml delegates to the validated pack
// install, and bare manifests validate ALL before the first write.
// Duplicate ids skip with named reasons — never fatal, never silent.
func (m *Model) addFromSource(src string) (string, error) {
	dir := src
	sub := ""
	if i := strings.Index(src, "#"); i >= 0 {
		dir, sub = src[:i], src[i+1:]
	}
	if isURL(dir) {
		c, err := os.MkdirTemp("", "dhi-import-*")
		if err != nil {
			return "", err
		}
		defer func() { _ = os.RemoveAll(c) }()
		dst := filepath.Join(c, "src")
		if _, err := gitcore.Clone(context.Background(), dir, dst); err != nil {
			return "", err
		}
		dir = dst
	} else if info, err := os.Stat(dir); err != nil || !info.IsDir() {
		return "", fmt.Errorf("source %s is not a directory", dir)
	}
	if sub != "" {
		dir = filepath.Join(dir, filepath.FromSlash(sub))
		if info, err := os.Stat(dir); err != nil || !info.IsDir() {
			return "", fmt.Errorf("subpath %q not found in source", sub)
		}
	}

	// pack.toml present → the existing pack flow (provenance tracked).
	if _, err := os.Stat(filepath.Join(dir, "pack.toml")); err == nil {
		res, err := (&pack.Installer{WS: m.d.WS}).Install(context.Background(), dir)
		if err != nil {
			return "", err
		}
		if rerr := m.reloadAfterImport(); rerr != nil {
			return fmt.Sprintf("installed pack %s (%d agents); reload failed: %s",
				res.Pack, len(res.Agents), rerr), nil
		}
		return fmt.Sprintf("installed pack %s (%d agents)", res.Pack, len(res.Agents)), nil
	}

	// Bare manifests: strict-parse every candidate BEFORE the first
	// write — one bad file refuses the whole import, named.
	cands, err := scanManifests(dir)
	if err != nil {
		return "", err
	}
	if len(cands) == 0 {
		return "", fmt.Errorf("no pack.toml or agent manifests found in %s", dir)
	}
	var imported, skipped []string
	for _, a := range cands {
		if err := m.d.Org.CreateAgent(m.d.WS, a); err != nil {
			skipped = append(skipped, a.ID+" ("+err.Error()+")")
			continue
		}
		imported = append(imported, a.ID)
	}
	reloadNote := ""
	if rerr := m.reloadAfterImport(); rerr != nil {
		reloadNote = "; reload failed: " + rerr.Error()
	}
	summary := fmt.Sprintf("imported %d: %s", len(imported), strings.Join(imported, ", "))
	if len(skipped) > 0 {
		summary += fmt.Sprintf("; skipped %d: %s", len(skipped), strings.Join(skipped, ", "))
	}
	return summary + reloadNote, nil
}

// reloadAfterImport drives the live-roster seam; nil seam = next launch.
func (m *Model) reloadAfterImport() error {
	if m.d.Reload == nil {
		return nil
	}
	return m.d.Reload()
}

// scanManifests parses every .toml under dir (recursively), id from the
// filename stem. All-or-nothing: any unparseable file aborts with the
// file + reason named.
func scanManifests(dir string) ([]*manifest.Agent, error) {
	var out []*manifest.Agent
	var errs []string
	err := filepath.Walk(dir, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if info.IsDir() || !strings.HasSuffix(path, ".toml") {
			return nil
		}
		data, err := os.ReadFile(path)
		if err != nil {
			errs = append(errs, path+": "+err.Error())
			return nil
		}
		id := strings.TrimSuffix(filepath.Base(path), ".toml")
		a, err := manifest.Parse(id, data)
		if err != nil {
			errs = append(errs, filepath.Base(path)+": "+err.Error())
			return nil
		}
		out = append(out, a)
		return nil
	})
	if err != nil {
		return nil, err
	}
	if len(errs) > 0 {
		return nil, fmt.Errorf("invalid manifest(s): %s", strings.Join(errs, "; "))
	}
	return out, nil
}

func isURL(s string) bool {
	for _, p := range []string{"http://", "https://", "git://", "ssh://", "git@"} {
		if strings.HasPrefix(s, p) {
			return true
		}
	}
	return false
}

// Click implements the clickHandler seam (F-041): a click on a rail row
// jumps to that section (dialogs and forms swallow clicks).
func (m *Model) Click(x, y int) bool {
	const railW = 16
	if m.dlg != nil || m.form.open {
		return false
	}
	if x >= railW {
		// CONFIG rows (F-055): content line i is setting i (no scroll
		// window); click selects, a second click changes it like enter.
		line := y - 1 // panel top border
		if m.sec != secConfig || line < 0 || line >= len(m.cfgRowAt) || m.cfgRowAt[line] < 0 {
			return false
		}
		row := m.cfgRowAt[line]
		if m.cursor == row {
			m.HandleKey("enter")
		} else {
			m.cursor = row
		}
		return true
	}
	rail := &kit.Rail{Rows: m.railRows(), Active: int(m.sec), Width: railW, Height: maxInt(m.height, 10)}
	if i, ok := rail.RowAt(y); ok {
		m.sec = sectionID(i)
		return true
	}
	return false
}

// View renders the docked settings surface: section rail (the same
// left-rail IA as every other surface, F-025) + the section panel with
// its keymap on the chrome HintBar. Dialogs overlay the whole view.
func (m *Model) View() string {
	w := maxInt(m.width, 40)
	h := maxInt(m.height, 10)

	railW := 16
	paneW := w - railW
	if paneW < 24 {
		paneW = 24
	}

	rail := (&kit.Rail{
		Rows:   m.railRows(),
		Active: int(m.sec),
		Width:  railW,
		Height: h,
	}).View()

	pane := m.sectionPane(paneW, h)

	view := lipgloss.JoinHorizontal(lipgloss.Top, rail, pane)
	switch {
	case m.dlg != nil:
		// Dialogs overlay the panel over a dimmed backdrop (F-024),
		// never replace its content. A form dialog's body is its form.
		if m.dform != nil {
			m.dlg.Lines = m.dform.ViewWidth(min(w-railW-8, 96))
			if m.valueHint != "" {
				for _, l := range kit.WrapWords(m.valueHint, min(w-railW-8, 96)) {
					m.dlg.Lines = append(m.dlg.Lines, theme.Hint().Render(l))
				}
			}
		}
		view = kit.Overlay(strings.Split(view, "\n"), m.dlg.View(), w, h)
	case m.form.open:
		// The legacy agent form rides the same overlay system.
		box := kit.Modal{Title: m.formTitle(), Lines: m.formView()}
		view = kit.Overlay(strings.Split(view, "\n"), box.View(), w, h)
	}
	return view
}

func (m *Model) railRows() []kit.RailRow {
	rows := make([]kit.RailRow, 0, secCount)
	for s := sectionID(0); s < secCount; s++ {
		rows = append(rows, kit.RailRow{Label: s.label()})
	}
	return rows
}

// sectionPane renders the active section's panel: body + the chrome
// HintBar (status/flash left, keymap right).
func (m *Model) sectionPane(w, h int) string {
	p := kit.NewPanel(strings.ToLower(m.sec.label()), true)

	var content []string
	switch m.sec {
	case secProject:
		content = m.projectView(w - 4)
	case secAgents:
		content = m.agentsView()
	case secBrain:
		content = m.brainView(w - 4)
	case secProposals:
		content = m.proposalsView(w - 4)
	case secLibrary:
		content = strings.Split(m.libraryBody(w), "\n")
	case secTeams:
		content = m.teamsView()
	case secPacks:
		content = m.packsView()
	case secStandards:
		content = m.standardsView()
	case secConventions:
		content = m.conventionsView(w - 4)
	case secWorkflows:
		content = m.workflowsView()
	case secAutopilots:
		content = m.autopilotsView()
	case secMarketplace:
		content = m.marketplaceView(w)
	default:
		content = m.configView(w-4, h-3)
	}
	inner := w - 4 // panel edges + horizontal padding
	for len(content) < h-3 {
		content = append(content, "")
	}
	content = content[:h-3]
	content = append(content, kit.HintBar(inner, m.statusFlash(), m.sectionHints()...))

	p.SetContent(content...)
	p.Width, p.Height = w, h
	return p.View()
}

// statusFlash renders the flash on the chrome bar: failure names red.
func (m *Model) statusFlash() string {
	if m.flash == "" {
		return ""
	}
	if strings.HasPrefix(m.flash, "failed") {
		return theme.ChromeStatus(theme.Current.Danger).Render(m.flash)
	}
	return theme.ChromeStatus(theme.Current.Success).Render(m.flash)
}

// sectionHints is the per-section keymap for the HintBar.
func (m *Model) sectionHints() []string {
	global := []string{"[ ] sections", "ctrl+s write"}
	var sec []string
	switch m.sec {
	case secConfig:
		sec = []string{"enter/l change", "h back", "e type a value"}
	case secAgents:
		sec = []string{"n new", "e edit", "a archive", "x delete", "v profile"}
		if m.selectedHasGrants() {
			sec = append(sec, "r revoke grants") // only when there is something to revoke
		}
	case secBrain:
		sec = []string{"p pin", "x retire", "s share", "m memory", "/ filter"}
	case secProposals:
		sec = []string{"v view", "y approve", "n reject", "x archive"}
	case secProject:
		sec = []string{"e edit", "d draft from repo", "i interview me"}
	case secTeams:
		sec = []string{"a add by role", "n new", "e edit", "x delete"}
	case secPacks:
		sec = []string{"i install", "x uninstall"}
	case secStandards:
		sec = []string{"w workspace", "t team", "g agent", "v preview"}
	case secConventions:
		sec = []string{"enter use", "p learn from example", "g learn from git", "n new from selected", "e edit", "d delete"}
	case secWorkflows:
		sec = []string{"n new", "d default", "v preview"}
	case secLibrary:
		sec = []string{"n new", "p persona", "e edit", "x delete", "r run script", "v card"}
	case secAutopilots:
		sec = []string{"n new", "t rituals", "e arm/pause", "r run now", "c task cards", "o last run", "x remove"}
	case secMarketplace:
		sec = []string{"r refresh", "/ search", "enter install", "t pin key", "T clear", "v inspect"}
	}
	return append(sec, global...)
}

// formTitle names the legacy agent modal.
func (m *Model) formTitle() string {
	switch {
	case m.form.kind == formSource:
		return "add from source (path or git URL, #sub/path to scope)"
	case m.form.orig != "":
		return "edit agent " + m.form.orig
	default:
		return "new agent"
	}
}

// configView renders CONFIG grouped under section headers (F-064):
// dotted leaders tie each key to its value, booleans read as colored
// ✓/✗, defaults are muted. budget is the visible row count; a taller
// list windows around the cursor. m.cfgRowAt maps each visible line to
// its setting (-1 for headers) so clicks land on the right row.
func (m *Model) configView(w, budget int) []string {
	type line struct {
		text string
		row  int
	}
	var lines []line
	head := func(title string) {
		if len(lines) > 0 {
			lines = append(lines, line{"", -1})
		}
		t := strings.ToUpper(title)
		rule := theme.Rule(max(w-len(t)-3, 0))
		lines = append(lines, line{" " + theme.SectionHeader().Render(t) + " " + rule, -1})
	}
	m.cfgKeys = m.cfgKeys[:0]
	add := func(row int, key, value string) {
		m.cfgKeys = append(m.cfgKeys, key)
		lines = append(lines, line{settingRow(m.cursor == row, settingLabel(key), value), row})
	}
	pp := m.cfg.Profile.Principal("", "")
	head("you")
	add(rowIdentity, "git identity", m.identityValue())
	add(rowRole, "profile.role", pp.Role)
	add(rowTone, "profile.writing.tone", pp.Tone)
	add(rowLength, "profile.writing.length", pp.Length)
	add(rowEmoji, "profile.writing.emoji", boolValue(pp.Emoji))
	add(rowLanguage, "profile.writing.language", pp.Language)
	add(rowOrder, "profile.reading.order", pp.Order)
	add(rowFormat, "profile.reading.format", pp.Format)
	add(rowDetail, "profile.reading.detail", pp.Detail)
	head("appearance")
	add(rowTheme, "theme", m.themeValue())
	add(rowReducedMotion, "reduced_motion", boolValue(m.cfg.ReducedMotion))
	head("editor & terminal")
	add(rowTabWidth, "editor.tab_width", itoa(m.cfg.Editor.TabWidth))
	add(rowLineNumbers, "editor.line_numbers", boolValue(m.cfg.Editor.LineNumbers))
	add(rowScrollback, "terminal.scrollback", itoa(m.cfg.Terminal.Scrollback))
	add(rowLanguages, "editor.languages", m.languagesValue())
	head("agent scopes & sandbox")
	for i, name := range scopeRowNames {
		val := m.cfg.Scopes[name]
		if val == "" {
			val = theme.TextMuted().Render("default")
		}
		add(rowScopesBase+i, "scopes."+name, val)
	}
	add(rowSandbox, "security.sandbox", sandboxValue(m.cfg.Security.Sandbox))
	wt := m.cfg.Worktrees.Root
	if wt == "" {
		wt = theme.TextMuted().Render("inside .dhi/")
	}
	co := theme.TextMuted().Render("off")
	if m.cfg.Conventions.Commit.CoAuthorEnabled {
		co = m.cfg.Conventions.Commit.CoAuthor
	}
	head("worktrees & conventions")
	add(rowWorktrees, "worktrees.root", wt)
	add(rowBranchTask, "conventions.branch.task", m.cfg.Conventions.Branch.Task)
	add(rowCommitFormat, "conventions.commit.format", m.cfg.Conventions.Commit.Format)
	add(rowCommitCoAuthor, "conventions.commit.co_author", co)
	add(rowCopyright, "conventions.copyright", copyrightValue(m.cfg.Conventions.Copyright))
	add(rowLicense, "conventions.copyright.license", orMuted(m.cfg.Conventions.Copyright.License == "", "none", m.cfg.Conventions.Copyright.License))
	add(rowMaxSubject, "conventions.commit.max_subject", itoa(m.cfg.Conventions.Commit.MaxSubject)+" characters")
	head("agents & budgets")
	add(rowEngine, "engine", orMuted(m.cfg.Engine == "", "none — pick a coding CLI", strings.TrimPrefix(m.cfg.Engine, "cli:")))
	add(rowResume, "agents.resume", boolValue(m.cfg.Agents.Resume))
	add(rowAutonomy, "agents.autonomy", autonomyValue(m.cfg.Agents.Autonomy))
	add(rowMaxTurns, "agents.max_turns", orMuted(m.cfg.Agents.MaxTurns == 0, "CLI default", itoa(m.cfg.Agents.MaxTurns)))
	add(rowMaxIter, "agents.max_iterations", itoa(m.cfg.Agents.MaxIterations))
	add(rowStall, "agents.stall_seconds", orMuted(m.cfg.Agents.StallSeconds == 0, "off", stallText(m.cfg.Agents.StallSeconds)))
	add(rowReflect, "agents.reflect", boolValue(m.cfg.Agents.Reflect))
	add(rowReflectModel, "agents.reflect_model", orMuted(m.cfg.Agents.ReflectModel == "", "the employee's own model", m.cfg.Agents.ReflectModel))
	add(rowTracePrompts, "agents.trace_prompts", boolValue(m.cfg.Agents.TracePrompts))
	add(rowTaskUSD, "budgets.task_usd", orMuted(m.cfg.Budgets.TaskUSD == 0, "no limit", usd(m.cfg.Budgets.TaskUSD)))
	add(rowTaskTokens, "budgets.task_tokens", orMuted(m.cfg.Budgets.TaskTokens == 0, "no limit", tokensText(m.cfg.Budgets.TaskTokens)))
	add(rowAgentDayUSD, "budgets.agent_day_usd", orMuted(m.cfg.Budgets.AgentDayUSD == 0, "no limit", usd(m.cfg.Budgets.AgentDayUSD)+" / day"))
	add(rowWorkDayUSD, "budgets.workspace_day_usd", orMuted(m.cfg.Budgets.WorkspaceDayUSD == 0, "no limit", usd(m.cfg.Budgets.WorkspaceDayUSD)+" / day"))
	head("memory")
	add(rowMemPrompt, "memory.prompt_tokens", tokensText(m.cfg.Memory.PromptTokens)+" per prompt")
	add(rowMemSession, "memory.session_tokens", tokensText(m.cfg.Memory.SessionTokens)+" before a fresh session")
	add(rowMemJournal, "memory.journal_max", itoa(m.cfg.Memory.JournalMax)+" entries")
	add(rowMemLessons, "memory.lessons_max", itoa(m.cfg.Memory.LessonsMax)+" active lessons")
	add(rowMemPinned, "memory.pinned_max", orMuted(m.cfg.Memory.PinnedMax == 0, "no cap", itoa(m.cfg.Memory.PinnedMax)+" pinned"))
	add(rowMemDecay, "memory.decay_days", itoa(m.cfg.Memory.DecayDays)+" days unused")
	head("keys")
	add(rowKeys, "keys", m.keysValue())

	top := 0
	if budget > 0 && len(lines) > budget {
		cur := 0
		for i, l := range lines {
			if l.row == m.cursor {
				cur = i
			}
		}
		top = min(max(cur-budget/2, 0), len(lines)-budget)
		lines = lines[top : top+budget]
	}
	out := make([]string, len(lines))
	m.cfgRowAt = make([]int, len(lines))
	for i, l := range lines {
		out[i] = l.text
		m.cfgRowAt[i] = l.row
	}
	return out
}

// autonomyValue explains the envelope in place (F-067).
func autonomyValue(mode string) string {
	if mode == settings.AutonomyAsk {
		return "ask"
	}
	return theme.SuccessText().Render("envelope")
}

// stallText renders the watchdog window ("15m", "90s").
func stallText(sec int) string {
	if sec%60 == 0 {
		return itoa(sec/60) + "m"
	}
	return itoa(sec) + "s"
}

// orMuted renders muted when unset is true (a default), else value.
func orMuted(unset bool, muted, value string) string {
	if unset {
		return theme.TextMuted().Render(muted)
	}
	return value
}

func usd(v float64) string {
	s := strings.TrimRight(strings.TrimRight(fmt.Sprintf("%.2f", v), "0"), ".")
	return "$" + s
}

// tokensText renders 500000 → "500k", 1000000 → "1M".
func tokensText(n int) string {
	switch {
	case n >= 1_000_000 && n%1_000_000 == 0:
		return fmt.Sprintf("%dM", n/1_000_000)
	case n >= 1000 && n%1000 == 0:
		return fmt.Sprintf("%dk", n/1000)
	}
	return itoa(n)
}

// boolValue renders a boolean setting as a colored mark plus the word.
func boolValue(b bool) string {
	if b {
		return theme.SuccessText().Render(theme.GlyphCheck + " true")
	}
	return theme.TextMuted().Render(theme.GlyphCross + " false")
}

// leader pads name to w cells with a dotted leader ("name ·······").
func leader(name string, w int) string {
	n := w - len(name) - 2
	if n < 1 {
		return padTo(name, w)
	}
	return name + " " + theme.RuleText().Render(strings.Repeat("·", n)) + " "
}

// keysValue lists the key remaps (F-058); enter edits them (F-084).
func (m *Model) keysValue() string {
	if len(m.cfg.Keys) == 0 {
		return theme.Hint().Render(`none · enter to remap a key`)
	}
	pairs := make([]string, 0, len(m.cfg.Keys))
	for k, v := range m.cfg.Keys {
		pairs = append(pairs, k+" → "+v)
	}
	sort.Strings(pairs)
	return strings.Join(pairs, " · ")
}

func (m *Model) agentsView() []string {
	if m.d.WS == nil || m.d.Org == nil {
		return []string{theme.TextDim().Render(
			"(agent management unavailable — not inside a workspace)")}
	}
	rows, err := m.agentRows()
	if err != nil {
		return []string{theme.DangerText().Render("roster unavailable: " + err.Error())}
	}
	if len(rows) == 0 {
		return []string{theme.TextDim().Render("(no agents — \"n\" to create one)")}
	}
	out := []string{}
	for i, r := range rows {
		line := padTo(r.id, 12) + padTo(r.model, 14) + padTo(r.runtime, 10) +
			itoa(r.tools) + " tools"
		if r.archived {
			line = padTo(r.id, 12) + theme.TextDim().Render("archived")
		}
		if i == m.agentCur {
			out = append(out, theme.GlyphCursor+" "+theme.TabActive().Render(line))
		} else {
			out = append(out, "  "+theme.TextDim().Render(line))
		}
	}
	return append(out, m.grantsView()...)
}

// grantsView lists the remembered "allow always" grants (F-067) under
// the roster; r revokes the selected agent's.
func (m *Model) grantsView() []string {
	if m.d.Grants == nil {
		return nil
	}
	gs := m.d.Grants.Grants()
	out := []string{"", " " + theme.SectionHeader().Render("ALWAYS ALLOWED") + " " + theme.Rule(20)}
	if len(gs) == 0 {
		return append(out, "  "+theme.Hint().Render("none — answering an approval with \"always\" remembers it here"))
	}
	for _, g := range gs {
		line := padTo(g.Agent, 12) + padTo(g.Scope, 10)
		out = append(out, "  "+theme.TextDim().Render(line)+theme.Hint().Render("since "+g.At.Local().Format("Jan 2")))
	}
	return out
}

// selectedHasGrants reports whether the AGENTS cursor's agent has any
// remembered grant.
func (m *Model) selectedHasGrants() bool {
	if m.d.Grants == nil {
		return false
	}
	rows, _ := m.agentRows()
	if m.agentCur >= len(rows) {
		return false
	}
	for _, g := range m.d.Grants.Grants() {
		if g.Agent == rows[m.agentCur].id {
			return true
		}
	}
	return false
}

// revokeGrants forgets every grant of the selected agent.
func (m *Model) revokeGrants(agent string) {
	if m.d.Grants == nil {
		m.flash = "grants unavailable (no agent runtime)"
		return
	}
	n := 0
	for _, g := range m.d.Grants.Grants() {
		if g.Agent != agent {
			continue
		}
		if err := m.d.Grants.Revoke(g.Agent, g.Scope); err != nil {
			m.flash = "failed: " + err.Error()
			return
		}
		n++
	}
	if n == 0 {
		m.flash = agent + " has no remembered grants"
		return
	}
	m.flash = fmt.Sprintf("revoked %d grant(s) — %s asks again from its next action", n, agent)
}

// formView renders the agent modal body: label/value rows with the
// cursor field highlighted, the toggle shown bracketed, errors in danger.
func (m *Model) formView() []string {
	f := &m.form
	var out []string
	if f.orig != "" {
		out = append(out, theme.TextDim().Render("editing: "+f.orig))
	}
	out = append(out, f.f.View()...)
	if f.busy {
		out = append(out[:len(out)-1], theme.WarningText().Render(theme.GlyphBusy+" working… (clone/import in flight)"))
	}
	return out
}

// settingLabels name CONFIG rows in plain words (F-084): people read
// what a setting does, never the key it is stored under.
var settingLabels = map[string]string{
	"profile.role": "you are a", "profile.writing.tone": "writing tone", "profile.writing.length": "writing length",
	"profile.writing.emoji": "emoji in writing", "profile.writing.language": "writing language",
	"profile.reading.order": "replies start with", "profile.reading.format": "reply format",
	"profile.reading.detail": "reply detail",
	"reduced_motion":         "reduce motion", "editor.tab_width": "tab width", "editor.line_numbers": "line numbers",
	"terminal.scrollback": "terminal scrollback", "editor.languages": "editor languages",
	"scopes.read": "may read files", "scopes.write": "may write files", "scopes.exec": "may run commands",
	"scopes.network": "may use the network", "scopes.git": "may use git", "scopes.push": "may push",
	"scopes.admin": "may change admin settings", "security.sandbox": "sandbox",
	"worktrees.root": "task worktrees folder", "conventions.branch.task": "task branch name",
	"conventions.commit.format": "commit format", "conventions.commit.co_author": "co-author trailer",
	"conventions.copyright": "copyright header", "conventions.copyright.license": "license",
	"conventions.commit.max_subject": "commit subject limit",
	"engine":                         "default coding CLI", "agents.resume": "resume CLI sessions", "agents.autonomy": "autonomy",
	"agents.max_turns": "CLI turns per run", "agents.max_iterations": "verify → fix rounds",
	"agents.stall_seconds": "stall watchdog", "agents.reflect": "learn from turns",
	"agents.reflect_model": "reflection model", "agents.trace_prompts": "save full prompts",
	"budgets.task_usd": "budget per task", "budgets.task_tokens": "tokens per task",
	"budgets.agent_day_usd": "budget per employee a day", "budgets.workspace_day_usd": "budget per day",
	"memory.prompt_tokens": "prompt size", "memory.session_tokens": "session size",
	"memory.journal_max": "journal entries kept", "memory.lessons_max": "lessons kept",
	"memory.pinned_max": "pinned lessons", "memory.decay_days": "lessons fade after", "keys": "key remaps",
}

func settingLabel(key string) string {
	if l, ok := settingLabels[key]; ok {
		return l
	}
	return key
}

// settingNameW fits the longest label
// plus its dotted leader
// plus a two-cell gutter, so a value never runs into its name.
const settingNameW = 28

func settingRow(selected bool, name, value string) string {
	if selected {
		return theme.GlyphCursor + " " + theme.TabActive().Render(name) +
			strings.TrimPrefix(leader(name, settingNameW), name) + value
	}
	return "  " + theme.TextDim().Render(name) + strings.TrimPrefix(leader(name, settingNameW), name) + value
}

// themeValue shows "auto" while no theme was chosen: the live theme then
// follows the terminal's background (F-055).
func (m *Model) themeValue() string {
	if !m.cfg.ThemeSet {
		return "auto · " + theme.Current.Name + theme.Hint().Render("  (follows the terminal)")
	}
	return m.cfg.Theme
}

func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	neg := n < 0
	if neg {
		n = -n
	}
	var digits []byte
	for n > 0 {
		digits = append([]byte{byte('0' + n%10)}, digits...)
		n /= 10
	}
	if neg {
		return "-" + string(digits)
	}
	return string(digits)
}

func padTo(s string, w int) string {
	for len(s) < w {
		s += " "
	}
	return s
}

func maxInt(a, b int) int {
	if a > b {
		return a
	}
	return b
}
