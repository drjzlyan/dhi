package linediff

import (
	"fmt"
	"math/rand"
	"testing"
	"time"
)

// lcsOracle is the previous quadratic implementation of Diff, kept as the
// oracle for the Myers core (F-147). It returns the LCS length of a and b.
func lcsOracle(a, b []string) int {
	lcs := make([][]int, len(a)+1)
	for i := range lcs {
		lcs[i] = make([]int, len(b)+1)
	}
	for i := len(a) - 1; i >= 0; i-- {
		for j := len(b) - 1; j >= 0; j-- {
			if a[i] == b[j] {
				lcs[i][j] = lcs[i+1][j+1] + 1
			} else {
				lcs[i][j] = max(lcs[i+1][j], lcs[i][j+1])
			}
		}
	}
	return lcs[0][0]
}

// oracleMarks is the old Diff's walk over the LCS table.
func oracleMarks(older, newer []string) []Mark {
	n, m := len(older), len(newer)
	marks := make([]Mark, m)
	lcs := make([][]int, n+1)
	for i := range lcs {
		lcs[i] = make([]int, m+1)
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
	i, j := 0, 0
	for i < n || j < m {
		if i < n && j < m && older[i] == newer[j] {
			i++
			j++
			continue
		}
		di, dj := i, j
		for (i < n || j < m) && (i >= n || j >= m || older[i] != newer[j]) {
			switch {
			case j >= m:
				i++
			case i >= n:
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
				marks[dj+k] = Modified
			} else {
				marks[dj+k] = Added
			}
		}
		if dels > ins {
			at := max(dj+ins-1, 0)
			if at < len(marks) && marks[at] == None {
				marks[at] = DeletedBelow
			}
		}
	}
	return marks
}

// Several optimal alignments can exist, so marks may differ in placement;
// what must agree is the edit distance: the number of inserted lines
// (Added+Modified plus Added overflow) is m-LCS and deleted lines n-LCS.
func TestMyersMatchesLCSOracle(t *testing.T) {
	rng := rand.New(rand.NewSource(147))
	for iter := 0; iter < 3000; iter++ {
		alpha := 1 + rng.Intn(5)
		gen := func() []string {
			s := make([]string, rng.Intn(14))
			for i := range s {
				s[i] = fmt.Sprint(rng.Intn(alpha))
			}
			return s
		}
		a, b := gen(), gen()
		delA, insB, ok := myers(a, b)
		if !ok {
			t.Fatalf("not ok for %v -> %v", a, b)
		}
		dels, ins := 0, 0
		for _, d := range delA {
			if d {
				dels++
			}
		}
		for _, d := range insB {
			if d {
				ins++
			}
		}
		l := lcsOracle(a, b)
		if dels != len(a)-l || ins != len(b)-l {
			t.Fatalf("%v -> %v: dels=%d ins=%d, want %d/%d", a, b, dels, ins, len(a)-l, len(b)-l)
		}
		// The unflagged lines must be equal pairwise in order.
		var ka, kb []string
		for i, s := range a {
			if !delA[i] {
				ka = append(ka, s)
			}
		}
		for j, s := range b {
			if !insB[j] {
				kb = append(kb, s)
			}
		}
		if fmt.Sprint(ka) != fmt.Sprint(kb) {
			t.Fatalf("%v -> %v: kept %v vs %v", a, b, ka, kb)
		}
		// Diff's marks: changed-line counts agree with the oracle's.
		got, _ := Diff(a, b)
		want := oracleMarks(a, b)
		count := func(ms []Mark) (c int) {
			for _, m := range ms {
				if m == Added || m == Modified {
					c++
				}
			}
			return c
		}
		if len(got) != len(want) || count(got) != count(want) {
			t.Fatalf("%v -> %v: marks %v, oracle %v", a, b, glyphs(got), glyphs(want))
		}
	}
}

func bigFile(n int) []string {
	s := make([]string, n)
	for i := range s {
		s[i] = fmt.Sprintf("line %d of the file", i)
	}
	return s
}

func TestDiffLargeFileFewEdits(t *testing.T) {
	older := bigFile(50_000)
	newer := append([]string(nil), older...)
	newer[100] = "changed"
	newer[25_000] = "changed too"
	newer = append(newer[:40_000], newer[40_005:]...) // delete five lines
	newer = append(newer[:10], append([]string{"inserted"}, newer[10:]...)...)
	start := time.Now()
	marks, ok := Diff(older, newer)
	if el := time.Since(start); el > 200*time.Millisecond {
		t.Fatalf("diff took %v", el)
	}
	if !ok || len(marks) != len(newer) {
		t.Fatalf("ok=%v len=%d", ok, len(marks))
	}
	if marks[10] != Added || marks[101] != Modified || marks[25_001] != Modified {
		t.Fatalf("marks: %v %v %v", marks[10], marks[101], marks[25_001])
	}
	if marks[40_000] != DeletedBelow {
		t.Fatalf("deletion mark = %v", marks[40_000])
	}
}

func TestDiffGivesUpOnlyPastEditDistance(t *testing.T) {
	older := bigFile(20_000)
	newer := bigFile(20_000)
	for i := range newer {
		newer[i] = "new " + newer[i]
	}
	if marks, ok := Diff(older, newer); ok || marks != nil {
		t.Fatalf("ok=%v for a full rewrite past the cap", ok)
	}
	// Just under the cap still diffs.
	near := bigFile(20_000)
	for i := 0; i < 1000; i++ {
		near[i*7] = "x" + near[i*7]
	}
	if _, ok := Diff(older, near); !ok {
		t.Fatal("1000 scattered edits must diff")
	}
}
