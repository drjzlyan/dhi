package tools

import (
	"context"
	"fmt"
	"sync"

	"github.com/drjzlyan/dhi/internal/sandbox"
)

// Approval is one parked sandbox Ask decision awaiting a human verdict.
// The tool goroutine blocks in wait() while the UI inspects List() and
// rules via Resolve.
type Approval struct {
	ID     int
	Agent  string
	Op     sandbox.Op
	Target string // vpath or other display target
	Reason string // policy explanation

	verdict chan bool
}

// Approvals is the pending-approval queue shared by all agents in a
// workspace. OnRequest fires on Push so UIs can surface prompts without
// polling; it must be set before first use and never block. Changes()
// exposes a change signal channel for event-driven UIs.
type Approvals struct {
	mu        sync.Mutex
	seq       int
	pending   []*Approval
	OnRequest func(*Approval)
	changes   chan struct{}
}

// NewApprovals returns an empty queue.
func NewApprovals() *Approvals { return &Approvals{} }

// Changes returns a channel receiving a token whenever the pending set
// changes (push, resolve, cancel). The token carries no payload; callers
// re-read List(). The channel is created once and shared by all callers.
func (a *Approvals) Changes() <-chan struct{} {
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.changes == nil {
		a.changes = make(chan struct{}, 1)
	}
	return a.changes
}

func (a *Approvals) signal() {
	if a.changes == nil {
		return
	}
	select {
	case a.changes <- struct{}{}:
	default:
	}
}

// wait parks the caller until Resolve answers or ctx is done. It is the
// only method called from tool goroutines.
func (a *Approvals) wait(ctx context.Context, agent string, op sandbox.Op, target, reason string) error {
	ap := &Approval{
		Agent:   agent,
		Op:      op,
		Target:  target,
		Reason:  reason,
		verdict: make(chan bool, 1),
	}
	a.mu.Lock()
	a.seq++
	ap.ID = a.seq
	a.pending = append(a.pending, ap)
	cb := a.OnRequest
	a.mu.Unlock()
	if cb != nil {
		cb(ap)
	}
	a.signal()
	select {
	case allow := <-ap.verdict:
		if !allow {
			return fmt.Errorf("denied by operator: %s %s", op, target)
		}
		return nil
	case <-ctx.Done():
		a.remove(ap.ID)
		return ctx.Err()
	}
}

// List snapshots pending approvals oldest-first.
func (a *Approvals) List() []*Approval {
	a.mu.Lock()
	defer a.mu.Unlock()
	out := make([]*Approval, len(a.pending))
	copy(out, a.pending)
	return out
}

// Get returns a pending approval by id.
func (a *Approvals) Get(id int) (*Approval, bool) {
	a.mu.Lock()
	defer a.mu.Unlock()
	for _, ap := range a.pending {
		if ap.ID == id {
			return ap, true
		}
	}
	return nil, false
}

// Resolve answers the approval: true allows the parked operation to
// proceed. Unknown ids return false.
func (a *Approvals) Resolve(id int, allow bool) bool {
	a.mu.Lock()
	var found *Approval
	kept := a.pending[:0]
	for _, ap := range a.pending {
		if ap.ID == id {
			found = ap
			continue
		}
		kept = append(kept, ap)
	}
	a.pending = kept
	a.signal()
	a.mu.Unlock()
	if found == nil {
		return false
	}
	found.verdict <- allow
	close(found.verdict)
	return true
}

func (a *Approvals) remove(id int) {
	a.mu.Lock()
	kept := a.pending[:0]
	for _, ap := range a.pending {
		if ap.ID != id {
			kept = append(kept, ap)
		}
	}
	a.pending = kept
}
