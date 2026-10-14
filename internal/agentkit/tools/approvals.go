package tools

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"time"

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
	Scope  string // capability scope (F-030 P2), "write:<abs path>" (F-087) or "secret:<NAME>" (F-089); "" = no grant-memory
	// Missing marks a secret request whose value is not stored yet: the
	// answering surface collects it (masked) before allowing (F-089).
	Missing bool

	verdict chan decision
}

// IsAccess reports a sandbox-access or secret request (F-087, F-089): a
// plain yes remembers it for the workspace — either is useless for one
// call only.
func (ap *Approval) IsAccess() bool {
	return strings.HasPrefix(ap.Scope, AccessWrite) || strings.HasPrefix(ap.Scope, AccessExec) || ap.IsSecret()
}

// IsSecret reports a request to use a stored secret (F-089).
func (ap *Approval) IsSecret() bool { return strings.HasPrefix(ap.Scope, AccessSecret) }

// SecretName is the secret a request names ("" for other requests).
func (ap *Approval) SecretName() string {
	n, _ := strings.CutPrefix(ap.Scope, AccessSecret)
	if !ap.IsSecret() {
		return ""
	}
	return n
}

// AccessWrite prefixes a write-access scope: "write:" + absolute path.
const AccessWrite = "write:"

// AccessExec prefixes a run-programs-from scope: "exec:" + absolute dir.
const AccessExec = "exec:"

// AccessSecret prefixes a secret-use scope: "secret:" + NAME.
const AccessSecret = "secret:"

// decision is a human verdict: allow/deny plus where to remember it
// ("" = not remembered).
type decision struct {
	allow bool
	layer Layer
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
	grants    map[string]grantRec // agent|scope → always-allow (F-030 P2), with its layer (F-087)
	// grantsPath persists workspace grants across restarts (F-067);
	// userGrantsPath the "everywhere" ones (F-087). "" = in memory.
	grantsPath, userGrantsPath string
}

// Layer is how long a grant lasts (F-087): this DHI session only, this
// workspace (.dhi/grants.json), or every workspace (the user config).
type Layer string

const (
	LayerOnce       Layer = "once"
	LayerWorkspace  Layer = "workspace"
	LayerEverywhere Layer = "everywhere"
)

type grantRec struct {
	at    time.Time
	layer Layer
}

// NewApprovals returns an empty queue. The change-signal channel is
// created here and never reassigned, so Changes/signal may read it
// without a lock (an Approvals must come from this constructor).
func NewApprovals() *Approvals {
	return &Approvals{changes: make(chan struct{}, 1)}
}

// Changes returns a channel receiving a token whenever the pending set
// changes (push, resolve, cancel). The token carries no payload; callers
// re-read List(). The channel is created once and shared by all callers.
func (a *Approvals) Changes() <-chan struct{} { return a.changes }

// signal wakes Changes() listeners. a.changes is immutable after
// NewApprovals, so the field read is lock-free; the send is
// non-blocking. It must NOT take a.mu — Resolve already holds it.
func (a *Approvals) signal() {
	if a.changes == nil {
		return
	}
	select {
	case a.changes <- struct{}{}:
	default:
	}
}

// Ask parks a decision until Resolve answers or ctx is done. It is the
// seam UIs, guards, and the future CLI permission-prompt bridge use to
// surface an operator prompt.
func (a *Approvals) Ask(ctx context.Context, agent string, op sandbox.Op, target, reason string) error {
	return a.AskScope(ctx, agent, "", op, target, reason)
}

// AskScope is Ask with a capability scope (F-030 P2): a "grant always"
// verdict records an agent+scope grant so subsequent asks skip the
// prompt. An empty scope never grants.
func (a *Approvals) AskScope(ctx context.Context, agent, scope string, op sandbox.Op, target, reason string) error {
	return a.ask(ctx, &Approval{Agent: agent, Op: op, Target: target, Reason: reason, Scope: scope})
}

// AskSecret parks a request to use the secret name (F-089); missing
// tells the answering surface to collect the value first.
func (a *Approvals) AskSecret(ctx context.Context, agent, name, reason string, missing bool) error {
	return a.ask(ctx, &Approval{Agent: agent, Op: sandbox.OpRead, Target: name, Reason: reason,
		Scope: AccessSecret + name, Missing: missing})
}

func (a *Approvals) ask(ctx context.Context, ap *Approval) error {
	agent, scope, op, target := ap.Agent, ap.Scope, ap.Op, ap.Target
	if scope != "" && !ap.Missing && a.granted(agent, scope) {
		return nil
	}
	ap.verdict = make(chan decision, 1)
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
	case d := <-ap.verdict:
		if !d.allow {
			return fmt.Errorf("denied by operator: %s %s", op, target)
		}
		if d.layer != "" && scope != "" {
			a.grant(agent, scope, d.layer)
		}
		return nil
	case <-ctx.Done():
		a.remove(ap.ID)
		return ctx.Err()
	}
}

func grantKey(agent, scope string) string { return agent + "|" + scope }

// Granted reports a remembered grant of scope to agent (any layer).
func (a *Approvals) Granted(agent, scope string) bool { return a.granted(agent, scope) }

func (a *Approvals) granted(agent, scope string) bool {
	a.mu.Lock()
	defer a.mu.Unlock()
	_, ok := a.grants[grantKey(agent, scope)]
	return ok
}

func (a *Approvals) grant(agent, scope string, layer Layer) {
	a.mu.Lock()
	if a.grants == nil {
		a.grants = map[string]grantRec{}
	}
	a.grants[grantKey(agent, scope)] = grantRec{at: time.Now().UTC(), layer: layer}
	a.mu.Unlock()
	_ = a.saveGrants() // best-effort: the grant holds for this session regardless
}

// List snapshots pending approvals oldest-first.
func (a *Approvals) List() []*Approval {
	a.mu.Lock()
	defer a.mu.Unlock()
	out := make([]*Approval, len(a.pending))
	copy(out, a.pending)
	return out
}

// PendingFor reports whether agent is waiting on any pending approval.
func (a *Approvals) PendingFor(agent string) bool {
	a.mu.Lock()
	defer a.mu.Unlock()
	for _, ap := range a.pending {
		if ap.Agent == agent {
			return true
		}
	}
	return false
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
	return a.resolve(id, decision{allow: allow})
}

// ResolveAlways allows the approval and, when it carries a scope,
// records an agent+scope grant for this workspace so future asks skip
// the prompt.
func (a *Approvals) ResolveAlways(id int) bool {
	return a.ResolveFor(id, LayerWorkspace)
}

// ResolveFor allows the approval and remembers its scope at layer
// (F-087): once (this session), this workspace, or everywhere.
func (a *Approvals) ResolveFor(id int, layer Layer) bool {
	return a.resolve(id, decision{allow: true, layer: layer})
}

// Decide applies a human answer from any surface (F-087), so every
// surface means the same thing by the same key:
//   - "yes": allow; a sandbox-access request is remembered for this
//     workspace (one call is no use to a sandbox fixed at spawn).
//   - "once": allow; access lasts this DHI session.
//   - "always": allow and remember for this workspace.
//   - "everywhere": allow and remember for every workspace.
//   - "no": deny.
func (a *Approvals) Decide(ap *Approval, choice string) bool {
	switch choice {
	case "yes":
		if ap.IsAccess() {
			return a.ResolveFor(ap.ID, LayerWorkspace)
		}
		return a.Resolve(ap.ID, true)
	case "once":
		if ap.IsAccess() {
			return a.ResolveFor(ap.ID, LayerOnce)
		}
		return a.Resolve(ap.ID, true)
	case "always":
		return a.ResolveFor(ap.ID, LayerWorkspace)
	case "everywhere":
		return a.ResolveFor(ap.ID, LayerEverywhere)
	case "no":
		return a.Resolve(ap.ID, false)
	}
	return false
}

// DecideKeys maps the shared approval keys to Decide choices.
var DecideKeys = map[string]string{"y": "yes", "o": "once", "a": "always", "g": "everywhere", "n": "no"}

// DecideHint is the one-line legend for those keys.
const DecideHint = "y yes · o once · a always here · g everywhere · n no"

// Describe renders an approval for a card: "write /path" for access,
// else op and target.
func (ap *Approval) Describe() string {
	if p, ok := strings.CutPrefix(ap.Scope, AccessWrite); ok {
		return "write access to " + p
	}
	if p, ok := strings.CutPrefix(ap.Scope, AccessExec); ok {
		return "running programs from " + p
	}
	if n := ap.SecretName(); n != "" {
		if ap.Missing {
			return "the secret " + n + " (not stored yet: you'll type it, masked)"
		}
		return "use the secret " + n
	}
	return string(ap.Op) + " " + ap.Target
}

func (a *Approvals) resolve(id int, d decision) bool {
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
	found.verdict <- d
	close(found.verdict)
	return true
}

func (a *Approvals) remove(id int) {
	a.mu.Lock()
	defer a.mu.Unlock()
	kept := a.pending[:0]
	for _, ap := range a.pending {
		if ap.ID != id {
			kept = append(kept, ap)
		}
	}
	a.pending = kept
}
