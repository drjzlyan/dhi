package kit

import (
	"image/color"
	"strconv"
	"strings"

	"charm.land/lipgloss/v2"

	"github.com/drjzlyan/dhi/internal/tui/theme"
)

// Column is one lane of a Columns layout: a title, pre-rendered rows,
// and the lane's own card cursor. Accent (optional) colors the header
// dot — the board passes its status colors.
type Column struct {
	Title  string
	Cursor int
	Rows   []string
	Accent color.Color
}

// Columns lays out lanes side by side (F-024): each lane is an inset
// background block with a header; the active lane's cursor row is
// highlighted. App-agnostic — the board supplies statuses, the chat
// could supply groups.
//
// F-026 P3: lane bodies scroll with the cursor (a lane taller than
// Height keeps the cursor row visible; offset lives on the lane), and
// LaneWidth exposes the per-lane budget so callers can pre-render
// width-proportional rows.
type Columns struct {
	Cols   []Column
	Active int // focused lane
	Width  int // total width
	Height int // visible rows below the headers; 0 = all
	// CardH is the rows per card (F-064): each Rows entry then holds
	// CardH lines joined by "\n", and a thin rule separates cards. 0 or
	// 1 keeps the one-line rows with no rules.
	CardH int

	offsets map[int]int
}

// UnitH is the screen rows one card takes, its separator rule included.
func (c *Columns) UnitH() int {
	if c.CardH <= 1 {
		return 1
	}
	return c.CardH + 1
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

// LaneWidth returns lane i's body width (the deterministic View math).
func (c *Columns) LaneWidth(i int) int {
	if len(c.Cols) == 0 {
		return 0
	}
	laneW := c.Width / len(c.Cols)
	if laneW < 8 {
		laneW = 8
	}
	return laneW
}

// laneWindow returns the visible row window for lane i, following the
// lane cursor (list semantics: the cursor can never vanish into a
// clipped block — F-026 P3).
func (c *Columns) laneWindow(i, rows int) (start, end int) {
	col := &c.Cols[i]
	vis := rows
	if vis <= 0 {
		return 0, len(col.Rows)
	}
	vis = max(vis/c.UnitH(), 1)
	if len(col.Rows) <= vis {
		return 0, len(col.Rows)
	}
	if c.offsets == nil {
		c.offsets = map[int]int{}
	}
	off := c.offsets[i]
	if col.Cursor < off {
		off = col.Cursor
	}
	if col.Cursor >= off+vis {
		off = col.Cursor - vis + 1
	}
	c.offsets[i] = off
	return off, off + vis
}

// Window returns lane i's visible [start, end) cards as last rendered by
// View (click zones map a screen row back to a card with it: screen row
// dy is card start + dy/UnitH()).
func (c *Columns) Window(i int) (start, end int) {
	if i < 0 || i >= len(c.Cols) {
		return 0, 0
	}
	return c.laneWindow(i, c.Height)
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

// View renders the lanes: a header strip per lane, then Height body rows.
// F-064: headers sit on the header shade (the active lane's on the
// selection shade, its title in the accent) with the count as a quiet
// number; cards of CardH rows are separated by thin rules and the
// selected card is highlighted across all its rows.
func (c *Columns) View() string {
	if len(c.Cols) == 0 {
		return ""
	}
	laneW := c.LaneWidth(0)

	sel := theme.TabActive()
	inset := theme.InsetBg()
	headBg := theme.HeaderBg()

	lines := make([]string, 0, c.Height+1)
	var heads []string
	for i, col := range c.Cols {
		dot := ""
		if col.Accent != nil {
			dot = lipgloss.NewStyle().Foreground(col.Accent).Render(theme.GlyphDot) + " "
		}
		title := theme.TextDim().Render(col.Title)
		bg := headBg
		if i == c.Active {
			title = theme.TabActive().Render(col.Title)
			bg = lipgloss.NewStyle().Background(theme.Current.BgSelection)
		}
		t := " " + dot + title + "  " + theme.TextMuted().Render(strconv.Itoa(len(col.Rows)))
		heads = append(heads, PaintRow(t, laneW-1, bg)+" ")
	}
	lines = append(lines, strings.Join(heads, ""))

	unit := c.UnitH()
	cardH := max(c.CardH, 1)
	rows := c.Height
	if rows <= 0 {
		for _, col := range c.Cols {
			if n := len(col.Rows) * unit; n > rows {
				rows = n
			}
		}
	}
	lanes := make([][]string, len(c.Cols))
	for i := range c.Cols {
		col := &c.Cols[i]
		start, end := c.laneWindow(i, c.Height)
		var body []string
		if len(col.Rows) == 0 {
			body = append(body, PaintRow(" "+EmptyRow(), laneW-1, inset))
		}
		for k := start; k < end && k < len(col.Rows); k++ {
			st := inset
			if i == c.Active && k == col.Cursor {
				st = sel
			}
			card := strings.Split(col.Rows[k], "\n")
			for y := 0; y < cardH; y++ {
				ln := ""
				if y < len(card) {
					ln = card[y]
				}
				body = append(body, Restyle(st, padTo(clip(ln, laneW-1), laneW-1)))
			}
			if unit > cardH {
				body = append(body, PaintRow(theme.Rule(laneW-1), laneW-1, inset))
			}
		}
		lanes[i] = body
	}
	blank := inset.Render(strings.Repeat(" ", laneW-1))
	for y := 0; y < rows; y++ {
		var cells []string
		for i := range c.Cols {
			row := blank
			if y < len(lanes[i]) {
				row = lanes[i][y]
			}
			cells = append(cells, row+" ")
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
