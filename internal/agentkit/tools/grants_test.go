package tools

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/drjzlyan/dhi/internal/sandbox"
)

// F-067: "allow always" survives a restart, and a revoke asks again.
func TestGrantsPersistAndRevoke(t *testing.T) {
	path := filepath.Join(t.TempDir(), ".dhi", "grants.json")
	a := NewApprovals()
	if err := a.Persist(path); err != nil {
		t.Fatal(err)
	}
	a.OnRequest = func(ap *Approval) { go a.ResolveAlways(ap.ID) }
	if err := a.AskScope(context.Background(), "dev", "git", sandbox.OpExec, "tool git_commit", "x"); err != nil {
		t.Fatal(err)
	}

	b := NewApprovals() // the next session
	if err := b.Persist(path); err != nil {
		t.Fatal(err)
	}
	if gs := b.Grants(); len(gs) != 1 || gs[0].Agent != "dev" || gs[0].Scope != "git" || gs[0].At.IsZero() {
		t.Fatalf("grants = %+v", gs)
	}
	asked := false
	b.OnRequest = func(ap *Approval) { asked = true; go b.Resolve(ap.ID, true) }
	if err := b.AskScope(context.Background(), "dev", "git", sandbox.OpExec, "tool git_commit", "x"); err != nil || asked {
		t.Fatalf("a remembered grant must not ask (asked=%v err=%v)", asked, err)
	}
	if err := b.Revoke("dev", "git"); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	if err := b.AskScope(ctx, "dev", "git", sandbox.OpExec, "tool git_commit", "x"); err != nil || !asked {
		t.Fatalf("a revoked grant must ask again (asked=%v err=%v)", asked, err)
	}
	c := NewApprovals()
	if err := c.Persist(path); err != nil || len(c.Grants()) != 0 {
		t.Fatalf("revoke not persisted: %+v %v", c.Grants(), err)
	}
}

func TestGrantsMalformedRefuses(t *testing.T) {
	path := filepath.Join(t.TempDir(), "grants.json")
	if err := os.WriteFile(path, []byte("{nope"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := NewApprovals().Persist(path); err == nil {
		t.Fatal("a malformed grants file must refuse by name")
	}
}
