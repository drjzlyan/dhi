package linediff

// myers diffs a against b and reports which lines of a are deleted and
// which lines of b are inserted; the rest match pairwise in order. It is
// Myers' O((n+m)·D) algorithm in linear space (middle snake, divide and
// conquer). ok is false when the edit distance exceeds maxEditDistance.
func myers(a, b []string) (delA, insB []bool, ok bool) {
	// Intern lines to ints so the inner loop compares integers.
	ids := make(map[string]int32, len(a)+len(b))
	intern := func(ss []string) []int32 {
		out := make([]int32, len(ss))
		for i, s := range ss {
			id, seen := ids[s]
			if !seen {
				id = int32(len(ids))
				ids[s] = id
			}
			out[i] = id
		}
		return out
	}
	m := &myersState{a: intern(a), b: intern(b)}
	m.delA = make([]bool, len(a))
	m.insB = make([]bool, len(b))
	half := (maxEditDistance + 1) / 2
	lim := min((len(a)+len(b)+1)/2, half)
	m.off = lim + 1
	m.vf = make([]int, 2*lim+3)
	m.vb = make([]int, 2*lim+3)
	m.lim = lim
	if !m.rec(0, len(a), 0, len(b)) {
		return nil, nil, false
	}
	return m.delA, m.insB, true
}

type myersState struct {
	a, b       []int32
	delA, insB []bool
	vf, vb     []int
	off, lim   int
}

func (m *myersState) rec(aLo, aHi, bLo, bHi int) bool {
	for aLo < aHi && bLo < bHi && m.a[aLo] == m.b[bLo] {
		aLo++
		bLo++
	}
	for aLo < aHi && bLo < bHi && m.a[aHi-1] == m.b[bHi-1] {
		aHi--
		bHi--
	}
	if aLo == aHi {
		for j := bLo; j < bHi; j++ {
			m.insB[j] = true
		}
		return true
	}
	if bLo == bHi {
		for i := aLo; i < aHi; i++ {
			m.delA[i] = true
		}
		return true
	}
	x0, y0, x1, y1, found := m.middle(aLo, aHi, bLo, bHi)
	if !found {
		return false
	}
	return m.rec(aLo, aLo+x0, bLo, bLo+y0) && m.rec(aLo+x1, aHi, bLo+y1, bHi)
}

// middle finds the middle snake of the (non-trivial) subproblem: the
// snake runs from (x0,y0) to (x1,y1) relative to (aLo,bLo).
func (m *myersState) middle(aLo, aHi, bLo, bHi int) (x0, y0, x1, y1 int, found bool) {
	a, b := m.a[aLo:aHi], m.b[bLo:bHi]
	n, mm := len(a), len(b)
	delta := n - mm
	odd := delta&1 != 0
	maxD := min((n+mm+1)/2, m.lim)
	vf, vb, off := m.vf, m.vb, m.off
	vf[off+1] = 0
	vb[off+1] = 0
	for d := 0; d <= maxD; d++ {
		for k := -d; k <= d; k += 2 {
			var x int
			if k == -d || (k != d && vf[off+k-1] < vf[off+k+1]) {
				x = vf[off+k+1]
			} else {
				x = vf[off+k-1] + 1
			}
			y := x - k
			sx, sy := x, y
			for x < n && y < mm && a[x] == b[y] {
				x++
				y++
			}
			vf[off+k] = x
			if odd && k >= delta-(d-1) && k <= delta+(d-1) && x+vb[off+delta-k] >= n {
				return sx, sy, x, y, true
			}
		}
		for k := -d; k <= d; k += 2 {
			var x int
			if k == -d || (k != d && vb[off+k-1] < vb[off+k+1]) {
				x = vb[off+k+1]
			} else {
				x = vb[off+k-1] + 1
			}
			y := x - k
			sx, sy := x, y
			for x < n && y < mm && a[n-1-x] == b[mm-1-y] {
				x++
				y++
			}
			vb[off+k] = x
			if !odd && k >= delta-d && k <= delta+d && x+vf[off+delta-k] >= n {
				return n - x, mm - y, n - sx, mm - sy, true
			}
		}
	}
	return 0, 0, 0, 0, false
}
