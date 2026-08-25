// Package settings is DHI's Settings view: keyboard-navigable rows over
// the typed config schema with live application (theme swap) and
// automatic persistence to the nearest config file.
package settings

import (
	"charm.land/bubbletea/v2"

	"github.com/drjzlyan/dhi/internal/settings"
	"github.com/drjzlyan/dhi/internal/tui/kit"
	"github.com/drjzlyan/dhi/internal/tui/surfaces"
	"github.com/drjzlyan/dhi/internal/tui/theme"
)

// Model is the Settings surface.
type Model struct {
	cfg      settings.Config
	savePath string
	cursor   int
	width    int
	height   int
	flash    string
}

var _ surfaces.Surface = (*Model)(nil)

// New wires the surface to a loaded config and its persistence target.
func New(cfg settings.Config, savePath string) *Model {
	return &Model{cfg: cfg, savePath: savePath}
}

func (m *Model) Meta() surfaces.Meta    { return surfaces.Meta{ID: "settings", Title: "Settings"} }
func (m *Model) Init() tea.Cmd          { return nil }
func (m *Model) Update(tea.Msg) tea.Cmd { return nil }

func (m *Model) Resize(w, h int) {
	m.width, m.height = w, h
}

func (m *Model) HandleKey(key string) bool {
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
	case "ctrl+s":
		m.applyAndPersist()
		return true
	}
	return false
}

const (
	rowTheme = iota
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

// View renders the docked settings panel: rows at the top, status or
// keymap hints pinned to the foot — the same full-height panel language
// as the workspace and editor surfaces.
func (m *Model) View() string {
	w := maxInt(m.width, 40)
	h := maxInt(m.height, 10)

	foot := theme.Hint().Render("←/→ change · ctrl+s write · esc nothing")
	if m.flash != "" {
		foot = theme.SuccessText().Render(m.flash)
	}

	content := []string{
		settingRow(m.cursor == rowTheme, "theme",
			valueText(m.cfg.Theme)),
		settingRow(m.cursor == rowTabWidth, "editor.tab_width",
			valueText(itoa(m.cfg.Editor.TabWidth))),
		settingRow(m.cursor == rowLineNumbers, "editor.line_numbers",
			valueText(boolStr(m.cfg.Editor.LineNumbers))),
		settingRow(m.cursor == rowScrollback, "terminal.scrollback",
			valueText(itoa(m.cfg.Terminal.Scrollback))),
	}
	for len(content) < h-4 {
		content = append(content, "")
	}
	content = append(content, "", foot)

	p := kit.NewPanel("settings", true)
	p.SetContent(content...)
	p.Width, p.Height = w, h
	return p.View()
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
