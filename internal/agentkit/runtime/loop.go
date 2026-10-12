package runtime

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/drjzlyan/dhi/internal/agentkit/bus"
	"github.com/drjzlyan/dhi/internal/agentkit/dhitools"
	"github.com/drjzlyan/dhi/internal/agentkit/episode"
	"github.com/drjzlyan/dhi/internal/tasks"
)

// verifyTarget is one check the goal loop runs after a successful spawn
// (F-067): a command in a directory.
type verifyTarget struct {
	dir  string
	argv []string
}

func (v verifyTarget) String() string { return strings.Join(v.argv, " ") }

// verifyTargets finds what to check in a task worktree: a Go module runs
// `go test ./...`. Other ecosystems have no allowlisted test runner yet,
// so they get no verify — and the loop says so rather than claiming
// success. Only a task worktree is ever verified: the shared checkout is
// not the task's to judge.
func (r *Runtime) verifyTargets(task tasks.Task, workdir string) []verifyTarget {
	if r.taskWorktree(task) != filepath.Clean(workdir) || !exists(filepath.Join(workdir, "go.mod")) {
		return nil
	}
	return []verifyTarget{{dir: workdir, argv: []string{"go", "test", "./..."}}}
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

// envFailures mark a verify that could not run here — no network for a
// module download, a sandbox denial — as opposed to failing tests. Those
// are named to the human, never sent back as bugs to "fix".
var envFailures = []string{
	"dial tcp", "proxy.golang.org", "cannot find module providing", "no required module provides",
	"operation not permitted", "go: downloading", "connection refused", "no such host",
}

func envFailure(tail string) bool {
	low := strings.ToLower(tail)
	for _, s := range envFailures {
		if strings.Contains(low, s) {
			return true
		}
	}
	return false
}

// autopilotCard reports a per-run autopilot card ("auto-<slug>-<id>",
// workspace/autopilots.go): a recurring chore, not feature work, so it is
// never given a worktree.
func autopilotCard(slug string) bool { return strings.HasPrefix(slug, "auto-") }

func exists(p string) bool { _, err := os.Stat(p); return err == nil }

// verify runs the targets through the sandboxed command runner (the one
// the `run` tool uses). It returns the first failure's command and the
// tail of its output; ok when every target passed.
func (r *Runtime) verify(ctx context.Context, targets []verifyTarget) (cmd, tail string, ok bool) {
	for _, t := range targets {
		out, err := r.cfg.Run.Run(ctx, t.dir, t.argv, false)
		if err != nil {
			return t.String(), tailLines(out+"\n"+err.Error(), 40), false
		}
	}
	return "", "", true
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
	envelope, task string                          // autonomy envelope (worktree) and bound card
	plan           func([]dhitools.PlanStep) error // plan_update seam; nil = none
	memberRoots    map[string]string               // member → task worktree for the file tools
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
	if len(task.ChangeSets) > 0 || r.cfg.Provision == nil || autopilotCard(task.Slug) {
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
	"ask the human (@you) to attach one on the board, or make the change in place."
