// Package toolbridge executes DHI-namespaced actions on behalf of an
// agent (F-020 full IDE parity): task cards and PRs, the work-facing
// moves a human makes by hand. Agents request them by emitting a fenced
// ```dhi-action block in their final message — the same pattern as the
// editor's suggestion blocks. Every action is gated by the manifest's
// tools allowlist; every mutating action crosses the approvals seam the
// human already answers with y/n; results are text for the bus thread,
// so the agent sees the outcome on its next turn.
package toolbridge

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"regexp"
	"strings"

	"github.com/drjzlyan/dhi/internal/tasks"
)

// Actions are the DHI-namespaced builtins (also manifest allowlist
// names). Order matters only for documentation; see F-020 §Part A.
const (
	ActionTaskCreate = "task_create"
	ActionTaskStatus = "task_status"
	ActionTaskAssign = "task_assign"
	ActionPROpen     = "pr_open"
)

// All lists every bridge action.
func All() []string {
	return []string{ActionTaskCreate, ActionTaskStatus, ActionTaskAssign, ActionPROpen}
}

// Request is one parsed dhi-action block. Args stays raw until
// Dispatch decodes it against the action's strict shape — unknown keys
// refuse with the key named (F-011).
type Request struct {
	Action string          `json:"action"`
	Args   json.RawMessage `json:"args"`
}

var actionRe = regexp.MustCompile("(?s)```dhi-action\\s*\n(.*?)```")

// ParseActions extracts every dhi-action block from a final message.
// A malformed block yields an error carrying the block index — the
// caller posts refusals to the thread so the agent can correct itself;
// well-formed blocks still dispatch.
func ParseActions(text string) ([]Request, []error) {
	var out []Request
	var errs []error
	for i, m := range actionRe.FindAllStringSubmatch(text, -1) {
		var r Request
		dec := json.NewDecoder(bytes.NewReader([]byte(m[1])))
		if err := dec.Decode(&r); err != nil {
			errs = append(errs, fmt.Errorf("dhi-action[%d]: %w", i, err))
			continue
		}
		out = append(out, r)
	}
	return out, errs
}

// PRSeam opens a PR for a task's branch (reviewSvc-backed in main);
// the returned string is the human-readable result for the thread.
type PRSeam func(ctx context.Context, member, branch, title, base string) (string, error)

// Bridge executes actions against the real stores.
type Bridge struct {
	Tasks  *tasks.Store // nil = task actions refuse by name
	OpenPR PRSeam       // nil = pr_open refuses by name
	// Allow gates every action: false refuses with the action named
	// (the manifest's tools allowlist, enforced by the runtime wiring).
	Allow func(agentID, action string) bool
	// Approve parks a mutating action on the approvals seam (nil = the
	// action runs without a prompt — test and read-only contexts only).
	Approve func(ctx context.Context, agentID, detail string) error
	// Gate enforces the active feature workflow (F-031) at a seam for a
	// task, returning refusal reasons (nil = allowed). nil = no
	// enforcement.
	Gate func(taskSlug, seam string) []string
}

// taskCreateArgs is the strict task_create shape.
type taskCreateArgs struct {
	Slug     string `json:"slug"`
	Title    string `json:"title"`
	Assignee string `json:"assignee,omitempty"`
	Team     string `json:"team,omitempty"`
}

type taskSlugArgs struct {
	Slug string `json:"slug"`
}

type taskStatusArgs struct {
	Slug   string `json:"slug"`
	Status string `json:"status"`
}

type taskAssignArgs struct {
	Slug     string `json:"slug"`
	Assignee string `json:"assignee"`
}

type prOpenArgs struct {
	Slug  string `json:"slug"`
	Title string `json:"title"`
	Base  string `json:"base"`
}

// Dispatch decodes args strictly, then runs one action. The returned
// text is the result line for the bus thread.
func (b *Bridge) Dispatch(ctx context.Context, agentID string, r Request) (string, error) {
	if b.Allow != nil && !b.Allow(agentID, r.Action) {
		return "", fmt.Errorf("action %q is not in %s's tools allowlist", r.Action, agentID)
	}
	switch r.Action {
	case ActionTaskCreate:
		var a taskCreateArgs
		if err := strictDecode(r.Args, &a); err != nil {
			return "", err
		}
		return b.taskCreate(ctx, agentID, a)
	case ActionTaskStatus:
		var a taskStatusArgs
		if err := strictDecode(r.Args, &a); err != nil {
			return "", err
		}
		return b.taskStatus(ctx, agentID, a)
	case ActionTaskAssign:
		var a taskAssignArgs
		if err := strictDecode(r.Args, &a); err != nil {
			return "", err
		}
		return b.taskAssign(ctx, agentID, a)
	case ActionPROpen:
		var a prOpenArgs
		if err := strictDecode(r.Args, &a); err != nil {
			return "", err
		}
		return b.prOpen(ctx, agentID, a)
	default:
		return "", fmt.Errorf("unknown dhi action %q (valid: %s)", r.Action, strings.Join(All(), ", "))
	}
}

func strictDecode(raw json.RawMessage, v any) error {
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.DisallowUnknownFields()
	if err := dec.Decode(v); err != nil {
		return fmt.Errorf("dhi-action args: %w", err)
	}
	return nil
}

func (b *Bridge) taskCreate(ctx context.Context, agentID string, a taskCreateArgs) (string, error) {
	if b.Tasks == nil {
		return "", fmt.Errorf("task store unavailable")
	}
	detail := fmt.Sprintf("task_create %s (%s)", a.Slug, a.Title)
	if err := b.approve(ctx, agentID, detail); err != nil {
		return "", err
	}
	if err := b.Tasks.Create(a.Slug, a.Title, a.Assignee, a.Team); err != nil {
		return "", err
	}
	return "task created: " + a.Slug, nil
}

func (b *Bridge) taskStatus(ctx context.Context, agentID string, a taskStatusArgs) (string, error) {
	if b.Tasks == nil {
		return "", fmt.Errorf("task store unavailable")
	}
	detail := fmt.Sprintf("task_status %s → %s", a.Slug, a.Status)
	if err := b.approve(ctx, agentID, detail); err != nil {
		return "", err
	}
	if err := b.Tasks.SetStatus(a.Slug, tasks.Status(a.Status)); err != nil {
		return "", err
	}
	return fmt.Sprintf("task %s → %s", a.Slug, a.Status), nil
}

func (b *Bridge) taskAssign(ctx context.Context, agentID string, a taskAssignArgs) (string, error) {
	if b.Tasks == nil {
		return "", fmt.Errorf("task store unavailable")
	}
	detail := fmt.Sprintf("task_assign %s → %s", a.Slug, a.Assignee)
	if err := b.approve(ctx, agentID, detail); err != nil {
		return "", err
	}
	if err := b.Tasks.Assign(a.Slug, a.Assignee); err != nil {
		return "", err
	}
	return fmt.Sprintf("task %s assigned to %s", a.Slug, a.Assignee), nil
}

func (b *Bridge) prOpen(ctx context.Context, agentID string, a prOpenArgs) (string, error) {
	if b.OpenPR == nil {
		return "", fmt.Errorf("pr_open unavailable: no review seam wired")
	}
	// Workflow gate (F-031): tests-before-PR is a hard block.
	if b.Gate != nil {
		if reasons := b.Gate(a.Slug, "pr"); len(reasons) > 0 {
			return "", fmt.Errorf("workflow blocks PR: %s", strings.Join(reasons, "; "))
		}
	}
	detail := fmt.Sprintf("pr_open %s (%s)", a.Slug, a.Title)
	if err := b.approve(ctx, agentID, detail); err != nil {
		return "", err
	}
	// The task card's first changeset carries member + branch.
	t, ok := b.Tasks.Get(a.Slug)
	if !ok {
		return "", fmt.Errorf("unknown task %q", a.Slug)
	}
	if len(t.ChangeSets) == 0 {
		return "", fmt.Errorf("task %s has no worktree — attach one first (w)", a.Slug)
	}
	cs := t.ChangeSets[0]
	return b.OpenPR(ctx, cs.Member, cs.Branch, a.Title, a.Base)
}

func (b *Bridge) approve(ctx context.Context, agentID, detail string) error {
	if b.Approve == nil {
		return nil
	}
	return b.Approve(ctx, agentID, detail)
}
