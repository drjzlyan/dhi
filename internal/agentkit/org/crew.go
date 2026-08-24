// Crew operations: the org package owns the company lifecycle — creating,
// editing, archiving, and restoring agents. Manifest files under
// .dhi/agents stay the single identity store; org only orchestrates
// validated writes and archive moves, so hand-edited rosters and UI-made
// changes converge on identical on-disk state.
package org

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/drjzlyan/dhi/internal/agentkit/manifest"
	"github.com/drjzlyan/dhi/internal/workspace"
)

// RosterDir resolves the active roster directory of ws.
func RosterDir(ws *workspace.Workspace) string {
	return filepath.Join(ws.Root, workspace.DirAgents)
}

// LoadRoster parses every active agent manifest (thin alias so callers
// need only this package for crew reads).
func LoadRoster(ws *workspace.Workspace) ([]*manifest.Agent, error) {
	return manifest.LoadDir(RosterDir(ws))
}

// CreateAgent validates-then-writes a new manifest; existing ids are
// rejected (updates go through UpdateAgent explicitly).
func (o *Org) CreateAgent(ws *workspace.Workspace, a *manifest.Agent) error {
	dir := RosterDir(ws)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return fmt.Errorf("org: roster dir: %w", err)
	}
	if _, err := os.Stat(filepath.Join(dir, a.ID+".toml")); err == nil {
		return fmt.Errorf("org: agent %q already exists", a.ID)
	} else if !os.IsNotExist(err) {
		return err
	}
	if _, archived := o.archivedExists(ws, a.ID); archived {
		return fmt.Errorf("org: agent %q is archived (restore it instead)", a.ID)
	}
	return manifest.WriteFile(dir, a)
}

// UpdateAgent overwrites an existing manifest in place.
func (o *Org) UpdateAgent(ws *workspace.Workspace, a *manifest.Agent) error {
	dir := RosterDir(ws)
	if _, err := os.Stat(filepath.Join(dir, a.ID+".toml")); err != nil {
		return fmt.Errorf("org: unknown agent %q", a.ID)
	}
	return manifest.WriteFile(dir, a)
}

// ArchiveAgent moves its manifest out of the active roster. The runtime
// reloads rosters through Runtime.Reload — nothing here touches it.
func (o *Org) ArchiveAgent(ws *workspace.Workspace, id string) error {
	return manifest.Archive(RosterDir(ws), id)
}

// RestoreAgent brings an archived manifest back into rotation.
func (o *Org) RestoreAgent(ws *workspace.Workspace, id string) error {
	return manifest.Restore(RosterDir(ws), id)
}

// Archived lists archived agent ids sorted.
func (o *Org) Archived(ws *workspace.Workspace) []string {
	return manifest.ArchivedIDs(RosterDir(ws))
}

func (o *Org) archivedExists(ws *workspace.Workspace, id string) (string, bool) {
	for _, a := range o.Archived(ws) {
		if a == id {
			return id, true
		}
	}
	return "", false
}
