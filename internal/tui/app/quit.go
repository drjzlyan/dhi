package app

import (
	"fmt"

	tea "charm.land/bubbletea/v2"

	"github.com/drjzlyan/dhi/internal/tui/kit"
	"github.com/drjzlyan/dhi/internal/tui/surfaces"
	"github.com/drjzlyan/dhi/internal/tui/theme"
)

// requestQuit quits at once when nothing is unsaved; otherwise it opens
// the quit guard (F-115), which lists the unsaved work and asks.
func (a *App) requestQuit() tea.Cmd {
	var names []string
	for _, s := range a.surfaces {
		if u, ok := s.(surfaces.Unsaved); ok {
			names = append(names, u.Unsaved()...)
		}
	}
	if len(names) == 0 {
		a.quitting = true
		a.saveSessions()
		return tea.Quit
	}
	a.palette, a.showHelp = nil, false
	a.quitAsk, a.quitErr = names, ""
	return nil
}

// canHotExit is whether any surface can keep its unsaved work (F-161).
func (a *App) canHotExit() bool {
	for _, s := range a.surfaces {
		if _, ok := s.(surfaces.HotExit); ok {
			return true
		}
	}
	return false
}

// quitKey answers the quit guard: s saves everything and quits, k keeps the edits in swap files for the
// next launch (F-161) and quits, d quits
// without saving, esc (or ctrl+c again) goes back to work.
func (a *App) quitKey(key string) tea.Cmd {
	switch key {
	case "s", "S", "enter":
		for _, s := range a.surfaces {
			if u, ok := s.(surfaces.Unsaved); ok {
				if err := u.SaveAll(); err != nil {
					a.quitErr = err.Error()
					return nil
				}
			}
		}
		a.quitAsk, a.quitting = nil, true
		a.saveSessions()
		return tea.Quit
	case "k", "K":
		if !a.canHotExit() {
			return nil
		}
		for _, s := range a.surfaces {
			if h, ok := s.(surfaces.HotExit); ok {
				if err := h.KeepForNextTime(); err != nil {
					a.quitErr = err.Error()
					return nil
				}
			}
		}
		a.quitAsk, a.quitting = nil, true
		a.saveSessions()
		return tea.Quit
	case "d", "D":
		for _, s := range a.surfaces {
			if h, ok := s.(surfaces.HotExit); ok {
				h.DiscardSwaps()
			}
		}
		a.quitAsk, a.quitting = nil, true
		a.saveSessions()
		return tea.Quit
	case "esc", "ctrl+c", "n", "N":
		a.quitAsk, a.quitErr = nil, ""
	}
	return nil
}

// quitView is the guard's dialog.
func (a *App) quitView() string {
	lines := []string{
		theme.TextStyle().Render(fmt.Sprintf("%s unsaved:", kit.Count(len(a.quitAsk), "file"))),
	}
	for i, n := range a.quitAsk {
		if i == 6 {
			lines = append(lines, theme.TextMuted().Render(fmt.Sprintf("  … and %d more", len(a.quitAsk)-i)))
			break
		}
		lines = append(lines, theme.WarningText().Render("  ● ")+theme.TextStyle().Render(n))
	}
	keys := theme.Keycap().Render("s") + theme.TextMuted().Render(" save all and quit   ")
	if a.canHotExit() {
		keys += theme.Keycap().Render("k") + theme.TextMuted().Render(" keep for next time   ")
	}
	keys += theme.Keycap().Render("d") + theme.TextMuted().Render(" quit without saving   ") +
		theme.Keycap().Render("esc") + theme.TextMuted().Render(" keep working")
	lines = append(lines, "", keys)
	md := kit.Modal{Title: "Quit DHI?", Lines: lines, Error: a.quitErr}
	return md.View()
}

// saveSessions lets each surface write the session it will reopen (F-149).
func (a *App) saveSessions() {
	for _, s := range a.surfaces {
		if ss, ok := s.(surfaces.SessionSaver); ok {
			ss.SaveSession()
		}
	}
}
