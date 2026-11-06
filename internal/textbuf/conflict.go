package textbuf

import "strings"

// Merge-conflict support (F-156): a pure parser for git's conflict markers
// plus the editor commands that jump between and resolve them.

// Conflict is one marker block; every field is a 0-based buffer line.
type Conflict struct {
	Start int // <<<<<<< ours label
	Base  int // ||||||| base label (diff3 style), -1 when absent
	Sep   int // =======
	End   int // >>>>>>> theirs label
}

// ConflictChoice says which side(s) replace a conflict.
type ConflictChoice uint8

const (
	KeepOurs ConflictChoice = iota
	KeepTheirs
	KeepBoth // ours, then theirs
	KeepBase
)

// ConflictRegion tells which part of a conflict a line is in.
type ConflictRegion uint8

const (
	RegionNone ConflictRegion = iota
	RegionMarker
	RegionOurs
	RegionBase
	RegionTheirs
)

const conflictMarkerLen = 7

// markerAt reports whether line is exactly the 7-character marker ch,
// followed by the end of the line or a space (git's label separator). A
// longer or shorter run is ordinary text. A bare "=======" must end the line.
func markerAt(line string, ch byte) bool {
	line = strings.TrimSuffix(line, "\r")
	if len(line) < conflictMarkerLen {
		return false
	}
	for i := 0; i < conflictMarkerLen; i++ {
		if line[i] != ch {
			return false
		}
	}
	if len(line) == conflictMarkerLen {
		return true
	}
	return ch != '=' && line[conflictMarkerLen] == ' '
}

// ParseConflicts finds the well-formed conflict blocks in order. Inside a
// block, a marker out of order (a second "<<<<<<<", say) is content; a block
// that never closes in order is skipped and scanning resumes after its
// opening line, so text that merely looks like markers never swallows code.
func ParseConflicts(lines []string) []Conflict {
	var out []Conflict
	for i := 0; i < len(lines); i++ {
		if !markerAt(lines[i], '<') {
			continue
		}
		if c, ok := parseBlock(lines, i); ok {
			out = append(out, c)
			i = c.End
		}
	}
	return out
}

func parseBlock(lines []string, start int) (Conflict, bool) {
	c := Conflict{Start: start, Base: -1, Sep: -1}
	for j := start + 1; j < len(lines); j++ {
		l := lines[j]
		switch {
		case c.Sep < 0 && c.Base < 0 && markerAt(l, '|'):
			c.Base = j
		case c.Sep < 0 && markerAt(l, '='):
			c.Sep = j
		case c.Sep >= 0 && markerAt(l, '>'):
			c.End = j
			return c, true
		case c.Sep < 0 && markerAt(l, '>'):
			return c, false // closed before its separator: malformed
		}
	}
	return c, false
}

// Region classifies line n against c.
func (c Conflict) Region(n int) ConflictRegion {
	switch {
	case n < c.Start || n > c.End:
		return RegionNone
	case n == c.Start || n == c.Sep || n == c.End || n == c.Base:
		return RegionMarker
	case n < c.Sep && (c.Base < 0 || n < c.Base):
		return RegionOurs
	case n < c.Sep:
		return RegionBase
	}
	return RegionTheirs
}

// Resolve returns the lines that replace the whole block, or ok=false when
// the choice needs a base section the block does not have.
func (c Conflict) Resolve(lines []string, choice ConflictChoice) ([]string, bool) {
	oursEnd := c.Sep
	if c.Base >= 0 {
		oursEnd = c.Base
	}
	ours := lines[c.Start+1 : oursEnd]
	theirs := lines[c.Sep+1 : c.End]
	var out []string
	switch choice {
	case KeepOurs:
		out = append(out, ours...)
	case KeepTheirs:
		out = append(out, theirs...)
	case KeepBoth:
		out = append(append(out, ours...), theirs...)
	case KeepBase:
		if c.Base < 0 {
			return nil, false
		}
		out = append(out, lines[c.Base+1:c.Sep]...)
	}
	return out, true
}

// Conflicts lists the buffer's conflict blocks; the result is cached until
// the text changes.
func (e *Editor) Conflicts() []Conflict {
	if e.cfSeq != e.buf.seq+1 {
		e.cfBlocks = ParseConflicts(e.buf.lines)
		e.cfSeq = e.buf.seq + 1
	}
	return e.cfBlocks
}

// ConflictRegionAt tells which conflict region buffer line n is in.
func (e *Editor) ConflictRegionAt(n int) ConflictRegion {
	for _, c := range e.Conflicts() {
		if n < c.Start {
			break
		}
		if r := c.Region(n); r != RegionNone {
			return r
		}
	}
	return RegionNone
}

// jumpConflict moves to the opening line of the count-th next (dir 1) or
// previous (dir -1) conflict; it stops with a message at the last one.
func (e *Editor) jumpConflict(dir, count int) {
	cs := e.Conflicts()
	if len(cs) == 0 {
		e.message = "no merge conflicts in this file"
		return
	}
	line := e.buf.Cursor().Line
	moved := false
	for ; count > 0; count-- {
		idx := -1
		if dir > 0 {
			for i, c := range cs {
				if c.Start > line {
					idx = i
					break
				}
			}
		} else {
			for i := len(cs) - 1; i >= 0; i-- {
				if cs[i].Start < line {
					idx = i
					break
				}
			}
		}
		if idx < 0 {
			break
		}
		line, moved = cs[idx].Start, true
	}
	if !moved {
		e.message = "no more conflicts"
		return
	}
	e.buf.SetCursor(Pos{Line: line})
	for i, c := range cs {
		if c.Start == line {
			e.message = "conflict " + itoa(i+1) + " of " + itoa(len(cs))
		}
	}
}

// ResolveConflict replaces the conflict under the cursor with the chosen
// side(s) as one undo step. The cursor lands where the block began.
func (e *Editor) ResolveConflict(choice ConflictChoice) {
	line := e.buf.Cursor().Line
	for _, c := range e.Conflicts() {
		if line < c.Start || line > c.End {
			continue
		}
		repl, ok := c.Resolve(e.buf.lines, choice)
		if !ok {
			e.message = "this conflict has no base section (git config merge.conflictStyle diff3)"
			return
		}
		e.buf.snapshotBefore()
		next := append(append(append([]string(nil), e.buf.lines[:c.Start]...), repl...), e.buf.lines[c.End+1:]...)
		e.buf.lines = next
		e.buf.dirty = true
		e.buf.seq++
		e.buf.SetCursor(Pos{Line: min(c.Start, len(next)-1)})
		if left := len(e.Conflicts()); left == 0 {
			e.message = "conflict resolved; none left (:w to save)"
		} else {
			e.message = "conflict resolved, " + itoa(left) + " left"
		}
		return
	}
	e.message = "the cursor is not inside a merge conflict"
}

// conflictCommand handles :conflict ours|theirs|both|base|next|prev.
func (e *Editor) conflictCommand(arg string) {
	switch arg {
	case "ours":
		e.ResolveConflict(KeepOurs)
	case "theirs":
		e.ResolveConflict(KeepTheirs)
	case "both":
		e.ResolveConflict(KeepBoth)
	case "base":
		e.ResolveConflict(KeepBase)
	case "next":
		e.jumpConflict(1, 1)
	case "prev":
		e.jumpConflict(-1, 1)
	default:
		e.message = "usage: :conflict ours|theirs|both|base|next|prev  (]x [x jump)"
	}
}
