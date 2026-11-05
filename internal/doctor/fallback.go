package doctor

import (
	"fmt"
	"path/filepath"
	"strings"

	"github.com/drjzlyan/dhi/internal/agentkit/clirun"
	"github.com/drjzlyan/dhi/internal/agentkit/manifest"
	"github.com/drjzlyan/dhi/internal/settings"
	"github.com/drjzlyan/dhi/internal/workspace"
)

// Fallbacks lists each agent's fallback engine and whether it resolves
// (F-180, ADR-0048): one row, warning when a chosen fallback is not
// installed, since a failover would then have nowhere to go.
func Fallbacks(wsRoot string) []Check {
	if wsRoot == "" {
		return nil
	}
	roster, err := manifest.LoadDir(filepath.Join(wsRoot, workspace.DirAgents))
	if err != nil || len(roster) == 0 {
		return nil // agents/roster reports a broken roster
	}
	reg := clirun.NewRegistry(lookPath)
	def := ""
	if best, _ := settings.LoadBestEffort("", filepath.Join(wsRoot, ".dhi", "config.toml")); true {
		if name, perr := manifest.ParseEngine(best.Engine); perr == nil {
			def = name
		}
	}
	var parts []string
	warn := false
	for _, a := range roster {
		eng := a.Runtime
		if eng == "" {
			eng = def
		}
		st := manifest.FallbackFor(reg, a, eng)
		var d string
		switch {
		case st.Resolves && st.Model != "":
			d = fmt.Sprintf("%s → %s (%s, resolves)", orDefault(st.Engine), st.Fallback, st.Model)
		case st.Resolves:
			d = fmt.Sprintf("%s → %s (resolves)", orDefault(st.Engine), st.Fallback)
		case a.FallbackEngine == "" || a.FallbackEngine == manifest.FallbackNone:
			d = "no fallback (" + st.Why + ")"
		default:
			d = fmt.Sprintf("%s → %s: %s", orDefault(st.Engine), st.Fallback, st.Why)
			warn = true
		}
		parts = append(parts, a.ID+": "+d)
	}
	c := Check{Name: "agents/fallback", Status: OK, Detail: strings.Join(parts, "; ")}
	if warn {
		c.Status = Warn
		c.Detail += ". Install it or change the agent's fallback in Settings → AGENTS"
	}
	return []Check{c}
}

func orDefault(engine string) string {
	if engine == "" {
		return "default engine"
	}
	return engine
}
