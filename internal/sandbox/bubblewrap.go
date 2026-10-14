package sandbox

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// Bubblewrap wraps command execution in Linux's bwrap (bubblewrap).
// The process starts from fully unshared namespaces, re-binds the
// minimal system tree read-only, and binds DHI-managed trees: rw roots
// writable, ro roots read-only. Network stays shared (policy-engine
// territory, ADR-0006); everything else is namespaced away.
type Bubblewrap struct {
	bin          string
	rw           []string
	ro           []string
	allowNetwork bool
	guard        []string // read-only binds over rw subtrees (Guarded)
	reopen       []string // writable binds inside a guard
}

// bwrapSystemDirs are re-bound read-only so linked binaries, loaders,
// and /etc lookups keep working inside the namespace.
var bwrapSystemDirs = []string{"/usr", "/bin", "/sbin", "/lib", "/lib64", "/etc"}

// NewBubblewrap builds the adapter around a bwrap binary path. rw roots
// bind writable; ro roots bind read-only. Roots must be absolute.
func NewBubblewrap(bin string, rw, ro []string) (*Bubblewrap, error) {
	return NewBubblewrapNetwork(bin, rw, ro, true)
}

// NewBubblewrapNetwork builds a bubblewrap adapter that either shares
// the network namespace (allow) or unshares it (deny) — F-030 P2.
func NewBubblewrapNetwork(bin string, rw, ro []string, allowNetwork bool) (*Bubblewrap, error) {
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
	return &Bubblewrap{bin: bin, rw: rw, ro: ro, allowNetwork: allowNetwork}, nil
}

// WithExtraReadOnly implements ReadOnlyExtender: extra read-only binds.
func (b *Bubblewrap) WithExtraReadOnly(ro []string) (Sandbox, error) {
	if len(ro) == 0 {
		return b, nil
	}
	cp := *b
	cp.ro = mergeRoots(b.ro, ro)
	return &cp, nil
}

// WithExtraRoots implements RootExtender: a new Bubblewrap binding the
// extra rw roots on top of the existing set.
func (b *Bubblewrap) WithExtraRoots(rw []string) (Sandbox, error) {
	if len(rw) == 0 {
		return b, nil
	}
	seen := map[string]bool{}
	for _, r := range b.rw {
		seen[r] = true
	}
	merged := append([]string(nil), b.rw...)
	for _, r := range rw {
		if r == "" || seen[r] {
			continue
		}
		seen[r] = true
		merged = append(merged, r)
	}
	nb, err := NewBubblewrapNetwork(b.bin, merged, b.ro, b.allowNetwork)
	if err != nil {
		return nil, err
	}
	nb.guard, nb.reopen = b.guard, b.reopen
	return nb, nil
}

// WithGuarded implements Guarder: guard subtrees are bound read-only on
// top of the rw binds, then reopen subtrees writable again (a later bind
// shadows an earlier one). Paths that do not exist are skipped.
func (b *Bubblewrap) WithGuarded(guard, reopen []string) (Sandbox, error) {
	nb := *b
	nb.guard, nb.reopen = append([]string(nil), guard...), append([]string(nil), reopen...)
	return &nb, nil
}

// guardArgs are the bind flags for the guarded subtrees.
func (b *Bubblewrap) guardArgs() []string {
	var out []string
	for _, g := range b.guard {
		if _, err := os.Stat(g); err == nil {
			out = append(out, "--ro-bind", g, g)
		}
	}
	for _, r := range b.reopen {
		paths := []string{r}
		if dir, ok := strings.CutSuffix(r, "/*"); ok {
			// Every existing subdirectory, expanded at wrap time (a task
			// worktree created since the last spawn is bound too).
			paths = nil
			if es, err := os.ReadDir(dir); err == nil {
				for _, e := range es {
					if e.IsDir() {
						paths = append(paths, filepath.Join(dir, e.Name()))
					}
				}
			}
		}
		for _, p := range paths {
			if _, err := os.Stat(p); err == nil {
				out = append(out, "--bind", p, p)
			}
		}
	}
	return out
}

// Name implements Sandbox.
func (b *Bubblewrap) Name() string { return "bubblewrap" }

// Wrap implements Sandbox: bwrap flags then the original argv.
func (b *Bubblewrap) Wrap(argv []string) ([]string, error) {
	if len(argv) == 0 {
		return nil, fmt.Errorf("sandbox: empty argv")
	}
	net := "--share-net"
	if !b.allowNetwork {
		net = "--unshare-net"
	}
	out := make([]string, 0, len(argv)+len(b.rw)*2+len(b.ro)*2+len(bwrapSystemDirs)*2+8)
	out = append(out, b.bin,
		"--unshare-all", net, "--die-with-parent",
		"--dev-bind", "/dev", "/dev",
		"--proc", "/proc",
		"--tmpfs", "/tmp",
	)
	for _, d := range bwrapSystemDirs {
		// -try: a dir the distro lacks (/lib64 on arm64 Debian) is
		// skipped; a plain bind refused every spawn (live, M32).
		out = append(out, "--ro-bind-try", d, d)
	}
	// ro first, rw on top: a writable child of a read-only root (the
	// toolchain's caches inside the read-only toolchain) must stay
	// writable, and a later bind shadows an earlier one at the same place.
	for _, r := range b.ro {
		out = append(out, "--ro-bind", r, r)
	}
	for _, r := range b.rw {
		out = append(out, "--bind", r, r)
	}
	out = append(out, b.guardArgs()...)
	out = append(out, b.credentialMasks()...)
	return append(out, argv...), nil
}

// credentialMasks hides the credential paths that fall inside a bound
// root (F-088): bubblewrap shows only what is bound, but a CLI state root
// or a grant could cover ~/.config. A directory becomes an empty tmpfs,
// a file /dev/null.
func (b *Bubblewrap) credentialMasks() []string {
	var out []string
	for _, c := range CredentialPaths() {
		inside := false
		for _, r := range append(append([]string(nil), b.ro...), b.rw...) {
			if c == r || strings.HasPrefix(c, r+string(filepath.Separator)) {
				inside = true
				break
			}
		}
		if !inside {
			continue
		}
		fi, err := os.Stat(c)
		switch {
		case err != nil:
			continue
		case fi.IsDir():
			out = append(out, "--tmpfs", c)
		default:
			out = append(out, "--ro-bind", "/dev/null", c)
		}
	}
	return out
}

// WrapNetwork implements NetworkPolicy: it binds with the requested
// network posture for one invocation.
func (b *Bubblewrap) WrapNetwork(argv []string, allow bool) ([]string, error) {
	net := "--share-net"
	if !allow {
		net = "--unshare-net"
	}
	out := make([]string, 0, len(argv)+len(b.rw)*2+len(b.ro)*2+len(bwrapSystemDirs)*2+8)
	out = append(out, b.bin, "--unshare-all", net, "--die-with-parent",
		"--dev-bind", "/dev", "/dev", "--proc", "/proc", "--tmpfs", "/tmp")
	for _, d := range bwrapSystemDirs {
		// -try: a dir the distro lacks (/lib64 on arm64 Debian) is
		// skipped; a plain bind refused every spawn (live, M32).
		out = append(out, "--ro-bind-try", d, d)
	}
	// ro first, rw on top: a writable child of a read-only root (the
	// toolchain's caches inside the read-only toolchain) must stay
	// writable, and a later bind shadows an earlier one at the same place.
	for _, r := range b.ro {
		out = append(out, "--ro-bind", r, r)
	}
	for _, r := range b.rw {
		out = append(out, "--bind", r, r)
	}
	out = append(out, b.guardArgs()...)
	out = append(out, b.credentialMasks()...)
	return append(out, argv...), nil
}

// Roots returns copies of the rw/ro root lists (for doctor/tests).
func (b *Bubblewrap) Roots() (rw, ro []string) {
	return append([]string(nil), b.rw...), append([]string(nil), b.ro...)
}
