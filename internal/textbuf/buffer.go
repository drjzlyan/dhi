// Package textbuf is DHI's TUI-free editing core: a line-oriented text
// buffer with modal (nvim-inspired) semantics — motions, operators,
// registers, undo/redo, and ex-style commands. All columns are rune
// offsets; the package never touches the terminal.
package textbuf

import (
	"os"
	"strings"
)

// Pos addresses one rune: Line 0-based, Col 0-based rune offset within
// the line. Col == len(line) means EOL.
type Pos struct {
	Line int
	Col  int
}

func (p Pos) before(q Pos) bool {
	if p.Line != q.Line {
		return p.Line < q.Line
	}
	return p.Col < q.Col
}

const maxHistory = 200

// Buffer stores text as lines plus cursor state and undo history.
// Every mutating operation snapshots first, so undo granularity is
// operation-level.
type Buffer struct {
	lines   []string // no trailing newline on the last element
	cursor  Pos      // invariant: cursor.Line < len(lines); Col in rune range (Col may equal line length)
	wantCol int      // sticky column for vertical motion

	dirty bool
	undo  []snapshot
	redo  []snapshot

	groupDepth  int  // >0 while an undo group is open (one insert session)
	groupMarked bool // a snapshot was already taken inside the current group
}

type snapshot struct {
	lines  []string
	cursor Pos
	dirty  bool
}

// New creates a buffer from content; a trailing newline yields a final
// empty line like real editors.
func New(content string) *Buffer {
	content = strings.ReplaceAll(content, "\r\n", "\n")
	lines := strings.Split(content, "\n")
	b := &Buffer{lines: lines}
	b.clampCursor()
	return b
}

// Open reads path into a new buffer.
func Open(path string) (*Buffer, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	return New(string(data)), nil
}

// Save writes the buffer to path with a trailing newline.
func (b *Buffer) Save(path string) error {
	var sb strings.Builder
	for i, l := range b.lines {
		if i > 0 {
			sb.WriteByte('\n')
		}
		sb.WriteString(l)
	}
	sb.WriteByte('\n')
	if err := os.WriteFile(path, []byte(sb.String()), 0o644); err != nil {
		return err
	}
	b.dirty = false
	return nil
}

// Text renders current contents (lines joined by \n).
func (b *Buffer) Text() string { return strings.Join(b.lines, "\n") }

// Lines returns a copy of all lines.
func (b *Buffer) Lines() []string {
	out := make([]string, len(b.lines))
	copy(out, b.lines)
	return out
}

// LineCount reports the number of lines (≥1).
func (b *Buffer) LineCount() int { return len(b.lines) }

// Dirty reports unsaved changes.
func (b *Buffer) Dirty() bool { return b.dirty }

// Cursor returns the current position.
func (b *Buffer) Cursor() Pos { return b.cursor }

// Line returns line i (no bounds guard beyond panic-free clamp; callers
// stay within viewport math).
func (b *Buffer) Line(i int) string {
	if i < 0 || i >= len(b.lines) {
		return ""
	}
	return b.lines[i]
}

// SetCursor moves the cursor, clamped and resetting the sticky column.
func (b *Buffer) SetCursor(p Pos) {
	b.cursor = b.clampPos(p)
	b.wantCol = b.cursor.Col
}

// runes converts a line to its rune slice.
func (b *Buffer) runes(line int) []rune { return []rune(b.lines[line]) }

func (b *Buffer) clampPos(p Pos) Pos {
	if p.Line < 0 {
		p.Line = 0
	}
	if p.Line >= len(b.lines) {
		p.Line = len(b.lines) - 1
	}
	max := len([]rune(b.lines[p.Line]))
	if p.Col < 0 {
		p.Col = 0
	}
	if p.Col > max {
		p.Col = max
	}
	return p
}

func (b *Buffer) clampCursor() { b.cursor = b.clampPos(b.cursor) }

// snapshotBefore records state for undo; clears redo. Inside an open
// undo group only the first edit snapshots, so a whole insert session
// collapses into one undo step.
func (b *Buffer) snapshotBefore() {
	if b.groupDepth > 0 {
		if b.groupMarked {
			b.redo = nil
			return
		}
		b.groupMarked = true
	}
	b.undo = append(b.undo, snapshot{lines: append([]string(nil), b.lines...), cursor: b.cursor, dirty: b.dirty})
	if len(b.undo) > maxHistory {
		b.undo = b.undo[1:]
	}
	b.redo = nil
}

// BeginUndoGroup opens an edit session that undoes as one step.
func (b *Buffer) BeginUndoGroup() {
	b.groupDepth++
	if b.groupDepth == 1 {
		b.groupMarked = false
	}
}

// EndUndoGroup closes one nesting level of the edit session.
func (b *Buffer) EndUndoGroup() {
	if b.groupDepth > 0 {
		b.groupDepth--
		if b.groupDepth == 0 {
			b.groupMarked = false
		}
	}
}

// Undo restores the previous state; reports whether anything happened.
func (b *Buffer) Undo() bool {
	if len(b.undo) == 0 {
		return false
	}
	snap := b.undo[len(b.undo)-1]
	b.undo = b.undo[:len(b.undo)-1]
	b.redo = append(b.redo, snapshot{lines: append([]string(nil), b.lines...), cursor: b.cursor, dirty: b.dirty})
	b.lines = snap.lines
	b.cursor = snap.cursor
	b.dirty = snap.dirty
	b.clampCursor()
	return true
}

// Redo reapplies the most recently undone change.
func (b *Buffer) Redo() bool {
	if len(b.redo) == 0 {
		return false
	}
	snap := b.redo[len(b.redo)-1]
	b.redo = b.redo[:len(b.redo)-1]
	b.undo = append(b.undo, snapshot{lines: append([]string(nil), b.lines...), cursor: b.cursor, dirty: b.dirty})
	b.lines = snap.lines
	b.cursor = snap.cursor
	b.dirty = snap.dirty
	b.clampCursor()
	return true
}
