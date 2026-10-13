package setprop

import "testing"

func TestProposeReplaceResolve(t *testing.T) {
	s := Open(t.TempDir())
	a, err := s.Propose(Proposal{Key: "budgets.task_usd", Value: "2", Agent: "dev", Rationale: "r1"})
	if err != nil || a.Layer != LayerWorkspace || a.Status != StatusPending {
		t.Fatalf("propose = %+v %v", a, err)
	}
	b, _ := s.Propose(Proposal{Key: "budgets.task_usd", Value: "3", Agent: "dev", Rationale: "r2"})
	if p, _ := s.Pending(); len(p) != 1 || p[0].Value != "3" {
		t.Fatalf("a newer proposal for the same key must replace: %+v", p)
	}
	if _, err := s.Propose(Proposal{Key: "x", Layer: "team"}); err == nil {
		t.Fatal("bad layer must refuse")
	}
	if err := s.Resolve(b.ID, StatusApproved); err != nil {
		t.Fatal(err)
	}
	if p, _ := s.Pending(); len(p) != 0 {
		t.Fatal("resolved proposal still pending")
	}
	if err := s.Resolve(b.ID, StatusRejected); err == nil {
		t.Fatal("a resolved proposal cannot be resolved again")
	}
}
