package manifest

import "github.com/drjzlyan/dhi/internal/agentkit/clirun"

// FallbackStatus is one agent's fallback as doctor shows it (F-180).
type FallbackStatus struct {
	Agent    string
	Engine   string // the agent's effective engine, "cli:<name>" ("" = inherits)
	Fallback string // "cli:<name>", "none", or "" (never chosen)
	Model    string
	Resolves bool   // the fallback CLI is installed
	Why      string // the reason it does not resolve, or why there is none
}

// FallbackFor reports a manifest's fallback against a CLI registry. engine
// is the agent's effective CLI name ("" = unknown).
func FallbackFor(reg *clirun.Registry, a *Agent, engine string) FallbackStatus {
	st := FallbackStatus{Agent: a.ID, Fallback: a.FallbackEngine, Model: a.FallbackModel}
	if engine != "" {
		st.Engine = EngineString(engine)
	}
	switch {
	case a.FallbackEngine == "":
		st.Why = "no fallback chosen yet (none could be picked: no other CLI is installed)"
	case a.FallbackEngine == FallbackNone:
		st.Why = "none, by choice"
	case reg == nil:
		st.Why = "no CLI registry"
	default:
		if _, err := reg.Path(a.FallbackRuntime); err != nil {
			st.Why = a.FallbackRuntime + " is not installed"
		} else {
			st.Resolves = true
		}
	}
	return st
}
