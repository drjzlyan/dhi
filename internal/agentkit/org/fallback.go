package org

import (
	"fmt"

	"github.com/drjzlyan/dhi/internal/agentkit/manifest"
	"github.com/drjzlyan/dhi/internal/workspace"
)

// DefaultFallback picks the default fallback CLI for an agent whose own
// engine is primary ("" = it inherits the workspace default) from the CLIs
// installed here (F-180, ADR-0048). It returns "" and a note when there is
// none to offer. The app sets it once at start; nil picks nothing.
var DefaultFallback func(primary string) (cli, note string)

// ApplyFallback records the default fallback on an agent that has never
// had one chosen, so it is visible in its manifest and Settings. A choice
// (including "none") is never touched. It returns the note explaining a
// missing default ("" when one was set or nothing applies).
func ApplyFallback(a *manifest.Agent) string {
	if a.FallbackEngine != "" || DefaultFallback == nil {
		return ""
	}
	cli, note := DefaultFallback(a.Runtime)
	if cli == "" {
		return note
	}
	a.FallbackEngine, a.FallbackRuntime = manifest.EngineString(cli), cli
	return ""
}

// EnsureFallbacks gives every rostered agent without a recorded fallback
// the default one and writes it back. It returns the ids it changed.
// Agents whose default cannot be picked stay unset (a later start, with
// another CLI installed, tries again).
func EnsureFallbacks(ws *workspace.Workspace) ([]string, error) {
	if DefaultFallback == nil {
		return nil, nil
	}
	roster, err := LoadRoster(ws)
	if err != nil {
		return nil, err
	}
	var changed []string
	for _, a := range roster {
		if a.FallbackEngine != "" {
			continue
		}
		ApplyFallback(a)
		if a.FallbackEngine == "" {
			continue
		}
		if err := manifest.WriteFile(RosterDir(ws), a); err != nil {
			return changed, fmt.Errorf("org: fallback for %s: %w", a.ID, err)
		}
		changed = append(changed, a.ID)
	}
	return changed, nil
}
