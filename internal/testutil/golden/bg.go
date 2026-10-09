package golden

import (
	"fmt"
	"strings"
	"testing"

	"github.com/drjzlyan/dhi/internal/ansi"
)

// Box glyph sets: rounded (panels, dialogs) and double (help overlay).
var boxSets = []struct{ tl, tr, bl, br rune }{
	{'╭', '╮', '╰', '╯'},
	{'╔', '╗', '╚', '╝'},
}

// rightEdge are glyphs a box's right column may show: the plain edge or
// a scrollbar track/thumb.
func rightEdge(r rune) bool {
	return r == '│' || r == '║' || r == '█' || r == '┃'
}

func leftEdge(r rune) bool { return r == '│' || r == '║' || r == '┃' || r == '▌' }

type rect struct{ x0, y0, x1, y1 int } // corners, inclusive

// BgHoles checks the background integrity of a rendered frame (F-064):
// inside every box drawn with corner glyphs, each cell must carry a
// non-default background (no terminal-default holes punched by nested
// resets), and each row must keep its right edge in place (no fill or
// wash pushing past the border). It returns one message per problem.
func BgHoles(view string) []string {
	if !strings.Contains(view, "\x1b[") {
		return nil // pre-stripped text: nothing to check
	}
	lines := strings.Split(view, "\n")
	grid := make([][]ansi.Cell, len(lines))
	for y, l := range lines {
		grid[y] = ansi.Cells(l)
	}
	at := func(x, y int) rune {
		if y < 0 || y >= len(grid) || x < 0 || x >= len(grid[y]) {
			return 0
		}
		return grid[y][x].R
	}

	var rects []rect
	var probs []string
	for y := range grid {
		for x := range grid[y] {
			for _, bs := range boxSets {
				if grid[y][x].R != bs.tl {
					continue
				}
				x1 := -1
				for k := x + 1; k < len(grid[y]); k++ {
					if grid[y][k].R == bs.tr {
						x1 = k
						break
					}
					if grid[y][k].R == bs.tl {
						break
					}
				}
				if x1 < 0 {
					continue
				}
				y1 := -1
				for k := y + 1; k < len(grid); k++ {
					r := at(x, k)
					if r == bs.bl {
						y1 = k
						break
					}
					if !leftEdge(r) {
						break
					}
				}
				if y1 < 0 {
					continue
				}
				rects = append(rects, rect{x, y, x1, y1})
				for k := y + 1; k < y1; k++ {
					if !rightEdge(at(x1, k)) {
						probs = append(probs, fmt.Sprintf(
							"box at (%d,%d): row %d has no right edge at col %d (content overflowed the border?): %q",
							x, y, k, x1, ansi.Strip(lines[k])))
					}
				}
				if at(x1, y1) != bs.br {
					probs = append(probs, fmt.Sprintf(
						"box at (%d,%d): bottom-right corner missing at (%d,%d)", x, y, x1, y1))
				}
			}
		}
	}

	onBorder := func(x, y int) bool {
		for _, r := range rects {
			if (y == r.y0 || y == r.y1) && x >= r.x0 && x <= r.x1 {
				return true
			}
			if (x == r.x0 || x == r.x1) && y >= r.y0 && y <= r.y1 {
				return true
			}
		}
		return false
	}
	for _, r := range rects {
		for y := r.y0 + 1; y < r.y1; y++ {
			var holes []int
			for x := r.x0 + 1; x < r.x1; x++ {
				if x >= len(grid[y]) {
					holes = append(holes, x)
					continue
				}
				if !grid[y][x].Bg && !onBorder(x, y) {
					holes = append(holes, x)
				}
			}
			if len(holes) > 0 {
				probs = append(probs, fmt.Sprintf(
					"box at (%d,%d): row %d has %d unpainted cell(s) from col %d: %q",
					r.x0, r.y0, y, len(holes), holes[0], ansi.Strip(lines[y])))
			}
		}
	}
	return probs
}

// AssertBgIntegrity fails t when BgHoles reports problems.
func AssertBgIntegrity(t *testing.T, name, view string) {
	t.Helper()
	if probs := BgHoles(view); len(probs) > 0 {
		if len(probs) > 8 {
			probs = append(probs[:8], fmt.Sprintf("… and %d more", len(probs)-8))
		}
		t.Fatalf("%s: background integrity (F-064):\n  %s", name, strings.Join(probs, "\n  "))
	}
}
