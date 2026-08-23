package editor

import (
	"strings"

	"github.com/drjzlyan/dhi/internal/ansi"
)

// splitLines breaks rendered multi-line content into single rows.
func splitLines(s string) []string {
	return strings.Split(s, "\n")
}

// joinV stacks blocks vertically.
func joinV(blocks ...string) string {
	return strings.Join(blocks, "\n")
}

// joinH places two pre-rendered blocks side by side, padding the left
// block's rows to its widest line. Both blocks must have equal row
// counts for clean alignment; shorter sides get blank fill.
func joinH(left, right string) string {
	ls := strings.Split(left, "\n")
	rs := strings.Split(right, "\n")
	lw := 0
	for _, l := range ls {
		if w := len([]rune(ansi.Strip(l))); w > lw {
			lw = w
		}
	}
	n := len(ls)
	if len(rs) > n {
		n = len(rs)
	}
	out := make([]string, n)
	for i := 0; i < n; i++ {
		var l string
		if i < len(ls) {
			l = ls[i]
		}
		pad := lw - len([]rune(ansi.Strip(l)))
		if pad < 0 {
			pad = 0
		}
		var r string
		if i < len(rs) {
			r = rs[i]
		}
		out[i] = l + strings.Repeat(" ", pad) + r
	}
	return strings.Join(out, "\n")
}

func maxInt(a, b int) int {
	if a > b {
		return a
	}
	return b
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}

func clampIdx(i, hi int) int {
	if i < 0 {
		return 0
	}
	if i > hi {
		return hi
	}
	return i
}

func mainTitle(opened string) string {
	if opened == "" {
		return "editor"
	}
	return opened
}

func rowIndex(rows []treeRow, want *node) (int, bool) {
	for i, r := range rows {
		if r.node == want {
			return i, true
		}
	}
	return 0, false
}
