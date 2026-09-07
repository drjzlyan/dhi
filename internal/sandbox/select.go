package sandbox

import (
	"fmt"
	"os/exec"
	"runtime"
)

// LookPath matches exec.LookPath's signature so tests can inject a
// deterministic resolver.
type LookPath func(string) (string, error)

// systemLookPath is the production resolver.
var systemLookPath LookPath = exec.LookPath

// Detect reports which adapter this platform can run: "seatbelt" on
// darwin when sandbox-exec resolves, "bubblewrap" on linux when bwrap
// resolves, "noop" otherwise (including other GOOS values). The name is
// what doctor surfaces.
func Detect(goos string, look LookPath) string {
	switch goos {
	case "darwin":
		if _, err := look("sandbox-exec"); err == nil {
			return "seatbelt"
		}
	case "linux":
		if _, err := look("bwrap"); err == nil {
			return "bubblewrap"
		}
	}
	return "noop"
}

// Select builds the platform adapter for the given roots: rw roots are
// writable inside the sandbox, ro roots readable/executable only.
// Strict (ADR-0011): a missing helper binary or unsupported platform is
// an ERROR naming the fix — never a silent Noop downgrade. mode "off"
// returns Noop as the user's explicit opt-out.
func Select(goos string, look LookPath, mode string, rw, ro []string) (Sandbox, error) {
	if mode == "off" {
		return Noop{}, nil
	}
	bin := adapterBinary(goos)
	if bin == "" {
		return nil, fmt.Errorf("sandbox: no OS isolation adapter for %s (DHI requires one; see ADR-0011)", goos)
	}
	path, err := look(bin)
	if err != nil {
		return nil, fmt.Errorf("sandbox: %s not found (install it, or set security.sandbox = \"off\" in settings to opt out explicitly)", bin)
	}
	switch goos {
	case "darwin":
		return NewSeatbelt(path, rw, ro)
	case "linux":
		return NewBubblewrap(path, rw, ro)
	default:
		return nil, fmt.Errorf("sandbox: no OS isolation adapter for %s", goos)
	}
}

// Require verifies the platform helper exists without building an
// adapter (used when there is nothing to confine yet, e.g. no
// workspace): the strict requirement is on the helper, ADR-0011.
func Require(goos string, look LookPath) error {
	bin := adapterBinary(goos)
	if bin == "" {
		return fmt.Errorf("sandbox: no OS isolation adapter for %s (DHI requires one; see ADR-0011)", goos)
	}
	if _, err := look(bin); err != nil {
		return fmt.Errorf("sandbox: %s not found (install it, or set security.sandbox = \"off\" in settings to opt out explicitly)", bin)
	}
	return nil
}

// SelectDefault is Select with the host GOOS and exec.LookPath.
func SelectDefault(mode string, rw, ro []string) (Sandbox, error) {
	return Select(runtime.GOOS, systemLookPath, mode, rw, ro)
}

func adapterBinary(goos string) string {
	switch goos {
	case "darwin":
		return "sandbox-exec"
	case "linux":
		return "bwrap"
	default:
		return ""
	}
}
