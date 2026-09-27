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
	"strings"
	"time"

	"charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"

	"github.com/drjzlyan/dhi/internal/agentkit/bus"
	"github.com/drjzlyan/dhi/internal/agentkit/library"
	"github.com/drjzlyan/dhi/internal/agentkit/manifest"
	"github.com/drjzlyan/dhi/internal/agentkit/org"
	"github.com/drjzlyan/dhi/internal/agentkit/pack"
	"github.com/drjzlyan/dhi/internal/autopilot"
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
	cursor   int // CONFIG rows
	agentCur int // AGENTS roster rows
	libCur   int // LIBRARY listing rows
	teamCur  int // TEAMS rows
	packCur  int // PACKS rows
	stdCur   int // STANDARDS rows
	wfCur    int // WORKFLOWS rows
	autoCur  int // AUTOPILOTS rows

	detected map[string]string // runtime name → version ("" = not installed)
	lib      *library.Store    // lazy snapshot of the behaviour library

	form    agentForm // legacy agent modal (create/edit/source) — P5 folds into kit dialogs
	dlg     *kit.Modal
	dform   *kit.Form
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
	msg string
	err string
}

type sectionID uint8

const (
	secConfig sectionID = iota
	secAgents
	secLibrary
	secTeams
	secPacks
	secStandards
	secWorkflows
	secAutopilots
	secCount
)

func (s sectionID) label() string {
	switch s {
	case secConfig:
		return "CONFIG"
	case secAgents:
		return "AGENTS"
	case secLibrary:
		return "LIBRARY"
	case secTeams:
		return "TEAMS"
	case secPacks:
		return "PACKS"
	case secStandards:
		return "STANDARDS"
	case secWorkflows:
		return "WORKFLOWS"
	case secAutopilots:
		return "AUTOPILOTS"
	default:
		return "CONFIG"
	}
}

// TurnHandler is the narrow runtime seam autopilot run-now dispatches
// through (satisfied by *runtime.Runtime).
type TurnHandler interface {
	Handle(ctx context.Context, msg bus.Message)
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
		if m.form.kind == formSource {
			m.form.open = false
		}
		if m.dlg != nil && m.dkind == dlgPackInstall {
			m.closeDialog()
		}
		if ev.err != "" {
			m.flash = "failed: " + ev.err
		} else {
			m.flash = ev.msg
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
	case secLibrary:
		return m.libraryKey(key)
	case secTeams:
		return m.teamsKey(key)
	case secPacks:
		return m.packsKey(key)
	case secStandards:
		return m.standardsKey(key)
	case secWorkflows:
		return m.workflowsKey(key)
	case secAutopilots:
		return m.autopilotsKey(key)
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
		m.cycle(1)
		return true
	case "left", "h":
		m.cycle(-1)
		return true
	}
	return false
}

const (
	rowTheme = iota
	rowReducedMotion
	rowTabWidth
	rowLineNumbers
	rowScrollback
	rowScopesBase // then 7 capability-scope rows (F-030 P2)
	rowCount      = rowScopesBase + 7
)

// scopeRowNames orders the settings scope rows.
var scopeRowNames = []string{"read", "write", "exec", "network", "git", "push", "admin"}

// scopeEffects is the cycle order for a scope row.
var scopeEffects = []string{"auto", "ask", "deny"}

// cycle applies one modification step and persists + applies live.
func (m *Model) cycle(dir int) {
	switch m.cursor {
	case rowTheme:
		names := []string{theme.Dark().Name, theme.Light().Name}
		for i, n := range names {
			if n == m.cfg.Theme {
				m.cfg.Theme = names[(i+dir+len(names))%len(names)]
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
	default:
		if m.cursor >= rowScopesBase {
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
	if m.savePath == "" {
		m.flash = "(no config path — change is session-only)"
		return
	}
	if err := m.cfg.Save(m.savePath); err != nil {
		m.flash = "save failed: " + err.Error()
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

func (f *agentForm) fields() []kit.Field {
	if f.f == nil {
		return nil
	}
	return f.f.Fields
}

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
	if f.orig != "" && id != f.orig {
		f.setErr("id is immutable — archive this agent and create a new one")
		return
	}
	a := &manifest.Agent{
		ID: id, Name: name, Model: model, System: system,
		Runtime: runtime, Tools: tools,
		Role: role, Skills: skills,
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
		// never replace its content.
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
	switch {
	case m.sec == secAgents:
		content = m.agentsView()
	case m.sec == secLibrary:
		content = strings.Split(m.libraryBody(w), "\n")
	case m.sec == secTeams:
		content = m.teamsView()
	case m.sec == secPacks:
		content = m.packsView()
	case m.sec == secStandards:
		content = m.standardsView()
	case m.sec == secWorkflows:
		content = m.workflowsView()
	case m.sec == secAutopilots:
		content = m.autopilotsView()
	default:
		content = m.configView()
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
		sec = []string{"enter/l change", "h/l back"}
	case secAgents:
		sec = []string{"n new", "e edit", "a archive", "x delete", "v profile"}
	case secTeams:
		sec = []string{"n new", "e edit", "x delete"}
	case secPacks:
		sec = []string{"i install", "x uninstall"}
	case secStandards:
		sec = []string{"w workspace", "t team", "g agent", "v preview"}
	case secWorkflows:
		sec = []string{"n new", "d default", "v preview"}
	case secAutopilots:
		sec = []string{"n new", "e arm/pause", "r run now", "x remove"}
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

func (m *Model) configView() []string {
	rows := []string{
		settingRow(m.cursor == rowTheme, "theme",
			valueText(m.cfg.Theme)),
		settingRow(m.cursor == rowReducedMotion, "reduced_motion",
			valueText(boolStr(m.cfg.ReducedMotion))),
		settingRow(m.cursor == rowTabWidth, "editor.tab_width",
			valueText(itoa(m.cfg.Editor.TabWidth))),
		settingRow(m.cursor == rowLineNumbers, "editor.line_numbers",
			valueText(boolStr(m.cfg.Editor.LineNumbers))),
		settingRow(m.cursor == rowScrollback, "terminal.scrollback",
			valueText(itoa(m.cfg.Terminal.Scrollback))),
	}
	for i, name := range scopeRowNames {
		val := m.cfg.Scopes[name]
		if val == "" {
			val = "default"
		}
		rows = append(rows, settingRow(m.cursor == rowScopesBase+i, "scopes."+name, valueText(val)))
	}
	return rows
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
	return out
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

func settingRow(selected bool, name, value string) string {
	namePart := padTo(name, 24)
	if selected {
		return theme.GlyphCursor + " " + theme.TabActive().Render(namePart) + value
	}
	return "  " + theme.TextDim().Render(namePart) + value
}

func valueText(v string) string { return v }

func boolStr(b bool) string {
	if b {
		return "true"
	}
	return "false"
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
