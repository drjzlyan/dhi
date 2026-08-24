// Package doctor runs DHI's self-check suite. The same report feeds
// `dhi doctor [--json]` and the in-app health panel; statuses are
// coarse (ok/warn/fail) and every failure degrades visibly rather than
// aborting (ADR-0005).
package doctor

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/drjzlyan/dhi/internal/agentkit/manifest"
	"github.com/drjzlyan/dhi/internal/settings"
	"github.com/drjzlyan/dhi/internal/toolchain"
	"github.com/drjzlyan/dhi/internal/workspace"
)

// Status is the outcome severity of one check.
type Status string

// Check statuses.
const (
	OK   Status = "ok"
	Warn Status = "warn"
	Fail Status = "fail"
)

// Check is one named probe result.
type Check struct {
	Name   string `json:"name"`
	Status Status `json:"status"`
	Detail string `json:"detail,omitempty"`
}

// Report aggregates checks from all suites.
type Report struct {
	Checks  []Check `json:"checks"`
	Healthy bool    `json:"healthy"`
}

// Run executes every registered suite.
func Run(toolRoot, wsRoot string) Report {
	var r Report
	r.Checks = append(r.Checks, Toolchain(toolRoot)...)
	r.Checks = append(r.Checks, Workspace(wsRoot)...)
	r.Checks = append(r.Checks, Config(wsRoot)...)
	r.Checks = append(r.Checks, Agents(wsRoot)...)
	r.Healthy = true
	for _, c := range r.Checks {
		if c.Status == Fail {
			r.Healthy = false
			break
		}
	}
	return r
}

// JSON renders the report for `dhi doctor --json`.
func (r Report) JSON() ([]byte, error) {
	data, err := json.MarshalIndent(r, "", "  ")
	if err != nil {
		return nil, fmt.Errorf("doctor: encode report: %w", err)
	}
	return append(data, '\n'), nil
}

// Toolchain probes the hermetic prefix: existence, writability, lockfile
// sanity, locked payloads on disk, and staging cleanliness.
func Toolchain(root string) []Check {
	if root == "" {
		return nil
	}
	m := toolchain.New(root)

	if _, err := os.Stat(filepath.Join(root)); os.IsNotExist(err) {
		return []Check{{
			Name:   "toolchain/prefix",
			Status: Warn,
			Detail: fmt.Sprintf("%s not installed yet (first bootstrap will create it)", root),
		}}
	}

	checks := []Check{}
	f, err := os.CreateTemp(root, ".doctor-*")
	if err != nil {
		checks = append(checks, Check{Name: "toolchain/prefix", Status: Fail,
			Detail: fmt.Sprintf("%s is not writable: %v", root, err)})
		return checks
	}
	tmpName := f.Name()
	f.Close()
	os.Remove(tmpName)
	checks = append(checks, Check{Name: "toolchain/prefix", Status: OK, Detail: root})

	lf, err := m.ReadLockfile()
	switch {
	case err != nil:
		checks = append(checks, Check{Name: "toolchain/lockfile", Status: Fail, Detail: err.Error()})
		return checks
	case len(lf.Tools) == 0:
		checks = append(checks, Check{Name: "toolchain/lockfile", Status: Warn, Detail: "nothing installed yet"})
	default:
		for name, locked := range lf.Tools {
			dir := filepath.Join(root, locked.Path)
			if _, err := os.Stat(dir); err != nil {
				checks = append(checks, Check{Name: "toolchain/" + name, Status: Fail,
					Detail: fmt.Sprintf("locked %s missing on disk: %s", locked.Version, dir)})
				continue
			}
			checks = append(checks, Check{Name: "toolchain/" + name, Status: OK,
				Detail: fmt.Sprintf("locked %s", locked.Version)})
		}
	}

	if entries, _ := os.ReadDir(filepath.Join(root, "staging")); len(entries) > 0 {
		checks = append(checks, Check{Name: "toolchain/staging", Status: Warn,
			Detail: fmt.Sprintf("%d leftover staging dir(s)", len(entries))})
	}
	return checks
}

// Workspace probes the DHI workspace at root (skipped with a warning
// when root is not a workspace).
func Workspace(root string) []Check {
	if root == "" {
		return nil
	}
	ws, err := workspace.Load(root)
	if err != nil {
		return []Check{{Name: "workspace/config", Status: Warn, Detail: err.Error()}}
	}
	checks := []Check{{Name: "workspace/config", Status: OK,
		Detail: fmt.Sprintf("%d member(s)", len(ws.Members))}}
	for _, dir := range []string{
		workspace.DirAgents, workspace.DirMemory, workspace.DirKnowledge,
		workspace.DirChannels, workspace.DirTasks,
	} {
		info, err := os.Stat(filepath.Join(root, dir))
		if err != nil || !info.IsDir() {
			checks = append(checks, Check{Name: "workspace/" + filepath.Base(dir), Status: Warn,
				Detail: dir + " reserved but absent"})
		}
	}
	return checks
}

// Config probes settings files for unknown keys (F-006: doctor reports
// them). Only the workspace file is probed here; the user-level file is
// covered by the same routine when callers have its path.
func Config(wsRoot string) []Check {
	if wsRoot == "" {
		return nil
	}
	path := filepath.Join(wsRoot, ".dhi", "config.toml")
	data, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return nil
	}
	if err != nil {
		return []Check{{Name: "settings/config", Status: Warn, Detail: err.Error()}}
	}
	unknown, uerr := settings.UnknownKeys(data)
	if uerr != nil {
		return []Check{{Name: "settings/config", Status: Warn,
			Detail: path + ": " + uerr.Error()}}
	}
	if len(unknown) == 0 {
		return []Check{{Name: "settings/config", Status: OK}}
	}
	return []Check{{Name: "settings/config", Status: Warn,
		Detail: "unknown keys in " + path + ": " + strings.Join(unknown, ", ")}}
}

// Agents validates the roster under .dhi/agents (F-007): every manifest
// must parse, and each declared provider env var should be set (missing
// keys warn — turns fail later otherwise).
func Agents(wsRoot string) []Check {
	if wsRoot == "" {
		return nil
	}
	roster, err := manifest.LoadDir(filepath.Join(wsRoot, workspace.DirAgents))
	if err != nil {
		return []Check{{Name: "agents/roster", Status: Fail, Detail: err.Error()}}
	}
	if len(roster) == 0 {
		return nil // no crew is a valid configuration
	}
	checks := []Check{{Name: "agents/roster", Status: OK,
		Detail: fmt.Sprintf("%d agent(s): %s", len(roster), joinIDs(roster))}}
	for _, a := range roster {
		if a.EnvVar == "" {
			continue
		}
		if os.Getenv(a.EnvVar) == "" {
			checks = append(checks, Check{Name: "agents/" + a.ID, Status: Warn,
				Detail: fmt.Sprintf("%s not set; %s cannot reach its provider", a.EnvVar, a.ID)})
		}
	}
	return checks
}

func joinIDs(roster []*manifest.Agent) string {
	ids := make([]string, 0, len(roster))
	for _, a := range roster {
		ids = append(ids, a.ID)
	}
	return strings.Join(ids, ", ")
}
