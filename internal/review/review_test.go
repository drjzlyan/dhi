package review

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/drjzlyan/dhi/internal/workspace"
)

func setupStore(t *testing.T) (*Store, *workspace.Workspace) {
	t.Helper()
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, "main"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := workspace.Create(root, "main"); err != nil {
		t.Fatal(err)
	}
	ws, err := workspace.Load(root)
	if err != nil {
		t.Fatal(err)
	}
	s, err := Open(ws)
	if err != nil {
		t.Fatal(err)
	}
	return s, ws
}

func sampleReview() Review {
	return Review{
		ID: "api-pr-42", Title: "Add feature",
		Target:    Target{Kind: KindPR, Member: "main", Base: "main", Head: "abc123", PRNumber: 42},
		Status:    Pending,
		Viewed:    map[string]bool{},
		Channel:   "#api-pr-42",
		WorkRel:   ".dhi/reviews/api-pr-42/main",
		CreatedAt: time.Now(), UpdatedAt: time.Now(),
	}
}

func TestCreatePersistReload(t *testing.T) {
	s, ws := setupStore(t)
	r := sampleReview()
	if err := s.Create(r); err != nil {
		t.Fatalf("Create: %v", err)
	}
	if _, err := s.AddThread(r.ID, Thread{File: "a.go", Line: 7, Side: SideNew, Comments: []Comment{
		{Author: "you", Text: "why here?", At: time.Now()},
	}}); err != nil {
		t.Fatalf("AddThread: %v", err)
	}
	if err := s.ToggleViewed(r.ID, "b.go"); err != nil {
		t.Fatal(err)
	}

	reloaded, err := Open(ws)
	if err != nil {
		t.Fatal(err)
	}
	got, ok := reloaded.Get(r.ID)
	if !ok || got.Title != "Add feature" || got.Target.PRNumber != 42 ||
		got.Target.Kind != KindPR || got.Channel != "#api-pr-42" || got.WorkRel == "" {
		t.Fatalf("reloaded = %+v (found=%v)", got, ok)
	}
	if len(got.Threads) != 1 || got.Threads[0].File != "a.go" || got.Threads[0].Line != 7 ||
		got.Threads[0].ID != 1 || got.Threads[0].Comments[0].Author != "you" {
		t.Fatalf("threads = %+v", got.Threads)
	}
	if !got.Viewed["b.go"] {
		t.Errorf("viewed mark lost")
	}
	if reloaded.Warnings() != nil {
		t.Errorf("unexpected warnings: %v", reloaded.Warnings())
	}
}

func TestCommentLifecycleAndSubmit(t *testing.T) {
	s, _ := setupStore(t)
	r := sampleReview()
	if err := s.Create(r); err != nil {
		t.Fatal(err)
	}
	id, _ := s.AddThread(r.ID, Thread{File: "x.go", Line: 3})
	if err := s.AppendComment(r.ID, id, Comment{Author: "you", Text: "nit", Pending: true}); err != nil {
		t.Fatal(err)
	}
	if err := s.AppendComment(r.ID, 99, Comment{}); err == nil {
		t.Error("unknown thread accepted")
	}
	got, _ := s.Get(r.ID)
	if got.PendingCount() != 1 {
		t.Fatalf("pending = %d, want 1", got.PendingCount())
	}
	if err := s.SetResolved(r.ID, id, true); err != nil {
		t.Fatal(err)
	}
	if err := s.Submit(r.ID); err != nil {
		t.Fatal(err)
	}
	got, _ = s.Get(r.ID)
	if got.Status != Submitted || got.PendingCount() != 0 || !got.Threads[0].Resolved {
		t.Errorf("submit state wrong: %+v", got)
	}
}

func TestRemoveAndWarnings(t *testing.T) {
	s, ws := setupStore(t)
	if err := s.Create(sampleReview()); err != nil {
		t.Fatal(err)
	}
	bad := filepath.Join(s.dir(), "bad-id.toml")
	os.WriteFile(bad, []byte(`schema = 99
title = "?"`), 0o644)

	reloaded, _ := Open(ws)
	if len(reloaded.Warnings()) != 1 {
		t.Fatalf("warnings = %v", reloaded.Warnings())
	}
	if _, ok := reloaded.Get("bad-id"); ok {
		t.Error("malformed card loaded")
	}
	if err := reloaded.Remove("api-pr-42"); err != nil {
		t.Fatalf("Remove: %v", err)
	}
	reopened, _ := Open(ws)
	if _, ok := reopened.Get("api-pr-42"); ok {
		t.Error("card survived removal")
	}
	if len(reopened.List()) != 0 {
		t.Errorf("List not empty after removal: %v", reopened.List())
	}
}

func TestValidationOnCreate(t *testing.T) {
	s, _ := setupStore(t)
	bad := sampleReview()
	bad.ID = "Bad ID"
	if err := s.Create(bad); err == nil {
		t.Error("bad id accepted")
	}
	dup := sampleReview()
	if err := s.Create(dup); err != nil {
		t.Fatal(err)
	}
	if err := s.Create(dup); err == nil {
		t.Error("duplicate accepted")
	}
}

func TestSubscribePings(t *testing.T) {
	s, _ := setupStore(t)
	ch, cancel := s.Subscribe()
	defer cancel()
	go func() { _ = s.Create(sampleReview()) }()
	select {
	case c := <-ch:
		if c.Kind != ReviewCreated || c.ID != "api-pr-42" {
			t.Errorf("change = %+v", c)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("no change ping")
	}
}

func TestNextThreadIDIncrements(t *testing.T) {
	r := Review{Threads: []Thread{{ID: 4}}}
	if r.nextThreadID() != 5 {
		t.Errorf("next = %d", r.nextThreadID())
	}
}
