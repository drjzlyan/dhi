package toolbridge

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/drjzlyan/dhi/internal/tasks"
	"github.com/drjzlyan/dhi/internal/workspace"
)

func testStore(t *testing.T) *tasks.Store {
	t.Helper()
	ts, err := tasks.Open(&workspace.Workspace{Root: t.TempDir()})
	if err != nil {
		t.Fatal(err)
	}
	return ts
}

func TestParseActions(t *testing.T) {
	text := "Done with the work.\n" +
		"```dhi-action\n{\"action\":\"task_create\",\"args\":{\"slug\":\"fix-login\",\"title\":\"Fix login\"}}\n```\n" +
		"middle prose ignored\n" +
		"```dhi-action\nnot json\n```"
	reqs, errs := ParseActions(text)
	if len(reqs) != 1 || reqs[0].Action != ActionTaskCreate {
		t.Fatalf("reqs = %+v", reqs)
	}
	if len(errs) != 1 || !strings.Contains(errs[0].Error(), "dhi-action[1]") {
		t.Fatalf("errs = %v", errs)
	}
	if reqs, _ := ParseActions("no blocks here"); len(reqs) != 0 {
		t.Fatalf("plain text yielded %d actions", len(reqs))
	}
}

func TestDispatchTaskActions(t *testing.T) {
	ts := testStore(t)
	b := &Bridge{Tasks: ts}
	ctx := context.Background()

	res, err := b.Dispatch(ctx, "scout", Request{Action: ActionTaskCreate,
		Args: []byte(`{"slug":"fix-login","title":"Fix login","assignee":"scout"}`)})
	if err != nil || res != "task created: fix-login" {
		t.Fatalf("create = %q, %v", res, err)
	}
	if tk, ok := ts.Get("fix-login"); !ok || tk.Title != "Fix login" || tk.Assignee != "scout" {
		t.Fatalf("card = %+v", tk)
	}

	res, err = b.Dispatch(ctx, "scout", Request{Action: ActionTaskStatus,
		Args: []byte(`{"slug":"fix-login","status":"in-review"}`)})
	if err != nil || !strings.Contains(res, "in-review") {
		t.Fatalf("status = %q, %v", res, err)
	}
	res, err = b.Dispatch(ctx, "scout", Request{Action: ActionTaskAssign,
		Args: []byte(`{"slug":"fix-login","assignee":"muse"}`)})
	if err != nil || !strings.Contains(res, "muse") {
		t.Fatalf("assign = %q, %v", res, err)
	}
}

func TestDispatchGatesAndRefusals(t *testing.T) {
	ts := testStore(t)
	allowed := map[string]bool{"task_status": true}
	b := &Bridge{Tasks: ts, Allow: func(agentID, action string) bool { return allowed[action] }}
	ctx := context.Background()

	// Un-allowlisted action refuses with the name.
	if _, err := b.Dispatch(ctx, "scout", Request{Action: ActionTaskCreate,
		Args: []byte(`{"slug":"x","title":"X"}`)}); err == nil ||
		!strings.Contains(err.Error(), `not in scout's tools allowlist`) {
		t.Fatalf("allowlist gate = %v", err)
	}
	// Unknown action names the valid set.
	if _, err := b.Dispatch(ctx, "scout", Request{Action: "teleport",
		Args: []byte(`{}`)}); err == nil || !strings.Contains(err.Error(), "teleport") {
		t.Fatalf("unknown action = %v", err)
	}
	// Unknown keys refuse with the key named (strict args).
	if _, err := b.Dispatch(ctx, "scout", Request{Action: ActionTaskStatus,
		Args: []byte(`{"slug":"x","status":"ok","junk":1}`)}); err == nil ||
		!strings.Contains(err.Error(), "junk") {
		t.Fatalf("strict args = %v", err)
	}
	// Bad status names the value.
	if _, err := b.Dispatch(ctx, "scout", Request{Action: ActionTaskStatus,
		Args: []byte(`{"slug":"x","status":"done-ish"}`)}); err == nil ||
		!strings.Contains(err.Error(), "done-ish") {
		t.Fatalf("bad status = %v", err)
	}
	// No task store refuses by name.
	empty := &Bridge{Allow: func(string, string) bool { return true }}
	if _, err := empty.Dispatch(ctx, "scout", Request{Action: ActionTaskStatus,
		Args: []byte(`{"slug":"x","status":"ok"}`)}); err == nil ||
		!strings.Contains(err.Error(), "task store unavailable") {
		t.Fatalf("nil store = %v", err)
	}
}

func TestApprovalsGateMutatingActions(t *testing.T) {
	ts := testStore(t)
	denied := false
	b := &Bridge{Tasks: ts, Approve: func(ctx context.Context, agentID, detail string) error {
		if denied {
			return context.Canceled
		}
		return nil
	}}
	ctx := context.Background()
	if _, err := b.Dispatch(ctx, "scout", Request{Action: ActionTaskCreate,
		Args: []byte(`{"slug":"ok","title":"OK"}`)}); err != nil {
		t.Fatalf("approved create = %v", err)
	}
	denied = true
	if _, err := b.Dispatch(ctx, "scout", Request{Action: ActionTaskCreate,
		Args: []byte(`{"slug":"no","title":"No"}`)}); err == nil {
		t.Fatal("denied create ran")
	}
	if _, ok := ts.Get("no"); ok {
		t.Fatal("denied action wrote anyway")
	}
}

func TestPROpenRefusals(t *testing.T) {
	ts := testStore(t)
	ctx := context.Background()

	// No seam wired: named refusal.
	b := &Bridge{Tasks: ts}
	if _, err := b.Dispatch(ctx, "scout", Request{Action: ActionPROpen,
		Args: []byte(`{"slug":"x","title":"T","base":"main"}`)}); err == nil ||
		!strings.Contains(err.Error(), "no review seam") {
		t.Fatalf("nil seam = %v", err)
	}

	// Task without a worktree names the attach path.
	_ = ts.Create("x", "X", "scout", "")
	var got string
	b = &Bridge{Tasks: ts, OpenPR: func(ctx context.Context, member, branch, title, base string) (string, error) {
		got = member + "@" + branch
		return "PR #1", nil
	}}
	if _, err := b.Dispatch(ctx, "scout", Request{Action: ActionPROpen,
		Args: []byte(`{"slug":"x","title":"T","base":"main"}`)}); err == nil ||
		!strings.Contains(err.Error(), "no worktree") {
		t.Fatalf("no worktree = %v", err)
	}

	// With a changeset, the seam gets the card's member+branch.
	if err := ts.RecordChangeSet("x", tasks.ChangeSet{Member: "api", Branch: "task/x", Path: "repo"}); err != nil {
		t.Fatal(err)
	}
	res, err := b.Dispatch(ctx, "scout", Request{Action: ActionPROpen,
		Args: []byte(`{"slug":"x","title":"T","base":"main"}`)})
	if err != nil || res != "api: PR #1" || got != "api@task/x" {
		t.Fatalf("pr_open = %q, %v (member@branch %q)", res, err, got)
	}
}

func TestPROpenCoversAllChangesets(t *testing.T) {
	ts := testStore(t)
	_ = ts.Create("multi", "Multi", "scout", "")
	_ = ts.RecordChangeSet("multi", tasks.ChangeSet{Member: "api", Branch: "task/m", Path: "r1"})
	_ = ts.RecordChangeSet("multi", tasks.ChangeSet{Member: "web", Branch: "task/m", Path: "r2"})
	var calls []string
	b := &Bridge{Tasks: ts, OpenPR: func(_ context.Context, member, branch, title, base string) (string, error) {
		calls = append(calls, member+"@"+branch)
		return "PR-" + member, nil
	}}
	res, err := b.Dispatch(context.Background(), "scout", Request{Action: ActionPROpen,
		Args: []byte(`{"slug":"multi","title":"T","base":"main"}`)})
	if err != nil {
		t.Fatalf("Dispatch: %v", err)
	}
	if strings.Join(calls, ",") != "api@task/m,web@task/m" {
		t.Fatalf("calls = %v", calls)
	}
	if !strings.Contains(res, "api: PR-api") || !strings.Contains(res, "web: PR-web") {
		t.Fatalf("result = %q", res)
	}
}

func TestPROpenNamesMemberFailure(t *testing.T) {
	ts := testStore(t)
	_ = ts.Create("mixed", "Mixed", "scout", "")
	_ = ts.RecordChangeSet("mixed", tasks.ChangeSet{Member: "api", Branch: "b", Path: "r1"})
	_ = ts.RecordChangeSet("mixed", tasks.ChangeSet{Member: "web", Branch: "b", Path: "r2"})
	b := &Bridge{Tasks: ts, OpenPR: func(_ context.Context, member, branch, title, base string) (string, error) {
		if member == "web" {
			return "", errors.New("no remote")
		}
		return "PR", nil
	}}
	_, err := b.Dispatch(context.Background(), "scout", Request{Action: ActionPROpen,
		Args: []byte(`{"slug":"mixed","title":"T","base":"main"}`)})
	if err == nil || !strings.Contains(err.Error(), "web") {
		t.Fatalf("member failure must be named: %v", err)
	}
}

func TestPROpenBlockedByWorkflowGate(t *testing.T) {
	ts := testStore(t)
	_ = ts.Create("x", "X", "scout", "")
	_ = ts.RecordChangeSet("x", tasks.ChangeSet{Member: "api", Branch: "task/x", Path: "repo"})
	opened := false
	b := &Bridge{
		Tasks:  ts,
		OpenPR: func(context.Context, string, string, string, string) (string, error) { opened = true; return "PR", nil },
		Gate: func(slug, seam string) []string {
			if seam == "pr" {
				return []string{"step test requires run:test to pass"}
			}
			return nil
		},
	}
	_, err := b.Dispatch(context.Background(), "scout", Request{Action: ActionPROpen,
		Args: []byte(`{"slug":"x","title":"T","base":"main"}`)})
	if err == nil || !strings.Contains(err.Error(), "workflow blocks PR") {
		t.Fatalf("gated PR = %v", err)
	}
	if opened {
		t.Fatal("gated PR must not reach the seam")
	}
}

func TestReviewBypassRoutesThroughApprovalsAndRecords(t *testing.T) {
	ts := testStore(t)
	_ = ts.Create("x", "X", "scout", "")
	var prompts []string
	b := &Bridge{
		Tasks: ts,
		Approve: func(ctx context.Context, agentID, detail string) error {
			prompts = append(prompts, detail)
			return nil
		},
		Gate: func(slug, seam string) []string {
			if seam == "review" {
				return []string{"step review needs an explicit approval"}
			}
			return nil
		},
	}
	res, err := b.Dispatch(context.Background(), "scout", Request{Action: ActionTaskStatus,
		Args: []byte(`{"slug":"x","status":"done"}`)})
	if err != nil {
		t.Fatalf("Dispatch: %v", err)
	}
	if !strings.Contains(res, "bypass recorded") {
		t.Fatalf("result = %q", res)
	}
	if len(prompts) != 2 || !strings.Contains(prompts[1], "bypass review") {
		t.Fatalf("approvals prompts = %v, want a review-bypass prompt", prompts)
	}
	got, _ := ts.Get("x")
	if got.Status != tasks.Done || len(got.Bypasses) != 1 || got.Bypasses[0].Step != "review" {
		t.Fatalf("bypass not recorded: %+v", got)
	}
}

func TestReviewBypassDeniedLeavesTask(t *testing.T) {
	ts := testStore(t)
	_ = ts.Create("y", "Y", "scout", "")
	calls := 0
	b := &Bridge{
		Tasks: ts,
		Approve: func(ctx context.Context, agentID, detail string) error {
			calls++
			if strings.Contains(detail, "bypass review") {
				return errors.New("human declined")
			}
			return nil
		},
		Gate: func(string, string) []string { return []string{"unmet review"} },
	}
	if _, err := b.Dispatch(context.Background(), "scout", Request{Action: ActionTaskStatus,
		Args: []byte(`{"slug":"y","status":"done"}`)}); err == nil {
		t.Fatal("declined bypass must refuse")
	}
	if got, _ := ts.Get("y"); got.Status == tasks.Done || len(got.Bypasses) != 0 {
		t.Fatalf("declined bypass must not complete the task: %+v", got)
	}
}
