// Package linediff computes per-line change marks between two versions of
// a text, for editor gutters. It is a Myers O((n+m)·D) diff over
// lines with an edit-distance guard, not a patch generator.
package linediff

// Mark classifies one line of the new text.
type Mark uint8

// Marks. A deletion has no line of its own in the new text, so it is
// attached to the line above it (or line 0 when the deletion is first).
const (
	None Mark = iota
	Added
	Modified
	DeletedBelow
)

// maxCells bounds the LCS table of Hunks (F-117 proposal review), which
// still uses the quadratic table; Diff no longer does.
const maxCells = 4_000_000

// maxEditDistance bounds Myers' D (inserted plus deleted lines) in Diff;
// past it Diff reports ok=false rather than stalling the render loop. File
// length alone never trips it (F-147).
const maxEditDistance = 5000

// Diff returns one Mark per line of newer. ok is false when the inputs
// are too large to diff cheaply.
func Diff(older, newer []string) (marks []Mark, ok bool) {
	n, m := len(older), len(newer)
	marks = make([]Mark, m)
	if n == 0 {
		for i := range marks {
			marks[i] = Added
		}
		return marks, true
	}
	if m == 0 {
		return marks, true
	}
	// Trim the common prefix/suffix: most edits are local, and it keeps
	// the table small.
	pre := 0
	for pre < n && pre < m && older[pre] == newer[pre] {
		pre++
	}
	suf := 0
	for suf < n-pre && suf < m-pre && older[n-1-suf] == newer[m-1-suf] {
		suf++
	}
	a, b := older[pre:n-suf], newer[pre:m-suf]
	if len(a) == 0 && len(b) == 0 {
		return marks, true
	}
	delA, insB, ok := myers(a, b)
	if !ok {
		return nil, false
	}
	// Walk the edit script collecting runs of deletions/insertions between
	// matched lines; a run with both sides pairs up as Modified.
	i, j := 0, 0
	for i < len(a) || j < len(b) {
		if i < len(a) && j < len(b) && !delA[i] && !insB[j] {
			i++
			j++
			continue
		}
		di, dj := i, j
		for i < len(a) && delA[i] {
			i++
		}
		for j < len(b) && insB[j] {
			j++
		}
		dels, ins := i-di, j-dj
		for k := 0; k < ins; k++ {
			if k < dels {
				marks[pre+dj+k] = Modified
			} else {
				marks[pre+dj+k] = Added
			}
		}
		if dels > ins {
			at := pre + dj + ins - 1
			if at < 0 {
				at = 0
			}
			if at < len(marks) && marks[at] == None {
				marks[at] = DeletedBelow
			}
		}
	}
	return marks, true
}
