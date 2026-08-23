// Package app implements the DHI shell: tab navigation, focus routing,
// global keybindings, help overlay, and the statusline.
package app

import (
	"fmt"
	"strconv"
	"strings"

	"charm.land/bubbletea/v2"

	"github.com/drjzlyan/dhi/internal/tui/kit"
	"github.com/drjzlyan/dhi/internal/tui/surfaces"
	"github.com/drjzlyan/dhi/internal/tui/theme"
)

// Gate is a full-body takeover shown before normal surfaces (first-run
// bootstrap). While the gate is active it owns Update/View; global keys
// still quit, and once Finished() reports true the shell resumes
// ordinary routing permanently.
type Gate interface {
	Init() tea.Cmd
	Resize(width, height int)
	Update(tea.Msg) tea.Cmd
	View() string
	Finished() bool
}

// App is the root Bubble Tea model.
type App struct {
	version  string
	surfaces []surfaces.Surface
	active   int
	tabs     *kit.Tabs
	status   *kit.StatusLine

	gate    Gate
	gateRan bool

	width, height int
	showHelp      bool
	quitting      bool
}

// New wires the shell around an ordered surface registry (index i answers
// key strconv.Itoa(i+1)).
func New(version string, regs ...surfaces.Surface) *App {
	a := &App{version: version, surfaces: regs}
	pairs := make([][2]string, len(regs))
	for i, s := range regs {
		pairs[i] = [2]string{s.Meta().ID, s.Meta().Title}
	}
	a.tabs = kit.NewTabs(pairs...)
	a.status = kit.DefaultStatusLine(regs[0].Meta().Title)
	return a
}

// Active returns the focused surface.
func (a *App) Active() surfaces.Surface { return a.surfaces[a.active] }

// SetGate installs a first-run gate (e.g. toolchain bootstrap). Call
// before Init.
func (a *App) SetGate(g Gate) { a.gate = g }

// gateActive reports whether the gate currently owns the body.
func (a *App) gateActive() bool { return a.gate != nil && !a.gateRan }

// Init satisfies tea.Model; every surface gets its Init cmd.
func (a *App) Init() tea.Cmd {
	var cmds []tea.Cmd
	if a.gate != nil {
		cmds = append(cmds, a.gate.Init())
	}
	for _, s := range a.surfaces {
		if c := s.Init(); c != nil {
			cmds = append(cmds, c)
		}
	}
	if len(cmds) == 0 {
		return nil
	}
	return tea.Batch(cmds...)
}

// Update routes messages: gate → global keys → surface keys; broadcast
// resizes.
func (a *App) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		a.width, a.height = msg.Width, msg.Height
		for _, s := range a.surfaces {
			s.Resize(a.bodyWidth(), a.bodyHeight())
		}
		if a.gate != nil {
			a.gate.Resize(a.bodyWidth(), a.bodyHeight())
		}
		return a, nil

	case tea.KeyPressMsg:
		if a.gateActive() {
			switch msg.String() {
			case "ctrl+c", "ctrl+q":
				a.quitting = true
				return a, tea.Quit
			}
			return a, nil // gate owns all other input
		}
		if cmd, handled := a.handleGlobal(msg.String()); handled {
			return a, cmd
		}
		a.Active().HandleKey(msg.String())
		return a, nil

	default:
		if a.gateActive() {
			cmd := a.gate.Update(msg)
			if a.gate.Finished() {
				a.gateRan = true
			}
			return a, cmd
		}
		return a, a.Active().Update(msg)
	}
}

func (a *App) handleGlobal(key string) (tea.Cmd, bool) {
	switch key {
	case "ctrl+c", "ctrl+q":
		a.quitting = true
		return tea.Quit, true
	case "?":
		a.showHelp = !a.showHelp
		return nil, true
	case "tab":
		a.selectSurface((a.active + 1) % len(a.surfaces))
		return nil, true
	case "shift+tab":
		a.selectSurface((a.active - 1 + len(a.surfaces)) % len(a.surfaces))
		return nil, true
	case "1", "2", "3", "4", "5", "6", "7", "8", "9":
		n, _ := strconv.Atoi(key)
		if n <= len(a.surfaces) {
			a.selectSurface(n - 1)
			return nil, true
		}
	}
	return nil, false
}

func (a *App) selectSurface(i int) {
	if a.tabs.SetActive(i) {
		a.active = i
		a.status = kit.DefaultStatusLine(a.Active().Meta().Title)
	}
}

func (a *App) bodyWidth() int { return a.width }
func (a *App) bodyHeight() int {
	h := a.height - theme.Current.HeightTab - theme.Current.HeightState
	if h < 3 {
		h = 3
	}
	return h
}

// View composes tab bar + active surface + optional help overlay + statusline.
func (a *App) View() tea.View {
	v := tea.NewView(a.compose())
	v.AltScreen = true
	v.BackgroundColor = theme.Current.Bg
	return v
}

func (a *App) compose() string {
	a.tabs.Width = a.width
	bar := a.tabs.View()

	a.status.Center = ""
	a.status.Width = a.width
	status := a.status.View()

	if a.gateActive() {
		return bar + "\n" + a.gate.View() + "\n" + status
	}

	body := a.Active().View()
	out := bar + "\n" + body + "\n" + status
	if a.showHelp {
		out = a.tabs.View() + "\n" + kit.Center(a.helpView(), a.width, a.bodyHeight()) + "\n" + status
	}
	return out
}

func (a *App) helpView() string {
	rows := [][2]string{
		{"1-5", "jump between views"},
		{"tab / shift+tab", "cycle views"},
		{"?", "toggle this help"},
		{"ctrl+c", "quit DHI"},
	}
	lines := []string{theme.Brand().Render("DHI — global keys"), ""}
	for _, r := range rows {
		lines = append(lines, "  "+theme.TabActive().Render(padKey(r[0]))+"  "+theme.TextDim().Render(r[1]))
	}
	return theme.HelpOverlay().Render(strings.Join(lines, "\n"))
}

func padKey(k string) string { return fmt.Sprintf("%-16s", k) }
