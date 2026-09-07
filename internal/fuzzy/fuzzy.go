// Package fuzzy implements subsequence matching with IDE-style scoring:
// contiguity and separator boundaries rank candidates, ties break toward
// shorter paths. Pure functions; deterministic ordering.
package fuzzy

import (
	"sort"
	"strings"
	"unicode"
)

const (
	scoreBase      = 4  // every matched rune
	scoreConsec    = 12 // extends a contiguous run
	scoreBoundary  = 8  // match right after start or /_-.
	scoreCamel     = 6  // lower→upper transition boundary
	penaltyGapLead = -1 // per leading rune skipped before first match
)

// Match reports whether pattern occurs in s as a case-insensitive
// subsequence, with a heuristic quality score (higher = better).
// Greedy matching alone mis-scores when an early partial word shadows a
// fully-contiguous one ("alpha/app.go"), so every start position of the
// first rune is tried and the best alignment wins.
func Match(pattern, s string) (int, bool) {
	return matchRunes([]rune(strings.ToLower(pattern)), []rune(strings.ToLower(s)))
}

// greedyFrom scores a left-to-right alignment beginning exactly at
// tr[start].
func greedyFrom(pr, tr []rune, start int) (int, bool) {
	score := 0
	prevMatch := -2
	firstMatch := -1
	pi := 0
	for ti := start; ti < len(tr) && pi < len(pr); ti++ {
		if tr[ti] != pr[pi] {
			continue
		}
		score += scoreBase
		if ti == prevMatch+1 {
			score += scoreConsec
		}
		switch {
		case ti == 0:
			score += scoreBoundary
		case isBoundary(byte(tr[ti-1])):
			score += scoreBoundary
		case unicode.IsLower(tr[ti-1]) && unicode.IsUpper(tr[ti]):
			score += scoreCamel
		}
		if firstMatch < 0 {
			firstMatch = ti
		}
		prevMatch = ti
		pi++
		// The remaining text can no longer hold the remaining pattern.
		if len(tr)-ti-1 < len(pr)-pi {
			break
		}
	}
	if pi < len(pr) {
		return 0, false
	}
	if firstMatch > 0 {
		score += penaltyGapLead * min(firstMatch, 16)
	}
	return score, true
}

// Result couples one candidate with its match quality.
type Result struct {
	Index int
	Score int
}

// Rank returns indexes of items matching pattern, best first. Ties order
// by index (stable input order). Empty pattern keeps input order.
func Rank(pattern string, items []string) []Result {
	pr := []rune(strings.ToLower(pattern))
	trs := make([][]rune, len(items))
	for i, s := range items {
		trs[i] = []rune(strings.ToLower(s))
	}
	return rankRunes(pr, items, trs)
}

// Index is a pre-lowered candidate set: built once per (re)index, so
// every keystroke's rank costs no per-item lowercase or rune-slice
// allocations (F-010).
type Index struct {
	items []string
	trs   [][]rune
}

// NewIndex lowers and rune-slices every candidate once.
func NewIndex(items []string) *Index {
	trs := make([][]rune, len(items))
	for i, s := range items {
		trs[i] = []rune(strings.ToLower(s))
	}
	return &Index{items: items, trs: trs}
}

// Items returns the underlying candidates.
func (ix *Index) Items() []string { return ix.items }

// Rank works like Rank over the pre-lowered set; only the pattern is
// lowered per call.
func (ix *Index) Rank(pattern string) []Result {
	return rankRunes([]rune(strings.ToLower(pattern)), ix.items, ix.trs)
}

// rankRunes scores every candidate from pre-lowered rune slices and
// returns matches best-first.
func rankRunes(pr []rune, items []string, trs [][]rune) []Result {
	type scored struct {
		idx   int
		score int
	}
	out := make([]scored, 0, 64)
	for i, tr := range trs {
		if len(pr) > len(tr) {
			continue
		}
		sc, ok := matchRunes(pr, tr)
		if !ok {
			continue
		}
		out = append(out, scored{i, sc})
	}
	sort.SliceStable(out, func(a, b int) bool {
		if out[a].score != out[b].score {
			return out[a].score > out[b].score
		}
		return out[a].idx < out[b].idx
	})
	res := make([]Result, len(out))
	for i, s := range out {
		res[i] = Result{Index: s.idx, Score: s.score}
	}
	return res
}

// matchRunes reports whether pr occurs in tr as a subsequence with the
// heuristic score (the lowered core of Match).
func matchRunes(pr, tr []rune) (int, bool) {
	if len(pr) == 0 {
		return 0, true
	}
	if len(pr) > len(tr) {
		return 0, false
	}
	best := -1
	found := false
	for start := 0; start+len(pr) <= len(tr); start++ {
		if tr[start] != pr[0] {
			continue
		}
		sc, ok := greedyFrom(pr, tr, start)
		if ok && (!found || sc > best) {
			best, found = sc, true
		}
	}
	if !found {
		return 0, false
	}
	return best, true
}

func isBoundary(b byte) bool {
	switch b {
	case '/', '-', '_', '.', ' ':
		return true
	}
	return false
}
