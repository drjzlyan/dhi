package workspace

import (
	"testing"

	"github.com/drjzlyan/dhi/internal/inbox"
	"github.com/drjzlyan/dhi/internal/tasks"
	"github.com/drjzlyan/dhi/internal/workspace"
)

func TestInboxProposalAcceptDecline(t *testing.T) {
	m, ws := newSurface(t)
	ts, err := tasks.Open(ws)
	if err != nil {
		t.Fatal(err)
	}
	m.taskStore = ts
	if err := ws.SetDependencies([]workspace.Dependency{{From: "beta", To: "alpha", Kind: "api"}}); err != nil {
		t.Fatal(err)
	}

	// Decline: nothing is created.
	_ = ts.Create("origin", "Origin", "", "")
	_ = ts.SeedPropagations("origin", []tasks.Propagation{{FromMember: "beta", ToMember: "alpha", Kind: "api"}})
	m.decideProposal(inbox.Item{Kind: inbox.Dependency, TaskSlug: "origin", FromMember: "beta", ToMember: "alpha", DepKind: "api"}, false)
	got, _ := ts.Get("origin")
	if len(got.PendingPropagations()) != 0 {
		t.Fatalf("decline left a pending proposal: %+v", got.Propagations)
	}
	if _, ok := ts.Get("dep-origin-alpha"); ok {
		t.Fatal("decline must not create a task")
	}

	// Accept: creates a linked task and records it.
	_ = ts.Create("origin2", "Origin2", "", "")
	_ = ts.SeedPropagations("origin2", []tasks.Propagation{{FromMember: "beta", ToMember: "alpha", Kind: "api"}})
	m.decideProposal(inbox.Item{Kind: inbox.Dependency, TaskSlug: "origin2", FromMember: "beta", ToMember: "alpha", DepKind: "api"}, true)
	got, _ = ts.Get("origin2")
	if len(got.PendingPropagations()) != 0 || got.Propagations[0].Decision != tasks.PropAccepted ||
		got.Propagations[0].CreatedSlug != "dep-origin2-alpha" {
		t.Fatalf("accept did not link: %+v", got.Propagations)
	}
	if _, ok := ts.Get("dep-origin2-alpha"); !ok {
		t.Fatal("accept must create the linked task")
	}
}
