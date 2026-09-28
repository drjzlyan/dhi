package settings

import (
	"fmt"
	"strings"

	"github.com/drjzlyan/dhi/internal/agentkit/manifest"
	"github.com/drjzlyan/dhi/internal/agentkit/org"
	"github.com/drjzlyan/dhi/internal/agentkit/pack"
	"github.com/drjzlyan/dhi/internal/tui/kit"
	"github.com/drjzlyan/dhi/internal/tui/theme"
	"github.com/drjzlyan/dhi/internal/workspace"
)

// Dialog state (F-023/F-024): every management action rides one
// kit.Modal, optionally hosting a kit.Form. dkind selects the submit
// branch; dtarget names the entity being edited or confirmed.
type dialogKind uint8

const (
	dlgTeam dialogKind = iota
	dlgTeamDelete
	dlgPackInstall
	dlgPackUninstall
	dlgStdLayer
	dlgStdPreview
	dlgWorkflowNew
	dlgAutoNew
	dlgAutoDelete
	dlgLibNew
	dlgLibEdit
	dlgLibDelete
	dlgRegistrySource
	dlgRegistryInstall
	dlgDisplay // read-only: profile, standards preview, last-run info
)

func (m *Model) openFormDialog(title string, kind dialogKind, target string, fields ...kit.Field) {
	m.dform = kit.NewForm(title, fields...)
	m.dlg = &kit.Modal{Title: title}
	m.dkind = kind
	m.dtarget = target
}

func (m *Model) openConfirmDialog(title, line, target string, kind dialogKind) {
	m.dform = nil
	m.dlg = &kit.Modal{Title: title, Lines: []string{
		line,
		"",
		theme.Hint().Render("enter confirm · esc/n cancel"),
	}}
	m.dkind = kind
	m.dtarget = target
}

func (m *Model) openDisplayDialog(title string, lines []string) {
	m.dform = nil
	// Display dialogs scroll: profiles and last-run dumps overflow any
	// fixed height (F-026 P6 — silent 24-line truncation dies).
	m.dlg = &kit.Modal{Title: title, Lines: lines, Scrollable: true,
		Height: minInt(m.height-6, 30), Width: maxInt(m.width/2, 46)}
	m.dkind = dlgDisplay
	m.dtarget = ""
}

func minInt(a, b int) int {
	if a < b {
		return a
	}
	return b
}

func (m *Model) closeDialog() {
	m.dlg, m.dform, m.dkind, m.dtarget = nil, nil, 0, ""
}

// dialogKey implements the focus trap: while a dialog is open every
// key is consumed. Forms use the canonical kit.Form contract.
func (m *Model) dialogKey(key string) {
	if m.dform != nil {
		switch m.dform.HandleKey(key) {
		case kit.FormSubmit:
			m.submitDialog()
		case kit.FormCancel:
			m.closeDialog()
		}
		return
	}
	switch key {
	case "enter":
		m.submitConfirmDialog()
	case "esc", "n", "q":
		m.closeDialog()
	}
}

func (m *Model) submitDialog() {
	if m.dform == nil {
		return
	}
	switch m.dkind {
	case dlgTeam:
		m.submitTeam()
	case dlgPackInstall:
		m.submitPackInstall()
	case dlgStdLayer:
		m.submitStdLayer()
	case dlgStdPreview:
		m.showStdPreview()
	case dlgWorkflowNew:
		m.submitWorkflowNew()
	case dlgAutoNew:
		m.submitAutoNew()
	case dlgLibNew, dlgLibEdit:
		m.submitLibrary()
	case dlgRegistrySource:
		m.submitRegistrySource()
	}
}

func (m *Model) submitConfirmDialog() {
	target := m.dtarget
	switch m.dkind {
	case dlgTeamDelete:
		if err := m.d.Org.DeleteTeam(target); err != nil {
			m.dlg.Lines = append(m.dlg.Lines[:2], theme.DangerText().Render(err.Error()))
			return
		}
		m.closeDialog()
		m.flash = "team " + target + " deleted"
	case dlgPackUninstall:
		if m.d.WS == nil {
			m.closeDialog()
			m.flash = "failed: pack management unavailable — not inside a workspace"
			return
		}
		if err := (&pack.Installer{WS: m.d.WS}).Uninstall(target); err != nil {
			m.dlg.Lines = append(m.dlg.Lines[:2], theme.DangerText().Render(err.Error()))
			return
		}
		m.closeDialog()
		m.flash = "pack " + target + " uninstalled"
	case dlgLibDelete:
		m.submitLibraryDelete(target)
	case dlgRegistryInstall:
		m.submitRegistryInstall(target)
	case dlgAutoDelete:
		if m.d.Autopilots == nil {
			m.closeDialog()
			m.flash = "failed: autopilot store unavailable"
			return
		}
		if err := m.d.Autopilots.Remove(target); err != nil {
			m.dlg.Lines = append(m.dlg.Lines[:2], theme.DangerText().Render(err.Error()))
			return
		}
		m.closeDialog()
		m.flash = "autopilot " + target + " removed"
	default:
		m.closeDialog()
	}
}

// ---- shared helpers ----

// orgLoadRoster reads the active roster from disk (the identity store).
func orgLoadRoster(ws *workspace.Workspace) ([]*manifest.Agent, error) {
	return org.LoadRoster(ws)
}

// rosterIDs loads the active roster ids from disk; a read failure
// degrades to an empty set — forms stay usable, the roster rows just
// list fewer choices.
func (m *Model) rosterIDs() []string {
	if m.d.WS == nil {
		return nil
	}
	roster, err := orgLoadRoster(m.d.WS)
	if err != nil {
		return nil
	}
	ids := make([]string, 0, len(roster))
	for _, a := range roster {
		ids = append(ids, a.ID)
	}
	return ids
}

// agentOnRoster refuses run-nows for dangling cards by name (ADR-0011).
func (m *Model) agentOnRoster(id string) (bool, error) {
	if m.d.WS == nil {
		return false, fmt.Errorf("not inside a workspace")
	}
	roster, err := orgLoadRoster(m.d.WS)
	if err != nil {
		return false, fmt.Errorf("roster unavailable: %w", err)
	}
	for _, a := range roster {
		if a.ID == id {
			return true, nil
		}
	}
	return false, nil
}

func csvSplit(s string) []string {
	var out []string
	for _, part := range strings.Split(s, ",") {
		if p := strings.TrimSpace(part); p != "" {
			out = append(out, p)
		}
	}
	return out
}
