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
func Match(pattern, s string) (int, bool) {
	p := strings.ToLower(pattern)
	t := strings.ToLower(s)
	if p == "" {
		return 0, true
	}
	pr, tr := []rune(p), []rune(t)
	pi := 0
	score := 0
	prevMatch := -2
	firstMatch := -1
	for ti, r := range tr {
		if pi < len(pr) && pr[pi] == r {
			score += scoreBase
			if ti == prevMatch+1 {
				score += scoreConsec
			}
			if ti == 0 {
				score += scoreBoundary
			} else if isBoundary(t[ti-1]) {
				score += scoreBoundary
			} else if unicode.IsLower(rune(t[ti-1])) && unicode.IsUpper(rune(t[ti])) {
				score += scoreCamel
			}
			if firstMatch < 0 {
				firstMatch = ti
			}
			prevMatch = ti
			pi++
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
	type scored struct {
		idx   int
		score int
	}
	out := make([]scored, 0, len(items))
	for i, s := range items {
		sc, ok := Match(pattern, s)
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

func isBoundary(b byte) bool {
	switch b {
	case '/', '-', '_', '.', ' ':
		return true
	}
	return false
}
