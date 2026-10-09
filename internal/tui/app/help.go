package app

import (
	"fmt"
	"strings"

	"github.com/drjzlyan/dhi/internal/ansi"
	"github.com/drjzlyan/dhi/internal/tui/kit"
	"github.com/drjzlyan/dhi/internal/tui/theme"
)

// helpState is the keyboard-help overlay (F-026 P7, searchable since
// F-054): modal while open, sized to the terminal, scrollable, and
// filterable by typing after "/".
type helpState struct {
	scroll    int
	filter    string
	filtering bool
}

// helpProvider is the contextual-help seam (F-026 P7): surfaces with
// it contribute their live key sections (the same wording as their
// chrome HintBar); surfaces without it get globals only.
type helpProvider interface {
	HelpSections() [][2]string // (keys, description) pairs, current context
}

// helpSection is one titled group of key rows.
type helpSection struct {
	title string
	rows  [][2]string
}

func (a *App) openHelp() {
	a.showHelp = true
	a.help = helpState{}
	a.observe("help")
}

func (a *App) helpSections() []helpSection {
	secs := []helpSection{{title: "DHI — global keys", rows: [][2]string{
		{fmt.Sprintf("1-%d", len(a.surfaces)), "jump between views"},
		{"tab / shift+tab", "cycle views"},
		{"ctrl+p", "command palette (search every action)"},
		{"?", "toggle this help"},
		{"ctrl+c", "quit DHI"},
	}}}
	if hp, ok := a.Active().(helpProvider); ok {
		if rows := kit.DedupeHelpRows(hp.HelpSections()); len(rows) > 0 {
			secs = append(secs, helpSection{title: a.Active().Meta().Title + " — here", rows: rows})
		}
	}
	return secs
}

// helpKey drives the open overlay; it owns every key but ctrl+c.
func (a *App) helpKey(key string) {
	h := &a.help
	if h.filtering {
		switch key {
		case "esc":
			h.filtering, h.filter = false, ""
		case "enter":
			h.filtering = false
		case "backspace":
			if r := []rune(h.filter); len(r) > 0 {
				h.filter = string(r[:len(r)-1])
			}
		case "space":
			h.filter += " "
		default:
			if len([]rune(key)) == 1 {
				h.filter += key
			}
		}
		h.scroll = 0
		return
	}
	switch key {
	case "esc", "?", "q":
		if h.filter != "" && key == "esc" {
			h.filter, h.scroll = "", 0
			return
		}
		a.showHelp = false
	case "/":
		h.filtering = true
	case "j", "down":
		h.scroll++
	case "k", "up":
		h.scroll--
	case "pgdown", "ctrl+d", " ", "space":
		h.scroll += 10
	case "pgup", "ctrl+u":
		h.scroll -= 10
	case "g", "home":
		h.scroll = 0
	case "G", "end":
		h.scroll = 1 << 20
	}
	// helpView clamps scroll against the real row count.
}

// helpView renders the overlay box: never wider than the terminal less
// a 1-cell margin, never taller than the body; long descriptions wrap
// under their key; rows past the window scroll.
func (a *App) helpView() string {
	boxW := min(78, a.width-2)
	innerW := max(boxW-6, 10) // double border + horizontal padding 2+2
	keyW := 4
	secs := a.helpSections()
	for _, s := range secs {
		for _, r := range s.rows {
			keyW = max(keyW, ansi.Width(r[0]))
		}
	}
	keyW = min(keyW, max(innerW/3, 6))
	descW := max(innerW-keyW-4, 8)

	needle := strings.ToLower(a.help.filter)
	var rows []string
	for _, s := range secs {
		var sec []string
		for _, r := range s.rows {
			if needle != "" && !strings.Contains(strings.ToLower(r[0]+" "+r[1]), needle) {
				continue
			}
			desc := kit.WrapWords(r[1], descW)
			if len(desc) == 0 {
				desc = []string{""}
			}
			sec = append(sec, "  "+keycap(kit.ClipEllipsis(kit.DisplayKeys(r[0]), keyW), keyW)+"  "+theme.TextDim().Render(desc[0]))
			for _, more := range desc[1:] {
				sec = append(sec, strings.Repeat(" ", keyW+4)+theme.TextDim().Render(more))
			}
		}
		if len(sec) == 0 {
			continue
		}
		if len(rows) > 0 {
			rows = append(rows, "")
		}
		rows = append(rows, theme.TabActive().Render(s.title), "")
		rows = append(rows, sec...)
	}
	if len(rows) == 0 {
		rows = []string{theme.TextDim().Render("no keys match \"" + a.help.filter + "\"")}
	}

	// Window: body rows minus border (2), padding (2) and footer (2).
	win := max(a.bodyHeight()-6, 3)
	maxScroll := max(len(rows)-win, 0)
	a.help.scroll = max(min(a.help.scroll, maxScroll), 0)
	view := rows[a.help.scroll:min(a.help.scroll+win, len(rows))]

	var foot string
	switch {
	case a.help.filtering:
		foot = theme.TabActive().Render("/ "+a.help.filter) + "▏" + theme.Hint().Render("  enter keep · esc clear")
	case maxScroll > 0:
		foot = theme.Hint().Render(fmt.Sprintf("%d/%d · j/k scroll · / search · esc close", a.help.scroll+min(win, len(rows)), len(rows)))
	default:
		foot = theme.Hint().Render("/ search · esc close")
	}
	lines := make([]string, 0, len(view)+2)
	for _, l := range view {
		lines = append(lines, kit.ClipEllipsis(l, innerW))
	}
	lines = append(lines, "", kit.ClipEllipsis(foot, innerW))
	return theme.HelpOverlay().Width(boxW).Render(strings.Join(lines, "\n"))
}

// keycap renders one key fragment as a raised pill padded to w cells.
func keycap(k string, w int) string {
	if pad := w - ansi.Width(k); pad > 0 {
		k += strings.Repeat(" ", pad)
	}
	return theme.Keycap().Render(k)
}
