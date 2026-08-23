package sandbox

import (
	"fmt"
	"path/filepath"
)

// Sandbox is the OS-isolation seam (ADR-0006): adapters wrap command
// execution in platform sandboxes (bubblewrap on Linux, seatbelt on
// macOS). The default Noop adapter changes nothing so core milestones
// stay unblocked while isolation strengthens incrementally.
type Sandbox interface {
	// Name identifies the adapter ("noop", "seatbelt", "bubblewrap").
	Name() string
	// Wrap returns argv to execute for the requested command.
	Wrap(argv []string) ([]string, error)
}

// Noop is the pass-through adapter used until OS adapters land.
type Noop struct{}

// Name implements Sandbox.
func (Noop) Name() string { return "noop" }

// Wrap implements Sandbox by returning argv unchanged.
func (Noop) Wrap(argv []string) ([]string, error) {
	if len(argv) == 0 {
		return nil, fmt.Errorf("sandbox: empty argv")
	}
	return argv, nil
}

// Guard couples a Jail with a Policy and the active Sandbox adapter;
// it is the single seam agents go through for fs/exec requests.
type Guard struct {
	Jail    *Jail
	Policy  *Policy
	Sandbox Sandbox
}

// NewGuard wires a guard, defaulting to a Noop sandbox.
func NewGuard(jail *Jail, policy *Policy) *Guard {
	return &Guard{Jail: jail, Policy: policy, Sandbox: Noop{}}
}

// Check evaluates op against absPath: outside the jail denies regardless
// of policy; inside consults the policy relative to the owning root.
func (g *Guard) Check(op Op, absPath string) Decision {
	_, rel, ok := g.Jail.Root(filepath.Clean(absPath))
	if !ok {
		return Decision{Effect: Deny, Reason: "path outside workspace jail"}
	}
	return g.Policy.Evaluate(op, filepath.ToSlash(rel))
}

// Exec returns the wrapped argv after confirming exec is allowed by
// policy (exec rules carry no path scope; they are workspace-wide).
func (g *Guard) Exec(argv []string) ([]string, Decision, error) {
	d := g.Policy.Evaluate(OpExec, "")
	if d.Effect != Allow {
		return nil, d, fmt.Errorf("sandbox: exec denied: %s", d.Reason)
	}
	wrapped, err := g.Sandbox.Wrap(argv)
	return wrapped, d, err
}
