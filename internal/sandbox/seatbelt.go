package sandbox

import (
	"fmt"
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
	bin     string
	profile string
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
	if bin == "" {
		return nil, fmt.Errorf("sandbox: seatbelt: empty binary path")
	}
	p, err := seatbeltProfile(rw, ro)
	if err != nil {
		return nil, err
	}
	return &Seatbelt{bin: bin, profile: p}, nil
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
func seatbeltProfile(rw, ro []string) (string, error) {
	if len(rw) == 0 {
		return "", fmt.Errorf("sandbox: seatbelt: no rw roots")
	}
	var b strings.Builder
	b.WriteString("(version 1)\n")
	b.WriteString("(deny default)\n")
	// Network is policy-engine territory (ADR-0006); fs/process are the
	// OS layer's job.
	b.WriteString("(allow network*)\n")
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
	return b.String(), nil
}

// sbplQuote renders an absolute path as an SBPL string literal.
func sbplQuote(p string) string {
	if !strings.HasPrefix(p, "/") {
		p = "/" + p
	}
	return `"` + strings.ReplaceAll(strings.ReplaceAll(p, `\`, `\\`), `"`, `\"`) + `"`
}
