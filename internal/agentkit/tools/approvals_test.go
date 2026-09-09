package tools

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/drjzlyan/dhi/internal/sandbox"
)

func TestApprovalFlow(t *testing.T) {
	a := NewApprovals()
	req := make(chan *Approval, 1)
	a.OnRequest = func(ap *Approval) { req <- ap }

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	done := make(chan error, 1)
	go func() { done <- a.Ask(ctx, "scout", sandbox.OpWrite, "api/x.md", "policy: docs/** asks") }()

	ap := <-req
	if ap.Agent != "scout" || ap.Op != sandbox.OpWrite || ap.Target != "api/x.md" {
		t.Fatalf("approval = %+v", ap)
	}
	list := a.List()
	if len(list) != 1 || list[0].ID != ap.ID {
		t.Fatalf("pending = %+v", list)
	}
	if !a.Resolve(ap.ID, true) {
		t.Fatal("resolve failed for pending id")
	}
	if err := <-done; err != nil {
		t.Fatalf("allowed wait errored: %v", err)
	}
	if n := len(a.List()); n != 0 {
		t.Fatalf("pending after resolve = %d", n)
	}
}

func TestApprovalDenyAndUnknownResolve(t *testing.T) {
	a := NewApprovals()
	req := make(chan *Approval, 1)
	a.OnRequest = func(ap *Approval) { req <- ap }

	done := make(chan error, 1)
	go func() { done <- a.Ask(context.Background(), "scout", sandbox.OpWrite, "x", "") }()
	ap := <-req

	if !a.Resolve(ap.ID, false) {
		t.Fatal("deny resolve failed")
	}
	if err := <-done; err == nil {
		t.Fatal("denied wait must error")
	}
	if a.Resolve(99, true) {
		t.Fatal("unknown id must not resolve")
	}
}

func TestApprovalChangesSignals(t *testing.T) {
	a := NewApprovals()
	ch := a.Changes()
	// Fresh queue: nothing pending yet, so no signal.
	select {
	case <-ch:
		t.Fatal("signal before any change")
	default:
	}
	done := make(chan error, 1)
	go func() { done <- a.Ask(context.Background(), "scout", sandbox.OpRead, "api", "") }()
	select {
	case <-ch:
	case <-time.After(2 * time.Second):
		t.Fatal("no signal after push")
	}
	// Resolving the pushed approval signals again.
	if n := len(a.List()); n != 1 {
		t.Fatalf("pending = %d", n)
	}
	a.Resolve(a.List()[0].ID, true)
	if err := <-done; err != nil {
		t.Fatal(err)
	}
}

func TestApprovalCancelRemovesPending(t *testing.T) {
	a := NewApprovals()
	req := make(chan *Approval, 1)
	a.OnRequest = func(ap *Approval) { req <- ap }
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() {
		err := a.Ask(ctx, "scout", sandbox.OpExec, "ls", "")
		done <- err
	}()
	<-req
	if len(a.List()) != 1 {
		t.Fatal("approval not parked")
	}
	cancel()
	if err := <-done; !errors.Is(err, context.Canceled) {
		t.Fatalf("cancel error = %v", err)
	}
	// the parked approval is removed on cancel
	deadline := time.After(2 * time.Second)
	for len(a.List()) != 0 {
		select {
		case <-time.After(10 * time.Millisecond):
		case <-deadline:
			t.Fatal("pending not cleared on cancel")
		}
	}
}
