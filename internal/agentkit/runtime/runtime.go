// Package runtime is the agent turn engine: it owns the roster, routes
// bus mentions to the right agent, assembles prompts from channel
// history, and drives each agent through its registered host CLI inside
// the OS sandbox, posting the transcript and final reply back to the
// bus. UIs subscribe to the bus; they never talk to providers directly.
// DHI ships no model engine of its own (ADR-0013): every rostered agent
// thinks through a host CLI.
package runtime

import (
	"context"
	"fmt"
	"path/filepath"
	"strings"
	"sync"

	"github.com/drjzlyan/dhi/internal/agentkit/bus"
	"github.com/drjzlyan/dhi/internal/agentkit/clirun"
	"github.com/drjzlyan/dhi/internal/agentkit/dhitools"
	"github.com/drjzlyan/dhi/internal/agentkit/knowledge"
	"github.com/drjzlyan/dhi/internal/agentkit/library"
	"github.com/drjzlyan/dhi/internal/agentkit/manifest"
	"github.com/drjzlyan/dhi/internal/agentkit/memory"
	"github.com/drjzlyan/dhi/internal/agentkit/org"
	"github.com/drjzlyan/dhi/internal/agentkit/scopes"
	"github.com/drjzlyan/dhi/internal/agentkit/standards"
	"github.com/drjzlyan/dhi/internal/agentkit/toolbridge"
	"github.com/drjzlyan/dhi/internal/agentkit/tools"
	"github.com/drjzlyan/dhi/internal/agentkit/workflow"
	"github.com/drjzlyan/dhi/internal/gitcore"
	"github.com/drjzlyan/dhi/internal/ideation"
	"github.com/drjzlyan/dhi/internal/sandbox"
	"github.com/drjzlyan/dhi/internal/search"
	"github.com/drjzlyan/dhi/internal/tasks"
	"github.com/drjzlyan/dhi/internal/workspace"
)

// historyWindow caps how much channel context feeds a prompt.
const historyWindow = 50

// Config wires the runtime's seams. Every rostered agent runs through a
// registered host CLI (ADR-0012/0013); the CLI registry is required.
type Config struct {
	WS        *workspace.Workspace
	Bus       *bus.Bus
	Approvals *tools.Approvals
	// Sandbox is the OS-isolation adapter applied to every Guard built
	// for agents (F-010). Nil means Noop: path-jail + policy only.
	Sandbox sandbox.Sandbox
	// CLIs is the host CLI runtime registry (F-013, ADR-0012). Every
	// rostered agent's engine resolves against it; nil refuses all
	// agents at build time (strict, no silent fallback).
	CLIs *clirun.Registry
	// DefaultEngine is the workspace default engine (ADR-0019),
	// "cli:<name>". Agents whose manifest declares no engine inherit it;
	// when it is also empty such agents refuse at build time by name.
	DefaultEngine string
	// CLIEnv is the base environment for CLI spawns (hermetic toolchain
	// PATH and the like). The executor extends it with each CLI's
	// declared pass-through — the exact set, nothing else (ADR-0012 §4).
	CLIEnv []string
	// Tasks persists run records onto task cards (F-013). Nil disables
	// card recording; the transcript still lives on the bus.
	Tasks *tasks.Store
	// Org supplies team membership for layered coding standards; nil
	// disables team layers.
	Org *org.Org
	// Standards injects layered coding instructions into every turn's
	// system prompt (built-ins apply even without a document).
	Standards bool
	// Workflows resolves and injects the agent's active feature workflow
	// (F-031) and enforces its gates at DHI's seams. A malformed
	// workflow definition refuses the turn by name (ADR-0011).
	Workflows bool
	// PR opens a PR for a task's branch (F-020 pr_open; the review
	// service in main). nil = pr_open refuses by name.
	PR toolbridge.PRSeam
	// Memory gives agents persistent context across turns (M14 P1):
	// the journal tail and notes ride the system block. Nil = no
	// memory injection (the block names it, never silent).
	Memory *memory.Store
	// Knowledge retrieves workspace KB hits relevant to the trigger
	// (M14 P1). Nil = no KB injection (named, never silent).
	Knowledge knowledge.KnowledgeStore
	// Search backs the workspace_search IDE tool (F-028); nil omits
	// the tool from the served surface.
	Search search.Searcher
	// Git is the hermetic git CLI runner backing git_diff (M15 P1); nil
	// makes the diff tool refuse by name (never a host fallback).
	Git *gitcore.Runner
	// Identity resolves the user's git identity for git_commit (F-029);
	// nil makes the commit tool refuse by name.
	Identity gitcore.IdentityFunc
	// Sessions backs the ideation read tools (M15 P1); nil omits them.
	Sessions *ideation.Store
	// Run executes the `run` tool's allowlisted commands (M15 P1); nil
	// makes the tool refuse by name.
	Run dhitools.CommandRunner
	// Editor is the app-owned editor seam (ADR-0023); nil makes the
	// editor/LSP tools refuse by name.
	Editor dhitools.EditorAPI
	// WorkspaceScopes is the workspace capability layer (F-030 P2):
	// resolved under team and agent scopes.
	WorkspaceScopes scopes.Set
}

// Runtime manages rostered agents and executes their turns.
type Runtime struct {
	cfg    Config
	mu     sync.Mutex
	agents map[string]*entry
	roster chan struct{} // pinged after every Reload

	libOnce sync.Once
	libRef  *library.Store
}

// lib lazily opens the behaviour library (nil-safe; a nil workspace
// keeps it nil).
func (r *Runtime) lib() *library.Store {
	if r.cfg.WS == nil {
		return nil
	}
	r.libOnce.Do(func() { r.libRef = library.Open(r.cfg.WS) })
	return r.libRef
}

type entry struct {
	m       *manifest.Agent
	guard   *sandbox.Guard // isolation seam (doctor/diagnostics surface)
	cli     *clirun.CLI    // host CLI adapter
	cliPath string         // resolved binary path for the CLI runtime
	turnMu  sync.Mutex     // one turn at a time per agent
}

// New builds per-agent entries from the roster. Agents whose manifest
// fails are skipped with an error return only if none load.
func New(cfg Config, roster []*manifest.Agent) (*Runtime, error) {
	if len(roster) == 0 {
		return nil, fmt.Errorf("runtime: empty roster")
	}
	// Strict (ADR-0011): production must name an OS sandbox adapter.
	// sandbox.Noop{} is the explicit opt-out; nil is not a fallback.
	if cfg.Sandbox == nil {
		return nil, fmt.Errorf("runtime: config.Sandbox is required (pass sandbox.Noop{} to opt out explicitly)")
	}
	r := &Runtime{cfg: cfg, agents: map[string]*entry{}, roster: make(chan struct{}, 1)}
	// Admit each rostered CLI's state + binary roots into the OS sandbox
	// BEFORE guards capture the adapter: claude lives under ~/.local and
	// writes ~/.claude — the workspace jail alone denies its exec (exit
	// 71 with no diagnosis). Noop and non-extending adapters skip this.
	if err := r.extendSandboxForRoster(roster); err != nil {
		return nil, err
	}
	jailRoots := make([]string, 0, len(cfg.WS.Members())+2)
	for _, m := range cfg.WS.Members() {
		jailRoots = append(jailRoots, m.Path)
	}
	// The reserved .dhi tree is jailed separately so agents can write
	// ideation artifacts under .dhi/sessions/ (F-004) while policies
	// stay deny-by-default (ADR-0006/0010).
	jailRoots = append(jailRoots, filepath.Join(cfg.WS.Root, workspace.DHIDir))
	for _, m := range roster {
		e, err := r.buildEntry(m, jailRoots)
		if err != nil {
			return nil, err
		}
		r.agents[m.ID] = e
	}
	return r, nil
}

// extendSandboxForRoster merges every rostered CLI's sandbox roots into
// the OS adapter: the CLI's declared StateRoots (claude → ~/.claude)
// plus the binary's own directory tree (the resolved symlink target and
// its parent — claude is ~/.local/bin/claude →
// ~/.local/share/claude/versions/<v>). Adapters that don't extend
// (Noop) are skipped; extension failures refuse the runtime by name.
func (r *Runtime) extendSandboxForRoster(roster []*manifest.Agent) error {
	if r.cfg.Sandbox == nil || r.cfg.CLIs == nil {
		return nil
	}
	re, ok := r.cfg.Sandbox.(sandbox.RootExtender)
	if !ok {
		return nil // noop and test adapters have no roots to grow
	}
	seen := map[string]bool{}
	var extras []string
	add := func(p string) {
		if p == "" || seen[p] {
			return
		}
		seen[p] = true
		extras = append(extras, p)
	}
	for _, m := range roster {
		name, err := r.engineName(m)
		if err != nil {
			continue // buildEntry names the missing engine precisely
		}
		c, ok := r.cfg.CLIs.Get(name)
		if !ok {
			continue // buildEntry names the unknown runtime precisely
		}
		for _, root := range c.StateRoot() {
			add(root)
		}
		path, err := r.cfg.CLIs.Path(name)
		if err != nil {
			continue // same: the missing binary is the entry's named error
		}
		add(filepath.Dir(path))
		if resolved, err := filepath.EvalSymlinks(path); err == nil {
			add(filepath.Dir(resolved))
			add(filepath.Dir(filepath.Dir(resolved)))
		}
	}
	if len(extras) == 0 {
		return nil
	}
	ext, err := re.WithExtraRoots(extras)
	if err != nil {
		return fmt.Errorf("runtime: sandbox roots for rostered CLIs: %w", err)
	}
	r.cfg.Sandbox = ext
	return nil
}

// engineName resolves an agent's effective CLI engine: the manifest's
// engine/runtime when declared, else the workspace default engine, else
// a named refusal (ADR-0019, ADR-0011). Empty is never a silent
// fallback.
func (r *Runtime) engineName(m *manifest.Agent) (string, error) {
	if m.Runtime != "" {
		return m.Runtime, nil
	}
	def := strings.TrimSpace(r.cfg.DefaultEngine)
	if def == "" {
		return "", fmt.Errorf("no engine: set `engine = \"cli:<name>\"` in the manifest or a default `engine` in settings")
	}
	name, err := manifest.ParseEngine(def)
	if err != nil {
		return "", fmt.Errorf("default engine: %w", err)
	}
	if name == "" {
		return "", fmt.Errorf("no engine: settings default engine is empty")
	}
	return name, nil
}

// agentScopes resolves an agent's effective capability set (F-030 P2):
// default → team(s) → manifest, later wins. Team scopes are validated by
// org; an invalid entry is skipped (never a silent grant).
func (r *Runtime) agentScopes(m *manifest.Agent) scopes.Set {
	layers := []scopes.Set{scopes.Default()}
	if len(r.cfg.WorkspaceScopes) > 0 {
		layers = append(layers, r.cfg.WorkspaceScopes)
	}
	if r.cfg.Org != nil {
		for _, slug := range r.cfg.Org.TeamsOf(m.ID) {
			t, ok := r.cfg.Org.Team(slug)
			if !ok || len(t.Scopes) == 0 {
				continue
			}
			layer := scopes.Set{}
			for name, eff := range t.Scopes {
				sc, err := scopes.ParseScope(name)
				if err != nil {
					continue
				}
				e, err := scopes.ParseEffect(eff)
				if err != nil {
					continue
				}
				layer[sc] = e
			}
			layers = append(layers, layer)
		}
	}
	if len(m.Scopes) > 0 {
		layers = append(layers, m.Scopes)
	}
	return scopes.Resolve(layers...)
}

// AgentEngine reports the CLI engine an agent resolves to (effective),
// for diagnostics and profile display.
func (r *Runtime) AgentEngine(id string) (string, bool) {
	r.mu.Lock()
	e, ok := r.agents[id]
	r.mu.Unlock()
	if !ok {
		return "", false
	}
	return e.cli.Name, true
}

// buildEntry wires one agent's host CLI and OS-sandbox guard. The
// manifest's `engine` (or the workspace default) is the exec
// authorization; the guard wraps every CLI spawn through the same
// OS-sandbox seam every other exec uses (ADR-0012 §3).
func (r *Runtime) buildEntry(m *manifest.Agent, jailRoots []string) (*entry, error) {
	if r.cfg.CLIs == nil {
		return nil, fmt.Errorf("runtime: %s: engine %q needs a CLI registry (config.CLIs is nil)", m.ID, m.Engine)
	}
	name, err := r.engineName(m)
	if err != nil {
		return nil, fmt.Errorf("runtime: %s: %w", m.ID, err)
	}
	c, ok := r.cfg.CLIs.Get(name)
	if !ok {
		return nil, fmt.Errorf("runtime: %s: unknown CLI runtime %q", m.ID, name)
	}
	path, err := r.cfg.CLIs.Path(name)
	if err != nil {
		return nil, fmt.Errorf("runtime: %s: %w (install %s to use this engine)", m.ID, err, c.Bin)
	}
	var policy *sandbox.Policy
	if m.Policy() != nil {
		policy = m.Policy()
	} else {
		policy = &sandbox.Policy{} // deny-all default
	}
	jail, err := sandbox.NewJail(jailRoots...)
	if err != nil {
		return nil, fmt.Errorf("runtime: jail: %w", err)
	}
	guard := sandbox.NewGuard(jail, policy)
	guard.Sandbox = r.cfg.Sandbox
	return &entry{m: m, guard: guard, cli: c, cliPath: path}, nil
}

// Changes pings once after every successful Reload so UIs can refresh
// rosters (best-effort delivery).
func (r *Runtime) Changes() <-chan struct{} { return r.roster }

// Reload swaps the active roster atomically: entries are rebuilt first,
// then the map is replaced under lock. Turns already running against old
// entries finish untouched (they hold their own turnMu); new turns bind
// to the new entries. An empty roster clears the crew.
func (r *Runtime) Reload(roster []*manifest.Agent) error {
	// A live-reloaded roster may introduce runtimes the boot profile
	// never admitted; extend again (the adapter merges, never shrinks).
	if err := r.extendSandboxForRoster(roster); err != nil {
		return fmt.Errorf("runtime: reload aborted; previous roster kept: %w", err)
	}
	jailRoots := make([]string, 0, len(r.cfg.WS.Members())+2)
	for _, m := range r.cfg.WS.Members() {
		jailRoots = append(jailRoots, m.Path)
	}
	jailRoots = append(jailRoots, filepath.Join(r.cfg.WS.Root, workspace.DHIDir))
	next := make(map[string]*entry, len(roster))
	for _, m := range roster {
		e, err := r.buildEntry(m, jailRoots)
		if err != nil {
			return fmt.Errorf("runtime: reload aborted; previous roster kept: %w", err)
		}
		next[m.ID] = e
	}
	r.mu.Lock()
	r.agents = next
	r.mu.Unlock()
	select {
	case r.roster <- struct{}{}:
	default:
	}
	return nil
}

// AgentIDs lists rostered agents sorted.
func (r *Runtime) AgentIDs() []string {
	r.mu.Lock()
	defer r.mu.Unlock()
	out := make([]string, 0, len(r.agents))
	for id := range r.agents {
		out = append(out, id)
	}
	sortStrings(out)
	return out
}

// Bus exposes the message bus for UI subscribers.
func (r *Runtime) Bus() *bus.Bus { return r.cfg.Bus }

// Approvals exposes the shared approval queue for UIs. The in-house
// tool gate that filled it is gone (ADR-0013 §3); CLI permission prompts
// will repopulate it as a forward adapter feature.
func (r *Runtime) Approvals() *tools.Approvals { return r.cfg.Approvals }

// Handle processes one inbound message: any mentioned rostered agent
// (or the sole DM addressee) runs a turn in its own goroutine. It returns
// immediately after dispatching.
func (r *Runtime) Handle(ctx context.Context, msg bus.Message) {
	for _, id := range r.targets(msg) {
		go func(id string) {
			_ = r.Turn(ctx, id, msg)
		}(id)
	}
}

// targets resolves which agents a message addresses.
func (r *Runtime) targets(msg bus.Message) []string {
	if strings.HasPrefix(msg.Channel, "dm:") {
		id := strings.TrimPrefix(msg.Channel, "dm:")
		if _, ok := r.agents[id]; ok && msg.Author != id {
			return []string{id}
		}
		return nil
	}
	var out []string
	for _, id := range bus.Mentions(msg.Text) {
		if _, ok := r.agents[id]; ok && id != msg.Author {
			out = append(out, id)
		}
	}
	return out
}

// Turn executes one full conversational turn for agentID in response to
// trigger: history → prompted host CLI spawn → streamed transcript →
// final reply posted to the trigger's channel/thread (F-013/ADR-0012).
func (r *Runtime) Turn(ctx context.Context, agentID string, trigger bus.Message) error {
	r.mu.Lock()
	e, ok := r.agents[agentID]
	r.mu.Unlock()
	if !ok {
		return fmt.Errorf("runtime: unknown agent %q", agentID)
	}
	e.turnMu.Lock()
	defer e.turnMu.Unlock()

	// Strict (F-011): malformed standards refuse the turn with the
	// named path instead of silently degrading to built-ins.
	if r.cfg.Standards {
		if err := standards.Check(r.cfg.WS.Root); err != nil {
			return fmt.Errorf("runtime: %s: %w", agentID, err)
		}
	}
	// Strict (ADR-0020): a malformed workflow definition refuses the turn
	// by name rather than silently falling back to the builtin.
	if r.cfg.Workflows {
		if _, _, err := r.activeWorkflow(e.m); err != nil {
			return fmt.Errorf("runtime: %s: %w", agentID, err)
		}
	}

	return r.cliTurn(ctx, e, trigger)
}

// activeWorkflow resolves the agent's active feature workflow (F-031),
// most specific first: agent manifest → each team → workspace default →
// builtin `feature`. It returns the slug and the loaded definition. A
// malformed local definition or a named-but-missing slug refuses.
func (r *Runtime) activeWorkflow(m *manifest.Agent) (string, *workflow.Definition, error) {
	var prec []string
	if m != nil && strings.TrimSpace(m.Workflow) != "" {
		prec = append(prec, m.Workflow)
	}
	if r.cfg.Org != nil && m != nil {
		for _, slug := range r.cfg.Org.TeamsOf(m.ID) {
			if t, ok := r.cfg.Org.Team(slug); ok && strings.TrimSpace(t.Workflow) != "" {
				prec = append(prec, t.Workflow)
			}
		}
	}
	def, err := workflow.WorkspaceDefault(r.cfg.WS.Root)
	if err != nil {
		return "", nil, err
	}
	if def != "" {
		prec = append(prec, def)
	}
	slug, err := workflow.Resolve(r.cfg.WS.Root, prec)
	if err != nil {
		return "", nil, err
	}
	wd, err := workflow.Load(r.cfg.WS.Root, slug)
	if err != nil {
		return "", nil, err
	}
	return slug, wd, nil
}

// workflowEnforcer builds the F-031 gate + run-observer for a turn's
// served tools. The gate evaluates the agent's active workflow against
// the task's real progress; the observer learns that the declared test
// command passed (tests-before-PR). Both nil when workflows are off or
// the workflow cannot be resolved (a malformed one is refused at Turn).
func (r *Runtime) workflowEnforcer(m *manifest.Agent, trigger bus.Message) (gate func(string) []string, onRun func([]string, error)) {
	if !r.cfg.Workflows {
		return nil, nil
	}
	_, def, err := r.activeWorkflow(m)
	if err != nil || def == nil {
		return nil, nil
	}
	task, hasTask := r.taskFor(trigger)
	taskSlug := ""
	if hasTask {
		taskSlug = task.Slug
	}
	// progress reads the task fresh each call so gates see durable state
	// (a worktree attached or tests passing during the turn).
	progress := func() workflow.Progress {
		p := workflow.Progress{}
		if t, ok := r.taskFor(trigger); ok {
			p.Worktree = len(t.ChangeSets) > 0
			p.TestsPass = t.TestsPass
		}
		return p
	}
	gate = func(seam string) []string {
		return verdictReasons(def.CheckGate(seam, progress()))
	}
	onRun = func(argv []string, runErr error) {
		if runErr == nil && len(argv) >= 2 && argv[0] == "go" && argv[1] == "test" && taskSlug != "" {
			_ = r.cfg.Tasks.SetTestsPass(taskSlug, true)
		}
	}
	return gate, onRun
}

// taskWorkflowGate builds a gate for a task from its durable workflow
// state (worktree + tests-pass) and the agent's active workflow. Used at
// seams that run outside a served-tool turn (the PR action).
func (r *Runtime) taskWorkflowGate(m *manifest.Agent, task tasks.Task) func(string) []string {
	if !r.cfg.Workflows {
		return nil
	}
	_, def, err := r.activeWorkflow(m)
	if err != nil || def == nil {
		return nil
	}
	p := workflow.Progress{Worktree: len(task.ChangeSets) > 0, TestsPass: task.TestsPass}
	return func(seam string) []string { return verdictReasons(def.CheckGate(seam, p)) }
}

func verdictReasons(vs []workflow.Verdict) []string {
	if len(vs) == 0 {
		return nil
	}
	out := make([]string, 0, len(vs))
	for _, v := range vs {
		out = append(out, v.Reason)
	}
	return out
}

// taskFor returns the task bound to the trigger's thread.
func (r *Runtime) taskFor(trigger bus.Message) (tasks.Task, bool) {
	if r.cfg.Tasks == nil {
		return tasks.Task{}, false
	}
	return r.cfg.Tasks.FindByThread(trigger.Channel, trigger.Thread)
}

// taskHasWorktree reports whether the task bound to the trigger has an
// attached worktree (its commit/PR actions must happen there).
func (r *Runtime) taskHasWorktree(trigger bus.Message) bool {
	if r.cfg.Tasks == nil {
		return false
	}
	t, ok := r.cfg.Tasks.FindByThread(trigger.Channel, trigger.Thread)
	return ok && len(t.ChangeSets) > 0
}

// teamLookup adapts the org registry for standards resolution; nil org
// yields a lookup that matches nothing.
func (r *Runtime) teamLookup() standards.TeamLookup {
	if r.cfg.Org == nil {
		return nil
	}
	return func(agentID string) []string { return r.cfg.Org.TeamsOf(agentID) }
}

// stripMention removes this agent's own @token from the trigger text.
func stripMention(text, id string) string {
	return strings.TrimSpace(strings.ReplaceAll(text, "@"+id, ""))
}

func sortStrings(s []string) {
	for i := 1; i < len(s); i++ {
		for j := i; j > 0 && s[j] < s[j-1]; j-- {
			s[j], s[j-1] = s[j-1], s[j]
		}
	}
}

// Manifest returns the parsed manifest for id (false when not rostered).
// Satisfies the profile.Roster seam so inspection UIs never need the
// concrete runtime.
func (r *Runtime) Manifest(id string) (*manifest.Agent, bool) {
	r.mu.Lock()
	defer r.mu.Unlock()
	e, ok := r.agents[id]
	if !ok {
		return nil, false
	}
	return e.m, true
}

// Guard exposes the isolation seam for agent id (false when not
// rostered): doctor and diagnostics read the active OS-sandbox adapter
// from it; tool code never bypasses it.
func (r *Runtime) Guard(id string) (*sandbox.Guard, bool) {
	r.mu.Lock()
	defer r.mu.Unlock()
	e, ok := r.agents[id]
	if !ok {
		return nil, false
	}
	return e.guard, true
}
