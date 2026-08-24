// Package workspace is DHI's landing view: the company of agents —
// member repos, organization, channels, tasks, and inspection. P1 ships
// live member management (add/rename/remove without restart); later M4
// phases fill the remaining sections (P2 org, P3 channels, P4 tasks,
// P5 inspection).
package workspace

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"time"

	"charm.land/bubbletea/v2"

	"github.com/drjzlyan/dhi/internal/ansi"
	"github.com/drjzlyan/dhi/internal/gitcore"
	"github.com/drjzlyan/dhi/internal/tui/branding"
	"github.com/drjzlyan/dhi/internal/tui/kit"
	"github.com/drjzlyan/dhi/internal/tui/surfaces"
	"github.com/drjzlyan/dhi/internal/tui/theme"
	"github.com/drjzlyan/dhi/internal/workspace"
)

// cloneTimeout bounds an add-by-URL clone; the UI stays responsive via
// the event pump either way.
const cloneTimeout = 5 * time.Minute

// Model is the Workspace landing surface.
type Model struct {
	version string
	ws      *workspace.Workspace
	width   int
	height  int
	cursor  int

	form formState

	events    chan wsEvent
	cancelSub func()
}

var _ surfaces.Surface = (*Model)(nil)

type wsEvent struct {
	cloneErr string // add-by-URL result; "" on success
	ping     bool   // roster changed elsewhere
}

// New returns the workspace model. A nil ws renders the not-a-workspace
// empty state (all keys inert).
func New(version string, ws *workspace.Workspace) *Model {
	return &Model{
		version: version,
		ws:      ws,
		events:  make(chan wsEvent, 16),
	}
}

func (m *Model) Meta() surfaces.Meta { return surfaces.Meta{ID: "workspace", Title: "Workspace"} }

// Init starts the roster-change pump for re-render triggers.
func (m *Model) Init() tea.Cmd {
	if m.ws == nil {
		return nil
	}
	ch, cancel := m.ws.Subscribe()
	m.cancelSub = cancel
	go func() {
		for range ch {
			select {
			case m.events <- wsEvent{ping: true}:
			default:
			}
		}
	}()
	return m.listen()
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

// Update handles async events: roster pings re-render; clone results
// register the new member or surface the error in the form.
func (m *Model) Update(msg tea.Msg) tea.Cmd {
	switch msg := msg.(type) {
	case wsEvent:
		if !msg.ping && m.form.busy { // clone result (success or failure)
			m.form.busy = false
			if msg.cloneErr != "" {
				m.form.err = msg.cloneErr
			} else {
				m.form = formState{} // success closes the modal
			}
		}
		return m.listen()
	}
	return nil
}

// modalKind enumerates the overlay states.
type modalKind uint8

const (
	fNone modalKind = iota
	fAdd
	fRename
	fRemoveConfirm
)

// formState is the active modal (zero kind = none).
type formState struct {
	kind  modalKind
	orig  string // member the modal operates on (rename/remove)
	name  []rune // add/new-name input
	path  []rune // add input: local dir or git URL
	field int    // 0 name, 1 path (add only)
	busy  bool   // async clone running
	err   string
}

func (f *formState) target() string { return f.orig }

func (m *Model) HandleKey(key string) bool {
	if m.ws == nil {
		return false
	}
	if m.form.kind != fNone {
		return m.formKey(key)
	}

	members := m.ws.Members()
	switch key {
	case "j", "down":
		if m.cursor < len(members)-1 {
			m.cursor++
		}
		return true
	case "k", "up":
		if m.cursor > 0 {
			m.cursor--
		}
		return true
	case "a", "n":
		m.form = formState{kind: fAdd}
		return true
	case "r", "enter":
		if m.cursor < len(members) {
			m.form = formState{kind: fRename, orig: members[m.cursor].Name,
				name: []rune(members[m.cursor].Name)}
		}
		return true
	case "d", "delete":
		if m.cursor < len(members) {
			m.form = formState{kind: fRemoveConfirm, orig: members[m.cursor].Name,
				name: []rune(members[m.cursor].Name)}
		}
		return true
	}
	return false
}

// formKey routes input while a modal is open.
func (m *Model) formKey(key string) bool {
	f := &m.form
	if f.busy {
		return true // swallow everything while the clone runs
	}
	switch key {
	case "esc":
		m.form = formState{}
		return true
	case "enter":
		m.submitForm()
		return true
	case "tab":
		if f.kind == fAdd {
			f.field = (f.field + 1) % 2
		}
		return true
	case "backspace":
		buf := f.currentTargetBuf()
		if len(*buf) > 0 {
			*buf = (*buf)[:len(*buf)-1]
		}
		return true
	}
	if r := []rune(key); len(r) == 1 && r[0] >= 32 {
		buf := f.currentTargetBuf()
		*buf = append(*buf, r[0])
		return true
	}
	return false
}

func (f *formState) currentTargetBuf() *[]rune {
	if f.kind == fAdd && f.field == 1 {
		return &f.path
	}
	return &f.name
}

// submitForm applies the pending mutation; failures keep the modal open
// with the error inline.
func (m *Model) submitForm() {
	f := &m.form
	switch f.kind {
	case fAdd:
		name := strings.TrimSpace(string(f.name))
		loc := strings.TrimSpace(string(f.path))
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
		m.form = formState{}
	case fRename:
		newName := strings.TrimSpace(string(f.name))
		if newName == f.target() {
			m.form = formState{}
			return
		}
		if err := m.ws.RenameMember(f.target(), newName); err != nil {
			f.err = err.Error()
			return
		}
		m.form = formState{}
	case fRemoveConfirm:
		if err := m.ws.RemoveMember(f.target()); err != nil {
			f.err = err.Error()
			return
		}
		if m.cursor >= len(m.ws.Members())-1 && m.cursor > 0 {
			m.cursor--
		}
		m.form = formState{}
	}
}

// isCloneSource reports whether loc is a git URL rather than a local
// directory path. SSH-style scp syntax counts; bare host/path does not.
func isCloneSource(loc string) bool {
	for _, p := range []string{"http://", "https://", "git://", "ssh://", "git@"} {
		if strings.HasPrefix(loc, p) {
			return true
		}
	}
	return false
}

// cloneAndRegister runs off the UI thread: clone URL into <root>/<name>,
// then register it through the normal AddMember path so persistence and
// notifications stay single-sourced.
func (m *Model) cloneAndRegister(name, url, dst string) {
	ctx, cancel := context.WithTimeout(context.Background(), cloneTimeout)
	defer cancel()
	if _, err := gitcore.Clone(ctx, url, dst); err != nil {
		os.RemoveAll(dst) // never leave half-clones behind
		select {
		case m.events <- wsEvent{cloneErr: err.Error()}:
		default:
		}
		return
	}
	var ev wsEvent
	if err := m.ws.AddMember(name, dst); err != nil {
		ev = wsEvent{cloneErr: err.Error()}
	}
	select {
	case m.events <- ev:
	default:
	}
}

// View renders the operations floor: members panel + roadmap sections,
// or the brand hero when not inside a DHI workspace.
func (m *Model) View() string {
	if m.ws == nil {
		return m.heroView("not inside a DHI workspace")
	}

	body := m.membersSection() + "\n" + upcomingSections()

	if m.form.kind != fNone {
		body = m.modalView(body)
	}
	return kit.Center(body, maxInt(m.width, 40), maxInt(m.height, 10))
}

func (m *Model) heroView(hint string) string {
	lines := strings.Split(branding.HeroBlock(m.version), "\n")
	lines = append(lines, "", theme.Hint().Render(hint))
	return kit.Center(strings.Join(lines, "\n"), maxInt(m.width, 40), maxInt(m.height, 10))
}

const memberColWidth = 12

func (m *Model) membersSection() string {
	members := m.ws.Members()
	var rows []string
	rows = append(rows, theme.Brand().Render("DHI "+m.version)+"  "+
		theme.TextDim().Render("company of agents"))
	rows = append(rows, "")
	if len(members) == 0 {
		rows = append(rows, theme.TextDim().Render("no member repos — press a to add one"))
	}
	for i, mem := range members {
		glyph := "  "
		style := theme.TextDim()
		if i == m.cursor {
			glyph = string(theme.GlyphCursor) + " "
			style = theme.TabActive()
		}
		rows = append(rows, glyph+style.Render(padTo(mem.Name, memberColWidth))+
			theme.Hint().Render(shorten(mem.Path, 44)))
	}
	rows = append(rows, "")
	rows = append(rows, theme.Hint().Render(
		"a add · r rename · d remove · j/k move"))
	return strings.Join(rows, "\n")
}

// upcomingSections keeps the remaining M4 workstreams visible without
// pretending they exist yet.
func upcomingSections() string {
	lines := []string{
		capLine("org", "teams · marketplace packs", "P2"),
		capLine("channels", "#general · DMs · threads", "P3"),
		capLine("tasks", "kanban · ChangeSets", "P4"),
		capLine("inspect", "memory · knowledge · activity", "P5"),
	}
	out := make([]string, 0, len(lines)+1)
	out = append(out, theme.TextDim().Render(strings.Repeat("─", 46)))
	for _, l := range lines {
		out = append(out, theme.TextDim().Render(l))
	}
	return strings.Join(out, "\n")
}

// modalView overlays the active form centered above a dimmed body.
func (m *Model) modalView(body string) string {
	f := &m.form
	p := kit.NewPanel(modalTitle(f.kind), true)

	switch f.kind {
	case fAdd:
		p.SetContent(
			inputRow("name ", string(f.name), f.field == 0),
			inputRow("path ", string(f.path), f.field == 1),
			"",
			hintOrErr(f, "local dir or git URL · tab next field"),
		)
	case fRename:
		p.SetContent(
			theme.TextDim().Render("rename "+f.target()),
			inputRow("new  ", string(f.name), true),
			"",
			hintOrErr(f, "enter rename · esc cancel"),
		)
	case fRemoveConfirm:
		target := f.target()
		if _, ok := m.ws.Member(target); ok && len(m.ws.Members()) <= 1 {
			p.SetContent(
				theme.DangerText().Render("cannot remove the last member"),
				"",
				theme.Hint().Render("esc close"))
		} else {
			p.SetContent(
				"remove "+theme.TabActive().Render(target)+"?",
				theme.TextDim().Render("unregisters the repo; the working tree"),
				theme.TextDim().Render("on disk is never deleted."),
				"",
				hintOrErr(f, "enter confirm removal · esc keep"),
			)
		}
	}

	overlay := p.View()
	blanked := dimLines(body)
	return stackOver(blanked, overlay)
}

func modalTitle(k modalKind) string {
	switch k {
	case fAdd:
		return "add member"
	case fRename:
		return "rename member"
	case fRemoveConfirm:
		return "remove member"
	}
	return ""
}

func hintOrErr(f *formState, hint string) string {
	switch {
	case f.busy:
		return theme.TabActive().Render("cloning… (esc aborts view only)")
	case f.err != "":
		return theme.DangerText().Render(f.err)
	default:
		return theme.Hint().Render(hint)
	}
}

func inputRow(label, value string, focused bool) string {
	cursor := " "
	style := theme.Hint()
	if focused {
		cursor = string(theme.GlyphCursor)
		style = theme.SuccessText()
	}
	return cursor + " " + style.Render(padTo(label, 6)) + value + "▏"
}

func dimLines(s string) string {
	var out []string
	for _, l := range strings.Split(s, "\n") {
		out = append(out, theme.TextDim().Render(l))
	}
	return strings.Join(out, "\n")
}

// stackOver places overlay on top of body, centered, replacing the
// covered region line-by-line so total geometry stays fixed.
func stackOver(body, overlay string) string {
	bl := strings.Split(body, "\n")
	ol := strings.Split(overlay, "\n")
	vOffset := (len(bl) - len(ol)) / 2
	if vOffset < 0 {
		vOffset = 0
	}
	for i, line := range ol {
		y := vOffset + i
		if y >= len(bl) {
			break
		}
		bl[y] = line
	}
	return strings.Join(bl, "\n")
}

func hcenter(line string, axis int) string {
	gap := axis - visibleWidth(line)
	if gap <= 0 {
		return line
	}
	return strings.Repeat(" ", gap/2) + line
}

func visibleWidth(s string) int { return len([]rune(ansi.Strip(s))) }

func shorten(p string, n int) string {
	if len(p) <= n {
		return p
	}
	return "…" + p[len(p)-n+1:]
}

func padTo(s string, w int) string {
	if rn := len([]rune(s)); rn < w {
		return s + strings.Repeat(" ", w-rn)
	}
	return s
}

func capLine(name, desc, milestone string) string {
	return "  " + theme.TabActive().Render(padTo(name, 10)) +
		theme.TextDim().Render(desc) +
		"  " + theme.Hint().Render(milestone)
}

func maxInt(a, b int) int {
	if a > b {
		return a
	}
	return b
}
