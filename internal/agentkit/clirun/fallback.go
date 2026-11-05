package clirun

import "strings"

// FallbackOrder is the fixed order DHI picks a default fallback engine in
// (G-C5, ADR-0048): the first installed CLI in this list that is not the
// agent's own. It matches the setup wizard's default-engine preference;
// CLIs not listed come after, alphabetically.
var FallbackOrder = []string{"claude", "codex", "opencode", "cursor-agent", "copilot", "antigravity"}

// FallbackNone is the manifest value of "no fallback, deliberately".
const FallbackNone = "none"

// PickFallback chooses the default fallback CLI for an agent whose own
// engine is primary, from the installed CLI names. It returns the CLI name
// ("" = none) and a note naming why there is none (empty when one was
// picked). Pure: the same inputs always give the same answer.
func PickFallback(primary string, installed []string) (cli, note string) {
	rank := func(n string) int {
		for i, p := range FallbackOrder {
			if p == n {
				return i
			}
		}
		return len(FallbackOrder)
	}
	best := ""
	for _, n := range installed {
		if n == "" || n == primary {
			continue
		}
		if best == "" || rank(n) < rank(best) || rank(n) == rank(best) && n < best {
			best = n
		}
	}
	if best == "" {
		if primary == "" {
			return "", "no fallback engine: no CLI is installed"
		}
		return "", "no fallback engine: " + primary + " is the only installed CLI"
	}
	return best, ""
}

// Installed lists the registered CLIs whose binary resolves on this
// machine, in registry order. Cheap (a PATH lookup, no --version probe).
func (r *Registry) Installed() []string {
	var out []string
	for _, c := range r.All() {
		if _, err := r.look(c.Bin); err == nil {
			out = append(out, c.Name)
		}
	}
	return out
}

// FallbackLabel renders a manifest fallback value for people.
func FallbackLabel(v string) string {
	switch strings.TrimSpace(v) {
	case "":
		return "not set"
	case FallbackNone:
		return "none"
	}
	return strings.TrimPrefix(v, "cli:")
}
