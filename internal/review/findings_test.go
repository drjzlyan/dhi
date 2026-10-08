package review

import (
	"context"
	"strings"
	"testing"
)

func TestParseFindingsAcceptsTheShapesAnEmployeeActuallyWrites(t *testing.T) {
	obj := "Here you go:\n```json\n{\"summary\":\" Mostly fine. \",\"findings\":[" +
		"{\"file\":\"main.go\",\"line\":4,\"side\":\"NEW\",\"severity\":\"WARN\",\"comment\":\"println in prod code\"}," +
		"{\"file\":\"\",\"line\":1,\"comment\":\"no file: dropped\"}," +
		"{\"file\":\"main.go\",\"line\":-3,\"comment\":\"bad line: dropped\"}," +
		"{\"file\":\"main.go\",\"line\":3,\"side\":\"old\",\"comment\":\"removed guard\"}]}\n```\nThanks!"
	f, ok := ParseFindings(obj)
	if !ok || f.Summary != "Mostly fine." || len(f.Findings) != 2 {
		t.Fatalf("parsed = %+v ok=%v", f, ok)
	}
	if f.Findings[0].Side != "new" || f.Findings[0].Severity != "warn" || f.Findings[1].Side != "old" {
		t.Fatalf("normalisation: %+v", f.Findings)
	}

	arr, ok := ParseFindings("```\n[{\"file\":\"a.go\",\"line\":2,\"comment\":\"x\"}]\n```")
	if !ok || len(arr.Findings) != 1 || arr.Findings[0].Side != "new" {
		t.Fatalf("bare array = %+v ok=%v", arr, ok)
	}
	bare, ok := ParseFindings(`{"summary":"LGTM","findings":[]}`)
	if !ok || bare.Summary != "LGTM" {
		t.Fatalf("bare object = %+v ok=%v", bare, ok)
	}
	clean, ok := ParseFindings("```json\n{\"findings\": []}\n```")
	if !ok || len(clean.Findings) != 0 {
		t.Fatalf("a clean answer must parse as clean, got %+v ok=%v", clean, ok)
	}
	for _, junk := range []string{"LGTM, nice work", "```json\n{not json\n```", "```\nplain code\n```", ""} {
		if _, ok := ParseFindings(junk); ok {
			t.Errorf("%q parsed as findings", junk)
		}
	}
}

func TestAddSuggestionsAnchorsFindingsAndKeepsAnUnstructuredReply(t *testing.T) {
	_, st, _, r := submitFixture(t)
	n, err := st.AddSuggestions(r.ID, "sage", Findings{
		Summary:  "One concern.",
		Findings: []Finding{{File: "main.go", Line: 4, Side: "new", Severity: "warn", Comment: "println"}},
	}, "")
	if err != nil || n != 2 {
		t.Fatalf("n=%d err=%v", n, err)
	}
	cur, _ := st.Get(r.ID)
	if len(cur.Threads) != 2 || cur.OpenSuggestions() != 2 {
		t.Fatalf("threads = %+v", cur.Threads)
	}
	var anchored, note *Thread
	for i := range cur.Threads {
		if cur.Threads[i].File == SummaryFile {
			note = &cur.Threads[i]
		} else {
			anchored = &cur.Threads[i]
		}
	}
	c := anchored.Comments[0]
	if anchored.Line != 4 || anchored.Side != SideNew || !c.Suggested || c.Author != "sage" || c.Severity != "warn" || note == nil {
		t.Fatalf("anchored=%+v note=%+v", anchored, note)
	}

	// An unparseable reply is kept, whole, as one review-level suggestion.
	_, st2, _, r2 := submitFixture(t)
	n, err = st2.AddSuggestions(r2.ID, "sage", Findings{}, "I think the locking is wrong.")
	if err != nil || n != 1 {
		t.Fatalf("fallback n=%d err=%v", n, err)
	}
	cur2, _ := st2.Get(r2.ID)
	if cur2.Threads[0].File != SummaryFile || cur2.Threads[0].Comments[0].Text != "I think the locking is wrong." {
		t.Fatalf("fallback thread = %+v", cur2.Threads[0])
	}
}

func TestAnAcceptedReviewLevelNoteJoinsTheSummaryWithoutTheOffDiffHeading(t *testing.T) {
	svc, st, gh, r := submitFixture(t)
	st.AddSuggestions(r.ID, "sage", Findings{Summary: "Overall this needs tests."}, "")
	cur, _ := st.Get(r.ID)
	if err := st.AcceptSuggestion(r.ID, cur.Threads[0].ID, 0, Human, ""); err != nil {
		t.Fatal(err)
	}
	if _, _, err := svc.SubmitReview(context.Background(), r, VerdictComment, "My view:"); err != nil {
		t.Fatal(err)
	}
	body := gh.submitted[0].Body
	if body != "My view:\n\nOverall this needs tests." || strings.Contains(body, "outside the diff") {
		t.Fatalf("body = %q", body)
	}
	if len(gh.submitted[0].Comments) != 0 {
		t.Fatalf("a review-level note must not become an inline comment: %+v", gh.submitted[0].Comments)
	}
}
