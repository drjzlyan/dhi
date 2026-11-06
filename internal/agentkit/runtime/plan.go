package runtime

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"

	"github.com/drjzlyan/dhi/internal/agentkit/behavior"
	"github.com/drjzlyan/dhi/internal/agentkit/bus"
	"github.com/drjzlyan/dhi/internal/agentkit/dhitools"
	"github.com/drjzlyan/dhi/internal/agentkit/episode"
	"github.com/drjzlyan/dhi/internal/agentkit/tools"
	"github.com/drjzlyan/dhi/internal/sandbox"
	"github.com/drjzlyan/dhi/internal/tasks"
)

// F-144: gated plan mode and the read-only review and ask modes. A plan
// turn runs with no write tool (the adapter's read-only mode, and DHI's
// own tools at read scope), the agent publishes its plan with plan_update,
// and the human approves it on an approval card. Approval posts the plan
// as the human's next message, so the normal write turn carries it in its
// prompt; rejection writes nothing and leaves the plan as a draft.

// ReadOnlyTurn runs one read-only turn of agentID in mode (plan, review
// or ask). trigger is the human's message in the thread; request is the
// text after the slash command. It blocks until the turn, and for a plan
// the human's verdict, is done; the chat runs it in the background.
func (r *Runtime) ReadOnlyTurn(ctx context.Context, agentID string, trigger bus.Message, mode, request string) error {
	r.mu.Lock()
	e, ok := r.agents[agentID]
	r.mu.Unlock()
	if !ok {
		return fmt.Errorf("runtime: unknown agent %q", agentID)
	}
	if !behavior.ModeReadOnly(mode) {
		return fmt.Errorf("runtime: unknown mode %q", mode)
	}
	refuse := func(format string, a ...any) error {
		msg := fmt.Sprintf(format, a...)
		r.alert(trigger, agentID, msg)
		return fmt.Errorf("runtime: %s: %w (%s)", agentID, errAlerted, msg)
	}
	if !e.cli.ReadOnlyOK {
		// Refused by name (ADR-0011): running it with writes on would
		// make "read-only" a promise nothing keeps.
		return refuse("/%s needs a read-only run and %s has none (%s); nothing was started", mode, e.cli.Name, e.cli.ReadOnlyWhy)
	}
	r.waitStart(trigger.Channel)
	e.turnMu.Lock()
	r.waitDone(trigger.Channel)
	defer e.turnMu.Unlock()
	key := workKey(trigger.Channel, bus.ThreadOf(trigger))
	r.workStart(key)
	defer r.workDone(key)
	ctx, release := r.cancellable(ctx, key)
	defer release()
	r.workStart(agentKey(agentID))
	defer r.workDone(agentKey(agentID))

	task, _ := r.boundTask(trigger)
	tune := r.tuning()
	if msg, ok := r.budgetCheck(agentID, task.Slug, tune.Budgets); !ok {
		return refuse("I stopped: the budget is reached — %s. Raise it in Settings → CONFIG, then ask again.", msg)
	}
	ep := r.openEpisode(agentID, task.Slug, trigger)
	var (
		mu    sync.Mutex
		steps []dhitools.PlanStep
	)
	var seam func([]dhitools.PlanStep) error
	if mode == behavior.ModePlan {
		if r.cfg.Episodes != nil {
			_ = r.cfg.Episodes.Update(agentID, ep.Key, func(cur *episode.Episode) { cur.Plan = nil })
		}
		persist := r.planSeam(agentID, ep.Key)
		seam = func(s []dhitools.PlanStep) error {
			mu.Lock()
			steps = append([]dhitools.PlanStep(nil), s...)
			mu.Unlock()
			if persist != nil {
				return persist(s)
			}
			return nil
		}
	}
	serve := r.serveToolsFor(e, trigger, turnTools{
		plan: seam, readOnly: true, memberRoots: r.memberRoots(task), task: task.Slug,
	})
	if serve == nil && mode == behavior.ModePlan {
		return refuse("/plan needs DHI's plan_update tool and %s cannot be given DHI's tools in this workspace; nothing was started", e.cli.Name)
	}
	mcpConfig, mcpEndpoint, mcpGeminiDir, webDenied := "", "", "", false
	if serve != nil {
		defer serve.stop()
		mcpConfig, mcpEndpoint, mcpGeminiDir, webDenied = serve.configPath, serve.endpoint, serve.geminiDir, serve.webDenied
	}
	if e.m.Timeout > 0 {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, e.m.Timeout)
		defer cancel()
	}

	asked := trigger
	asked.Text = request
	// Never resumed: a resumed session is the write mode's, and codex's
	// resume form takes no sandbox flag.
	prompt, system := r.cliPromptMode(ctx, e, asked, "", 0, &ep, false, mode)
	run, _ := r.cliSpawnOnce(ctx, e, trigger, spawnInput{
		prompt: prompt, system: system, workdir: r.cliWorkdir(trigger),
		maxTurns: tune.MaxTurns, stall: tune.Stall, model: ep.Model, readOnly: true,
		mcpConfig: mcpConfig, mcpEndpoint: mcpEndpoint, mcpGeminiDir: mcpGeminiDir, webDenied: webDenied,
		budgets: tune.Budgets, taskSlug: task.Slug,
	})
	r.recordRun(trigger, run)
	r.recordSpend(run, task.Slug)
	if run.Status != tasks.RunOK {
		return refuse("the %s turn failed: %s", mode, run.Error)
	}
	summary := strings.TrimSpace(sanitizeReply(run.Summary))
	if mode != behavior.ModePlan {
		if summary != "" {
			_, _ = r.cfg.Bus.Post(bus.Message{
				Channel: trigger.Channel, Thread: trigger.Thread,
				Author: agentID, Text: summary, Kind: bus.KindReply,
			})
		}
		return nil
	}

	mu.Lock()
	got := steps
	mu.Unlock()
	if len(got) == 0 {
		return refuse("I produced no plan: plan_update was never called, so there is nothing to approve. %s", summary)
	}
	return r.gatePlan(ctx, agentID, trigger, summary, got)
}

// gatePlan shows a published plan to the human and, on approval, starts
// the write turn with the plan in its prompt.
func (r *Runtime) gatePlan(ctx context.Context, agentID string, trigger bus.Message, summary string, got []dhitools.PlanStep) error {
	plan := PlanText(got)
	reply := plan
	if summary != "" {
		reply = summary + "\n\n" + plan
	}
	_, _ = r.cfg.Bus.Post(bus.Message{
		Channel: trigger.Channel, Thread: trigger.Thread,
		Author: agentID, Text: reply, Kind: bus.KindReply,
	})
	if r.cfg.Approvals == nil {
		r.notice(trigger, agentID, "no approvals queue is wired; the plan stays a draft and nothing was written")
		return nil
	}
	// The card waits for the human however long it takes; the plan turn
	// is over, so nothing else is held.
	var titles []string
	for _, s := range got {
		titles = append(titles, s.Title)
	}
	err := r.cfg.Approvals.AskScope(context.WithoutCancel(ctx), agentID, tools.AccessPlan+agentID, sandbox.OpRead,
		strings.Join(titles, "\n"), "approve to let "+agentID+" carry it out; reject keeps it as a draft and writes nothing")
	if err != nil {
		if errors.Is(err, context.Canceled) {
			return nil
		}
		r.notice(trigger, agentID, "plan rejected: nothing was written; the plan stays a draft (send a message to revise it)")
		return nil
	}
	approved, perr := r.cfg.Bus.Post(bus.Message{
		Channel: trigger.Channel, Thread: bus.ThreadOf(trigger), Author: bus.Human,
		Text: "Plan approved. Carry it out, step by step, and keep plan_update current:\n" + plan,
	})
	if perr != nil {
		return fmt.Errorf("runtime: %s: post approval: %w", agentID, perr)
	}
	r.Handle(context.WithoutCancel(ctx), approved)
	return nil
}

// PlanText renders a plan as numbered lines, the form the approval and
// the write turn's prompt carry.
func PlanText(steps []dhitools.PlanStep) string {
	var b strings.Builder
	for i, s := range steps {
		fmt.Fprintf(&b, "%d. %s\n", i+1, s.Title)
	}
	return strings.TrimRight(b.String(), "\n")
}
