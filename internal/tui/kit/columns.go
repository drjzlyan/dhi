package kit

import (
	"strconv"
	"strings"

	"github.com/drjzlyan/dhi/internal/tui/theme"
)

// Column is one lane of a Columns layout: a title, pre-rendered rows,
// and the lane's own card cursor.
type Column struct {
	Title  string
	Cursor int
	Rows   []string
}

// Columns lays out lanes side by side (F-024): each lane is an inset
// background block with a header; the active lane's cursor row is
// highlighted. App-agnostic — the board supplies statuses, the chat
// could supply groups.
type Columns struct {
	Cols   []Column
	Active int // focused lane
	Width  int // total width
	Height int // visible rows below the headers; 0 = all
}

// Left / Right / Up / Down move the active lane or its cursor, clamped.
func (c *Columns) Left() {
	if c.Active > 0 {
		c.Active--
	}
}
func (c *Columns) Right() {
	if c.Active < len(c.Cols)-1 {
		c.Active++
	}
}

func (c *Columns) Up()   { c.move(-1) }
func (c *Columns) Down() { c.move(1) }

func (c *Columns) move(d int) {
	if c.Active < 0 || c.Active >= len(c.Cols) {
		return
	}
	col := &c.Cols[c.Active]
	if len(col.Rows) == 0 {
		return
	}
	col.Cursor = clamp(col.Cursor+d, 0, len(col.Rows)-1)
}

// Selected returns the active lane index and its cursor row.
func (c *Columns) Selected() (int, int) {
	if c.Active < 0 || c.Active >= len(c.Cols) {
		return 0, 0
	}
	return c.Active, c.Cols[c.Active].Cursor
}

// HandleKey consumes left/right/up/down plus h/j/k/l; false otherwise.
func (c *Columns) HandleKey(key string) bool {
	switch key {
	case "left", "h":
		c.Left()
	case "right", "l":
		c.Right()
	case "up", "k":
		c.Up()
	case "down", "j":
		c.Down()
	default:
		return false
	}
	return true
}

// View renders the lanes: header row per lane, then Height body rows.
func (c *Columns) View() string {
	if len(c.Cols) == 0 {
		return ""
	}
	laneW := c.Width / len(c.Cols)
	if laneW < 8 {
		laneW = 8
	}

	header := theme.TextDim().Render
	active := theme.TabActive()
	sel := theme.TabActive()
	inset := theme.InsetBg()

	lines := make([]string, 0, c.Height+1)
	var heads []string
	for i, col := range c.Cols {
		t := " " + col.Title + " (" + strconv.Itoa(len(col.Rows)) + ") "
		if i == c.Active {
			heads = append(heads, padTo(active.Render(t), laneW))
		} else {
			heads = append(heads, padTo(header(t), laneW))
		}
	}
	lines = append(lines, strings.Join(heads, ""))

	rows := c.Height
	if rows <= 0 {
		for _, col := range c.Cols {
			if len(col.Rows) > rows {
				rows = len(col.Rows)
			}
		}
	}
	for y := 0; y < rows; y++ {
		var cells []string
		for i := range c.Cols {
			col := &c.Cols[i]
			var row string
			switch {
			case y < len(col.Rows):
				row = clip(col.Rows[y], laneW-1)
				if i == c.Active && y == col.Cursor {
					row = sel.Render(row)
				} else {
					row = inset.Render(row)
				}
			case len(col.Rows) == 0 && y == 0:
				row = inset.Render(clip(EmptyRow(), laneW-1))
			default:
				row = inset.Render(strings.Repeat(" ", laneW-1))
			}
			cells = append(cells, padTo(row, laneW))
		}
		lines = append(lines, strings.Join(cells, ""))
	}
	out := make([]string, 0, len(lines))
	for _, l := range lines {
		out = append(out, padTo(l, c.Width))
	}
	return strings.Join(out, "\n")
}

// EmptyRow is the dim placeholder for an empty lane.
func EmptyRow() string {
	return theme.TextMuted().Render("—")
}
