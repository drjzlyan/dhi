package linediff

// Hunk is one contiguous change between two versions: Old lines starting
// at OldAt are replaced by New (either side may be empty).
type Hunk struct {
	OldAt int
	Old   []string
	New   []string
}

// Hunks returns the changes that turn older into newer, in order. ok is
// false when the inputs are too large to diff cheaply (F-117 proposal
// review falls back to one whole-block hunk).
func Hunks(older, newer []string) (hunks []Hunk, ok bool) {
	n, m := len(older), len(newer)
	if (n+1)*(m+1) > maxCells {
		return []Hunk{{OldAt: 0, Old: older, New: newer}}, false
	}
	lcs := make([][]int32, n+1)
	for i := range lcs {
		lcs[i] = make([]int32, m+1)
	}
	for i := n - 1; i >= 0; i-- {
		for j := m - 1; j >= 0; j-- {
			if older[i] == newer[j] {
				lcs[i][j] = lcs[i+1][j+1] + 1
			} else {
				lcs[i][j] = max(lcs[i+1][j], lcs[i][j+1])
			}
		}
	}
	var cur *Hunk
	flush := func() {
		if cur != nil {
			hunks = append(hunks, *cur)
			cur = nil
		}
	}
	i, j := 0, 0
	for i < n || j < m {
		switch {
		case i < n && j < m && older[i] == newer[j]:
			flush()
			i, j = i+1, j+1
		case j < m && (i == n || lcs[i][j+1] >= lcs[i+1][j]):
			if cur == nil {
				cur = &Hunk{OldAt: i}
			}
			cur.New = append(cur.New, newer[j])
			j++
		default:
			if cur == nil {
				cur = &Hunk{OldAt: i}
			}
			cur.Old = append(cur.Old, older[i])
			i++
		}
	}
	flush()
	return hunks, true
}

// Apply rebuilds the new text from older keeping only the hunks whose
// accept flag is true; the others leave older's lines as they were.
func Apply(older []string, hunks []Hunk, accept []bool) []string {
	var out []string
	at := 0
	for k, h := range hunks {
		out = append(out, older[at:h.OldAt]...)
		if k < len(accept) && accept[k] {
			out = append(out, h.New...)
		} else {
			out = append(out, h.Old...)
		}
		at = h.OldAt + len(h.Old)
	}
	return append(out, older[at:]...)
}
