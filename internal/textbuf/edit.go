package textbuf

import "strings"

// InsertRune inserts r at the cursor and advances past it.
func (b *Buffer) InsertRune(r rune) {
	b.InsertString(string(r))
}

// InsertString inserts s (may contain \n) at the cursor, leaving the
// cursor after the inserted text.
func (b *Buffer) InsertString(s string) {
	if s == "" {
		return
	}
	b.snapshotBefore()
	parts := strings.Split(s, "\n")
	cur := b.runes(b.cursor.Line)
	head := string(cur[:b.cursor.Col])
	tail := string(cur[b.cursor.Col:])

	if len(parts) == 1 {
		b.lines[b.cursor.Line] = head + parts[0] + tail
		b.cursor.Col += len([]rune(parts[0]))
	} else {
		mid := make([]string, 0, len(parts))
		for i, p := range parts {
			switch i {
			case 0:
				mid = append(mid, head+p)
			case len(parts) - 1:
				mid = append(mid, p+tail)
			default:
				mid = append(mid, p)
			}
		}
		newLines := append([]string{}, b.lines[:b.cursor.Line]...)
		newLines = append(newLines, mid...)
		newLines = append(newLines, b.lines[b.cursor.Line+1:]...)
		b.lines = newLines
		b.cursor.Line += len(parts) - 1
		last := len([]rune(parts[len(parts)-1]))
		b.cursor.Col = last
	}
	b.wantCol = b.cursor.Col
	b.dirty = true
	b.clampCursor()
}

// BreakLine splits the line at the cursor (Enter key).
func (b *Buffer) BreakLine() { b.InsertString("\n") }

// DeleteBackspace removes the rune before the cursor; at line start it
// joins with the previous line. Reports whether anything changed.
func (b *Buffer) DeleteBackspace() bool {
	if b.cursor.Col > 0 {
		b.snapshotBefore()
		cur := b.runes(b.cursor.Line)
		b.lines[b.cursor.Line] = string(cur[:b.cursor.Col-1]) + string(cur[b.cursor.Col:])
		b.cursor.Col--
		b.wantCol = b.cursor.Col
		b.dirty = true
		return true
	}
	if b.cursor.Line > 0 {
		b.snapshotBefore()
		prev := b.runes(b.cursor.Line - 1)
		joinCol := len(prev)
		b.lines[b.cursor.Line-1] = string(prev) + b.lines[b.cursor.Line]
		b.lines = append(b.lines[:b.cursor.Line], b.lines[b.cursor.Line+1:]...)
		b.cursor.Line--
		b.cursor.Col = joinCol
		b.wantCol = joinCol
		b.dirty = true
		return true
	}
	return false
}

// DeleteForward removes the rune under the cursor; at EOL it joins the
// next line up. Reports whether anything changed.
func (b *Buffer) DeleteForward() bool {
	line := b.runes(b.cursor.Line)
	if b.cursor.Col < len(line) {
		b.snapshotBefore()
		b.lines[b.cursor.Line] = string(line[:b.cursor.Col]) + string(line[b.cursor.Col+1:])
		b.dirty = true
		b.clampCursor()
		return true
	}
	if b.cursor.Line < len(b.lines)-1 {
		b.snapshotBefore()
		b.lines[b.cursor.Line] += b.lines[b.cursor.Line+1]
		b.lines = append(b.lines[:b.cursor.Line+1], b.lines[b.cursor.Line+2:]...)
		b.dirty = true
		b.clampCursor()
		return true
	}
	return false
}

// YankRange returns the text spanned by a..z (exclusive of z), without
// changing anything. Linewise ranges are expressed by callers passing
// full-line positions (col 0 → col 0 of next line). A z with
// Line == LineCount() acts as end-of-buffer sentinel.
func (b *Buffer) yankRange(a, z Pos) string {
	a, z = order(a, z)
	if a == z {
		return ""
	}
	eof := z.Line >= b.LineCount()
	if a.Line == z.Line {
		r := b.runes(a.Line)
		return string(r[a.Col:z.Col])
	}
	var sb strings.Builder
	sb.WriteString(string(b.runes(a.Line)[a.Col:]))
	for l := a.Line + 1; l < z.Line; l++ {
		sb.WriteByte('\n')
		sb.WriteString(b.lines[l])
	}
	if !eof {
		sb.WriteByte('\n')
		sb.WriteString(string(b.runes(z.Line)[:z.Col]))
	}
	return sb.String()
}

// deleteRange removes the span a..z (exclusive of z), returning the
// removed text and leaving the cursor at a (clamped). EOF-sentinel z
// (Line == LineCount()) deletes through the last line.
func (b *Buffer) deleteRange(a, z Pos) string {
	a, z = order(a, z)
	text := b.yankRange(a, z)
	if a == z {
		return ""
	}
	b.snapshotBefore()
	eof := z.Line >= b.LineCount()

	if a.Line == z.Line {
		r := b.runes(a.Line)
		b.lines[a.Line] = string(r[:a.Col]) + string(r[z.Col:])
	} else {
		head := string(b.runes(a.Line)[:a.Col])
		var tail string
		if !eof {
			tail = string(b.runes(z.Line)[z.Col:])
		}
		merged := head + tail
		if merged == "" {
			newLines := append([]string{}, b.lines[:a.Line]...)
			b.lines = newLines
		} else {
			newLines := append([]string{}, b.lines[:a.Line]...)
			newLines = append(newLines, merged)
			if !eof {
				newLines = append(newLines, b.lines[z.Line+1:]...)
			}
			b.lines = newLines
		}
	}
	b.dirty = true
	if len(b.lines) == 0 {
		b.lines = []string{""}
	}
	b.cursor = b.clampPos(a)
	b.wantCol = b.cursor.Col
	return text
}

// insertAt inserts s at pos; used for paste and change-completions.
func (b *Buffer) insertAt(pos Pos, s string) {
	old := b.cursor
	b.cursor = b.clampPos(pos)
	b.InsertString(s)
	b.cursor = old // insertAt does not move the anchor cursor
	b.clampCursor()
}

// paste inserts register text after (or before) the cursor position.
// Linewise text inserts whole lines below/above the current one.
func (b *Buffer) paste(text string, linewise bool, after bool) Pos {
	b.snapshotBefore()
	if linewise {
		lines := strings.Split(strings.TrimSuffix(text, "\n"), "\n")
		at := b.cursor.Line
		if after {
			at++
		}
		newLines := append([]string{}, b.lines[:at]...)
		newLines = append(newLines, lines...)
		newLines = append(newLines, b.lines[at:]...)
		b.lines = newLines
		b.cursor = Pos{Line: at, Col: firstNonSpaceRune(lines[0])}
	} else {
		col := b.cursor.Col
		if after {
			col++
			line := b.runes(b.cursor.Line)
			if col > len(line) {
				col = len(line)
			}
		}
		b.insertAt(Pos{Line: b.cursor.Line, Col: col}, text)
		b.cursor.Col += len([]rune(text)) - 1
		if b.cursor.Col < 0 {
			b.cursor.Col = 0
		}
	}
	b.wantCol = b.cursor.Col
	b.dirty = true
	b.clampCursor()
	return b.cursor
}

// Order returns positions sorted (line-major).
func Order(a, z Pos) (Pos, Pos) {
	if a.before(z) {
		return a, z
	}
	return z, a
}

func order(a, z Pos) (Pos, Pos) {
	return Order(a, z)
}

func firstNonSpaceRune(line string) int {
	for i, r := range []rune(line) {
		if r != ' ' && r != '\t' {
			return i
		}
	}
	return 0
}
