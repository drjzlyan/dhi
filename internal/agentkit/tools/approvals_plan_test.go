package tools

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/drjzlyan/dhi/internal/sandbox"
)

// F-144: a plan card is a one-time yes or no; no choice remembers it.
func TestPlanApprovalIsNeverRemembered(t *testing.T) {
	a := NewApprovals()
	errc := make(chan error, 1)
	go func() {
		errc <- a.AskScope(context.Background(), "scout", AccessPlan+"scout", sandbox.OpRead, "Add the flag\nTest it", "why")
	}()
	var ap *Approval
	for i := 0; i < 200 && ap == nil; i++ {
		if l := a.List(); len(l) > 0 {
			ap = l[0]
		}
		time.Sleep(5 * time.Millisecond)
	}
	if ap == nil || !ap.IsPlan() || ap.IsAccess() {
		t.Fatalf("card = %+v", ap)
	}
	if d := ap.Describe(); !strings.Contains(d, "2 steps") || !strings.Contains(d, "Add the flag; Test it") {
		t.Errorf("Describe = %q", d)
	}
	a.Decide(ap, "everywhere")
	if err := <-errc; err != nil {
		t.Fatal(err)
	}
	if a.Granted("scout", ap.Scope) {
		t.Fatal("a plan approval must not become a grant")
	}
}
