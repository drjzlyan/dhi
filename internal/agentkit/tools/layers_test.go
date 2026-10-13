package tools

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/drjzlyan/dhi/internal/sandbox"
)

func waitPending(a *Approvals) *Approval {
	for {
		if l := a.List(); len(l) > 0 {
			return l[0]
		}
		<-a.Changes()
	}
}

// F-087: a grant lands in the layer the human chose — once in memory,
// this workspace in .dhi/grants.json, everywhere in the user file — and
// both files load back.
func TestGrantLayersPersistToTheirFiles(t *testing.T) {
	dir := t.TempDir()
	ws, user := filepath.Join(dir, "ws.json"), filepath.Join(dir, "user.json")
	a := NewApprovals()
	if err := a.Persist(ws); err != nil {
		t.Fatal(err)
	}
	if err := a.PersistUser(user); err != nil {
		t.Fatal(err)
	}
	for _, c := range []struct {
		scope string
		layer Layer
	}{{"write:/a", LayerOnce}, {"write:/b", LayerWorkspace}, {"network", LayerEverywhere}} {
		done := make(chan error, 1)
		go func() { done <- a.AskScope(context.Background(), "anvil", c.scope, sandbox.OpWrite, c.scope, "") }()
		a.ResolveFor(waitPending(a).ID, c.layer)
		if err := <-done; err != nil {
			t.Fatal(err)
		}
	}
	read := func(p string) []Grant {
		var gs []Grant
		b, _ := os.ReadFile(p)
		_ = json.Unmarshal(b, &gs)
		return gs
	}
	if gs := read(ws); len(gs) != 1 || gs[0].Scope != "write:/b" {
		t.Fatalf("workspace file = %+v", gs)
	}
	if gs := read(user); len(gs) != 1 || gs[0].Scope != "network" {
		t.Fatalf("user file = %+v", gs)
	}
	if got := a.Targets("anvil"); len(got) != 2 {
		t.Fatalf("targets = %v (once + workspace)", got)
	}
	b := NewApprovals()
	_ = b.Persist(ws)
	_ = b.PersistUser(user)
	if got := b.Targets("anvil"); len(got) != 1 || got[0] != "/b" {
		t.Fatalf("after restart targets = %v (once is gone)", got)
	}
	if !b.granted("anvil", "network") {
		t.Fatal("everywhere grant not loaded")
	}
	if err := b.Revoke("anvil", "network"); err != nil || len(read(user)) != 0 {
		t.Fatalf("revoke: %v %+v", err, read(user))
	}
}
