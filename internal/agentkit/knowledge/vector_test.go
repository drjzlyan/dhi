package knowledge

import (
	"context"
	"testing"
)

// TestVectorRetrievalFindsByMeaningfulWords pins F-063: a question finds
// entries that share its words (any order, plurals folded) without the
// question appearing verbatim, ranks the most relevant first, needs no
// ripgrep, and returns nothing for unrelated questions.
func TestVectorRetrievalFindsByMeaningfulWords(t *testing.T) {
	st := fixture(t, Auto) // nil searcher: no ripgrep at all
	for _, c := range []Contribution{
		{Title: "Deploying the API", Body: "Deploys go through the release workflow: tag vX.Y.Z, CI builds and signs.", Tags: []string{"release"}, Author: "atlas"},
		{Title: "Rate limits", Body: "The public API allows 100 requests per minute per token.", Author: "forge"},
		{Title: "Office plants", Body: "Water the ficus on Mondays.", Author: "quill"},
	} {
		if _, _, err := st.Contribute(c); err != nil {
			t.Fatal(err)
		}
	}
	hits, err := st.Search(context.Background(), "how do we deploy a new API release?", 5)
	if err != nil {
		t.Fatal(err)
	}
	if len(hits) == 0 || hits[0].Entry.Title != "Deploying the API" {
		t.Fatalf("hits = %+v, want the deploy entry first", hits)
	}
	if hits[0].Snippet == "" {
		t.Fatal("a vector hit needs a snippet (the best matching line)")
	}
	for _, h := range hits {
		if h.Entry.Title == "Office plants" {
			t.Fatalf("unrelated entry matched: %+v", hits)
		}
	}
	if hits, _ := st.Search(context.Background(), "quantum chromodynamics lattice", 5); len(hits) != 0 {
		t.Fatalf("unrelated query matched: %+v", hits)
	}
	if hits, _ := st.Search(context.Background(), "requests per minute limit", 5); len(hits) == 0 || hits[0].Entry.Title != "Rate limits" {
		t.Fatalf("rate-limit question: %+v", hits)
	}
}

func TestTermsFoldsPluralsAndDropsStopwords(t *testing.T) {
	got := terms("How do we deploy the APIs, quickly?")
	want := []string{"deploy", "api", "quickly"}
	if len(got) != len(want) {
		t.Fatalf("terms = %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("terms = %v, want %v", got, want)
		}
	}
}
