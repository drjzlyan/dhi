// Package doctor runs DHI's self-check suite. The same report feeds
// `dhi doctor [--json]` and the in-app health panel; statuses are
// coarse (ok/warn/fail) and every failure degrades visibly rather than
// aborting (ADR-0005).
package doctor

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/drjzlyan/dhi/internal/tasks"

	agentkitStandards "github.com/drjzlyan/dhi/internal/agentkit/standards"

	"github.com/drjzlyan/dhi/internal/agentkit/manifest"
	"github.com/drjzlyan/dhi/internal/agentkit/org"
	"github.com/drjzlyan/dhi/internal/gitcore"
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
	r.Checks = append(r.Checks, Git(toolRoot)...)
	r.Checks = append(r.Checks, Workspace(wsRoot)...)
	r.Checks = append(r.Checks, Config(wsRoot)...)
	r.Checks = append(r.Checks, Agents(wsRoot)...)
	r.Checks = append(r.Checks, Standards(wsRoot)...)
	r.Checks = append(r.Checks, Tasks(wsRoot)...)
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

// Git probes hermetic git readiness (ADR-0009): silent while the
// embedded registry carries no git pin (pre-release), otherwise the
// lockfile entry, shim link, and `git --version` must all agree with
// the pinned version.
func Git(toolRoot string) []Check {
	mf, err := toolchain.Embedded()
	if err != nil {
		return []Check{{Name: "git/shim", Status: Warn,
			Detail: "embedded registry unreadable: " + err.Error()}}
	}
	pin, ok := mf.Tools["git"]
	if !ok {
		return nil // pin not flipped yet; feature absent by design
	}
	return gitChecks(pin.Version, toolRoot)
}

func gitChecks(version, root string) []Check {
	if _, err := os.Stat(root); os.IsNotExist(err) {
		return []Check{{Name: "git/shim", Status: Warn,
			Detail: fmt.Sprintf("git %s pending first bootstrap", version)}}
	}
	m := toolchain.New(root)

	lf, err := m.ReadLockfile()
	if err != nil {
		return []Check{{Name: "git/shim", Status: Fail, Detail: err.Error()}}
	}
	locked, isLocked := lf.Tools["git"]
	switch {
	case !isLocked:
		return []Check{{Name: "git/shim", Status: Warn,
			Detail: fmt.Sprintf("not installed yet (bootstrap will fetch git %s)", version)}}
	case locked.Version != version:
		return []Check{{Name: "git/shim", Status: Warn,
			Detail: fmt.Sprintf("locked %s, registry pins %s (bootstrap will upgrade)",
				locked.Version, version)}}
	}

	shim := m.GitBin()
	if _, err := os.Stat(shim); err != nil {
		return []Check{{Name: "git/shim", Status: Fail,
			Detail: fmt.Sprintf("locked %s but shim missing: %s", locked.Version, shim)}}
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	r := gitcore.NewRunner(shim, m.GitEnv(nil))
	got, err := r.Version(ctx)
	if err != nil {
		return []Check{{Name: "git/version", Status: Fail,
			Detail: strings.TrimSpace(err.Error())}}
	}
	if got != version {
		return []Check{{Name: "git/version", Status: Fail,
			Detail: fmt.Sprintf("shim reports %s, registry pins %s", got, version)}}
	}
	return []Check{{Name: "git/version", Status: OK,
		Detail: fmt.Sprintf("hermetic git %s", got)}}
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
		Detail: fmt.Sprintf("%d member(s)", len(ws.Members()))}}
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

// Standards probes .dhi/standards.toml (F-003 layered instructions):
// parse failures warn (the runtime silently degrades to built-ins), and
// references to unknown teams or agents warn so typos surface.
func Standards(wsRoot string) []Check {
	if wsRoot == "" {
		return nil
	}
	snap, err := agentkitStandards.Inspect(wsRoot)
	if err != nil {
		return []Check{{Name: "standards/config", Status: Warn,
			Detail: err.Error() + " (runtime falls back to built-ins)"}}
	}
	total := len(snap.Workspace) + len(snap.Teams) + len(snap.Agents)
	if total == 0 {
		return []Check{{Name: "standards/config", Status: OK,
			Detail: "no custom layers; built-in defaults apply"}}
	}

	var warnings []string
	o, oerr := org.Load(wsRoot)
	validTeams := map[string]bool{}
	if oerr == nil {
		for _, t := range o.Teams() {
			validTeams[t.Name] = true
		}
	}
	for slug := range snap.Teams {
		if oerr != nil || !validTeams[slug] {
			warnings = append(warnings, "team "+slug+" not in org.toml")
		}
	}
	roster, rerr := manifest.LoadDir(filepath.Join(wsRoot, workspace.DirAgents))
	validIDs := map[string]bool{}
	if rerr == nil {
		for _, a := range roster {
			validIDs[a.ID] = true
		}
		for _, id := range manifest.ArchivedIDs(filepath.Join(wsRoot, workspace.DirAgents)) {
			validIDs[id] = true
		}
	}
	for id := range snap.Agents {
		if rerr != nil || !validIDs[id] {
			warnings = append(warnings, "agent "+id+" not on roster")
		}
	}
	sort.Strings(warnings)

	detail := fmt.Sprintf("%d workspace, %d team, %d agent rule(s)",
		len(snap.Workspace), len(snap.Teams), len(snap.Agents))
	if len(warnings) > 0 {
		return []Check{{Name: "standards/config", Status: Warn,
			Detail: detail + "; " + strings.Join(warnings, "; ")}}
	}
	return []Check{{Name: "standards/config", Status: OK, Detail: detail}}
}

// Tasks probes .dhi/tasks/ (F-003 kanban): malformed cards warn (they
// are skipped at load), dangling assignee/team references warn.
func Tasks(wsRoot string) []Check {
	if wsRoot == "" {
		return nil
	}
	ws, err := workspace.Load(wsRoot)
	if err != nil {
		return nil // not a workspace; workspace/config already reported
	}
	store, err := tasks.Open(ws)
	if err != nil {
		return []Check{{Name: "tasks/store", Status: Warn, Detail: err.Error()}}
	}
	all := store.List()
	if w := store.Warnings(); len(w) > 0 {
		return []Check{{Name: "tasks/store", Status: Warn,
			Detail: fmt.Sprintf("%d malformed card(s): %s", len(w), strings.Join(w, "; "))}}
	}
	if len(all) == 0 {
		return nil // no cards is healthy
	}

	var warnings []string
	validTeams := map[string]bool{}
	if o, oerr := org.Load(wsRoot); oerr == nil {
		for _, tm := range o.Teams() {
			validTeams[tm.Name] = true
		}
	}
	roster, rerr := manifest.LoadDir(filepath.Join(wsRoot, workspace.DirAgents))
	validIDs := map[string]bool{"you": true}
	if rerr == nil {
		for _, a := range roster {
			validIDs[a.ID] = true
		}
	}
	members := map[string]bool{}
	for _, m := range ws.Members() {
		members[m.Name] = true
	}
	for _, t := range all {
		if t.Assignee != "" && !validIDs[t.Assignee] {
			warnings = append(warnings, t.Slug+": assignee "+t.Assignee+" not on roster")
		}
		if t.Team != "" && !validTeams[t.Team] {
			warnings = append(warnings, t.Slug+": team "+t.Team+" not in org.toml")
		}
		for _, cs := range t.ChangeSets {
			if !members[cs.Member] {
				warnings = append(warnings, t.Slug+": changeset member "+cs.Member+" not registered")
			}
		}
	}
	sort.Strings(warnings)
	detail := fmt.Sprintf("%d task(s)", len(all))
	if len(warnings) > 0 {
		return []Check{{Name: "tasks/store", Status: Warn,
			Detail: detail + "; " + strings.Join(warnings, "; ")}}
	}
	return []Check{{Name: "tasks/store", Status: OK, Detail: detail}}
}
