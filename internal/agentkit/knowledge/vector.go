package knowledge

import (
	"hash/fnv"
	"math"
	"strings"
	"unicode"
)

// Lexical vector retrieval (F-063, ADR-0007's seam). Each entry and each
// query becomes a sparse TF-IDF vector over hashed word unigrams and
// bigrams, compared by cosine similarity. It is NOT a semantic embedding
// — no model, nothing downloaded, pure Go (ADR-0005) — but it matches on
// the words a question shares with an entry instead of requiring the
// whole question to appear verbatim, which is what the ripgrep phrase
// search did.

// vecDims bounds the hashed feature space (collisions are rare at KB
// sizes and only blur, never break, ranking).
const vecDims = 1 << 18

type sparse map[uint32]float64

// stopwords carry no topic (kept small and English; others pass through).
var stopwords = map[string]bool{
	"a": true, "an": true, "and": true, "are": true, "as": true, "at": true, "be": true,
	"by": true, "can": true, "do": true, "does": true, "for": true, "from": true, "how": true,
	"i": true, "in": true, "is": true, "it": true, "of": true, "on": true, "or": true,
	"our": true, "should": true, "that": true, "the": true, "this": true, "to": true,
	"we": true, "what": true, "when": true, "where": true, "which": true, "who": true,
	"why": true, "with": true, "you": true, "your": true,
}

// terms lowercases, splits on non-alphanumerics, drops stopwords and
// folds simple plurals ("deploys" ~ "deploy").
func terms(text string) []string {
	var out []string
	for _, f := range strings.FieldsFunc(strings.ToLower(text), func(r rune) bool {
		return !unicode.IsLetter(r) && !unicode.IsDigit(r)
	}) {
		if stopwords[f] || len(f) < 2 {
			continue
		}
		if len(f) > 3 && strings.HasSuffix(f, "s") && !strings.HasSuffix(f, "ss") {
			f = strings.TrimSuffix(f, "s")
		}
		out = append(out, f)
	}
	return out
}

// features are the hashed unigrams and bigrams of text with counts.
func features(text string) sparse {
	ts := terms(text)
	v := sparse{}
	for i, t := range ts {
		v[hashFeature(t)]++
		if i > 0 {
			v[hashFeature(ts[i-1]+" "+t)] += 0.5 // phrase evidence, weighted below words
		}
	}
	return v
}

func hashFeature(s string) uint32 {
	h := fnv.New32a()
	_, _ = h.Write([]byte(s))
	return h.Sum32() % vecDims
}

// vectorIndex is a TF-IDF model over a corpus of documents.
type vectorIndex struct {
	docs []sparse
	idf  map[uint32]float64
}

func newVectorIndex(docs []string) *vectorIndex {
	ix := &vectorIndex{idf: map[uint32]float64{}}
	df := map[uint32]int{}
	for _, d := range docs {
		f := features(d)
		ix.docs = append(ix.docs, f)
		for k := range f {
			df[k]++
		}
	}
	n := float64(len(docs))
	for k, c := range df {
		ix.idf[k] = math.Log(1+n/float64(c)) + 1 // smoothed, always positive
	}
	for i, d := range ix.docs {
		ix.docs[i] = ix.weigh(d)
	}
	return ix
}

// weigh applies sublinear TF × IDF and L2-normalizes.
func (ix *vectorIndex) weigh(f sparse) sparse {
	out := sparse{}
	var norm float64
	for k, tf := range f {
		idf, ok := ix.idf[k]
		if !ok {
			continue // a query term no document has
		}
		w := (1 + math.Log(tf)) * idf
		out[k] = w
		norm += w * w
	}
	if norm == 0 {
		return out
	}
	norm = math.Sqrt(norm)
	for k := range out {
		out[k] /= norm
	}
	return out
}

// similarity is the cosine similarity of query q to every document.
func (ix *vectorIndex) similarity(q string) []float64 {
	qv := ix.weigh(features(q))
	out := make([]float64, len(ix.docs))
	for i, d := range ix.docs {
		var dot float64
		for k, w := range qv {
			dot += w * d[k]
		}
		out[i] = dot
	}
	return out
}

// bestLine picks the body line sharing the most terms with the query
// (the hit's snippet when ripgrep found nothing).
func bestLine(body, query string) string {
	want := map[string]bool{}
	for _, t := range terms(query) {
		want[t] = true
	}
	best, bestN := "", 0
	for _, l := range strings.Split(body, "\n") {
		n := 0
		for _, t := range terms(l) {
			if want[t] {
				n++
			}
		}
		if n > bestN {
			best, bestN = strings.TrimSpace(l), n
		}
	}
	return best
}
