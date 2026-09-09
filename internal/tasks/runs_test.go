package tasks

import (
	"testing"
	"time"
)

func TestRecordRunAppendsAndPersists(t *testing.T) {
	s, ws := setupStore(t)
	if err := s.Create("fix-login", "Fix login race", "alice", "frontend"); err != nil {
		t.Fatalf("Create: %v", err)
	}
	now := time.Now().UTC()
	r := Run{
		ID: "run-1", Agent: "alice",
		Started: now.Add(-5 * time.Minute), Finished: now,
		Status:   RunOK,
		Summary:  "Fixed the race via a mutex.",
		TokensIn: 100, TokensOut: 40, CostUSD: 0.01,
	}
	if err := s.RecordRun("fix-login", r); err != nil {
		t.Fatalf("RecordRun: %v", err)
	}
	task, _ := s.Get("fix-login")
	if len(task.Runs) != 1 {
		t.Fatalf("Runs = %d, want 1", len(task.Runs))
	}
	if task.Runs[0].ID != "run-1" || task.Runs[0].Summary != "Fixed the race via a mutex." {
		t.Errorf("Run[0] = %+v", task.Runs[0])
	}

	// second run appends; the card reloads with both (history contract)
	if err := s.RecordRun("fix-login", Run{
		ID: "run-2", Agent: "alice",
		Started: now.Add(-1 * time.Minute), Finished: now,
		Status: RunError, Error: "network", TokensIn: -1, TokensOut: -1,
	}); err != nil {
		t.Fatalf("RecordRun (2): %v", err)
	}
	reloaded, err := Open(ws)
	if err != nil {
		t.Fatal(err)
	}
	task, _ = reloaded.Get("fix-login")
	if len(task.Runs) != 2 || task.Runs[1].Status != RunError || task.Runs[1].TokensIn != -1 {
		t.Errorf("reloaded Runs = %+v", task.Runs)
	}
}

func TestRecordRunValidation(t *testing.T) {
	s, _ := setupStore(t)
	if err := s.Create("k", "K", "alice", ""); err != nil {
		t.Fatalf("Create: %v", err)
	}
	now := time.Now().UTC()
	base := Run{ID: "r", Agent: "alice", Started: now, Finished: now, Status: RunOK}
	for name, mut := range map[string]func(*Run){
		"missing id":       func(r *Run) { r.ID = "" },
		"missing agent":    func(r *Run) { r.Agent = "" },
		"missing started":  func(r *Run) { r.Started = time.Time{} },
		"missing finished": func(r *Run) { r.Finished = time.Time{} },
		"bad status":       func(r *Run) { r.Status = "maybe" },
		"bad tokens":       func(r *Run) { r.TokensIn = -2 },
	} {
		r := base
		mut(&r)
		if err := s.RecordRun("k", r); err == nil {
			t.Errorf("%s: expected error", name)
		}
	}
}

func TestFindByThread(t *testing.T) {
	s, _ := setupStore(t)
	if err := s.Create("a", "A", "alice", ""); err != nil {
		t.Fatal(err)
	}
	if err := s.BindThread("a", "#alice", 7); err != nil {
		t.Fatal(err)
	}
	if err := s.Create("b", "B", "bob", ""); err != nil {
		t.Fatal(err)
	}

	task, ok := s.FindByThread("#alice", 7)
	if !ok || task.Slug != "a" {
		t.Errorf("FindByThread = %+v ok=%v, want a", task, ok)
	}
	if _, ok := s.FindByThread("#alice", 8); ok {
		t.Error("wrong thread matched")
	}
	if _, ok := s.FindByThread("", 7); ok {
		t.Error("empty channel matched")
	}
}
