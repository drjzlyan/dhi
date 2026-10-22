package settings

import (
	"github.com/drjzlyan/dhi/internal/tui/kit"
	"github.com/drjzlyan/dhi/internal/tui/theme"
)

// SETUP (F-108): every onboarding step, done or not, a keystroke away —
// re-run one to change what it set up (it opens with your current
// values), or reset them all and walk the setup again.

func (m *Model) setupView(w int) []string {
	out := []string{theme.TextDim().Render("Change anything you set up: enter re-runs a step with what you have now"), ""}
	if m.d.Setup == nil {
		return append(out, theme.TextDim().Render("(setup is not available here)"))
	}
	items := m.d.Setup()
	if len(items) == 0 {
		return append(out, theme.TextDim().Render("(no setup steps apply here)"))
	}
	m.setupCur = min(m.setupCur, len(items)-1)
	cl := kit.Checklist{Items: items, Cursor: m.setupCur, Focus: true, ActionKey: "⏎ re-run"}
	out = append(out, cl.View(w)...)
	return append(out, "", theme.Hint().Render("R resets every step's record (your settings stay) and starts the setup again"))
}

func (m *Model) setupKey(key string) bool {
	if m.d.Setup == nil {
		return false
	}
	items := m.d.Setup()
	switch key {
	case "j", "down":
		if m.setupCur < len(items)-1 {
			m.setupCur++
		}
	case "k", "up":
		if m.setupCur > 0 {
			m.setupCur--
		}
	case "enter", "e":
		if m.setupCur < len(items) && m.d.RunSetup != nil {
			m.d.RunSetup(items[m.setupCur].ID)
		}
	case "R":
		if m.d.ResetSetup != nil {
			m.openConfirmDialog("set up again",
				"start the setup again from the first step? Your settings, team and integrations stay; each step opens with them.", "", dlgSetupReset)
		}
	default:
		return false
	}
	return true
}

func (m *Model) resetSetupConfirmed() {
	m.closeDialog()
	if err := m.d.ResetSetup(); err != nil {
		m.flash = "failed: " + err.Error()
		return
	}
	m.flash = "setup restarted"
}

// GoToSection selects the section labelled label ("SETUP").
func (m *Model) GoToSection(label string) bool {
	for s := sectionID(0); s < secCount; s++ {
		if s.label() == label {
			m.sec = s
			return true
		}
	}
	return false
}
