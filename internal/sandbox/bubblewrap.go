package sandbox

import (
	"fmt"
)

// Bubblewrap wraps command execution in Linux's bwrap (bubblewrap).
// The process starts from fully unshared namespaces, re-binds the
// minimal system tree read-only, and binds DHI-managed trees: rw roots
// writable, ro roots read-only. Network stays shared (policy-engine
// territory, ADR-0006); everything else is namespaced away.
type Bubblewrap struct {
	bin string
	rw  []string
	ro  []string
}

// bwrapSystemDirs are re-bound read-only so linked binaries, loaders,
// and /etc lookups keep working inside the namespace.
var bwrapSystemDirs = []string{"/usr", "/bin", "/sbin", "/lib", "/lib64", "/etc"}

// NewBubblewrap builds the adapter around a bwrap binary path. rw roots
// bind writable; ro roots bind read-only. Roots must be absolute.
func NewBubblewrap(bin string, rw, ro []string) (*Bubblewrap, error) {
	if bin == "" {
		return nil, fmt.Errorf("sandbox: bubblewrap: empty binary path")
	}
	if len(rw) == 0 {
		return nil, fmt.Errorf("sandbox: bubblewrap: no rw roots")
	}
	for _, r := range append(append([]string(nil), rw...), ro...) {
		if len(r) == 0 || r[0] != '/' {
			return nil, fmt.Errorf("sandbox: bubblewrap: root %q is not absolute", r)
		}
	}
	return &Bubblewrap{bin: bin, rw: rw, ro: ro}, nil
}

// Name implements Sandbox.
func (b *Bubblewrap) Name() string { return "bubblewrap" }

// Wrap implements Sandbox: bwrap flags then the original argv.
func (b *Bubblewrap) Wrap(argv []string) ([]string, error) {
	if len(argv) == 0 {
		return nil, fmt.Errorf("sandbox: empty argv")
	}
	out := make([]string, 0, len(argv)+len(b.rw)*2+len(b.ro)*2+len(bwrapSystemDirs)*2+8)
	out = append(out, b.bin,
		"--unshare-all", "--share-net", "--die-with-parent",
		"--dev-bind", "/dev", "/dev",
		"--proc", "/proc",
		"--tmpfs", "/tmp",
	)
	for _, d := range bwrapSystemDirs {
		out = append(out, "--ro-bind", d, d)
	}
	for _, r := range b.rw {
		out = append(out, "--bind", r, r)
	}
	for _, r := range b.ro {
		out = append(out, "--ro-bind", r, r)
	}
	return append(out, argv...), nil
}

// Roots returns copies of the rw/ro root lists (for doctor/tests).
func (b *Bubblewrap) Roots() (rw, ro []string) {
	return append([]string(nil), b.rw...), append([]string(nil), b.ro...)
}
