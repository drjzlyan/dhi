package kit

import (
	"strings"

	"charm.land/lipgloss/v2"

	"github.com/drjzlyan/dhi/internal/tui/theme"
)

// Item is one selectable row.
type Item struct {
	Title string
	Desc  string // dimmed inline detail; shares the row style on inset rows
	Badge string // right-aligned status chip
	Group bool   // header row: not selectable, cursor skips it
}

// List is a keyboard-navigable vertical list. Keys are handled via HandleKey
// using keystroke strings ("up", "down", "home", "end", "g", "G", "j", "k").
type List struct {
	Items  []Item
	Cursor int
	Width  int
	Height int  // visible rows; 0 = all
	Inset  bool // render rows on the inset shade (sidebar zones, F-025)

	offset int
}

// SetItems replaces list contents, resetting the cursor to the first
// selectable row.
func (l *List) SetItems(items []Item) {
	l.Items = items
	l.Cursor = 0
	l.offset = 0
	for l.Cursor < len(l.Items) && l.Items[l.Cursor].Group {
		l.Cursor++
	}
	if l.Cursor >= len(l.Items) {
		l.Cursor = clamp(len(l.Items)-1, 0, len(l.Items)-1)
	}
}

// Up / Down / Home / End move the cursor with clamping and scroll adjust.
func (l *List) Up()   { l.moveTo(l.Cursor - 1) }
func (l *List) Down() { l.moveTo(l.Cursor + 1) }

func (l *List) moveTo(i int) {
	if len(l.Items) == 0 {
		return
	}
	i = clamp(i, 0, len(l.Items)-1)
	if l.Items[i].Group {
		// Group rows are headers, never the cursor: step to the nearest
		// selectable row in the travel direction; none → stay put.
		dir := 1
		if i < l.Cursor {
			dir = -1
		}
		found := false
		for j := i; j >= 0 && j < len(l.Items); j += dir {
			if !l.Items[j].Group {
				i = j
				found = true
				break
			}
		}
		if !found {
			return
		}
	}
	l.Cursor = clamp(i, 0, len(l.Items)-1)
	l.scroll()
}

func (l *List) scroll() {
	h := l.Height
	if h <= 0 {
		return
	}
	if l.Cursor < l.offset {
		l.offset = l.Cursor
	}
	if l.Cursor >= l.offset+h {
		l.offset = l.Cursor - h + 1
	}
}

// Selected returns the current item (ok=false when empty).
// RowAt maps a visible row (0 = the first rendered row) to its item
// index; false past the end (F-055 click routing).
func (l *List) RowAt(y int) (int, bool) {
	i := l.offset + y
	if y < 0 || i >= len(l.Items) || (l.Height > 0 && y >= l.Height) {
		return 0, false
	}
	return i, true
}

func (l *List) Selected() (Item, bool) {
	if l.Cursor < len(l.Items) {
		return l.Items[l.Cursor], true
	}
	return Item{}, false
}

// HandleKey consumes navigation keystrokes; returns false for others.
func (l *List) HandleKey(key string) bool {
	switch key {
	case "up", "k":
		l.Up()
	case "down", "j":
		l.Down()
	case "home", "g":
		l.moveTo(0)
	case "end", "G":
		l.moveTo(len(l.Items) - 1)
	default:
		return false
	}
	return true
}

// View renders visible rows: cursor marker, title, dim inline desc,
// badge right-aligned. Titles clip with an ellipsis and badges always
// keep their spacing — rows never overflow the width (F-026 P1).
func (l *List) View() string {
	rows := l.visibleRows()
	var out []string
	for i, it := range rows {
		idx := l.offset + i
		if it.Group {
			head := theme.TextMuted().Render(padTo(" "+strings.ToUpper(it.Title), l.Width))
			if l.Inset {
				head = Restyle(theme.RailMuted(), padTo(" "+strings.ToUpper(it.Title), l.Width))
			}
			out = append(out, head)
			continue
		}
		if l.Inset {
			out = append(out, l.insetRow(idx, it))
			continue
		}
		marker := "  "
		st := theme.TabInactive()
		if idx == l.Cursor {
			marker = theme.GlyphCursor + " "
			st = theme.TabActive()
		}
		line := marker + ClipEllipsis(it.Title, l.titleBudget())
		if it.Desc != "" {
			badgeW := 0
			if it.Badge != "" {
				badgeW = runeWidth("["+it.Badge+"]") + 1
			}
			if rem := clamp(l.Width-badgeW-runeWidth(line)-1, 0, l.Width); rem >= 3 {
				line += " " + theme.TextMuted().Render(ClipEllipsis(it.Desc, rem))
			}
		}
		if it.Badge != "" {
			badge := "[" + it.Badge + "]"
			gap := l.Width - runeWidth(line) - runeWidth(badge)
			line += strings.Repeat(" ", clamp(gap, 1, l.Width)) + theme.Hint().Render(badge)
		}
		out = append(out, Restyle(st, padTo(line, l.Width)))
	}
	return strings.Join(out, "\n")
}

// titleBudget is the widest the title may render: width minus the
// cursor marker and one leading space for a badge.
func (l *List) titleBudget() int {
	return clamp(l.Width-3, 1, l.Width)
}

// insetRow renders one row on the inset shade: each segment carries its
// own background (padding inside the style) so the row bg survives the
// per-segment SGR resets. The desc shares the base segment — a second
// style inside the base would drop the row background.
func (l *List) insetRow(idx int, it Item) string {
	base := theme.RailDim()
	badgeSt := theme.RailMuted()
	marker := "  "
	if idx == l.Cursor {
		marker = theme.GlyphCursor + " "
		base = theme.TabActive()
		badgeSt = lipgloss.NewStyle().
			Background(theme.Current.BgSelection).
			Foreground(theme.Current.TextMuted)
	}
	badgeW := 0
	if it.Badge != "" {
		badgeW = runeWidth("["+it.Badge+"]") + 1
	}
	title := ClipEllipsis(it.Title, clamp(l.Width-badgeW-2, 1, l.Width))
	line := marker + title
	if it.Desc != "" {
		if rem := clamp(l.Width-badgeW-runeWidth(line)-1, 0, l.Width); rem >= 3 {
			line += " " + ClipEllipsis(it.Desc, rem)
		}
	}
	// Restyle, not Render: a styled title (an accent dir name, a glyph)
	// ends in a reset that would drop the inset shade mid-row (F-064).
	if it.Badge == "" {
		return Restyle(base, padTo(line, l.Width))
	}
	badge := "[" + it.Badge + "]"
	return Restyle(base, padTo(line, l.Width-runeWidth(badge)-1)+" ") +
		badgeSt.Render(badge)
}

// Cues report whether rows sit above/below the window (F-026 P1).
func (l *List) Cues() (up, down bool) {
	if l.Height <= 0 {
		return false, false
	}
	up = l.offset > 0
	down = l.offset+l.Height < len(l.Items)
	return up, down
}

// Indicator renders the inline scroll cue for pane footers: "" when
// fully visible, "▲", "▼", or "▲▼" otherwise.
func (l *List) Indicator() string {
	s := Scroller{Total: len(l.Items), Height: l.Height, offset: l.offset}
	return s.Indicator()
}

// Scrollbar renders a one-column thumb track of h rows for panes
// painting a scrollbar beside the list.
func (l *List) Scrollbar(h int) string {
	s := Scroller{Total: len(l.Items), Height: l.Height, offset: l.offset}
	return s.Scrollbar(h)
}

// Scroller exposes the list's scroll window so a surface can paint a
// pane scrollbar (Panel.SetScroll) without reaching into offset.
func (l *List) Scroller() Scroller {
	return Scroller{Total: len(l.Items), Height: l.Height, offset: l.offset}
}

func (l *List) visibleRows() []Item {
	items := l.Items
	if l.Height > 0 && l.Height < len(items) {
		end := l.offset + l.Height
		if end > len(items) {
			end = len(items)
		}
		items = l.Items[l.offset:end]
	}
	return items
}
