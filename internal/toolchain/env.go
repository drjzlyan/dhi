package toolchain

import (
	"os"
	"strings"
)

// Env returns the environment for DHI's child processes (terminals, LSP
// servers, git, agents): the shim directory is prepended to PATH so the
// hermetic tools resolve first — and only here. The user's shell and
// desktop are never touched (ADR-0005).
//
// A nil base inherits os.Environ(); an explicitly empty base stays
// empty apart from the shim-only PATH, keeping fully isolated children
// free of host leakage.
func (m *Manager) Env(base []string) []string {
	if base == nil {
		base = os.Environ()
	}
	shim := m.ShimDir()
	out := make([]string, 0, len(base)+1)
	sawPath := false
	for _, kv := range base {
		if kv == "PATH=" || strings.HasPrefix(kv, "PATH=") {
			cur := strings.TrimPrefix(kv, "PATH=")
			if firstPathEntry(cur) == shim {
				out = append(out, kv) // idempotent: already shim-first
			} else if cur == "" {
				out = append(out, "PATH="+shim)
			} else {
				out = append(out, "PATH="+shim+string(os.PathListSeparator)+cur)
			}
			sawPath = true
			continue
		}
		out = append(out, kv)
	}
	if !sawPath {
		switch {
		case len(base) == 0: // explicitly empty env stays host-free
			out = append(out, "PATH="+shim)
		case os.Getenv("PATH") == "":
			out = append(out, "PATH="+shim)
		default:
			out = append(out, "PATH="+shim+string(os.PathListSeparator)+os.Getenv("PATH"))
		}
	}
	return out
}

func firstPathEntry(pathList string) string {
	first, _, _ := strings.Cut(pathList, string(os.PathListSeparator))
	return first
}
