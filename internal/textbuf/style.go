package textbuf

import "strings"

// Style hooks for EditorConfig (F-160). None of them runs unless the
// surface calls it, so a file nobody configured saves byte-exact (F-115).

// IsNew reports a buffer with no content on disk yet: an empty file or a
// scratch buffer. Only such a buffer may take a line ending from config.
func (b *Buffer) IsNew() bool { return b.diskPath == "" || b.newFile }

// SetCRLF sets the line ending a save writes; it changes nothing unless
// the buffer IsNew, so an existing file keeps its own endings.
func (b *Buffer) SetCRLF(crlf bool) bool {
	if !b.IsNew() {
		return false
	}
	b.crlf = crlf
	return true
}

// EnsureFinalNewline makes the buffer end with a newline (one undo step).
func (b *Buffer) EnsureFinalNewline() bool {
	if b.lines[len(b.lines)-1] == "" {
		return false
	}
	b.snapshotBefore()
	b.lines = append(b.lines, "")
	b.dirty = true
	b.seq++
	return true
}

// TrimTrailingWhitespace strips spaces and tabs from every line end (one
// undo step) and reports how many lines changed.
func (b *Buffer) TrimTrailingWhitespace() int {
	edits := map[int]string{}
	for i, l := range b.lines {
		if t := strings.TrimRight(l, " \t"); t != l {
			edits[i] = t
		}
	}
	return b.ReplaceLines(edits)
}
