package runtime

import (
	"context"
	"fmt"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/drjzlyan/dhi/internal/agentkit/bus"
	"github.com/drjzlyan/dhi/internal/agentkit/dhitools"
	"github.com/drjzlyan/dhi/internal/agentkit/episode"
	"github.com/drjzlyan/dhi/internal/agentkit/scopes"
	"github.com/drjzlyan/dhi/internal/agentkit/verifyplan"
	"github.com/drjzlyan/dhi/internal/sandbox"
	"github.com/drjzlyan/dhi/internal/tasks"
)

// verifyTarget is one check the goal loop runs after a successful spawn
// (F-067, F-076): a detected (or configured) plan for a directory.
type verifyTarget struct {
	dir  string
	argv []string
	plan verifyplan.Target
}

func (v verifyTarget) String() string { return strings.Join(v.argv, " ") }

// verifyTargets finds what to check in a task worktree (F-076): the
// member's configured `verify`, else what verifyplan detects — Go,
// JS/TS, Python, Rust, Maven, Gradle or .NET. Only a task worktree is
// ever verified: the shared checkout is not the task's to judge.
func (r *Runtime) verifyTargets(task tasks.Task, workdir string) []verifyTarget {
	if r.isTracking(task) || r.taskWorktree(task) != filepath.Clean(workdir) {
		return nil
	}
	var o verifyplan.Override
	if r.cfg.WS != nil {
		if m, ok := r.cfg.WS.Member(task.ChangeSets[0].Member); ok {
			o = verifyplan.Override{Argv: m.Verify, Set: m.VerifySet}
		}
	}
	var out []verifyTarget
	for _, t := range verifyplan.Plan(workdir, o) {
		out = append(out, verifyTarget{dir: t.Dir, argv: t.Test, plan: t})
	}
	return out
}

// taskWorktree is the card's first worktree, absolute ("" = none).
func (r *Runtime) taskWorktree(task tasks.Task) string {
	if len(task.ChangeSets) == 0 {
		return ""
	}
	return filepath.Clean(filepath.Join(r.cfg.WS.Root, task.ChangeSets[0].Path))
}

// memberRoots maps each of the card's members to its worktree, for the
// served file tools (F-067).
func (r *Runtime) memberRoots(task tasks.Task) map[string]string {
	if len(task.ChangeSets) == 0 {
		return nil
	}
	out := map[string]string{}
	for _, cs := range task.ChangeSets {
		out[cs.Member] = filepath.Join(r.cfg.WS.Root, cs.Path)
	}
	return out
}

// worktreeState fingerprints dir (HEAD + porcelain status) through DHI's
// hermetic git, so the loop verifies only work that changed something —
// a status report or a summary turn on a card never trips `go test`.
// ok is false when it cannot tell (no git seam): the loop then verifies.
func (r *Runtime) worktreeState(ctx context.Context, dir string) (string, bool) {
	if r.cfg.Git == nil {
		return "", false
	}
	head, _, err := r.cfg.Git.Run(ctx, dir, "rev-parse", "HEAD")
	if err != nil {
		return "", false
	}
	st, _, err := r.cfg.Git.Run(ctx, dir, "status", "--porcelain")
	if err != nil {
		return "", false
	}
	return strings.TrimSpace(head) + "\n" + st, true
}

// envFailure reports a verify that could not run here — no network for
// a dependency, a missing toolchain, a sandbox denial — as opposed to
// failing tests. Those are named to the human, never sent back as bugs.
func envFailure(tail string) bool { return verifyplan.EnvFailure(tail) }

// autopilotCard reports a per-run autopilot card ("auto-<slug>-<id>",
// workspace/autopilots.go): a recurring chore, not feature work, so it is
// never given a worktree.
func autopilotCard(slug string) bool { return strings.HasPrefix(slug, "auto-") }

// verifyResult is one verify pass: the failing command and its output
// tail, or ok. env marks a check that could not run here.
type verifyResult struct {
	cmd, tail string
	ok, env   bool
}

// verify runs each target through the sandboxed command runner (the one
// the `run` tool uses): first its dependency install when the lockfiles
// changed since the last one (network, gated by the agent's network
// scope — an ask crosses the approvals inbox and "always" persists),
// then the test command offline.
func (r *Runtime) verify(ctx context.Context, agentID string, sc scopes.Set, targets []verifyTarget) verifyResult {
	for _, t := range targets {
		if res, ok := r.ensureTools(ctx, agentID, t); !ok {
			return res
		}
		if res, ok := r.prepare(ctx, agentID, sc, t); !ok {
			return res
		}
		out, err := r.cfg.Run.Run(ctx, t.dir, t.argv, false)
		if err != nil {
			tail := tailLines(out+"\n"+err.Error(), 40)
			return verifyResult{cmd: t.String(), tail: tail, env: envFailure(tail)}
		}
	}
	return verifyResult{ok: true}
}

// ensureTools installs the optional managed toolchains a target needs
// (F-076) — Rust, a JDK, Maven, Gradle, .NET — after the human approves
// the download in the approvals inbox ("allow always" covers later ones).
func (r *Runtime) ensureTools(ctx context.Context, agentID string, t verifyTarget) (verifyResult, bool) {
	tc := r.cfg.Toolchain
	if tc == nil {
		return verifyResult{}, true
	}
	for _, name := range t.plan.Tools {
		size, optional := tc.Optional(name)
		if !optional || tc.Installed(name) {
			continue
		}
		what := fmt.Sprintf("install the managed %s toolchain (%d MB download)", name, (size+(1<<20)-1)>>20)
		if r.cfg.Approvals == nil {
			return verifyResult{cmd: t.String(), tail: "the " + name + " toolchain is not installed", env: true}, false
		}
		if err := r.cfg.Approvals.AskScope(ctx, agentID, "toolchain", sandbox.OpExec, what,
			"DHI needs it to run this project's tests"); err != nil {
			return verifyResult{cmd: t.String(), tail: "the " + name + " toolchain is not installed: " + err.Error(), env: true}, false
		}
		if err := tc.Install(ctx, name); err != nil {
			return verifyResult{cmd: t.String(), tail: "installing the " + name + " toolchain failed: " + err.Error(), env: true}, false
		}
	}
	return verifyResult{}, true
}

// verifyDepsScope is the approvals grant scope of verify's dependency
// installs (F-076).
const verifyDepsScope = "verify-deps"

// prepare installs a target's dependencies once per lockfile state.
func (r *Runtime) prepare(ctx context.Context, agentID string, sc scopes.Set, t verifyTarget) (verifyResult, bool) {
	if len(t.plan.Prepare) == 0 {
		return verifyResult{}, true
	}
	fp := t.plan.Fingerprint()
	r.prepMu.Lock()
	done := r.prepared[t.dir] == fp
	r.prepMu.Unlock()
	if done {
		return verifyResult{}, true
	}
	first := strings.Join(t.plan.Prepare[0], " ")
	switch sc.EffectFor(scopes.Network) {
	case scopes.Deny:
		return verifyResult{cmd: first, tail: "dependencies were not installed: network is denied (scopes.network)", env: true}, false
	case scopes.Ask:
		if r.cfg.Approvals == nil {
			return verifyResult{cmd: first, tail: "dependencies were not installed: network needs approval and no approvals queue is running", env: true}, false
		}
		// Its own grant scope: "allow always" here covers dependency
		// installs for verification only, never the agent's own network
		// tools (which ask under scopes.Network).
		if err := r.cfg.Approvals.AskScope(ctx, agentID, verifyDepsScope, sandbox.OpNet,
			first+" in "+t.dir, "install dependencies so DHI can run the tests"); err != nil {
			return verifyResult{cmd: first, tail: "dependencies were not installed: " + err.Error(), env: true}, false
		}
	}
	for _, argv := range t.plan.Prepare {
		out, err := r.cfg.Run.Run(ctx, t.dir, argv, true)
		if err != nil {
			return verifyResult{cmd: strings.Join(argv, " "), tail: tailLines(out+"\n"+err.Error(), 40), env: true}, false
		}
	}
	r.prepMu.Lock()
	if r.prepared == nil {
		r.prepared = map[string]string{}
	}
	r.prepared[t.dir] = fp
	r.prepMu.Unlock()
	return verifyResult{}, true
}

func tailLines(s string, n int) string {
	lines := strings.Split(strings.TrimRight(s, "\n"), "\n")
	return strings.Join(lastN(lines, n), "\n")
}

var volatileRe = regexp.MustCompile(`[0-9]+(\.[0-9]+)?(ms|s|µs)?|0x[0-9a-f]+`)

// failureSig normalises a verify failure so the same failure twice reads
// the same (timings, addresses and counts masked): the stuck detector.
func failureSig(tail string) string {
	return volatileRe.ReplaceAllString(strings.TrimSpace(tail), "#")
}

// verifyBrief is what the next spawn is told after a failed check.
func verifyBrief(cmd, tail string, iter, max int) string {
	return fmt.Sprintf("\n\nDHI verified your work by running `%s`, and it failed (round %d of %d):\n```\n%s\n```\n"+
		"Fix the cause (not the test), then finish. Do not redo work that already passes.", cmd, iter, max, tail)
}

// turnTools is the per-turn context the served IDE tools need beyond the
// agent itself (F-067).
type turnTools struct {
	envelope, task string                                                                            // autonomy envelope (worktree) and bound card
	plan           func([]dhitools.PlanStep) error                                                   // plan_update seam; nil = none
	memberRoots    map[string]string                                                                 // member → task worktree for the file tools
	lesson         func(text, scope string, tags []string) (string, error)                           // lesson_record seam; nil = none
	workBranch     func(ctx context.Context, slug, title, member string, carry bool) (string, error) // work_branch seam (F-086); nil = none
	access         func(ctx context.Context, kind, target, reason string) (string, error)            // permission_request seam (F-087); nil = none
}

// envelopeFor decides the autonomy envelope (F-067, ADR-0032): only a
// task turn working in its card's own worktree, with autonomy on.
func (r *Runtime) envelopeFor(task tasks.Task, hasTask bool, workdir string, tune Tuning) string {
	if !hasTask || tune.Autonomy != "envelope" || len(task.ChangeSets) == 0 {
		return ""
	}
	wt := filepath.Join(r.cfg.WS.Root, task.ChangeSets[0].Path)
	if filepath.Clean(wt) != filepath.Clean(workdir) {
		return ""
	}
	return wt
}

// planSeam stores an agent's published plan on its episode.
func (r *Runtime) planSeam(agentID, key string) func([]dhitools.PlanStep) error {
	if r.cfg.Episodes == nil {
		return nil
	}
	return func(steps []dhitools.PlanStep) error {
		return r.cfg.Episodes.Update(agentID, key, func(e *episode.Episode) { e.Plan = steps })
	}
}

// humanNote posts a reply addressed to the human (an @you mention lands
// in the Inbox) — the loop's way to ask for help it cannot get itself.
func (r *Runtime) humanNote(trigger bus.Message, author, text string) {
	_, _ = r.cfg.Bus.Post(bus.Message{
		Channel: trigger.Channel, Thread: trigger.Thread,
		Author: author, Text: "@" + bus.Human + " " + text, Kind: bus.KindReply,
	})
}

// provisionWorktree gives a task turn its own worktree before the first
// spawn (F-067): unattended work must happen where DHI can see, verify,
// and later open a PR from it — an agent left to make its own lands it
// somewhere DHI never learns about. Only an unambiguous case is
// provisioned (one member); otherwise, and on failure, a named notice.
func (r *Runtime) provisionWorktree(agentID string, trigger bus.Message, task tasks.Task) tasks.Task {
	if len(task.ChangeSets) > 0 || r.cfg.Provision == nil || autopilotCard(task.Slug) || r.isTracking(task) {
		return task
	}
	members := r.cfg.WS.Members()
	if len(members) != 1 {
		r.notice(trigger, agentID, fmt.Sprintf(
			"task %s has no worktree and the workspace has %d members — attach one on the board so the work has a branch",
			task.Slug, len(members)))
		return task
	}
	if err := r.cfg.Provision(task.Slug, members[0].Name); err != nil {
		r.notice(trigger, agentID, "could not create the task worktree ("+err.Error()+"); working in the shared checkout")
		return task
	}
	t, ok := r.cfg.Tasks.Get(task.Slug)
	if !ok || len(t.ChangeSets) == 0 {
		return task
	}
	cs := t.ChangeSets[0]
	r.notice(trigger, agentID, fmt.Sprintf("created worktree %s on branch %s for task %s", cs.Path, cs.Branch, t.Slug))
	return t
}

// noWorktreeNote replaces the workflow's "create a worktree" step when DHI
// could not provision one: a worktree the agent makes on its own is one
// DHI never learns about, so the work would be invisible (the P2 eval
// failure).
const noWorktreeNote = "\n- DHI could not give this task its own worktree. Do NOT create one yourself (DHI would never see the work): " +
	"call work_branch (with member when there are several), or ask the human (@you) to attach one on the board."
