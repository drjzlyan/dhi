// Package settings is DHI's Settings view: keyboard-navigable sections
// over the typed config schema (CONFIG) and the agent roster (AGENTS —
// F-018 full CRUD) with live application (theme swap), automatic
// persistence, and roster changes that go live through the reload seam.
package settings

import (
	"strings"

	"charm.land/bubbletea/v2"

	"github.com/drjzlyan/dhi/internal/agentkit/manifest"
	"github.com/drjzlyan/dhi/internal/agentkit/org"
	"github.com/drjzlyan/dhi/internal/settings"
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

	form   agentForm // open modal (agent create/edit)
	flash  string
	width  int
	height int
}

var _ surfaces.Surface = (*Model)(nil)

type sectionID uint8

const (
	secConfig sectionID = iota
	secAgents
	secCount
)

func (s sectionID) label() string {
	switch s {
	case secConfig:
		return "CONFIG"
	case secAgents:
		return "AGENTS"
	default:
		return "CONFIG"
	}
}

// Deps wires workspace-scoped services. Zero fields degrade the AGENTS
// section to visible "unavailable" rows rather than errors (F-011).
type Deps struct {
	WS   *workspace.Workspace
	Org  *org.Org
	CLIs []string // registered runtime names (form toggle options)
	// Reload swaps the live runtime roster after a successful crew
	// write; nil means changes apply on next launch (named in flash).
	Reload func() error
}

// New wires the surface to a loaded config, its persistence target,
// and (optionally) the workspace services behind agent management.
func New(cfg settings.Config, savePath string, d Deps) *Model {
	return &Model{cfg: cfg, savePath: savePath, d: d}
}

func (m *Model) Meta() surfaces.Meta    { return surfaces.Meta{ID: "settings", Title: "Settings"} }
func (m *Model) Init() tea.Cmd          { return nil }
func (m *Model) Update(tea.Msg) tea.Cmd { return nil }

func (m *Model) Resize(w, h int) {
	m.width, m.height = w, h
}

func (m *Model) HandleKey(key string) bool {
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
	if m.sec == secAgents {
		return m.agentsKey(key)
	}
	return m.configKey(key)
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
	rowCount
)

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
		m.form = newAgentForm(m.d.CLIs)
		return true
	case "e":
		if m.agentCur < len(rows) && m.d.WS != nil {
			// Prefill from the full manifest, not the summary row.
			if roster, err := org.LoadRoster(m.d.WS); err == nil {
				for _, a := range roster {
					if a.ID == rows[m.agentCur].id {
						m.form = editAgentForm(a, m.d.CLIs)
						return true
					}
				}
			}
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

type agentField struct {
	label  string
	value  string
	toggle []string // non-empty: left/right cycles
	val    int
}

func (f *agentField) text() string {
	if len(f.toggle) > 0 {
		return f.toggle[f.val]
	}
	return f.value
}

func (f *agentField) cycle(dir int) {
	if len(f.toggle) == 0 {
		return
	}
	f.val = (f.val + dir + len(f.toggle)) % len(f.toggle)
}

type agentForm struct {
	open   bool
	orig   string // "" = create; else the id being edited
	cur    int
	fields []agentField
	err    string
}

func newAgentForm(clis []string) agentForm {
	if len(clis) == 0 {
		clis = []string{"claude"}
	}
	return agentForm{open: true, fields: []agentField{
		{label: "id     "},
		{label: "name   "},
		{label: "model  "},
		{label: "system "},
		{label: "runtime", toggle: clis},
		{label: "tools  "},
	}}
}

func editAgentForm(a *manifest.Agent, clis []string) agentForm {
	f := newAgentForm(clis)
	f.orig = a.ID
	f.fields[0].value = a.ID
	f.fields[1].value = a.Name
	f.fields[2].value = a.Model
	f.fields[3].value = a.System
	f.fields[5].value = strings.Join(a.Tools, ", ")
	for i, c := range clis {
		if c == a.Runtime {
			f.fields[4].val = i
			break
		}
	}
	return f
}

func (m *Model) formKey(key string) bool {
	f := &m.form
	switch key {
	case "esc":
		f.open = false
		return true
	case "enter":
		m.submitAgent()
		return true
	case "tab":
		f.cur = (f.cur + 1) % len(f.fields)
		return true
	case "left":
		f.fields[f.cur].cycle(-1)
		return true
	case "right":
		f.fields[f.cur].cycle(1)
		return true
	case "backspace":
		fld := &f.fields[f.cur]
		if len(fld.toggle) == 0 && len(fld.value) > 0 {
			fld.value = fld.value[:len(fld.value)-1]
		}
		return true
	}
	fld := &f.fields[f.cur]
	if len(fld.toggle) == 0 {
		if r := []rune(key); len(r) == 1 && r[0] >= 32 {
			fld.value += key
			return true
		}
	}
	return true // the modal swallows everything else
}

// submitAgent validates via the manifest round-trip (the strict parse
// names every problem) then writes through the org crew seam.
func (m *Model) submitAgent() {
	f := &m.form
	if m.d.WS == nil || m.d.Org == nil {
		f.err = "agent management unavailable: not inside a workspace"
		return
	}
	id := strings.TrimSpace(f.fields[0].text())
	name := strings.TrimSpace(f.fields[1].text())
	model := strings.TrimSpace(f.fields[2].text())
	system := strings.TrimSpace(f.fields[3].text())
	runtime := f.fields[4].text()
	var tools []string
	for _, t := range strings.Split(f.fields[5].text(), ",") {
		if t = strings.TrimSpace(t); t != "" {
			tools = append(tools, t)
		}
	}
	if f.orig != "" && id != f.orig {
		f.err = "id is immutable — archive this agent and create a new one"
		return
	}
	a := &manifest.Agent{
		ID: id, Name: name, Model: model, System: system,
		Runtime: runtime, Tools: tools,
	}
	// Strict round-trip: Marshal → Parse names every validation error
	// before anything touches disk.
	data, err := manifest.Marshal(a)
	if err == nil {
		_, err = manifest.Parse(a.ID, data)
	}
	if err != nil {
		f.err = err.Error()
		return
	}
	if f.orig == "" {
		err = m.d.Org.CreateAgent(m.d.WS, a)
	} else {
		err = m.d.Org.UpdateAgent(m.d.WS, a)
	}
	if err != nil {
		f.err = err.Error()
		return
	}
	f.open = false
	rows, _ := m.agentRows()
	clampAgentCursor(&m.agentCur, len(rows))
	m.afterCrewWrite(nil, "agent "+id+" saved", "")
}

func clampAgentCursor(c *int, n int) {
	if *c >= n {
		*c = n - 1
	}
	if *c < 0 {
		*c = 0
	}
}

// View renders the docked settings panel: section strip, section body,
// status or keymap hints pinned to the foot — the same full-height
// panel language as the workspace and editor surfaces.
func (m *Model) View() string {
	w := maxInt(m.width, 40)
	h := maxInt(m.height, 10)

	strip := m.sectionStrip()

	foot := theme.Hint().Render("←/→ change · [ ] sections · ctrl+s write")
	if m.flash != "" {
		foot = theme.SuccessText().Render(m.flash)
	}

	var content []string
	if m.form.open {
		content = m.formView()
	} else if m.sec == secAgents {
		content = m.agentsView()
	} else {
		content = m.configView()
	}
	for len(content) < h-6 {
		content = append(content, "")
	}
	content = append(content, "", foot)

	p := kit.NewPanel("settings", true)
	body := append([]string{strip, ""}, content...)
	p.SetContent(body...)
	p.Width, p.Height = w, h
	return p.View()
}

func (m *Model) sectionStrip() string {
	var parts []string
	for s := sectionID(0); s < secCount; s++ {
		if s == m.sec {
			parts = append(parts, theme.TabActive().Render("["+s.label()+"]"))
		} else {
			parts = append(parts, theme.TextDim().Render(s.label()))
		}
	}
	return strings.Join(parts, " · ")
}

func (m *Model) configView() []string {
	return []string{
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
}

func (m *Model) agentsView() []string {
	hint := theme.Hint().Render("n new · e edit · a archive/restore · x delete")
	if m.d.WS == nil || m.d.Org == nil {
		return []string{hint, theme.TextDim().Render(
			"(agent management unavailable — not inside a workspace)")}
	}
	rows, err := m.agentRows()
	if err != nil {
		return []string{hint, theme.DangerText().Render("roster unavailable: " + err.Error())}
	}
	if len(rows) == 0 {
		return []string{hint, theme.TextDim().Render("(no agents — \"n\" to create one)")}
	}
	out := []string{hint}
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

// formView renders the agent modal: label/value rows with the cursor
// field highlighted, the toggle shown bracketed, errors in danger.
func (m *Model) formView() []string {
	f := &m.form
	title := "new agent"
	if f.orig != "" {
		title = "edit agent " + f.orig
	}
	out := []string{theme.Brand().Render(title), ""}
	for i, fld := range f.fields {
		cursor := "  "
		style := theme.TextDim()
		if i == f.cur {
			cursor = theme.GlyphCursor + " "
			style = theme.TabActive()
		}
		v := fld.text()
		if len(fld.toggle) > 0 {
			v = "[" + v + "]"
		}
		out = append(out, cursor+style.Render(padTo(fld.label, 8))+" "+v)
	}
	out = append(out, "")
	if f.err != "" {
		out = append(out, theme.DangerText().Render(f.err))
	} else {
		out = append(out, theme.Hint().Render("tab field · ←/→ cycle · ⏎ save · esc cancel"))
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
