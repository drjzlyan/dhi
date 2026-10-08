// Package linediff computes per-line change marks between two versions of
// a text, for editor gutters. It is a plain LCS diff over lines with a
// size guard, not a patch generator.
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

// maxCells bounds the DP table; past it Diff reports ok=false rather than
// stalling the render loop on a huge file.
const maxCells = 4_000_000

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
	if (len(a)+1)*(len(b)+1) > maxCells {
		return nil, false
	}
	// lcs[i][j] = LCS length of a[i:], b[j:].
	lcs := make([][]int32, len(a)+1)
	for i := range lcs {
		lcs[i] = make([]int32, len(b)+1)
	}
	for i := len(a) - 1; i >= 0; i-- {
		for j := len(b) - 1; j >= 0; j-- {
			if a[i] == b[j] {
				lcs[i][j] = lcs[i+1][j+1] + 1
			} else if lcs[i+1][j] >= lcs[i][j+1] {
				lcs[i][j] = lcs[i+1][j]
			} else {
				lcs[i][j] = lcs[i][j+1]
			}
		}
	}
	// Walk the table collecting runs of deletions/insertions between
	// matched lines; a run with both sides pairs up as Modified.
	i, j := 0, 0
	for i < len(a) || j < len(b) {
		if i < len(a) && j < len(b) && a[i] == b[j] {
			i++
			j++
			continue
		}
		di, dj := i, j
		for (i < len(a) || j < len(b)) && (i >= len(a) || j >= len(b) || a[i] != b[j]) {
			switch {
			case j >= len(b):
				i++
			case i >= len(a):
				j++
			case lcs[i+1][j] >= lcs[i][j+1]:
				i++
			default:
				j++
			}
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
