package gitdiff

import "unicode"

// MarkKey addresses one changed line: removed lines by their old number,
// added lines by their new number (unique within a file).
type MarkKey struct {
	Kind Kind
	No   int
}

// KeyOf is the mark key for a line (zero key for context).
func KeyOf(l Line) MarkKey {
	switch l.Kind {
	case Del:
		return MarkKey{Del, l.OldNo}
	case Add:
		return MarkKey{Add, l.NewNo}
	}
	return MarkKey{}
}

// maxWordTokens bounds the LCS table per line pair (tokens, not bytes).
const maxWordTokens = 300

// WordMarks finds, for every removed line that pairs with an added line in
// the same replace block, the runes that differ — so a renderer can
// highlight the words that changed rather than the whole line. The result
// maps each paired line (both sides) to a per-rune "changed" slice. Lines
// that were rewritten almost entirely carry no marks: highlighting
// everything says nothing.
func WordMarks(h Hunk) map[MarkKey][]bool {
	out := map[MarkKey][]bool{}
	for _, row := range Pair(h) {
		l, r := row.Left, row.Right
		if l == nil || r == nil || l.Kind != Del || r.Kind != Add {
			continue
		}
		om, nm, ok := diffWords(l.Text, r.Text)
		if !ok {
			continue
		}
		out[KeyOf(*l)], out[KeyOf(*r)] = om, nm
	}
	return out
}

// token is a run of word characters, a run of spaces, or one other rune.
type token struct{ start, end int } // rune offsets, end exclusive

func tokenize(rs []rune) []token {
	var out []token
	for i := 0; i < len(rs); {
		j := i + 1
		switch {
		case isWord(rs[i]):
			for j < len(rs) && isWord(rs[j]) {
				j++
			}
		case unicode.IsSpace(rs[i]):
			for j < len(rs) && unicode.IsSpace(rs[j]) {
				j++
			}
		}
		out = append(out, token{i, j})
		i = j
	}
	return out
}

func isWord(r rune) bool { return r == '_' || unicode.IsLetter(r) || unicode.IsDigit(r) }

func eq(a []rune, x token, b []rune, y token) bool {
	if x.end-x.start != y.end-y.start {
		return false
	}
	for k := 0; k < x.end-x.start; k++ {
		if a[x.start+k] != b[y.start+k] {
			return false
		}
	}
	return true
}

// diffWords marks the runes of a and b not covered by their longest common
// token subsequence. ok is false when there is nothing useful to show
// (identical, too long, or almost everything changed).
func diffWords(a, b string) (am, bm []bool, ok bool) {
	ra, rb := []rune(a), []rune(b)
	ta, tb := tokenize(ra), tokenize(rb)
	if len(ta) == 0 || len(tb) == 0 || len(ta) > maxWordTokens || len(tb) > maxWordTokens {
		return nil, nil, false
	}
	// LCS over tokens.
	lcs := make([][]int, len(ta)+1)
	for i := range lcs {
		lcs[i] = make([]int, len(tb)+1)
	}
	for i := len(ta) - 1; i >= 0; i-- {
		for j := len(tb) - 1; j >= 0; j-- {
			switch {
			case eq(ra, ta[i], rb, tb[j]):
				lcs[i][j] = lcs[i+1][j+1] + 1
			case lcs[i+1][j] >= lcs[i][j+1]:
				lcs[i][j] = lcs[i+1][j]
			default:
				lcs[i][j] = lcs[i][j+1]
			}
		}
	}
	am, bm = make([]bool, len(ra)), make([]bool, len(rb))
	keepA, keepB := make([]bool, len(ta)), make([]bool, len(tb))
	for i, j := 0, 0; i < len(ta) && j < len(tb); {
		switch {
		case eq(ra, ta[i], rb, tb[j]):
			keepA[i], keepB[j] = true, true
			i++
			j++
		case lcs[i+1][j] >= lcs[i][j+1]:
			i++
		default:
			j++
		}
	}
	changed := func(ts []token, keep []bool, m []bool) (n int) {
		for i, t := range ts {
			if keep[i] {
				continue
			}
			for k := t.start; k < t.end; k++ {
				m[k] = true
				n++
			}
		}
		return n
	}
	na, nb := changed(ta, keepA, am), changed(tb, keepB, bm)
	if na == 0 && nb == 0 {
		return nil, nil, false // identical (e.g. a mode-only change)
	}
	// Mostly rewritten: marking nearly everything adds noise, not meaning.
	if na*10 >= len(ra)*8 && nb*10 >= len(rb)*8 {
		return nil, nil, false
	}
	return am, bm, true
}
