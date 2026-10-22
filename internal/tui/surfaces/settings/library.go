package settings

import (
	"fmt"
	"strings"

	"github.com/drjzlyan/dhi/internal/agentkit/library"
	"github.com/drjzlyan/dhi/internal/tui/kit"
	"github.com/drjzlyan/dhi/internal/tui/theme"
)

// The LIBRARY section (F-027 P2): browse the behaviour library —
// built-in + local roles and personas — author local cards, and delete
// them. Builtin cards are read-only by name. Skills have their own
// section (SKILLS, ADR-0043).

// libRow is one flat listing entry plus its kind for key dispatch.
type libRow struct {
	kind   string // "role" | "persona"
	slug   string
	source string // "builtin" | "local"
}

// libStore lazily snapshots the library (reopened after every write —
// the same reload discipline as the roster).
func (m *Model) libStore() *library.Store {
	if m.d.WS == nil {
		return nil
	}
	if m.lib == nil {
		if m.d.Library != nil {
			m.lib = m.d.Library
		} else {
			m.lib = library.Open(m.d.WS)
		}
	}
	return m.lib
}

// libRows snapshots the listing: roles group then personas group,
// sorted by slug inside each.
func (m *Model) libRows() []libRow {
	lib := m.libStore()
	if lib == nil {
		return nil
	}
	var rows []libRow
	for _, e := range lib.Roles() {
		rows = append(rows, libRow{kind: "role", slug: e.Slug, source: e.Source})
	}
	for _, e := range lib.Personas() {
		rows = append(rows, libRow{kind: "persona", slug: e.Slug, source: e.Source})
	}
	return rows
}

// libraryBody renders the grouped listing with source badges.
func (m *Model) libraryBody(w int) string {
	if m.d.WS == nil {
		return kit.Notice{Kind: kit.NoticeInfo, What: "The library needs a workspace",
			Why:   "roles and personas live in the workspace's .dhi folder.",
			Retry: "ctrl+p → Run setup wizard to create one here"}.String(w)
	}
	lib := m.libStore()
	rows := m.libRows()
	if len(rows) == 0 {
		out := theme.TextDim().Render("(empty — press n to author the first card)")
		for _, warn := range lib.Warnings() {
			out += "\n" + theme.DangerText().Render("⚠ "+warn)
		}
		return out
	}
	if m.libCur >= len(rows) {
		m.libCur = len(rows) - 1
	}
	var out []string
	lastKind := ""
	for i, r := range rows {
		if r.kind != lastKind {
			out = append(out, theme.TextMuted().Render(" "+strings.ToUpper(r.kind)+"S"))
			lastKind = r.kind
		}
		mark := "  "
		style := theme.TextDim()
		if i == m.libCur {
			mark = theme.GlyphCursor + " "
			style = theme.TabActive()
		}
		src := ""
		switch {
		case r.source == "builtin":
			src = theme.TextMuted().Render(" builtin")
		case r.source == "local":
			src = theme.TextMuted().Render(" local")
		}
		desc := ""
		if e, ok := m.libEntry(r); ok {
			desc = theme.Hint().Render(" " + kit.ClipEllipsis(e.Description, maxInt(w-32, 10)))
		}
		out = append(out, mark+style.Render(padTo(r.slug, 18))+src+desc)
	}
	for _, warn := range lib.Warnings() {
		out = append(out, theme.DangerText().Render("⚠ "+warn))
	}
	return strings.Join(out, "\n")
}

// libEntry resolves a listing row back to its card description.
func (m *Model) libEntry(r libRow) (library.Entry, bool) {
	lib := m.libStore()
	if lib == nil {
		return library.Entry{}, false
	}
	for _, e := range lib.Roles() {
		if r.kind == "role" && e.Slug == r.slug {
			return e, true
		}
	}
	for _, e := range lib.Personas() {
		if r.kind == "persona" && e.Slug == r.slug {
			return e, true
		}
	}
	return library.Entry{}, false
}

// libraryKey handles LIBRARY navigation and card authoring.
func (m *Model) libraryKey(key string) bool {
	rows := m.libRows()
	switch key {
	case "j", "down":
		if m.libCur < len(rows)-1 {
			m.libCur++
		}
		return true
	case "k", "up":
		if m.libCur > 0 {
			m.libCur--
		}
		return true
	case "n":
		m.openFormDialog("new role", dlgLibNew, "",
			kit.NewToggleField("kind  ", []string{"role"}, 0),
			kit.NewTextField("slug  ", ""),
			kit.NewTextField("desc  ", ""),
			kit.NewTextField("name  ", ""),
			kit.NewTextField("preset", ""),
			kit.NewTextField("tools ", ""),
			kit.NewTextField("body  ", ""),
		)
		return true
	case "p":
		m.openPersonaForm("")
		return true
	case "e":
		if m.libCur < len(rows) {
			r := rows[m.libCur]
			if r.kind == "persona" {
				m.openPersonaForm(r.slug)
				return true
			}
			if r.source != "local" {
				m.flash = "failed: builtin " + r.kind + "s are read-only — press n to author a local " + r.kind
				return true
			}
			m.openLibEdit(r)
			return true
		}
	case "x":
		if m.libCur < len(rows) && m.d.WS != nil {
			r := rows[m.libCur]
			if r.source != "local" {
				m.flash = "failed: builtin " + r.kind + "s are read-only"
				return true
			}
			m.openConfirmDialog("delete "+r.kind, "delete local "+r.kind+" "+r.slug+"?", r.kind+"/"+r.slug, dlgLibDelete)
			return true
		}
	case "enter", "v":
		if m.libCur < len(rows) {
			r := rows[m.libCur]
			m.openDisplayDialog(r.kind+" — "+r.slug, m.libCardLines(r))
			return true
		}
	}
	return false
}

// libCardLines renders the full card for the display dialog.
func (m *Model) libCardLines(r libRow) []string {
	lib := m.libStore()
	if r.kind == "role" {
		role, ok := lib.Role(r.slug)
		if !ok {
			return []string{theme.DangerText().Render("(card unavailable)")}
		}
		return []string{
			theme.TextDim().Render(role.Description),
			"",
			theme.Hint().Render("default tools: " + orDash(strings.Join(role.Tools, ", "))),
			theme.Hint().Render("policy preset: " + orDash(role.Preset)),
			"",
			role.System,
		}
	}
	if r.kind == "persona" {
		p, ok := lib.Persona(r.slug)
		if !ok {
			return []string{theme.DangerText().Render("(card unavailable)")}
		}
		return []string{
			theme.TextDim().Render(p.Description),
			"",
			theme.Hint().Render("tone: " + orDash(p.Tone) + " · verbosity: " + orDash(p.Verbosity)),
			"",
			p.Render(),
		}
	}
	return []string{theme.DangerText().Render("(card unavailable)")}
}

// openLibEdit prefills the authoring form from an existing local card.
func (m *Model) openLibEdit(r libRow) {
	lib := m.libStore()
	if r.kind == "role" {
		role, ok := lib.Role(r.slug)
		if !ok {
			return
		}
		m.openFormDialog("edit role "+r.slug, dlgLibEdit, "role/"+r.slug,
			kit.NewTextField("kind  ", "role"),
			kit.NewTextField("slug  ", r.slug),
			kit.NewTextField("desc  ", role.Description),
			kit.NewTextField("name  ", ""),
			kit.NewTextField("preset", role.Preset),
			kit.NewTextField("tools ", strings.Join(role.Tools, ", ")),
			kit.NewTextField("body  ", role.System),
		)
		return
	}
}

// submitLibrary creates or updates a local card through the store's
// strict round-trip; failures name the problem in the dialog.
func (m *Model) submitLibrary() {
	if m.d.WS == nil || m.libStore() == nil {
		m.closeDialog()
		m.flash = "failed: library unavailable — not inside a workspace"
		return
	}
	vals := m.dform.Values()
	kind, slug := strings.TrimSpace(vals[0]), strings.TrimSpace(vals[1])
	desc := strings.TrimSpace(vals[2])
	name := strings.TrimSpace(vals[3])
	preset := strings.TrimSpace(vals[4])
	var tools []string
	for _, t := range strings.Split(vals[5], ",") {
		if t = strings.TrimSpace(t); t != "" {
			tools = append(tools, t)
		}
	}
	body := vals[6]
	var err error
	switch kind {
	case "role":
		if name != "" {
			err = fmt.Errorf("name is a skill field — roles use desc/preset/tools/body")
			break
		}
		err = m.lib.WriteRole(m.d.WS, &library.Role{
			Slug: slug, Description: desc, Tools: tools, Preset: preset, System: body,
		})
	default:
		err = fmt.Errorf("skills are written in Settings → SKILLS (w)")
	}
	if err != nil {
		m.dform.SetError(err.Error())
		return
	}
	m.closeDialog()
	m.lib = library.Open(m.d.WS) // fresh snapshot
	m.flash = kind + " " + slug + " saved"
}

// submitLibraryDelete removes one local card.
func (m *Model) submitLibraryDelete(target string) {
	kind, slug := "", ""
	if i := strings.Index(target, "/"); i > 0 {
		kind, slug = target[:i], target[i+1:]
	}
	if m.d.WS == nil || m.libStore() == nil {
		m.closeDialog()
		m.flash = "failed: library unavailable — not inside a workspace"
		return
	}
	if err := m.lib.Delete(m.d.WS, kind, slug); err != nil {
		m.dlg.Lines = append(m.dlg.Lines[:2], theme.DangerText().Render(err.Error()))
		return
	}
	m.closeDialog()
	m.lib = library.Open(m.d.WS)
	m.flash = kind + " " + slug + " deleted"
}

// verbosities are the persona verbosity levels, in toggle order.
var verbosities = []string{library.VerbosityTerse, library.VerbosityBalanced, library.VerbosityThorough}

// openPersonaForm opens the persona authoring form: empty for a new card,
// prefilled from an existing one. Editing a builtin writes a LOCAL card with
// the same slug, which shadows the builtin — the builtin itself is never
// touched, and deleting the local card restores it.
func (m *Model) openPersonaForm(slug string) {
	if m.d.WS == nil || m.libStore() == nil {
		m.flash = "failed: library unavailable — not inside a workspace"
		return
	}
	title, target := "new persona", ""
	p := &library.Persona{Verbosity: library.VerbosityBalanced}
	if slug != "" {
		cur, ok := m.libStore().Persona(slug)
		if !ok {
			return
		}
		p, title, target = cur, "edit persona "+slug, "persona/"+slug
		if m.libStore().Source("persona", slug) != "local" {
			title = "customize persona " + slug + " (saves a local copy)"
		}
	}
	vi := 1
	for i, v := range verbosities {
		if v == p.Verbosity {
			vi = i
		}
	}
	m.openFormDialog(title, dlgPersona, target,
		kit.NewTextField("slug     ", slug),
		kit.NewTextField("desc     ", p.Description),
		kit.NewTextField("tone     ", p.Tone),
		kit.NewToggleField("verbosity", verbosities, vi),
		kit.NewTextField("traits   ", strings.Join(p.Traits, "; ")),
		kit.NewTextField("guidance ", p.Guidance),
	)
}

// submitPersona writes the persona card through the store's strict
// round-trip; the running crew picks it up on its next turn (the runtime
// re-reads the library when its files change).
func (m *Model) submitPersona() {
	if m.d.WS == nil || m.libStore() == nil {
		m.closeDialog()
		m.flash = "failed: library unavailable — not inside a workspace"
		return
	}
	vals := m.dform.Values()
	slug := strings.TrimSpace(vals[0])
	var traits []string
	for _, t := range strings.Split(vals[4], ";") {
		if t = strings.TrimSpace(t); t != "" {
			traits = append(traits, t)
		}
	}
	err := m.lib.WritePersona(m.d.WS, &library.Persona{
		Slug: slug, Description: strings.TrimSpace(vals[1]), Tone: strings.TrimSpace(vals[2]),
		Verbosity: strings.TrimSpace(vals[3]), Traits: traits, Guidance: strings.TrimSpace(vals[5]),
	})
	if err != nil {
		m.dform.SetError(err.Error())
		return
	}
	m.closeDialog()
	m.lib = library.Open(m.d.WS) // fresh snapshot
	m.flash = "persona " + slug + " saved — agents use it from their next turn"
}
