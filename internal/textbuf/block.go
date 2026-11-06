package textbuf

import (
	"math"
	"strings"
	"unicode"

	"github.com/mattn/go-runewidth"
)

// Block visual mode (ctrl+v, F-159): a rectangle by display column. A tab
// is as wide as the next tab stop and a wide rune takes two cells, so the
// block is measured in cells, not runes. A character that straddles a
// block edge is split into spaces, like vim.

const blockInf = math.MaxInt32 // right edge of a "$" block

// SetTabWidth tells the editor how wide a tab is on screen (the
// editor.tab_width setting); block columns are measured with it.
func (e *Editor) SetTabWidth(n int) {
	if n > 0 {
		e.tabW = n
	}
}

func (e *Editor) tabWidth() int {
	if e.tabW <= 0 {
		return 4
	}
	return e.tabW
}

// VisualBlock reports ctrl+v (block) visual mode.
func (e *Editor) VisualBlock() bool { return e.mode == ModeVisual && e.visualBlock }

// BlockRange is the selected rectangle: lines top..bot (inclusive) and
// display cells [left, right). right is math.MaxInt32 for a "$" block.
func (e *Editor) BlockRange() (top, bot, left, right int, ok bool) {
	if !e.VisualBlock() {
		return 0, 0, 0, 0, false
	}
	g := e.blockGeom()
	return g.top, g.bot, g.left, g.right, true
}

type blockGeom struct{ top, bot, left, right int }

// cellInfo gives each rune's first display cell and end cell.
func (e *Editor) cellInfo(line string) (rs []rune, start, end []int) {
	rs = []rune(line)
	start, end = make([]int, len(rs)), make([]int, len(rs))
	tw, col := e.tabWidth(), 0
	for i, r := range rs {
		w := runewidth.RuneWidth(r)
		if r == '\t' {
			w = tw - col%tw
		}
		start[i], end[i] = col, col+w
		col += w
	}
	return rs, start, end
}

func (e *Editor) lineWidth(line string) int {
	_, _, end := e.cellInfo(line)
	if len(end) == 0 {
		return 0
	}
	return end[len(end)-1]
}

// cellSpan is the cells the rune at col covers; past the end it is one
// virtual cell after the last rune.
func (e *Editor) cellSpan(line string, col int) (int, int) {
	rs, s, en := e.cellInfo(line)
	if col < len(rs) {
		return s[col], max(en[col], s[col]+1)
	}
	w := e.lineWidth(line)
	return w, w + 1
}

func (e *Editor) blockGeom() blockGeom {
	a, c := e.visualStart, e.buf.Cursor()
	as, ae := e.cellSpan(e.buf.Line(a.Line), a.Col)
	cs, ce := e.cellSpan(e.buf.Line(c.Line), c.Col)
	g := blockGeom{top: min(a.Line, c.Line), bot: max(a.Line, c.Line), left: min(as, cs), right: max(ae, ce)}
	if e.blockEOL {
		g.right = blockInf
	}
	return g
}

// blockSlice cuts line at display cells [l, r): what lies before, inside
// and after. A straddling character becomes spaces on the cut side(s).
func (e *Editor) blockSlice(line string, l, r int) (before, mid, after string) {
	rs, s, en := e.cellInfo(line)
	var b, m, a strings.Builder
	for i, ru := range rs {
		switch {
		case en[i] <= l:
			b.WriteRune(ru)
		case s[i] >= r:
			a.WriteRune(ru)
		case s[i] >= l && en[i] <= r:
			m.WriteRune(ru)
		default:
			if s[i] < l {
				b.WriteString(strings.Repeat(" ", l-s[i]))
			}
			m.WriteString(strings.Repeat(" ", min(en[i], r)-max(s[i], l)))
			if en[i] > r {
				a.WriteString(strings.Repeat(" ", en[i]-r))
			}
		}
	}
	return b.String(), m.String(), a.String()
}

func (e *Editor) splitCell(line string, cell int) (string, string) {
	b, _, a := e.blockSlice(line, cell, cell)
	return b, a
}

func (e *Editor) textWidth(s string) int { return e.lineWidth(s) }

// runeAtCell is the rune index of the first rune starting at or after cell.
func (e *Editor) runeAtCell(line string, cell int) int {
	_, s, _ := e.cellInfo(line)
	n := 0
	for _, v := range s {
		if v < cell {
			n++
		}
	}
	return n
}

// blockText yanks the block: one string per line (short lines give "").
func (e *Editor) blockText(g blockGeom) []string {
	out := make([]string, 0, g.bot-g.top+1)
	for l := g.top; l <= g.bot; l++ {
		_, m, _ := e.blockSlice(e.buf.lines[l], g.left, g.right)
		out = append(out, m)
	}
	return out
}

// blockSelection backs Selection() in block mode.
func (e *Editor) blockSelection() (string, Pos, Pos) {
	g := e.blockGeom()
	text := strings.Join(e.blockText(g), "\n")
	from := Pos{g.top, e.runeAtCell(e.buf.lines[g.top], g.left)}
	last := e.buf.lines[g.bot]
	to := Pos{g.bot, min(e.runeAtCell(last, g.right), len([]rune(last)))}
	return text, from, to
}

func (e *Editor) setBlockRegister(g blockGeom, parts []string) {
	e.register, e.registerLW = strings.Join(parts, "\n"), false
	e.blockReg, e.blockRegText = parts, e.register
	w := g.right - g.left
	if g.right == blockInf {
		w = 0
		for _, p := range parts {
			w = max(w, e.textWidth(p))
		}
	}
	e.blockW = w
}

// blockRegValid reports whether the unnamed register still holds the
// last block yank/delete.
func (e *Editor) blockRegValid() bool {
	return e.blockReg != nil && !e.registerLW && e.register == e.blockRegText
}

// editBlockLines applies fn to lines top..bot as one undo step and puts
// the cursor at the block's top-left.
func (e *Editor) editBlockLines(g blockGeom, fn func(line string) string) {
	e.buf.snapshotBefore()
	for l := g.top; l <= g.bot; l++ {
		e.buf.lines[l] = fn(e.buf.lines[l])
	}
	e.buf.dirty = true
	e.blockCursor(g)
}

func (e *Editor) blockCursor(g blockGeom) {
	e.buf.SetCursor(Pos{g.top, e.runeAtCell(e.buf.lines[g.top], g.left)})
}

func (e *Editor) endBlock() {
	e.mode = ModeNormal
	e.blockEOL = false
}

// keyBlock handles the block-only keys; false lets keyVisual go on.
func (e *Editor) keyBlock(key string) bool {
	if e.pendRepl {
		e.pendRepl = false
		if r := []rune(key); key == "space" || len(r) == 1 && r[0] >= 32 {
			ch := ' '
			if key != "space" {
				ch = r[0]
			}
			g := e.blockGeom()
			e.editBlockLines(g, func(line string) string {
				b, m, a := e.blockSlice(line, g.left, g.right)
				if m == "" {
					return line
				}
				n := e.textWidth(m) / max(runewidth.RuneWidth(ch), 1)
				return b + strings.Repeat(string(ch), n) + a
			})
			e.endBlock()
		}
		return true
	}
	g := e.blockGeom()
	switch key {
	case "ctrl+v":
		e.endBlock()
	case "v":
		e.visualBlock, e.visualLine, e.blockEOL = false, false, false
	case "V":
		e.visualBlock, e.visualLine, e.blockEOL = false, true, false
	case "O": // the other corner on the same line: swap the columns
		a, c := e.visualStart, e.buf.Cursor()
		as, _ := e.cellSpan(e.buf.Line(a.Line), a.Col)
		cs, _ := e.cellSpan(e.buf.Line(c.Line), c.Col)
		e.visualStart.Col = e.runeAtCell(e.buf.Line(a.Line), cs)
		e.buf.SetCursor(Pos{c.Line, e.runeAtCell(e.buf.Line(c.Line), as)})
	case "d", "x", "D":
		if key == "D" {
			e.blockEOL = true
			g = e.blockGeom()
		}
		e.setBlockRegister(g, e.blockText(g))
		e.editBlockLines(g, func(line string) string {
			b, _, a := e.blockSlice(line, g.left, g.right)
			return b + a
		})
		e.message = "deleted block"
		e.endBlock()
	case "y":
		e.setBlockRegister(g, e.blockText(g))
		e.yanked = &e.register
		e.blockCursor(g)
		e.message = "yanked block"
		e.endBlock()
	case "c", "C":
		if key == "C" {
			e.blockEOL = true
			g = e.blockGeom()
		}
		e.setBlockRegister(g, e.blockText(g))
		e.beginBlockInsert('c', g)
	case "I", "A":
		e.beginBlockInsert(rune(key[0]), g)
	case "r":
		e.pendRepl = true
	case "~", "u", "U":
		e.editBlockLines(g, func(line string) string {
			b, m, a := e.blockSlice(line, g.left, g.right)
			switch key {
			case "u":
				m = strings.ToLower(m)
			case "U":
				m = strings.ToUpper(m)
			default:
				m = strings.Map(func(r rune) rune {
					if unicode.IsUpper(r) {
						return unicode.ToLower(r)
					}
					return unicode.ToUpper(r)
				}, m)
			}
			return b + m + a
		})
		e.endBlock()
	case ">", "<":
		e.shiftBlock(g, key == ">", e.count())
		e.cntDigits = nil
		e.endBlock()
	default:
		return false
	}
	return true
}

// shiftBlock moves the text right of the block's left edge by one indent
// unit (count times); lines with nothing there are left alone.
func (e *Editor) shiftBlock(g blockGeom, in bool, count int) {
	unit := e.Indent()
	sw := e.textWidth(unit)
	e.editBlockLines(g, func(line string) string {
		for i := 0; i < max(count, 1); i++ {
			if e.lineWidth(line) <= g.left {
				return line
			}
			if in {
				b, a := e.splitCell(line, g.left)
				line = b + unit + a
				continue
			}
			rs, s, en := e.cellInfo(line)
			n := 0
			for j := range rs {
				if en[j] <= g.left {
					continue
				}
				if rs[j] != ' ' && rs[j] != '\t' || n >= sw {
					break
				}
				n += min(en[j]-max(s[j], g.left), sw-n)
			}
			if n > 0 {
				b, _, a := e.blockSlice(line, g.left, g.left+n)
				line = b + a
			}
		}
		return line
	})
}

// blockInsert is the state of a pending c / I / A: when insert mode ends
// the text typed on the first line is copied to the other lines.
type blockInsert struct {
	kind        rune // 'c', 'I', 'A'
	top, bot    int
	left, right int
	sc          int    // rune column where typing began on the top line
	old         string // the top line then
	nlines      int
}

func (e *Editor) beginBlockInsert(kind rune, g blockGeom) {
	e.buf.BeginUndoGroup() // the delete/padding and the typing undo as one step
	e.preGrouped = true
	if kind == 'c' {
		e.editBlockLines(g, func(line string) string {
			b, _, a := e.blockSlice(line, g.left, g.right)
			return b + a
		})
	}
	line := e.buf.lines[g.top]
	col := 0
	switch kind {
	case 'A':
		if g.right == blockInf {
			col = len([]rune(line))
		} else {
			if w := e.lineWidth(line); w < g.right {
				e.buf.snapshotBefore()
				line += strings.Repeat(" ", g.right-w)
				e.buf.lines[g.top] = line
				e.buf.dirty = true
			}
			b, a := e.splitCell(line, g.right)
			if b+a != line {
				e.buf.snapshotBefore()
				line = b + a
				e.buf.lines[g.top] = line
				e.buf.dirty = true
			}
			col = len([]rune(b))
		}
	default:
		if e.lineWidth(line) < g.left {
			col = len([]rune(line))
		} else {
			b, a := e.splitCell(line, g.left)
			if b+a != line {
				e.buf.snapshotBefore()
				line = b + a
				e.buf.lines[g.top] = line
				e.buf.dirty = true
			}
			col = len([]rune(b))
		}
	}
	e.buf.SetCursor(Pos{g.top, col})
	e.blockIns = &blockInsert{kind: kind, top: g.top, bot: g.bot, left: g.left, right: g.right, sc: col, old: line, nlines: len(e.buf.lines)}
	e.mode = ModeInsert
	e.blockEOL = false
}

// finishBlockInsert copies what was typed on the top line to the rest of
// the block when insert mode ends (esc).
func (e *Editor) finishBlockInsert() {
	bi := e.blockIns
	e.blockIns = nil
	if bi == nil || len(e.buf.lines) != bi.nlines || e.buf.Cursor().Line != bi.top {
		return
	}
	old, cur := []rune(bi.old), []rune(e.buf.lines[bi.top])
	if bi.sc > len(old) || len(cur) < len(old) {
		return
	}
	n := len(cur) - len(old)
	head, tail := string(old[:bi.sc]), string(old[bi.sc:])
	if !strings.HasPrefix(string(cur), head) || !strings.HasSuffix(string(cur), tail) || n == 0 {
		return
	}
	text := string(cur[bi.sc : bi.sc+n])
	if strings.Contains(text, "\n") {
		return
	}
	e.buf.snapshotBefore()
	for l := bi.top + 1; l <= bi.bot; l++ {
		line := e.buf.lines[l]
		w := e.lineWidth(line)
		at := bi.left
		if bi.kind == 'A' {
			if bi.right == blockInf {
				e.buf.lines[l] = line + text
				continue
			}
			at = bi.right
			if w < at {
				line += strings.Repeat(" ", at-w)
			}
		} else if w < at {
			continue // a short line is not touched
		}
		b, a := e.splitCell(line, at)
		e.buf.lines[l] = b + text + a
	}
	e.buf.dirty = true
}

// pasteBlock puts the block register in as a rectangle: column-aligned
// at the cursor, short lines padded with spaces, new lines past the end.
func (e *Editor) pasteBlock(count int, after bool) {
	line := e.buf.Line(e.buf.Cursor().Line)
	col := e.buf.Cursor().Col
	cs, ce := e.cellSpan(line, col)
	at := cs
	if after && col < len([]rune(line)) {
		at = ce
	} else if after {
		at = e.lineWidth(line)
	}
	e.buf.snapshotBefore()
	start := e.buf.Cursor().Line
	var topCol int
	for i, part := range e.blockReg {
		l := start + i
		if l >= len(e.buf.lines) {
			e.buf.lines = append(e.buf.lines, "")
		}
		cur := e.buf.lines[l]
		if w := e.lineWidth(cur); w < at {
			cur += strings.Repeat(" ", at-w)
		}
		b, a := e.splitCell(cur, at)
		var piece strings.Builder
		for k := 0; k < max(count, 1); k++ {
			piece.WriteString(part)
			if a != "" || k < max(count, 1)-1 {
				piece.WriteString(strings.Repeat(" ", max(e.blockW-e.textWidth(part), 0)))
			}
		}
		e.buf.lines[l] = b + piece.String() + a
		if i == 0 {
			topCol = len([]rune(b))
		}
	}
	e.buf.dirty = true
	e.buf.SetCursor(Pos{start, topCol})
}
