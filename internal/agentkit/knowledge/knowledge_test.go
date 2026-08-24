package knowledge

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/drjzlyan/dhi/internal/search"
	"github.com/drjzlyan/dhi/internal/workspace"
)

// fakeSearcher returns canned hits regardless of query.
type fakeSearcher struct {
	hits []search.Hit
}

func (f fakeSearcher) Search(_ context.Context, _ string, _ []string) (<-chan search.Hit, error) {
	ch := make(chan search.Hit, len(f.hits))
	for _, h := range f.hits {
		ch <- h
	}
	close(ch)
	return ch, nil
}

func fixture(t *testing.T, pol Policy) *Store {
	t.Helper()
	root := t.TempDir()
	os.MkdirAll(filepath.Join(root, "api"), 0o755)
	if err := workspace.Create(root, "api"); err != nil {
		t.Fatal(err)
	}
	ws, err := workspace.Load(root)
	if err != nil {
		t.Fatal(err)
	}
	st, err := Open(ws, pol, nil)
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	return st
}

func publish(t *testing.T, s *Store, c Contribution, at time.Time) Entry {
	t.Helper()
	s.mu.Lock()
	defer s.mu.Unlock()
	e, err := s.publish(deriveID(c.Title, c.Body), c, at)
	if err != nil {
		t.Fatalf("publish %q: %v", c.Title, err)
	}
	return e
}

func TestReviewPolicyQueuesThenApproves(t *testing.T) {
	s := fixture(t, Review)
	status, id, err := s.Contribute(Contribution{Title: "Deploy runbook", Body: "Run make deploy.", Author: "scout", Tags: []string{"ops"}})
	if err != nil || status != StatusQueued {
		t.Fatalf("status=%s id=%s err=%v", status, id, err)
	}
	if _, err := os.Stat(filepath.Join(s.entriesDir(), id+".md")); !os.IsNotExist(err) {
		t.Error("review contribution published immediately")
	}
	q, err := s.Pending()
	if err != nil || len(q) != 1 || q[0].Author != "scout" || q[0].Title != "Deploy runbook" {
		t.Fatalf("pending = %+v err %v", q, err)
	}

	e, err := s.Approve(q[0].ID)
	if err != nil {
		t.Fatalf("approve: %v", err)
	}
	if e.Author != "scout" || e.File != "entries/"+id+".md" {
		t.Errorf("entry provenance wrong: %+v", e)
	}
	body, err := os.ReadFile(filepath.Join(s.root, filepath.FromSlash(e.File)))
	if err != nil || !strings.Contains(string(body), "# Deploy runbook") {
		t.Errorf("body = %q err %v", body, err)
	}
	if left, _ := s.Pending(); len(left) != 0 {
		t.Errorf("pending not cleared: %+v", left)
	}

	var idx index
	b, _ := os.ReadFile(s.indexPath())
	json.Unmarshal(b, &idx)
	if len(idx.Entries) != 1 || idx.Entries[0].Author != "scout" || idx.Entries[0].ID != id || idx.Entries[0].Importance != 3 {
		t.Errorf("index provenance wrong: %+v (%s)", idx.Entries, b)
	}
}

func TestAutoPolicyPublishesImmediately(t *testing.T) {
	s := fixture(t, Auto)
	status, id, err := s.Contribute(Contribution{Title: "API conventions", Body: "Handlers return early.", Author: "owl"})
	if err != nil || status != StatusPublished {
		t.Fatalf("status=%s err=%v", status, err)
	}
	if p, _ := s.Pending(); len(p) != 0 {
		t.Errorf("auto policy queued anyway: %+v", p)
	}
	if _, err := os.Stat(filepath.Join(s.entriesDir(), id+".md")); err != nil {
		t.Errorf("entry missing: %v", err)
	}
}

func TestContributeValidationAndDeterministicIDs(t *testing.T) {
	s := fixture(t, Auto)
	if _, _, err := s.Contribute(Contribution{Title: "", Body: "x", Author: "a"}); err == nil {
		t.Error("empty title accepted")
	}
	if _, _, err := s.Contribute(Contribution{Title: "t", Body: "", Author: "a"}); err == nil {
		t.Error("empty body accepted")
	}
	if _, _, err := s.Contribute(Contribution{Title: "t", Body: "b"}); err == nil {
		t.Error("missing author accepted")
	}
	_, id1, _ := s.Contribute(Contribution{Title: "Same", Body: "same", Author: "a"})
	_, id2, _ := s.Contribute(Contribution{Title: "Same", Body: "same", Author: "a"})
	if id1 != id2 {
		t.Errorf("identical content derived different ids: %q vs %q", id1, id2)
	}
	_, id3, _ := s.Contribute(Contribution{Title: "Same", Body: "different", Author: "a"})
	if id3 == id1 {
		t.Errorf("different bodies share id %q", id1)
	}
}

func TestSearchScoresFreshnessAndImportance(t *testing.T) {
	s := fixture(t, Auto)

	oldImp := publish(t, s, Contribution{Title: "Old wisdom", Body: "deploy via CI", Importance: 5}, time.Now().AddDate(0, 0, -60))
	freshLow := publish(t, s, Contribution{Title: "New wisdom", Body: "deploy via CI too", Importance: 1}, time.Now())

	s.searcher = fakeSearcher{hits: []search.Hit{
		{Path: filepath.Join(s.entriesDir(), oldImp.ID+".md"), Line: 3, Text: "deploy via CI"},
		{Path: filepath.Join(s.entriesDir(), freshLow.ID+".md"), Line: 3, Text: "deploy"},
	}}
	hits, err := s.Search(context.Background(), "deploy", 10)
	if err != nil {
		t.Fatalf("search: %v", err)
	}
	if len(hits) != 2 {
		t.Fatalf("hits = %d, want 2 (both files matched)", len(hits))
	}
	// importance 5 + no freshness beats importance 1 + full freshness.
	if hits[0].Entry.ID != oldImp.ID {
		t.Errorf("ranked %q first; want high-importance entry", hits[0].Entry.Title)
	}
	if hits[0].Score <= hits[1].Score {
		t.Errorf("scores not strictly decreasing: %.2f then %.2f", hits[0].Score, hits[1].Score)
	}
	if hits[0].Snippet == "" {
		t.Error("snippet empty")
	}

	if got, _ := s.Search(context.Background(), "deploy", 1); len(got) != 1 {
		t.Error("limit ignored")
	}
	if got, _ := s.Search(context.Background(), "   ", 10); got != nil {
		t.Error("blank query should short-circuit")
	}
}

func TestApproveUnknownErrors(t *testing.T) {
	s := fixture(t, Review)
	if _, err := s.Approve("ghost"); err == nil {
		t.Error("unknown pending approved")
	}
}
