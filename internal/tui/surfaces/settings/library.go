package settings

import (
	"fmt"
	"strings"

	"github.com/drjzlyan/dhi/internal/agentkit/library"
	"github.com/drjzlyan/dhi/internal/agentkit/pack"
	"github.com/drjzlyan/dhi/internal/tui/kit"
	"github.com/drjzlyan/dhi/internal/tui/theme"
)

// The LIBRARY section (F-027 P2): browse the behaviour library —
// built-in + local roles and skills — author local cards, and delete
// them. Builtin cards are read-only by name.

// libRow is one flat listing entry plus its kind for key dispatch.
type libRow struct {
	kind   string // "role" | "skill"
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

// libRows snapshots the listing: roles group then skills group,
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
	for _, e := range lib.Skills() {
		rows = append(rows, libRow{kind: "skill", slug: e.Slug, source: e.Source})
	}
	return rows
}

// libPackBadges maps "role/slug" and "skill/slug" to the pack that
// installed the card (marketplace provenance), so the LIBRARY listing
// can badge pack-sourced entries.
func (m *Model) libPackBadges() map[string]string {
	if m.d.WS == nil {
		return nil
	}
	recs, err := (&pack.Installer{WS: m.d.WS}).Records()
	if err != nil {
		return nil
	}
	out := map[string]string{}
	for name, rec := range recs {
		for _, s := range rec.Roles {
			out["role/"+s] = name
		}
		for _, s := range rec.Skills {
			out["skill/"+s] = name
		}
	}
	return out
}

// libraryBody renders the grouped listing with source badges.
func (m *Model) libraryBody(w int) string {
	if m.d.WS == nil {
		return theme.TextDim().Render("(library unavailable — not inside a workspace)")
	}
	lib := m.libStore()
	rows := m.libRows()
	badges := m.libPackBadges()
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
		case badges[r.kind+"/"+r.slug] != "":
			src = theme.TextMuted().Render(" pack:" + kit.ClipEllipsis(badges[r.kind+"/"+r.slug], 16))
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
	for _, e := range lib.Skills() {
		if r.kind == "skill" && e.Slug == r.slug {
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
		m.openFormDialog("new library card", dlgLibNew, "",
			kit.NewToggleField("kind  ", []string{"role", "skill"}, 0),
			kit.NewTextField("slug  ", ""),
			kit.NewTextField("desc  ", ""),
			kit.NewTextField("name  ", ""),
			kit.NewTextField("preset", ""),
			kit.NewTextField("tools ", ""),
			kit.NewTextField("body  ", ""),
		)
		return true
	case "e":
		if m.libCur < len(rows) {
			r := rows[m.libCur]
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
	skill, ok := lib.Skill(r.slug)
	if !ok {
		return []string{theme.DangerText().Render("(card unavailable)")}
	}
	return []string{
		theme.TextDim().Render(skill.Name + " — " + skill.Description),
		"",
		skill.Body,
	}
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
	skill, ok := lib.Skill(r.slug)
	if !ok {
		return
	}
	m.openFormDialog("edit skill "+r.slug, dlgLibEdit, "skill/"+r.slug,
		kit.NewTextField("kind  ", "skill"),
		kit.NewTextField("slug  ", r.slug),
		kit.NewTextField("desc  ", skill.Description),
		kit.NewTextField("name  ", skill.Name),
		kit.NewTextField("preset", ""),
		kit.NewTextField("tools ", ""),
		kit.NewTextField("body  ", skill.Body),
	)
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
	case "skill":
		if preset != "" || len(tools) > 0 {
			err = fmt.Errorf("preset/tools are role fields — skills are instruction docs")
			break
		}
		err = m.lib.WriteSkill(m.d.WS, &library.Skill{
			Slug: slug, Name: name, Description: desc, Body: body,
		})
	default:
		err = fmt.Errorf("kind must be role or skill")
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
