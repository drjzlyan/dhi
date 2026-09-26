package sandbox

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// Seatbelt wraps command execution in macOS's sandbox-exec (SBPL
// profiles). The profile is deny-by-default with scoped allows derived
// from the root lists, so a wrapped process can only touch DHI-managed
// trees plus the system paths dyld and exec need. Network stays allowed:
// network permission is governed by the policy engine at the tool layer
// (ADR-0006); the OS layer is defense-in-depth against path/process
// escape.
type Seatbelt struct {
	bin          string
	profile      string
	rw           []string
	ro           []string
	allowNetwork bool
}

// seatbeltSystemDirs are read/exec allows every macOS process needs
// (dyld caches, libSystem, shells, core utils).
var seatbeltSystemDirs = []string{
	"/bin", "/usr/bin", "/sbin", "/usr/sbin", "/usr/lib", "/System",
	"/private/var/db/dyld", "/dev",
}

// NewSeatbelt builds the adapter around a sandbox-exec binary path and
// generates its profile. rw roots are readable+ writable; ro roots are
// readable/executable only. Roots must be absolute and canonical (jail
// roots already are — NewJail canonicalizes them).
func NewSeatbelt(bin string, rw, ro []string) (*Seatbelt, error) {
	return NewSeatbeltNetwork(bin, rw, ro, true)
}

// NewSeatbeltNetwork builds a seatbelt adapter whose profile either
// allows or denies network (F-030 P2: DHI-served children are
// network-denied by default; host agent CLIs keep network).
func NewSeatbeltNetwork(bin string, rw, ro []string, allowNetwork bool) (*Seatbelt, error) {
	if bin == "" {
		return nil, fmt.Errorf("sandbox: seatbelt: empty binary path")
	}
	p, err := seatbeltProfile(rw, ro, allowNetwork)
	if err != nil {
		return nil, err
	}
	return &Seatbelt{bin: bin, profile: p, rw: append([]string(nil), rw...),
		ro: append([]string(nil), ro...), allowNetwork: allowNetwork}, nil
}

// WrapNetwork implements NetworkPolicy: it re-generates the profile
// with the requested network posture for one invocation.
func (s *Seatbelt) WrapNetwork(argv []string, allow bool) ([]string, error) {
	if len(argv) == 0 {
		return nil, fmt.Errorf("sandbox: empty argv")
	}
	p, err := seatbeltProfile(s.rw, s.ro, allow)
	if err != nil {
		return nil, err
	}
	out := make([]string, 0, len(argv)+4)
	out = append(out, s.bin, "-p", p, "--")
	return append(out, argv...), nil
}

// WithExtraRoots implements RootExtender: a new Seatbelt whose profile
// also allows the extra rw roots (merged, deduped). Used by the runtime
// to admit each rostered CLI's state + binary roots (claude lives under
// ~/.local and writes ~/.claude — neither belongs to the workspace jail).
func (s *Seatbelt) WithExtraRoots(rw []string) (Sandbox, error) {
	if len(rw) == 0 {
		return s, nil
	}
	seen := map[string]bool{}
	for _, r := range s.rw {
		seen[r] = true
	}
	merged := append([]string(nil), s.rw...)
	for _, r := range rw {
		if r == "" || seen[r] {
			continue
		}
		seen[r] = true
		merged = append(merged, r)
	}
	return NewSeatbeltNetwork(s.bin, merged, s.ro, s.allowNetwork)
}

// Name implements Sandbox.
func (s *Seatbelt) Name() string { return "seatbelt" }

// Wrap implements Sandbox: `sandbox-exec -p <profile> -- argv…`.
func (s *Seatbelt) Wrap(argv []string) ([]string, error) {
	if len(argv) == 0 {
		return nil, fmt.Errorf("sandbox: empty argv")
	}
	out := make([]string, 0, len(argv)+4)
	out = append(out, s.bin, "-p", s.profile, "--")
	return append(out, argv...), nil
}

// Profile returns the generated SBPL source (exposed for doctor/tests).
func (s *Seatbelt) Profile() string { return s.profile }

// seatbeltProfile generates the SBPL source. Deny-by-default; every
// allow is scoped to a registered tree or a system path.
func seatbeltProfile(rw, ro []string, allowNetwork bool) (string, error) {
	if len(rw) == 0 {
		return "", fmt.Errorf("sandbox: seatbelt: no rw roots")
	}
	var b strings.Builder
	b.WriteString("(version 1)\n")
	b.WriteString("(deny default)\n")
	// Network posture is explicit (F-030 P2): DHI-served children are
	// denied by default; host agent CLIs pass allowNetwork=true.
	if allowNetwork {
		b.WriteString("(allow network*)\n")
	} else {
		b.WriteString("(deny network*)\n")
	}
	b.WriteString("(allow process-fork)\n")
	b.WriteString("(allow sysctl-read)\n")
	b.WriteString("(allow mach-lookup)\n")

	subpaths := func(roots []string) string {
		var sb strings.Builder
		for _, r := range roots {
			sb.WriteString(` (subpath ` + sbplQuote(r) + `)`)
		}
		return sb.String()
	}
	// Read: rw + ro roots + system dirs. Write: rw roots only.
	b.WriteString("(allow file-read*" + subpaths(rw) + subpaths(ro) + subpaths(seatbeltSystemDirs) + ")\n")
	b.WriteString("(allow file-write*" + subpaths(rw) + ")\n")
	// Exec: registered trees + system dirs.
	b.WriteString("(allow process-exec*" + subpaths(rw) + subpaths(ro) + subpaths(seatbeltSystemDirs) + ")\n")

	// Host CLIs (claude et al) read the world as the user — bun/node
	// runtimes touch system fonts, timezone data, cert stores, and the
	// CLI's own tool state; enumerating those per-CLI is unbounded and
	// every missed tree aborts the binary cryptically (SIGABRT, no
	// stderr). Reads therefore open to the user's scope, and the
	// credential trees are fenced explicitly (a deny wins over an allow
	// in SBPL). The REAL boundaries stay deny-default: writes are
	// jailed to the workspace + declared roots, and exec only to the
	// admitted trees (F-025/ADR-0012: the CLI runs as the user, the
	// sandbox guards path/process escape).
	b.WriteString("(allow file-read*)\n")
	for _, d := range seatbeltCredentialDenies() {
		if d != "" {
			b.WriteString(`(deny file-read* (subpath ` + sbplQuote(d) + `))` + "\n")
		}
	}
	return b.String(), nil
}

// seatbeltCredentialDenies lists the user's credential trees reads may
// never cross, even with broad file-read. Missing dirs are harmless
// (a deny on an absent path never matches).
func seatbeltCredentialDenies() []string {
	home, err := os.UserHomeDir()
	if err != nil || home == "" {
		return nil
	}
	return []string{
		filepath.Join(home, ".ssh"),
		filepath.Join(home, ".gnupg"),
		filepath.Join(home, ".aws"),
		filepath.Join(home, ".kube"),
		filepath.Join(home, ".config", "gcloud"),
		filepath.Join(home, ".azure"),
		filepath.Join(home, ".netrc"),
	}
}

// sbplQuote renders an absolute path as an SBPL string literal.
func sbplQuote(p string) string {
	if !strings.HasPrefix(p, "/") {
		p = "/" + p
	}
	return `"` + strings.ReplaceAll(strings.ReplaceAll(p, `\`, `\\`), `"`, `\"`) + `"`
}
