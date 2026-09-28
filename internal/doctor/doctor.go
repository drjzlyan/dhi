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
	"os/exec"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"time"

	"github.com/drjzlyan/dhi/internal/autopilot"
	"github.com/drjzlyan/dhi/internal/ideation"
	"github.com/drjzlyan/dhi/internal/sandbox"
	"github.com/drjzlyan/dhi/internal/tasks"

	agentkitStandards "github.com/drjzlyan/dhi/internal/agentkit/standards"
	agentkitWorkflow "github.com/drjzlyan/dhi/internal/agentkit/workflow"

	"github.com/drjzlyan/dhi/internal/agentkit/clirun"
	"github.com/drjzlyan/dhi/internal/agentkit/dhitools"
	"github.com/drjzlyan/dhi/internal/agentkit/manifest"
	"github.com/drjzlyan/dhi/internal/agentkit/org"
	"github.com/drjzlyan/dhi/internal/agentkit/scopes"
	"github.com/drjzlyan/dhi/internal/gitcore"
	"github.com/drjzlyan/dhi/internal/settings"
	"github.com/drjzlyan/dhi/internal/toolchain"
	"github.com/drjzlyan/dhi/internal/unread"
	"github.com/drjzlyan/dhi/internal/workspace"

	"github.com/drjzlyan/dhi/internal/agentkit/bus"
)

// lookPath resolves an executable on PATH; swappable in tests.
var lookPath = exec.LookPath

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
	r.Checks = append(r.Checks, Identity(toolRoot)...)
	r.Checks = append(r.Checks, Workspace(wsRoot)...)
	r.Checks = append(r.Checks, Config(wsRoot)...)
	r.Checks = append(r.Checks, Agents(wsRoot)...)
	r.Checks = append(r.Checks, AgentTools(wsRoot)...)
	r.Checks = append(r.Checks, Authority(wsRoot)...)
	r.Checks = append(r.Checks, Standards(wsRoot)...)
	r.Checks = append(r.Checks, Workflows(wsRoot)...)
	r.Checks = append(r.Checks, Dependencies(wsRoot)...)
	r.Checks = append(r.Checks, Runtimes()...)
	r.Checks = append(r.Checks, Tasks(wsRoot)...)
	r.Checks = append(r.Checks, RunStore(wsRoot)...)
	r.Checks = append(r.Checks, Autopilots(wsRoot)...)
	r.Checks = append(r.Checks, UnreadStore(wsRoot)...)
	r.Checks = append(r.Checks, Sessions(wsRoot)...)
	r.Checks = append(r.Checks, GH(toolRoot)...)
	r.Checks = append(r.Checks, Sandbox(sandboxMode(wsRoot))...)
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
	_ = f.Close()
	_ = os.Remove(tmpName)
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
	// Build stamps may prefix 'v' (ours do); compare semantically.
	trim := func(s string) string { return strings.TrimPrefix(s, "v") }
	if trim(got) != trim(version) {
		return []Check{{Name: "git/version", Status: Fail,
			Detail: fmt.Sprintf("shim reports %s, registry pins %s", got, version)}}
	}
	return []Check{{Name: "git/version", Status: OK,
		Detail: fmt.Sprintf("hermetic git %s", got)}}
}

// Identity reports the user's git identity (F-029): OK when user.name
// and user.email resolve through the hermetic git binary under the host
// config, Warn (naming the exact fix) when unset. Silent while git is
// not installed — the Git suite already covers that. Unset is a Warn,
// not a Fail: the IDE boots and agents run; commit paths refuse at use
// with the same message (ADR-0011, "refused capability surfaces at use").
func Identity(toolRoot string) []Check {
	mf, err := toolchain.Embedded()
	if err != nil {
		return nil
	}
	if _, ok := mf.Tools["git"]; !ok {
		return nil
	}
	m := toolchain.New(toolRoot)
	if _, err := os.Stat(m.GitBin()); err != nil {
		return nil
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	r := gitcore.NewRunner(m.GitBin(), m.GitIdentityEnv(nil))
	id, err := gitcore.ResolveIdentity(ctx, r)
	if err != nil {
		return []Check{{Name: "identity", Status: Warn, Detail: err.Error()}}
	}
	return []Check{{Name: "identity", Status: OK,
		Detail: fmt.Sprintf("%s <%s>", id.Name, id.Email)}}
}

// Authority reports each agent's effective capability scopes (F-030
// P2): default → workspace (settings [scopes]) → team → manifest. OK
// always; the detail lists non-default effects by agent.
func Authority(wsRoot string) []Check {
	if wsRoot == "" {
		return nil
	}
	roster, err := manifest.LoadDir(filepath.Join(wsRoot, workspace.DirAgents))
	if err != nil || len(roster) == 0 {
		return nil
	}
	wsScopes := scopes.Set{}
	if best, _ := settings.LoadBestEffort("", filepath.Join(wsRoot, ".dhi", "config.toml")); true {
		for name, eff := range best.Scopes {
			sc, e1 := scopes.ParseScope(name)
			e, e2 := scopes.ParseEffect(eff)
			if e1 == nil && e2 == nil {
				wsScopes[sc] = e
			}
		}
	}
	var company *org.Org
	if o, lerr := org.Load(wsRoot); lerr == nil {
		company = o
	}
	def := scopes.Default()
	order := []scopes.Scope{scopes.Read, scopes.Write, scopes.Exec, scopes.Network, scopes.Git, scopes.Push, scopes.Admin}
	var overrides []string
	for _, a := range roster {
		layers := []scopes.Set{def}
		if len(wsScopes) > 0 {
			layers = append(layers, wsScopes)
		}
		if company != nil {
			for _, slug := range company.TeamsOf(a.ID) {
				t, ok := company.Team(slug)
				if !ok || len(t.Scopes) == 0 {
					continue
				}
				l := scopes.Set{}
				for n, e := range t.Scopes {
					sc, e1 := scopes.ParseScope(n)
					ef, e2 := scopes.ParseEffect(e)
					if e1 == nil && e2 == nil {
						l[sc] = ef
					}
				}
				layers = append(layers, l)
			}
		}
		if len(a.Scopes) > 0 {
			layers = append(layers, a.Scopes)
		}
		eff := scopes.Resolve(layers...)
		var parts []string
		for _, sc := range order {
			if eff.EffectFor(sc) != def.EffectFor(sc) {
				parts = append(parts, string(sc)+"="+string(eff.EffectFor(sc)))
			}
		}
		if len(parts) > 0 {
			overrides = append(overrides, a.ID+": "+strings.Join(parts, " "))
		}
	}
	if len(overrides) == 0 {
		return []Check{{Name: "authority", Status: OK,
			Detail: "all agents use default capability scopes"}}
	}
	return []Check{{Name: "authority", Status: OK,
		Detail: "non-default scopes — " + strings.Join(overrides, "; ")}}
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
		workspace.DirChannels, workspace.DirTasks, workspace.DirSessions,
		workspace.DirAutopilots,
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

// Agents validates the roster under .dhi/agents: every manifest must
// parse (F-007). Each agent's runtime CLI availability is probed by the
// Runtimes suite; authentication is the CLI's own concern through its
// declared pass-through (ADR-0012 §4), so there is no engine key check
// any more (ADR-0013 §6).
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
	return []Check{{Name: "agents/roster", Status: OK,
		Detail: fmt.Sprintf("%d agent(s): %s", len(roster), joinIDs(roster))}}
}

// Runtimes probes the registered host agent CLIs (F-013, ADR-0012):
// a missing binary is a FAIL (DHI never installs host CLIs — install
// it or roster the agent to another runtime), a present-but-untested
// version is a FAIL (adapters pin exact versions; re-verify and bump
// the pin, or pin the CLI), and unset declared pass-through vars warn
// so auth failures surface before a run, not mid-run.
func Runtimes() []Check {
	reg := clirun.NewRegistry(lookPath)
	if len(reg.Names()) == 0 {
		return nil
	}
	var checks []Check
	detected := reg.Detect()
	for _, c := range reg.All() {
		v := detected[c.Name]
		if v == "" {
			checks = append(checks, Check{Name: "runtime/" + c.Name, Status: Fail,
				Detail: c.Bin + " not found on PATH (DHI never installs host CLIs)"})
			continue
		}
		if v != c.Tested {
			checks = append(checks, Check{Name: "runtime/" + c.Name, Status: Fail,
				Detail: fmt.Sprintf("%s %s untested (adapter pinned to %s); re-verify and bump the pin, or pin the CLI", c.Bin, v, c.Tested)})
			continue
		}
		checks = append(checks, Check{Name: "runtime/" + c.Name, Status: OK,
			Detail: fmt.Sprintf("%s %s (adapter pinned)", c.Bin, v)})
		for _, k := range c.EnvPass {
			if k == "HOME" {
				continue // always set; not worth a row
			}
			if os.Getenv(k) == "" {
				checks = append(checks, Check{Name: "runtime/" + c.Name + "/" + strings.ToLower(k), Status: Warn,
					Detail: fmt.Sprintf("declared pass-through %s unset; %s may fail to authenticate at run time", k, c.Bin)})
			}
		}
	}
	return checks
}

// AgentTools reports the IDE-tools capability (F-028/ADR-0017): the
// runtime serves DHI's own tools (tasks/KB/memory/channels/search) to
// agents over a per-turn loopback MCP endpoint. Exactly one row: ok
// when idle or when every agent that allowlists a served tool is on an
// adapter with verified MCP wiring; warn naming each agent whose
// runtime lacks it (its dhi-action fallback stays).
func AgentTools(wsRoot string) []Check {
	if wsRoot == "" {
		return nil
	}
	roster, err := manifest.LoadDir(filepath.Join(wsRoot, workspace.DirAgents))
	if err != nil || len(roster) == 0 {
		return nil // roster health is Agents()'s row
	}
	reg := clirun.NewRegistry(lookPath)
	// Resolve the workspace default engine so agents that inherit it are
	// measured against the engine they will actually run on (ADR-0019);
	// best-effort, since doctor must run on a broken install.
	defEngine := ""
	if best, _ := settings.LoadBestEffort("", filepath.Join(wsRoot, ".dhi", "config.toml")); true {
		defEngine = best.Engine
	}
	var ready, fallback []string
	for _, a := range roster {
		served := 0
		for _, t := range a.Tools {
			if dhitools.Serves(t) {
				served++
			}
		}
		if served == 0 {
			continue
		}
		eng := a.Runtime
		if eng == "" {
			if name, err := manifest.ParseEngine(defEngine); err == nil {
				eng = name
			}
		}
		if eng == "" {
			fallback = append(fallback, fmt.Sprintf(
				"%s allows %d IDE tool(s) but declares no engine and no workspace default",
				a.ID, served))
			continue
		}
		if c, ok := reg.Get(eng); ok && c.MCPOK {
			ready = append(ready, fmt.Sprintf("%s (%s, %d tool(s))", a.ID, eng, served))
			continue
		}
		fallback = append(fallback, fmt.Sprintf(
			"%s allows %d IDE tool(s) but engine %q has no verified MCP wiring",
			a.ID, served, eng))
	}
	switch {
	case len(ready) == 0 && len(fallback) == 0:
		return []Check{{Name: "agent-tools", Status: OK,
			Detail: "no agent allowlists IDE tools (serving idle)"}}
	case len(fallback) == 0:
		return []Check{{Name: "agent-tools", Status: OK,
			Detail: "serving to " + strings.Join(ready, "; ") +
				" (containment: native CLI tools are best-effort only)"}}
	default:
		parts := fallback
		if len(ready) > 0 {
			parts = append([]string{"serving to " + strings.Join(ready, ", ")}, fallback...)
		}
		return []Check{{Name: "agent-tools", Status: Warn,
			Detail: strings.Join(parts, "; ") + " (dhi-action fallback retained)"}}
	}
}

// joinIDs renders a roster's ids for one-line details.
func joinIDs(roster []*manifest.Agent) string {
	ids := make([]string, 0, len(roster))
	for _, a := range roster {
		ids = append(ids, a.ID)
	}
	return strings.Join(ids, ", ")
}

// Standards probes .dhi/standards.toml (F-003 layered instructions):
// parse failures FAIL (ADR-0011/F-011 — turns refuse on broken
// standards; there is no silent built-ins fallback), and references to
// unknown teams or agents warn so typos surface.
func Standards(wsRoot string) []Check {
	if wsRoot == "" {
		return nil
	}
	snap, err := agentkitStandards.Inspect(wsRoot)
	if err != nil {
		return []Check{{Name: "standards/config", Status: Fail,
			Detail: err.Error() + " (turns refuse until fixed)"}}
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

// RunStore probes the run schema recorded on cards (F-014 §Part C):
// malformed [[run]] blocks warn naming the card and line, and unreadable
// agent transcript dirs warn by name. Absent runs dir = OK (nothing
// recorded yet).
func RunStore(wsRoot string) []Check {
	if wsRoot == "" {
		return nil
	}
	warns := tasks.CheckRuns(wsRoot)
	if len(warns) == 0 {
		return nil
	}
	return []Check{{Name: "runs/store", Status: Warn,
		Detail: fmt.Sprintf("%d run record(s) unhealthy: %s", len(warns), strings.Join(warns, "; "))}}
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
	// Malformed cards FAIL (ADR-0011 strict data): the store skips them
	// silently at load; doctor is where they become visible by name.
	if w := store.Warnings(); len(w) > 0 {
		return []Check{{Name: "tasks/store", Status: Fail,
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

// Sessions probes .dhi/sessions/ (F-004 ideator): malformed cards FAIL
// (ADR-0011 strict data; they are skipped at load and must be visible
// by name), invited agents not on the roster warn.
func Sessions(wsRoot string) []Check {
	if wsRoot == "" {
		return nil
	}
	ws, err := workspace.Load(wsRoot)
	if err != nil {
		return nil // not a workspace; workspace/config already reported
	}
	store, err := ideation.Open(ws)
	if err != nil {
		return []Check{{Name: "sessions/store", Status: Warn, Detail: err.Error()}}
	}
	all := store.Sessions()
	if w := store.Warnings(); len(w) > 0 {
		return []Check{{Name: "sessions/store", Status: Fail,
			Detail: fmt.Sprintf("%d malformed card(s): %s", len(w), strings.Join(w, "; "))}}
	}
	if len(all) == 0 {
		return nil // no sessions is healthy
	}

	var warnings []string
	roster, rerr := manifest.LoadDir(filepath.Join(wsRoot, workspace.DirAgents))
	validIDs := map[string]bool{}
	if rerr == nil {
		for _, a := range roster {
			validIDs[a.ID] = true
		}
	}
	for _, s := range all {
		for _, a := range s.Agents {
			if !validIDs[a] {
				warnings = append(warnings, s.ID+": invited agent "+a+" not on roster")
			}
		}
	}
	sort.Strings(warnings)
	detail := fmt.Sprintf("%d session(s)", len(all))
	if len(warnings) > 0 {
		return []Check{{Name: "sessions/store", Status: Warn,
			Detail: detail + "; " + strings.Join(warnings, "; ")}}
	}
	return []Check{{Name: "sessions/store", Status: OK, Detail: detail}}
}

// Autopilots probes .dhi/autopilots/ (F-015): malformed cards FAIL
// (ADR-0011 strict data; they are skipped at load and must be visible
// by name), dangling agent refs warn (they only refuse at run time).
func Autopilots(wsRoot string) []Check {
	if wsRoot == "" {
		return nil
	}
	ws, err := workspace.Load(wsRoot)
	if err != nil {
		return nil // not a workspace; workspace/config already reported
	}
	store, err := autopilot.Open(ws)
	if err != nil {
		return []Check{{Name: "autopilots/store", Status: Warn, Detail: err.Error()}}
	}
	all := store.List()
	if w := store.Warnings(); len(w) > 0 {
		return []Check{{Name: "autopilots/store", Status: Fail,
			Detail: fmt.Sprintf("%d malformed card(s): %s", len(w), strings.Join(w, "; "))}}
	}
	if len(all) == 0 {
		return nil // no autopilots is healthy
	}

	var warnings []string
	roster, rerr := manifest.LoadDir(filepath.Join(wsRoot, workspace.DirAgents))
	validIDs := map[string]bool{}
	if rerr == nil {
		for _, a := range roster {
			validIDs[a.ID] = true
		}
	}
	for _, c := range all {
		if !validIDs[c.Agent] {
			warnings = append(warnings, c.Slug+": agent "+c.Agent+" not on roster")
		}
	}
	sort.Strings(warnings)
	detail := fmt.Sprintf("%d autopilot(s)", len(all))
	if len(warnings) > 0 {
		return []Check{{Name: "autopilots/store", Status: Warn,
			Detail: detail + "; " + strings.Join(warnings, "; ")}}
	}
	return []Check{{Name: "autopilots/store", Status: OK, Detail: detail}}
}

// UnreadStore probes the F-017 read-mark state (.dhi/unread.json) READ-ONLY
// — never seeds, never prunes (that is the store's Open). A missing file is
// the fresh-install state (silent); a strict-decode failure is a Fail naming
// the offending key/value; a snooze pointing at a channel absent from the
// bus warns by name; expired entries are counted (dropped on next open).
func UnreadStore(wsRoot string) []Check {
	if wsRoot == "" {
		return nil
	}
	ws, err := workspace.Load(wsRoot)
	if err != nil {
		return nil // not a workspace; workspace/config already reported
	}
	raw, err := os.ReadFile(filepath.Join(wsRoot, unread.File))
	if os.IsNotExist(err) {
		return nil // fresh install: healthy silence
	}
	if err != nil {
		return []Check{{Name: "unread/store", Status: Fail, Detail: err.Error()}}
	}
	data, derr := unread.Decode(raw)
	if derr != nil {
		return []Check{{Name: "unread/store", Status: Fail, Detail: derr.Error()}}
	}

	var b *bus.Bus
	if bb, err := bus.Open(ws); err == nil {
		b = bb
	}
	channels := map[string]bool{}
	if b != nil {
		for _, ch := range b.Channels() {
			channels[ch] = true
		}
	}
	var warnings []string
	expired := 0
	now := time.Now()
	for _, sn := range data.Snoozes {
		switch {
		case !sn.Until.After(now):
			expired++
		case b != nil && !channels[sn.Channel]:
			warnings = append(warnings,
				fmt.Sprintf("snooze msg %d: channel %s not on bus", sn.MessageID, sn.Channel))
		}
	}
	sort.Strings(warnings)
	detail := fmt.Sprintf("%d watermark(s), %d snooze(s)", len(data.Channels), len(data.Snoozes))
	if expired > 0 {
		detail += fmt.Sprintf("; %d expired (dropped on next open)", expired)
	}
	if len(warnings) > 0 {
		return []Check{{Name: "unread/store", Status: Warn,
			Detail: detail + "; " + strings.Join(warnings, "; ")}}
	}
	return []Check{{Name: "unread/store", Status: OK, Detail: detail}}
}

// GH probes the hermetic gh shim (registry-pinned, ADR-0011). The host
// `gh` lookup is gone; a missing shim is a Fail — PR flows refuse until
// bootstrap provides the pinned binary.
func GH(toolRoot string) []Check {
	if toolRoot == "" {
		return nil
	}
	shim := filepath.Join(toolRoot, "bin", "gh")
	if _, err := os.Stat(shim); err != nil {
		return []Check{{Name: "gh/cli", Status: Fail,
			Detail: "gh shim not installed — PR flows refuse until bootstrap installs the pinned gh (dispatch the pin pipeline)"}}
	}
	return []Check{{Name: "gh/cli", Status: OK, Detail: shim}}
}

// sandboxMode resolves the effective sandbox setting; any load problem
// degrades to auto here because doctor must report ON a broken install,
// not refuse to run (strict boot is cmd's job, ADR-0011).
func sandboxMode(wsRoot string) string {
	user, _ := settings.DefaultUserPath()
	ws := ""
	if wsRoot != "" {
		ws = filepath.Join(wsRoot, workspace.DHIDir, "config.toml")
	}
	cfg, err := settings.LoadBestEffort(user, ws)
	if err != nil {
		return settings.SandboxAuto
	}
	return cfg.Security.Sandbox
}

// Sandbox reports which OS-isolation adapter agent guards use (F-010/
// F-011). Missing helper in auto mode is a hard requirement — Fail;
// the boot audit blocks that boot too (ADR-0011). off is the user's
// explicit opt-out: Warn so the downgrade is never silent.
func Sandbox(mode string) []Check {
	switch mode {
	case settings.SandboxOff:
		return []Check{{Name: "sandbox/adapter", Status: Warn,
			Detail: "off in settings (explicit opt-out) — path-jail + policy only"}}
	case settings.SandboxAuto:
		name := sandbox.Detect(runtime.GOOS, exec.LookPath)
		if name == "noop" {
			return []Check{{Name: "sandbox/adapter", Status: Fail,
				Detail: "OS sandbox helper missing (sandbox-exec/bwrap) — boot blocks until installed (ADR-0011)"}}
		}
		return []Check{{Name: "sandbox/adapter", Status: OK, Detail: name}}
	default:
		return []Check{{Name: "sandbox/adapter", Status: Fail,
			Detail: fmt.Sprintf("unknown mode %q — boot refuses on strict settings", mode)}}
	}
}

// Workflows reports the F-031 workflow layer: malformed definitions, and
// agents/teams/tasks whose declared active workflow is missing. The
// builtin `feature` workflow always exists, so an empty workspace is OK.
func Workflows(wsRoot string) []Check {
	if wsRoot == "" {
		return nil
	}
	avail, err := agentkitWorkflow.Available(wsRoot)
	if err != nil {
		return []Check{{Name: "workflows", Status: Fail,
			Detail: err.Error() + " (turns refuse until fixed)"}}
	}
	known := map[string]bool{}
	for _, s := range avail {
		known[s] = true
	}
	var warnings []string

	roster, rerr := manifest.LoadDir(filepath.Join(wsRoot, workspace.DirAgents))
	if rerr == nil {
		for _, a := range roster {
			if a.Workflow != "" && !known[a.Workflow] {
				warnings = append(warnings, "agent "+a.ID+": workflow "+a.Workflow+" not found")
			}
		}
	}
	if o, oerr := org.Load(wsRoot); oerr == nil {
		for _, t := range o.Teams() {
			if t.Workflow != "" && !known[t.Workflow] {
				warnings = append(warnings, "team "+t.Name+": workflow "+t.Workflow+" not found")
			}
		}
	}
	if ws, werr := workspace.Load(wsRoot); werr == nil {
		if store, terr := tasks.Open(ws); terr == nil {
			for _, t := range store.List() {
				if t.Workflow != "" && !known[t.Workflow] {
					warnings = append(warnings, "task "+t.Slug+": workflow "+t.Workflow+" not found")
				}
			}
		}
	}

	sort.Strings(warnings)
	if len(warnings) > 0 {
		return []Check{{Name: "workflows", Status: Warn,
			Detail: strings.Join(warnings, "; ") + " (they run on the builtin feature workflow)"}}
	}
	return []Check{{Name: "workflows", Status: OK,
		Detail: fmt.Sprintf("%d workflow(s) available: %s", len(avail), strings.Join(avail, ", "))}}
}

// Dependencies reports the declared cross-project graph (F-032): a
// dangling endpoint warns by name; no edges is healthy.
func Dependencies(wsRoot string) []Check {
	if wsRoot == "" {
		return nil
	}
	ws, err := workspace.Load(wsRoot)
	if err != nil {
		return nil // workspace/config already reports the broken config
	}
	deps := ws.Dependencies()
	if len(deps) == 0 {
		return []Check{{Name: "dependencies", Status: OK, Detail: "no declared cross-project edges"}}
	}
	if dang := ws.DanglingDependencies(); len(dang) > 0 {
		parts := make([]string, 0, len(dang))
		for _, d := range dang {
			parts = append(parts, d.From+"→"+d.To+" ("+d.Kind+")")
		}
		return []Check{{Name: "dependencies", Status: Warn,
			Detail: fmt.Sprintf("%d dangling edge(s): %s (member not registered)",
				len(dang), strings.Join(parts, ", "))}}
	}
	return []Check{{Name: "dependencies", Status: OK,
		Detail: fmt.Sprintf("%d declared edge(s)", len(deps))}}
}
