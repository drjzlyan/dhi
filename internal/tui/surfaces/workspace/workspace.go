// Package workspace is DHI's landing view: the company of agents.
// Four sections — members, org, packs, standards — switched with [ ];
// each carries its own cursor and contextual keymap. P1 shipped member
// management; P2c adds live org/crew editing, marketplace pack install,
// and layered coding-standards editors. Channels/tasks/inspection land
// in P3–P5 and render as dim roadmap rows until then.
package workspace

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"time"

	"charm.land/bubbletea/v2"

	"github.com/drjzlyan/dhi/internal/agentkit/bus"
	"github.com/drjzlyan/dhi/internal/agentkit/org"
	"github.com/drjzlyan/dhi/internal/agentkit/pack"
	"github.com/drjzlyan/dhi/internal/agentkit/standards"
	"github.com/drjzlyan/dhi/internal/gitcore"
	"github.com/drjzlyan/dhi/internal/tui/surfaces"
	"github.com/drjzlyan/dhi/internal/workspace"
)

// Timeouts for async operations; the UI stays responsive either way.
const (
	cloneTimeout   = 5 * time.Minute
	installTimeout = 5 * time.Minute
)

// sectionID enumerates the switchable panes.
type sectionID uint8

const (
	secMembers sectionID = iota
	secOrg
	secPacks
	secStandards
	secChannels
	secCount
)

func (s sectionID) label() string {
	switch s {
	case secMembers:
		return "MEMBERS"
	case secOrg:
		return "ORG"
	case secPacks:
		return "PACKS"
	case secChannels:
		return "CHANNELS"
	default:
		return "STANDARDS"
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

	org       *org.Org
	orgErr    string
	packs     *pack.Installer
	stdRootOK bool

	form formState
	pane *chatPane

	events    chan wsEvent
	cancelSub func()
	cancelOrg func()
}

var _ surfaces.Surface = (*Model)(nil)

type wsEvent struct {
	kind       uint8 // evPing | evCloneDone | evInstallDone
	err        string
	packName   string
	packAgents []string
}

const (
	evPing uint8 = iota
	evCloneDone
	evInstallDone
)

// New returns the workspace model. A nil ws renders the not-a-workspace
// empty state (all keys inert). The message bus powers CHANNELS; rt may
// be nil (posting works, mentions just have no crew to trigger).
func New(version string, ws *workspace.Workspace, b *bus.Bus, rt turnHandler) *Model {
	m := &Model{
		version: version,
		ws:      ws,
		events:  make(chan wsEvent, 16),
	}
	if ws != nil {
		if o, err := org.Load(ws.Root); err == nil {
			m.org = o
		} else {
			m.orgErr = err.Error()
		}
		m.packs = &pack.Installer{WS: ws}
		if _, err := standards.Inspect(ws.Root); err == nil {
			m.stdRootOK = true
		}
		if b != nil {
			m.pane = newChatPane(b, rt, m.org)
		}
	}
	return m
}

func (m *Model) Meta() surfaces.Meta { return surfaces.Meta{ID: "workspace", Title: "Workspace"} }

// Init starts the change pumps for re-render triggers.
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
	return tea.Batch(cmds...)
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

// Update handles async events: pings re-render; clone/install results
// resolve the busy modal or surface the error inline.
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
		case evInstallDone:
			if m.form.kind == fPackInstall && m.form.busy {
				m.form.busy = false
				if msg.err != "" {
					m.form.err = msg.err
				} else {
					m.form.flash = "installed " + msg.packName +
						" (" + itoa(len(msg.packAgents)) + " agents)"
					m.form = formState{flash: m.form.flash}
					m.sec = secPacks
				}
			}
		}
		return m.listen()
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
	fTeamEdit
	fTeamDeleteConfirm
	fAgentNew
	fAgentArchiveConfirm
	fPackInstall
	fPackUninstallConfirm
	fStdLayerEdit
	fStdPreviewPrompt
	fStdPreviewShow
)

type field struct {
	label  string
	runes  []rune
	toggle []string // non-empty: left/right/space cycles values
	val    int      // selected toggle index
}

func (f *field) text() string { return string(f.runes) }

func (f *field) cycle(dir int) {
	if len(f.toggle) == 0 {
		return
	}
	f.val = (f.val + dir + len(f.toggle)) % len(f.toggle)
}

func (f *field) toggleValue() string {
	if len(f.toggle) == 0 {
		return ""
	}
	return f.toggle[f.val]
}

// formState is the active modal (zero kind = none). Fields carry all
// inputs; orig captures the entity being edited so renames of the name
// buffer cannot detach the target.
type formState struct {
	kind    modalKind
	orig    string
	fields  []field
	cur     int
	busy    bool
	err     string
	flash   string
	preview []string // fStdPreviewShow body
}

func textField(label, value string) field {
	return field{label: label, runes: []rune(value)}
}

func modeField(mode string) field {
	f := field{label: "mode ", toggle: []string{"extend", "replace"}}
	for i, v := range f.toggle {
		if v == mode {
			f.val = i
		}
	}
	return f
}

func (fs *formState) target() string { return fs.orig }

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

func (m *Model) sectionKey(key string) bool {
	switch key {
	case "[":
		m.sec = (m.sec - 1 + secCount) % secCount
		return true
	case "]":
		m.sec = (m.sec + 1) % secCount
		return true
	}

	switch m.sec {
	case secMembers:
		return m.membersKey(key)
	case secOrg:
		return m.orgKey(key)
	case secPacks:
		return m.packsKey(key)
	case secChannels:
		return m.pane.handleKey(key)
	default:
		return m.standardsKey(key)
	}
}

func (m *Model) moveCursor(n int) *int { return &m.cursors[m.sec] }

func clampCursor(c *int, n int) {
	if *c >= n {
		*c = maxInt(n-1, 0)
	}
	if *c < 0 {
		*c = 0
	}
}

func (m *Model) membersKey(key string) bool {
	members := m.ws.Members()
	c := &m.cursors[secMembers]
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
		m.form = formState{kind: fAdd, fields: []field{
			textField("name ", ""), textField("path ", ""),
		}}
		return true
	case "r", "enter":
		if len(members) > 0 {
			mem := members[*c]
			m.form = formState{kind: fRename, orig: mem.Name,
				fields: []field{textField("new  ", mem.Name)}}
		}
		return true
	case "d", "delete":
		if len(members) > 0 {
			m.form = formState{kind: fRemoveConfirm, orig: members[*c].Name}
		}
		return true
	}
	return false
}

func (m *Model) orgRows() (teams []org.Team, activeIDs, archivedIDs []string) {
	if m.org != nil {
		teams = m.org.Teams()
	}
	if roster, err := org.LoadRoster(m.ws); err == nil {
		for _, a := range roster {
			activeIDs = append(activeIDs, a.ID)
		}
	}
	if m.org != nil {
		archivedIDs = m.org.Archived(m.ws)
	}
	return teams, activeIDs, archivedIDs
}

// orgItemCount counts selectable rows (teams + crew incl. archived).
func (m *Model) orgItemCount() int {
	teams, active, archived := m.orgRows()
	return len(teams) + len(active) + len(archived)
}

func (m *Model) orgKey(key string) bool {
	c := &m.cursors[secOrg]
	clampCursor(c, m.orgItemCount())
	teams, active, archived := m.orgRows()

	move := func(n int) {
		if *c < n-1 {
			*c++
		}
	}
	up := func() {
		if *c > 0 {
			*c--
		}
	}
	teamAt := func() (org.Team, bool) {
		if *c < len(teams) {
			return teams[*c], true
		}
		return org.Team{}, false
	}
	activeAt := func() (string, bool) {
		idx := *c - len(teams)
		if idx >= 0 && idx < len(active) {
			return active[idx], true
		}
		return "", false
	}
	archivedAt := func() (string, bool) {
		idx := *c - len(teams) - len(active)
		if idx >= 0 && idx < len(archived) {
			return archived[idx], true
		}
		return "", false
	}

	switch key {
	case "j", "down":
		move(m.orgItemCount())
		return true
	case "k", "up":
		up()
		return true
	case "t":
		m.form = formState{kind: fTeamEdit, fields: []field{
			textField("team  ", ""), textField("lead  ", ""),
			textField("members (csv) ", ""),
		}}
		return true
	case "enter":
		if tm, ok := teamAt(); ok {
			m.form = formState{kind: fTeamEdit, orig: tm.Name, fields: []field{
				textField("team  ", tm.Name), textField("lead  ", tm.Lead),
				textField("members (csv) ", strings.Join(tm.Members, ",")),
			}}
			return true
		}
	case "x":
		if tm, ok := teamAt(); ok {
			m.form = formState{kind: fTeamDeleteConfirm, orig: tm.Name}
			return true
		}
		if id, ok := activeAt(); ok {
			m.form = formState{kind: fAgentArchiveConfirm, orig: id}
			return true
		}
	case "A":
		m.form = formState{kind: fAgentNew, fields: []field{
			textField("id    ", ""), textField("name  ", ""),
			textField("model ", ""), textField("system ", ""),
		}}
		return true
	case "R":
		if id, ok := archivedAt(); ok {
			if m.org != nil {
				if err := m.org.RestoreAgent(m.ws, id); err == nil {
					clampCursor(c, m.orgItemCount())
				} else {
					m.flashErr(err.Error())
				}
			}
			return true
		}
	}
	return false
}

func (m *Model) flashErr(msg string) {
	m.form = formState{kind: fNone, err: msg}
}

func (m *Model) packsKey(key string) bool {
	c := &m.cursors[secPacks]
	names, _ := m.installedNames()
	clampCursor(c, len(names))
	switch key {
	case "j", "down":
		if *c < len(names)-1 {
			*c++
		}
		return true
	case "k", "up":
		if *c > 0 {
			*c--
		}
		return true
	case "i", "a":
		m.form = formState{kind: fPackInstall, fields: []field{
			textField("source ", ""),
		}}
		return true
	case "x", "d":
		if *c < len(names) {
			m.form = formState{kind: fPackUninstallConfirm, orig: names[*c]}
			return true
		}
	}
	return false
}

func (m *Model) installedNames() ([]string, error) {
	if m.packs == nil {
		return nil, nil
	}
	return m.packs.Installed()
}

func (m *Model) standardsKey(key string) bool {
	rows := m.standardRows()
	c := &m.cursors[secStandards]
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
	case "w":
		snap, _ := standards.Inspect(m.ws.Root)
		m.form = formState{kind: fStdLayerEdit, orig: "@workspace",
			fields: []field{textField("rules (csv) ", strings.Join(snap.Workspace, ", "))}}
		return true
	case "t":
		if r := rows[*c]; r.kind == stdTeam {
			snap, _ := standards.Inspect(m.ws.Root)
			entries := snap.Teams[r.label]
			m.form = formState{kind: fStdLayerEdit, orig: "@team:" + r.label,
				fields: []field{textField("rules (csv) ", strings.Join(entries, ", "))}}
			return true
		}
	case "g":
		r := rows[*c]
		id := ""
		mode := standards.ModeExtend
		entries := []string(nil)
		switch r.kind {
		case stdAgent:
			id = r.label
			if ov, ok := m.agentOverride(id); ok {
				mode = ov.Mode
				entries = ov.Entries
			}
		case stdMember:
			id = r.label
		}
		m.form = formState{kind: fStdLayerEdit, orig: "@agent:" + id,
			fields: []field{
				textField("agent id ", id),
				modeField(mode),
				textField("rules (csv) ", strings.Join(entries, ", ")),
			}}
		return true
	case "v":
		r := rows[*c]
		id := ""
		if r.kind == stdAgent || r.kind == stdMember || r.kind == stdTeam {
			id = r.label
		}
		m.form = formState{kind: fStdPreviewPrompt, orig: id,
			fields: []field{textField("agent id ", id)}}
		return true
	}
	return false
}

func (m *Model) agentOverride(id string) (standards.AgentOverride, bool) {
	snap, err := standards.Inspect(m.ws.Root)
	if err != nil {
		return standards.AgentOverride{}, false
	}
	ov, ok := snap.Agents[id]
	return ov, ok
}

// standardRow is one selectable row of the standards section.
type stdRowKind uint8

const (
	stdWorkspace stdRowKind = iota
	stdTeam
	stdAgent
	stdMember
)

type stdRow struct {
	kind  stdRowKind
	label string
	count int
	mode  string // agent rows: extend|replace|"" (no layer yet)
}

func (m *Model) standardRows() []stdRow {
	snap, err := standards.Inspect(m.ws.Root)
	if err != nil {
		return []stdRow{{kind: stdWorkspace, label: "workspace", count: 0}}
	}
	rows := []stdRow{{kind: stdWorkspace, label: "workspace", count: len(snap.Workspace)}}
	if m.org != nil {
		for _, t := range m.org.Teams() {
			rows = append(rows, stdRow{kind: stdTeam, label: t.Name,
				count: len(snap.Teams[t.Name])})
		}
	}
	if roster, rerr := org.LoadRoster(m.ws); rerr == nil {
		for _, a := range roster {
			count := 0
			mode := ""
			if ov, ok := snap.Agents[a.ID]; ok {
				count = len(ov.Entries)
				mode = ov.Mode
			}
			rows = append(rows, stdRow{kind: stdAgent, label: a.ID,
				count: count, mode: mode})
		}
	}
	return rows
}

// ---- form keys & submission ----

func (m *Model) formKey(key string) bool {
	f := &m.form
	if f.busy {
		return true // swallow while async work runs
	}
	switch f.kind {
	case fRemoveConfirm, fTeamDeleteConfirm, fAgentArchiveConfirm, fPackUninstallConfirm:
		switch key {
		case "enter":
			m.submitConfirm()
			return true
		case "esc", "n":
			m.closeForm()
			return true
		}
		return true // confirm modals swallow everything else
	case fStdPreviewShow:
		m.closeForm()
		return true
	}

	switch key {
	case "esc":
		m.closeForm()
		return true
	case "enter":
		m.submitForm()
		return true
	case "tab":
		if len(f.fields) > 1 {
			f.cur = (f.cur + 1) % len(f.fields)
		}
		return true
	case "backspace":
		buf := &f.fields[f.cur].runes
		if len(*buf) > 0 {
			*buf = (*buf)[:len(*buf)-1]
		}
		return true
	case "left":
		if f.fields[f.cur].isToggle() {
			f.fields[f.cur].cycle(-1)
			return true
		}
	case "right":
		if f.fields[f.cur].isToggle() {
			f.fields[f.cur].cycle(1)
			return true
		}
	}
	if !f.fields[f.cur].isToggle() {
		if r := []rune(key); len(r) == 1 && r[0] >= 32 {
			f.fields[f.cur].runes = append(f.fields[f.cur].runes, r[0])
			return true
		}
	}
	return false
}

func (fl *field) isToggle() bool { return len(fl.toggle) > 0 }

func (m *Model) closeForm() {
	flash := m.form.flash
	m.form = formState{flash: flash}
}

func (m *Model) submitForm() {
	f := &m.form
	switch f.kind {
	case fAdd:
		name := strings.TrimSpace(f.fields[0].text())
		loc := strings.TrimSpace(f.fields[1].text())
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
		newName := strings.TrimSpace(f.fields[0].text())
		if newName == f.target() {
			m.closeForm()
			return
		}
		if err := m.ws.RenameMember(f.target(), newName); err != nil {
			f.err = err.Error()
			return
		}
		m.closeForm()
	case fTeamEdit:
		slug := strings.TrimSpace(f.fields[0].text())
		lead := strings.TrimSpace(f.fields[1].text())
		members := csv(f.fields[2].text())
		if slug == "" {
			f.err = "team name required"
			return
		}
		var err error
		if f.orig == "" {
			err = m.orgCreateTeam(slug, lead, members)
		} else {
			err = m.orgUpdateTeam(f.orig, lead, members)
		}
		if err != nil {
			f.err = err.Error()
			return
		}
		m.closeForm()
	case fAgentNew:
		id := strings.TrimSpace(f.fields[0].text())
		agent := manifestAgent(
			id,
			strings.TrimSpace(f.fields[1].text()),
			strings.TrimSpace(f.fields[2].text()),
			f.fields[3].text(),
		)
		if err := m.org.CreateAgent(m.ws, agent); err != nil {
			f.err = err.Error()
			return
		}
		m.closeForm()
	case fPackInstall:
		src := strings.TrimSpace(f.fields[0].text())
		if src == "" {
			f.err = "path or git URL required"
			return
		}
		f.busy = true
		f.err = ""
		go m.installPack(src)
	case fStdLayerEdit:
		switch {
		case strings.HasPrefix(f.orig, "@workspace"):
			if err := standards.Save(m.ws.Root,
				csv(f.fields[0].text()), nil, nil); err != nil {
				f.err = err.Error()
				return
			}
		case strings.HasPrefix(f.orig, "@team:"):
			slug := strings.TrimPrefix(f.orig, "@team:")
			slug = strings.TrimSpace(slug)
			if err := standards.Save(m.ws.Root, currentWorkspace(m.ws.Root),
				map[string][]string{slug: csv(f.fields[0].text())},
				currentAgents(m.ws.Root)); err != nil {
				f.err = err.Error()
				return
			}
		case strings.HasPrefix(f.orig, "@agent:"):
			id := strings.TrimSpace(f.fields[0].text())
			if err := workspace.ValidateName(id); err != nil {
				f.err = err.Error()
				return
			}
			mode := f.fields[1].toggleValue()
			entries := csv(f.fields[2].text())
			agents := currentAgents(m.ws.Root)
			if len(entries) == 0 {
				delete(agents, id)
			} else {
				agents[id] = standards.AgentOverride{Mode: mode, Entries: entries}
			}
			if err := standards.Save(m.ws.Root, currentWorkspace(m.ws.Root),
				currentTeams(m.ws.Root), agents); err != nil {
				f.err = err.Error()
				return
			}
		}
		m.closeForm()
	case fStdPreviewPrompt:
		id := strings.TrimSpace(f.fields[0].text())
		block := standards.Resolve(m.ws.Root, id, m.teamLookup())
		m.form.preview = strings.Split(block, "\n")
		m.form.kind = fStdPreviewShow
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
		c := &m.cursors[secMembers]
		clampCursor(c, len(m.ws.Members()))
		m.closeForm()
	case fTeamDeleteConfirm:
		if err := m.org.DeleteTeam(f.target()); err != nil {
			f.err = err.Error()
			return
		}
		clampCursor(&m.cursors[secOrg], m.orgItemCount())
		m.closeForm()
	case fAgentArchiveConfirm:
		if err := m.org.ArchiveAgent(m.ws, f.target()); err != nil {
			f.err = err.Error()
			return
		}
		clampCursor(&m.cursors[secOrg], m.orgItemCount())
		m.closeForm()
	case fPackUninstallConfirm:
		if err := m.packs.Uninstall(f.target()); err != nil {
			f.err = err.Error()
			return
		}
		names, _ := m.installedNames()
		clampCursor(&m.cursors[secPacks], len(names))
		m.closeForm()
	}
}

// read-modify-write helpers keep untouched layers intact when saving one.

func currentWorkspace(root string) []string {
	snap, err := standards.Inspect(root)
	if err != nil {
		return nil
	}
	return snap.Workspace
}

func currentTeams(root string) map[string][]string {
	out := map[string][]string{}
	if snap, err := standards.Inspect(root); err == nil {
		for slug, v := range snap.Teams {
			out[slug] = v
		}
	}
	return out
}

func currentAgents(root string) map[string]standards.AgentOverride {
	out := map[string]standards.AgentOverride{}
	if snap, err := standards.Inspect(root); err == nil {
		for id, ov := range snap.Agents {
			out[id] = ov
		}
	}
	return out
}

func (m *Model) orgCreateTeam(slug, lead string, members []string) error {
	if m.org == nil {
		return fmtErr("org registry unavailable: " + m.orgErr)
	}
	return m.org.CreateTeam(slug, lead, members)
}

func (m *Model) orgUpdateTeam(slug, lead string, members []string) error {
	if m.org == nil {
		return fmtErr("org registry unavailable: " + m.orgErr)
	}
	return m.org.UpdateTeam(slug, lead, members)
}

func (m *Model) teamLookup() standards.TeamLookup {
	if m.org == nil {
		return nil
	}
	return func(agentID string) []string { return m.org.TeamsOf(agentID) }
}

// async workers ----

func (m *Model) cloneAndRegister(name, url, dst string) {
	ctx, cancel := context.WithTimeout(context.Background(), cloneTimeout)
	defer cancel()
	if _, err := gitcore.Clone(ctx, url, dst); err != nil {
		os.RemoveAll(dst)
		m.send(wsEvent{kind: evCloneDone, err: err.Error()})
		return
	}
	if err := m.ws.AddMember(name, dst); err != nil {
		m.send(wsEvent{kind: evCloneDone, err: err.Error()})
		return
	}
	m.send(wsEvent{kind: evCloneDone})
}

func (m *Model) installPack(source string) {
	ctx, cancel := context.WithTimeout(context.Background(), installTimeout)
	defer cancel()
	res, err := m.packs.Install(ctx, source)
	if err != nil {
		m.send(wsEvent{kind: evInstallDone, err: err.Error()})
		return
	}
	m.send(wsEvent{kind: evInstallDone, packName: res.Pack, packAgents: res.Agents})
}

func isCloneSource(loc string) bool {
	for _, p := range []string{"http://", "https://", "git://", "ssh://", "git@"} {
		if strings.HasPrefix(loc, p) {
			return true
		}
	}
	return false
}
