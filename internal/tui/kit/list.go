package kit

import (
	"strings"

	"github.com/drjzlyan/dhi/internal/ansi"
	"github.com/drjzlyan/dhi/internal/tui/theme"
)

// Item is one selectable row.
type Item struct {
	Title string
	Desc  string // dimmed detail on the right or below
	Badge string // right-aligned status chip
}

// List is a keyboard-navigable vertical list. Keys are handled via HandleKey
// using keystroke strings ("up", "down", "home", "end", "g", "G", "j", "k").
type List struct {
	Items  []Item
	Cursor int
	Width  int
	Height int // visible rows; 0 = all

	offset int
}

// SetItems replaces list contents, resetting the cursor.
func (l *List) SetItems(items []Item) { l.Items = items; l.Cursor = 0; l.offset = 0 }

// Up / Down / Home / End move the cursor with clamping and scroll adjust.
func (l *List) Up()   { l.moveTo(l.Cursor - 1) }
func (l *List) Down() { l.moveTo(l.Cursor + 1) }

func (l *List) moveTo(i int) {
	if len(l.Items) == 0 {
		return
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

// View renders visible rows: cursor marker, title, badge right-aligned.
func (l *List) View() string {
	rows := l.visibleRows()
	var out []string
	for i, it := range rows {
		idx := l.offset + i
		marker := "  "
		st := theme.TabInactive()
		if idx == l.Cursor {
			marker = theme.GlyphCursor + " "
			st = theme.TabActive()
		}
		line := marker + it.Title
		if it.Badge != "" {
			badge := "[" + it.Badge + "]"
			gap := l.Width - runeWidth(ansi.Strip(line)) - runeWidth(badge) - 1
			if gap > 0 {
				line += strings.Repeat(" ", gap)
			}
			line += theme.Hint().Render(badge)
		}
		out = append(out, padTo(st.Render(line), l.Width))
	}
	return strings.Join(out, "\n")
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
