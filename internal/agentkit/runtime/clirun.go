package runtime

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync/atomic"
	"time"

	"github.com/drjzlyan/dhi/internal/agentkit/behavior"
	"github.com/drjzlyan/dhi/internal/agentkit/bus"
	"github.com/drjzlyan/dhi/internal/agentkit/clirun"
	"github.com/drjzlyan/dhi/internal/agentkit/dhitools"
	"github.com/drjzlyan/dhi/internal/agentkit/episode"
	"github.com/drjzlyan/dhi/internal/agentkit/lessons"
	"github.com/drjzlyan/dhi/internal/agentkit/mcpbridge"
	"github.com/drjzlyan/dhi/internal/agentkit/standards"
	"github.com/drjzlyan/dhi/internal/agentkit/trace"
	"github.com/drjzlyan/dhi/internal/agentkit/workflow"
	"github.com/drjzlyan/dhi/internal/mcp"
	"github.com/drjzlyan/dhi/internal/tasks"
	"github.com/drjzlyan/dhi/internal/workspace"
)

// cliTurn executes one full turn of a CLI-runtime agent (F-013,
// ADR-0012): flat prompt → sandbox-wrapped headless spawn in the task
// worktree → streamed transcript (posted to the trigger thread + one
// persisted JSONL per attempt) → finalize → run record on the task card
// + final reply in the thread. A failed or timed-out attempt retries per
// the manifest's retries budget (step 7), backing off between spawns.
// The manifest's `runtime` key is the exec authorization; the OS sandbox
// — from the same Guard seam every exec uses — is the boundary.
func (r *Runtime) cliTurn(ctx context.Context, e *entry, trigger bus.Message, tl *turnLog) error {
	// The KB retrieval rides the turn context but must not inherit a
	// timeout that would cut the run short: bounded by the caller's
	// context here, before the manifest timeout arms (the timeout
	// governs the CLI run, not context assembly).
	task, hasTask := r.boundTask(trigger)
	wfText := ""
	if r.cfg.Workflows {
		slug, wd, err := r.activeWorkflow(e.m)
		if err != nil {
			return err
		}
		// F-066: the feature workflow (worktree → test → commit → PR)
		// governs task work; a chat turn with no card is not a feature
		// and carrying the steps only sends the agent hunting for a
		// branch to push.
		if wd != nil && hasTask {
			wfText = workflow.Render(wd)
			_ = r.cfg.Tasks.SetWorkflow(task.Slug, slug)
		}
	}
	if hasTask {
		task = r.provisionWorktree(e.m.ID, trigger, task)
	}
	workdir := r.cliWorkdir(trigger)
	tune := r.tuning()
	ep := r.openEpisode(e.m.ID, task.Slug, trigger)
	if wfText != "" && len(task.ChangeSets) > 0 {
		// The worktree step is done (DHI did it): say so, so the agent
		// works in it instead of making another.
		if _, wd, err := r.activeWorkflow(e.m); err == nil && wd != nil {
			wfText = workflow.RenderDone(wd, workflow.Progress{Worktree: true, TestsPass: task.TestsPass})
		}
	} else if wfText != "" {
		wfText += noWorktreeNote
	}
	rl := &relaunch{}
	serve := r.serveToolsFor(e, trigger, turnTools{
		workBranch:  r.workBranchSeam(e.m.ID, trigger, rl),
		access:      r.accessSeam(e.m.ID, trigger, rl),
		envelope:    r.envelopeFor(task, hasTask, workdir, tune),
		task:        task.Slug,
		plan:        r.planSeam(e.m.ID, ep.Key),
		memberRoots: r.memberRoots(task),
		lesson:      r.lessonRecorder(e.m.ID),
	})
	resume := ""
	if tune.Resume && e.cli.ResumeOK && ep.Resumable(e.cli.Name, workdir) {
		resume = ep.Session
		// F-078: a session close to its context limit starts fresh with
		// the episode's summary rather than overflowing mid-task.
		if limit := tune.memoryTuning().SessionTokens; ep.LastContext > limit {
			r.notice(trigger, e.m.ID, fmt.Sprintf("the CLI session is at %d tokens (memory.session_tokens %d); starting a fresh session with a summary", ep.LastContext, limit))
			resume, ep.Session, ep.LastContext = "", "", 0
		}
	}
	level := 0
	prompt, system := r.cliPromptAt(ctx, e, trigger, wfText, level, &ep, resume != "")
	mcpConfig, mcpEndpoint, mcpGeminiDir := "", "", ""
	if serve != nil {
		defer serve.stop()
		mcpConfig, mcpEndpoint, mcpGeminiDir = serve.configPath, serve.endpoint, serve.geminiDir
	}

	if e.m.Timeout > 0 {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, e.m.Timeout)
		defer cancel()
	}

	// F-067 goal loop: on task work DHI checks the result itself and
	// sends failures back, rather than trusting "done".
	var targets []verifyTarget
	if hasTask && r.cfg.Run != nil {
		targets = r.verifyTargets(task, workdir)
	}
	startState, stateOK := r.worktreeState(ctx, workdir)
	maxIter := max(tune.MaxIterations, 1)
	iter, lastSig := 0, ""
	ep.Verify, ep.VerifyCmd, ep.VerifyTail, ep.Iterations = "", "", "", 0

	brief := ""
	freshFallback := false
	for attempt := 0; ; attempt++ {
		// F-066: budgets bind between spawns (a CLI runs to completion).
		if msg, ok := r.budgetCheck(e.m.ID, task.Slug, tune.Budgets); !ok {
			r.saveEpisode(ep)
			r.alert(trigger, e.m.ID, "I stopped: the budget is reached — "+msg+". Raise it in Settings → CONFIG, then ask again.")
			return fmt.Errorf("runtime: %s: budget: %s: %w", e.m.ID, msg, errAlerted)
		}
		run, events := r.cliSpawnOnce(ctx, e, trigger, spawnInput{
			prompt: prompt + brief, system: system, workdir: workdir, attempt: attempt,
			resume: resume, maxTurns: tune.MaxTurns, stall: tune.Stall,
			mcpConfig: mcpConfig, mcpEndpoint: mcpEndpoint, mcpGeminiDir: mcpGeminiDir,
		})
		r.recordRun(trigger, run)
		r.recordSpend(run, task.Slug)
		tl.runs = append(tl.runs, run.ID)
		tl.outcome = run.Summary
		if run.Status != tasks.RunOK {
			tl.outcome = run.Error
		}
		session := ""
		if e.cli.ResumeOK {
			session = run.Session
		}
		ep = episode.Fold(ep, run, e.cli.Name, workdir, session, trigger.ID)

		if run.Status == tasks.RunOK && rl.get() != "" {
			// F-086: the work moved (a new worktree) — this spawn's cwd
			// and sandbox are stale; the agent continues in a fresh run.
			r.saveEpisode(ep)
			r.continueTurn(ctx, e.m.ID, trigger, rl.get())
			return nil
		}
		if run.Status == tasks.RunOK {
			summary := sanitizeReply(run.Summary)
			changed := true
			if stateOK {
				now, ok := r.worktreeState(ctx, workdir)
				changed = !ok || now != startState
			}
			if len(targets) > 0 && changed {
				iter++
				ep.Iterations = iter
				res := r.verify(ctx, e.m.ID, r.agentScopes(e.m), targets)
				cmd, tail, ok := res.cmd, res.tail, res.ok
				if hasTask {
					_ = r.cfg.Tasks.SetTestsPass(task.Slug, ok)
				}
				ep.VerifyCmd, ep.VerifyTail = cmd, tail
				switch {
				case !ok && res.env:
					// Not the agent's bug: the check could not run here.
					ep.Verify = "env"
					r.notice(trigger, e.m.ID, fmt.Sprintf("could not verify: `%s` needs something this environment lacks (network, a module, a permission); the result is unverified:\n%s",
						cmd, strings.Join(lastN(strings.Split(tail, "\n"), 3), "\n")))
				case ok:
					ep.Verify, ep.VerifyCmd, ep.VerifyTail = "pass", targets[0].String(), ""
					r.notice(trigger, e.m.ID, fmt.Sprintf("verified: %s passed (round %d)", targets[0], iter))
				case failureSig(tail) == lastSig:
					tl.signal(lessons.SignalStuck)
					tl.fail("`" + cmd + "` failed the same way twice:\n" + tail)
					ep.Verify = "stuck"
					r.saveEpisode(ep)
					r.humanNote(trigger, e.m.ID, fmt.Sprintf(
						"I'm stuck: `%s` fails the same way after my fix. I need your call.\n```\n%s\n```", cmd, tail))
					return nil
				case iter >= maxIter:
					tl.signal(lessons.SignalStuck)
					tl.fail("`" + cmd + "` still failed after the last round:\n" + tail)
					ep.Verify = "fail"
					r.saveEpisode(ep)
					r.humanNote(trigger, e.m.ID, fmt.Sprintf(
						"`%s` still fails after %d rounds (agents.max_iterations). Last output:\n```\n%s\n```", cmd, iter, tail))
					return nil
				default:
					// Another round: continue the session with the failure.
					tl.signal(lessons.SignalVerify)
					tl.fail("`" + cmd + "` failed after your change:\n" + tail)
					ep.Verify = "fail"
					lastSig = failureSig(tail)
					r.saveEpisode(ep)
					r.notice(trigger, e.m.ID, fmt.Sprintf("verify failed: %s (round %d of %d) — sending it back", cmd, iter, maxIter))
					if session != "" && tune.Resume && ep.LastContext <= tune.memoryTuning().SessionTokens {
						resume, prompt = session, ""
					} else {
						resume = ""
						prompt, system = r.cliPromptAt(ctx, e, trigger, wfText, level, &ep, false)
					}
					brief = verifyBrief(cmd, tail, iter, maxIter)
					attempt-- // a verify round is not a failed attempt
					continue
				}
			} else if len(targets) == 0 && r.taskWorktree(task) != "" && r.cfg.Run != nil && iter == 0 {
				r.notice(trigger, e.m.ID, "no verify command detected for this project; the result is unverified")
			}
			if summary != "" {
				// F-036: the final reply re-enters dispatch so an
				// @mention hands the work to the addressee.
				if posted, err := r.cfg.Bus.Post(bus.Message{
					Channel: trigger.Channel, Thread: trigger.Thread,
					Author: e.m.ID, Text: summary, Kind: bus.KindReply,
				}); err == nil {
					ep.SeenID = max(ep.SeenID, posted.ID) // the session wrote it
					r.saveEpisode(ep)
					r.Handle(context.WithoutCancel(ctx), posted)
					return nil
				}
			}
			r.saveEpisode(ep)
			return nil
		}
		r.saveEpisode(ep)
		// F-065: react to the cause. Auth and a spent deadline cannot be
		// fixed by another spawn; an overflow retries compacted (once
		// there is nothing left to compact, it is final); everything
		// else retries with a brief of what failed and where it stopped.
		lastErr := fmt.Errorf("runtime: %s: run %s: %s", e.m.ID, run.ID, run.Error)
		fail := clirun.Classify(run.Status == tasks.RunTimeout, run.Error)
		// F-066: a resumed spawn that fails may be a dead session (expired,
		// pruned, another machine). Fall back to one fresh spawn — named,
		// never silent — without spending the retry budget.
		if resume != "" && fail.Retryable() && fail != clirun.FailStall && !freshFallback {
			freshFallback = true
			resume, ep.Session = "", ""
			prompt, system = r.cliPromptAt(ctx, e, trigger, wfText, level, &ep, false)
			brief = failureBrief(fail, run, events)
			r.notice(trigger, e.m.ID, "could not continue the earlier CLI session ("+string(fail)+"); starting a fresh one")
			attempt--
			continue
		}
		if !fail.Retryable() || attempt >= e.m.Retries ||
			fail == clirun.FailOverflow && level == maxCompaction {
			r.alert(trigger, e.m.ID, humanFailure(fail, e.cli))
			return fmt.Errorf("%w (%w)", lastErr, errAlerted)
		}
		if fail == clirun.FailOverflow {
			level++
			resume, ep.Session = "", "" // a compacted prompt starts a fresh session
			prompt, system = r.cliPromptAt(ctx, e, trigger, wfText, level, &ep, false)
		} else if session != "" && tune.Resume {
			// The failed attempt's session holds its progress (e.g. it
			// ran out of turns or stalled): continue it, don't start over.
			resume = session
			prompt, system = r.cliPromptAt(ctx, e, trigger, wfText, level, &ep, true)
		}
		brief = failureBrief(fail, run, events)
		if fail == clirun.FailCrash || fail == clirun.FailNoFinal {
			// Only the agent's own failures teach anything; a rate limit,
			// stall, overflow or timeout would just repeat in reflection.
			tl.signal(lessons.SignalRetry)
			tl.fail(brief)
		}
		wait := retryWait(fail, attempt)
		r.notice(trigger, e.m.ID, fmt.Sprintf("retrying (attempt %d/%d) in %s… (%s)",
			attempt+1, e.m.Retries, wait.Round(time.Second), fail))
		select {
		case <-time.After(wait):
		case <-ctx.Done():
			return fmt.Errorf("runtime: %s: %w", e.m.ID, ctx.Err())
		}
	}
}

// notice posts a status line into the trigger's thread under author. It
// is marked as stream so prompts never replay it as conversation.
func (r *Runtime) notice(trigger bus.Message, author, text string) {
	_, _ = r.cfg.Bus.Post(bus.Message{
		Channel: trigger.Channel, Thread: trigger.Thread,
		Author: author, Text: text, Kind: bus.KindStream,
	})
}

// cliRetryBackoff is the wait between retry spawns (F-013 step 7 says
// 30s in production; tests compress it to milliseconds via this var).
var cliRetryBackoff = 30 * time.Second

// cliSpawnOnce runs a single attempt: spawn the wrapped CLI, stream the
// transcript into the trigger thread, persist the event JSONL, finalize,
// and return the run record.
// spawnInput is one attempt's spawn parameters.
type spawnInput struct {
	prompt, system, workdir              string
	attempt                              int
	resume                               string // CLI session to continue ("" = fresh)
	maxTurns                             int
	stall                                time.Duration // watchdog; 0 = off
	model                                string        // "" = the agent's model
	quiet                                bool          // don't relay the stream (reflection)
	mcpConfig, mcpEndpoint, mcpGeminiDir string
}

func (r *Runtime) cliSpawnOnce(ctx context.Context, e *entry, trigger bus.Message, in spawnInput) (tasks.Run, []clirun.StreamEvent) {
	prompt, system, workdir := in.prompt, in.system, in.workdir
	model := e.m.CLIModel()
	if in.model != "" {
		model = in.model
	}
	started := time.Now().UTC()
	run := tasks.Run{
		ID: runID(started), Agent: e.m.ID,
		Runtime: "cli:" + e.m.Runtime, Model: e.m.CLIModel(), Attempt: in.attempt,
		Started: started, Finished: time.Now().UTC(),
		TokensIn: -1, TokensOut: -1,
	}

	// Oversized delivery (M14 P1): when the assembled system+prompt
	// exceeds the argv budget, prompt delivery moves to stdin where the
	// adapter has a verified path; adapters without one refuse by name
	// instead of dying on E2BIG (ADR-0011). The stdin blob carries the
	// shared tagged shape so the system block survives the move.
	stdin := ""
	if len(prompt)+len(system) > clirun.MaxPromptArg() {
		if !e.cli.StdinOK {
			run.Status = tasks.RunError
			run.Error = fmt.Sprintf(
				"assembled prompt exceeds the argv budget (%d KiB) and %s has no verified stdin delivery — shorten the thread history or the system block",
				clirun.MaxPromptArg()>>10, e.cli.Name)
			return run, nil
		}
		stdin = clirun.PromptWithSystem(system, prompt)
		prompt, system = "", ""
	}

	// The sandbox Wrap contract is binary-first: argv[0] is the CLI and
	// the result is the COMPLETE command line to exec (Noop returns it
	// unchanged; seatbelt/bubblewrap prepend their wrapper and a `--`
	// separator). Passing BuildArgs output alone made everything after
	// the wrapper's `--` land in the CLI's lap as positional args — for
	// claude that turned the sandbox invocation itself into the prompt
	// and the reply came back as plain text (no stream-json at all).
	argv := append([]string{e.cliPath}, e.cli.BuildArgs(clirun.RunInput{
		Prompt: prompt, System: system,
		Model: model, Workdir: workdir, Stdin: stdin,
		Resume: in.resume, MaxTurns: in.maxTurns,
		MCPConfig: in.mcpConfig, MCPURL: in.mcpEndpoint, MCPGeminiDir: in.mcpGeminiDir,
	})...)
	wrapped, err := r.spawnSandbox(e).Wrap(argv)
	if err != nil {
		run.Status = tasks.RunError
		run.Error = fmt.Sprintf("wrap: %v", err)
		return run, nil
	}

	// F-067 watchdog: a spawn that emits nothing for in.stall is killed
	// and classified "stall" (retryable; its session continues) instead
	// of holding the turn until a manifest timeout that may not exist.
	spawnCtx, stopSpawn := context.WithCancel(ctx)
	defer stopSpawn()
	var stalled atomic.Bool
	cmd := exec.CommandContext(spawnCtx, wrapped[0], wrapped[1:]...)
	cmd.Dir = workdir
	cmd.Env = r.cliEnv(e.cli, e.m.ID, in.mcpConfig)
	if stdin != "" {
		cmd.Stdin = strings.NewReader(stdin)
	}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		run.Status = tasks.RunError
		run.Error = fmt.Sprintf("stdout: %v", err)
		return run, nil
	}
	// A bounded stderr tail names why a CLI died without a final line
	// ("not logged in", "rate limited") so the failure can be classified
	// (F-065); read only after Wait, which joins exec's copy goroutine.
	stderr := &tailBuffer{max: 2048}
	cmd.Stderr = stderr
	// A grandchild that outlives the CLI (an MCP child, something the
	// agent backgrounded) can hold stderr open; never let that wedge Wait.
	cmd.WaitDelay = 2 * time.Second
	if err := cmd.Start(); err != nil {
		run.Status = tasks.RunError
		run.Error = fmt.Sprintf("spawn %s: %v", e.cli.Bin, err)
		return run, nil
	}

	// Stream the transcript into the trigger's thread as it happens, so
	// long CLI runs stay visible; the terminal line is kept for
	// Finalize. Events are also persisted (F-013 step 4) for the P2
	// run-replay surface.
	var final string
	var events []clirun.StreamEvent
	var stamps []time.Time // when each event arrived (F-074 trace)
	var dog *time.Timer
	if in.stall > 0 {
		dog = time.AfterFunc(in.stall, func() { stalled.Store(true); stopSpawn() })
	}
	for ev := range e.cli.ParseStream(stdout) {
		if dog != nil {
			dog.Reset(in.stall)
		}
		events = append(events, ev)
		stamps = append(stamps, time.Now().UTC())
		if ev.Denied != "" && !in.quiet {
			r.offerAccess(e.m.ID, trigger, ev.Denied)
		}
		if !in.quiet {
			r.cliEvent(e.m.ID, trigger, ev)
		}
		if ev.Kind == clirun.EventFinal {
			final = ev.Detail
		}
	}
	if dog != nil {
		dog.Stop()
	}
	waitErr := cmd.Wait()
	run.Finished = time.Now().UTC()
	run.Exit = exitCode(waitErr)

	switch {
	case stalled.Load():
		run.Status = tasks.RunError
		run.Error = fmt.Sprintf("stalled: no output for %s", in.stall)
	case ctx.Err() != nil:
		// The turn's context fired (manifest timeout or caller cancel);
		// the SIGKILLed CLI may still have flushed a partial stream.
		run.Status = tasks.RunTimeout
		run.Error = "run interrupted: " + ctx.Err().Error()
	case final != "":
		// The terminal line is the structured contract; classify via
		// the adapter. A non-zero exit with a result line is the CLI's
		// own message (e.g. error_max_turns), and we honor that over
		// the bare exit code.
		summary, u, ferr := e.cli.Finalize(final)
		if ferr != nil {
			run.Status = tasks.RunError
			run.Error = ferr.Error()
			break
		}
		run.Status = tasks.RunOK
		run.Summary = summary
		run.TokensIn, run.TokensOut = u.TokensIn, u.TokensOut
		run.CostUSD = u.CostUSD
		run.HasCost = u.HasCost
		run.CacheRead, run.CacheWrite = u.CacheRead, u.CacheWrite
		run.Session = u.SessionID
	case waitErr != nil:
		run.Status = tasks.RunError
		run.Error = cliErrText(waitErr)
		if last := stderr.lastLine(); last != "" {
			run.Error += ": " + truncateRunes(last, 300)
		}
	default:
		run.Status = tasks.RunError
		run.Error = "CLI stream ended without a final event"
	}

	tr := transcript{events: events, stamps: stamps, in: in, stderr: stderr.lastLine(),
		full: r.tuning().TracePrompts}
	run.Transcript = r.saveTranscript(tr, run)
	return run, events
}

// transcript is one attempt's record for the trace (F-074): the stream
// events with arrival times, what was sent, and the stderr tail.
type transcript struct {
	events []clirun.StreamEvent
	stamps []time.Time
	in     spawnInput
	stderr string
	full   bool // agents.trace_prompts: keep the prompt text itself
}

// saveTranscript persists one attempt as JSONL under
// .dhi/agents/<id>/runs/<run-id>-<attempt>.jsonl (F-013 step 4): first a
// prompt record (sizes, resume; the text itself with trace_prompts),
// then each event with its arrival time, then the stderr tail of a
// failed run. It is best-effort: a failure only loses the trace file.
func (r *Runtime) saveTranscript(tr transcript, run tasks.Run) string {
	dir := filepath.Join(r.cfg.WS.Root, ".dhi", "agents", run.Agent, "runs")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return ""
	}
	p := filepath.Join(dir, fmt.Sprintf("%s-%d.jsonl", run.ID, run.Attempt))
	f, err := os.Create(p)
	if err != nil {
		return ""
	}
	defer func() { _ = f.Close() }()
	enc := json.NewEncoder(f)
	put := func(rec trace.Record) bool {
		rec.Attempt = run.Attempt
		return enc.Encode(rec) == nil
	}
	sent := fmt.Sprintf("prompt %d chars · system %d chars", len([]rune(tr.in.prompt)), len([]rune(tr.in.system)))
	if tr.in.resume != "" {
		sent += " · resumed session " + tr.in.resume
	}
	if tr.in.model != "" {
		sent += " · model " + tr.in.model
	}
	if !put(trace.Record{Kind: "prompt", Detail: sent, At: run.Started}) {
		return ""
	}
	if tr.full {
		put(trace.Record{Kind: "system_text", Detail: tr.in.system, At: run.Started})
		put(trace.Record{Kind: "prompt_text", Detail: tr.in.prompt, At: run.Started})
	}
	for i, ev := range tr.events {
		if ev.Detail == "" {
			continue
		}
		rec := trace.Record{Kind: ev.Kind, Detail: ev.Detail}
		if i < len(tr.stamps) {
			rec.At = tr.stamps[i]
		}
		if !put(rec) {
			return ""
		}
	}
	if run.Status != tasks.RunOK && tr.stderr != "" {
		put(trace.Record{Kind: "stderr", Detail: tr.stderr, At: run.Finished})
	}
	return p
}

// serveSession is one per-turn IDE-tools server (nil when the agent
// allowlist carries no served tools, or the adapter has no verified
// MCP wiring).
type serveSession struct {
	configPath string // temp MCP config file ("" for inline adapters)
	endpoint   string // the loopback URL the adapter must reach
	geminiDir  string // per-turn --gemini_dir mirror ("" for other adapters)
	stop       func()
}

// serveTools starts the loopback IDE-tools endpoint for agents whose
// allowlist includes served tools on an MCP-capable adapter. The
// endpoint lives exactly as long as the turn (ADR-0017: no daemon);
// the temp config dies with it.
func (r *Runtime) serveTools(e *entry, trigger bus.Message) *serveSession {
	return r.serveToolsFor(e, trigger, turnTools{})
}

// serveToolsFor is serveTools with the turn's envelope, bound card and
// plan seam (F-067). The harness tools (plan_update) are served to every
// MCP-capable agent when their seam is wired, allowlist or not.
func (r *Runtime) serveToolsFor(e *entry, trigger bus.Message, tt turnTools) *serveSession {
	if r.cfg.WS == nil || e.cli == nil || !e.cli.MCPWired() {
		return nil
	}
	wantDHI, wantBridge := tt.plan != nil || tt.lesson != nil || r.cfg.Forge != nil || r.cfg.SetProps != nil, false
	for _, t := range e.m.Tools {
		if dhitools.Serves(t) {
			wantDHI = true
		}
		if strings.HasPrefix(t, mcpbridge.ToolPrefix) {
			wantBridge = true
		}
	}
	if !wantDHI && !wantBridge {
		return nil
	}
	var handler mcp.Handler
	if wantDHI {
		wfGate, onRun := r.workflowEnforcer(e.m, trigger)
		kids := r.childGate(trigger)
		gate := func(seam string) []string {
			var reasons []string
			if wfGate != nil {
				reasons = wfGate(seam)
			}
			return append(reasons, kids(seam)...)
		}
		handler = dhitools.Deps{
			Agent:       e.m,
			Tasks:       r.cfg.Tasks,
			KB:          r.cfg.Knowledge,
			Memory:      r.cfg.Memory,
			Bus:         r.cfg.Bus,
			WS:          r.cfg.WS,
			Search:      r.cfg.Search,
			Approvals:   r.cfg.Approvals,
			Git:         r.cfg.Git,
			Identity:    r.cfg.Identity,
			Sessions:    r.cfg.Sessions,
			Editor:      r.cfg.Editor,
			Scopes:      r.agentScopes(e.m),
			Gate:        gate,
			OnRun:       onRun,
			PR:          r.cfg.PR,
			Skills:      r.lib(),
			Conventions: r.cfg.Conventions,
			Channel:     trigger.Channel,
			Thread:      trigger.Thread,
			Workdir:     r.cliWorkdir(trigger),
			Relay:       func(m bus.Message) { r.Handle(context.Background(), m) },

			Envelope:     tt.envelope,
			EnvelopeTask: tt.task,
			PlanUpdate:   tt.plan,
			MemberRoots:  tt.memberRoots,
			LessonRecord: tt.lesson,
			WorkBranch:   tt.workBranch,
			Access:       tt.access,
			Forge:        r.cfg.Forge,
			SetProps:     r.cfg.SetProps,
			SettingCheck: r.cfg.SettingCheck,
			OnPR:         r.prHook(e.m.ID, trigger),
		}.Handler()
	}
	// Third-party MCP servers (F-034 part C): dialed under the sandbox,
	// allowlist-gated, network deny-by-default.
	var bridge *mcpbridge.Bridge
	if wantBridge && r.cfg.MCPServers != nil {
		dialCtx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		bridge = mcpbridge.New(dialCtx, mcpbridge.Deps{
			Servers:    r.cfg.MCPServers,
			Agent:      e.m,
			Sandbox:    r.cfg.Sandbox,
			Approvals:  r.cfg.Approvals,
			Scopes:     r.agentScopes(e.m),
			PathPrefix: r.cfg.ToolBin,
			HomeDir:    r.cfg.MCPHome,
		})
		cancel()
		if len(bridge.Tools()) > 0 {
			if handler == nil {
				handler = bridge
			} else {
				handler = compositeHandler{dhi: handler, bridge: bridge}
			}
		}
	}
	closeBridge := func() {
		if bridge != nil {
			bridge.Close()
		}
	}
	if handler == nil || len(handler.Tools()) == 0 {
		closeBridge()
		return nil
	}
	endpoint, stop, err := mcp.ServeLoopback(handler)
	if err != nil {
		// Named degrade, never silent: the turn proceeds without served
		// tools and the thread sees why (F-011).
		closeBridge()
		r.notice(trigger, e.m.ID, "IDE tools unavailable this turn: "+err.Error())
		return nil
	}
	stopAll := func() {
		closeBridge()
		stop()
	}
	sess := &serveSession{endpoint: endpoint, stop: stopAll}
	// Dir-delivered adapters (antigravity) need a per-turn config
	// directory on argv; the adapter builds a mirror of its real global
	// dir so OAuth/state survive (ADR-0012 §4 read pass-through).
	if e.cli.MCPGeminiDir != nil {
		base := filepath.Join(r.cfg.WS.Root, ".dhi", "mcp")
		dir, cleanup, derr := e.cli.MCPGeminiDir(base, e.cli.MCPConfigFile(endpoint))
		if derr != nil {
			stopAll()
			r.notice(trigger, e.m.ID, "IDE tools unavailable this turn: "+derr.Error())
			return nil
		}
		sess.configPath = dir
		sess.geminiDir = dir
		sess.stop = func() {
			cleanup()
			stopAll()
		}
		return sess
	}
	// Project-file adapters (cursor) need the config inside the worktree
	// at a fixed relative path; write it there and git-exclude it so the
	// agent's own commits never pick it up.
	if e.cli.MCPProjectFile != "" && e.cli.MCPConfigFile != nil {
		dir := r.cliWorkdir(trigger)
		path := filepath.Join(dir, e.cli.MCPProjectFile)
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err == nil {
			if werr := os.WriteFile(path, []byte(e.cli.MCPConfigFile(endpoint)), 0o644); werr == nil {
				unexclude := excludeFromGit(dir, e.cli.MCPProjectFile)
				sess.configPath = path
				sess.stop = func() {
					_ = os.Remove(path)
					unexclude()
					stopAll()
				}
				return sess
			}
		}
		stopAll()
		r.notice(trigger, e.m.ID, "IDE tools unavailable this turn: cannot write "+e.cli.MCPProjectFile)
		return nil
	}
	// File-delivered adapters get a temp config; inline adapters
	// (codex) take the endpoint straight on argv.
	if e.cli.MCPConfigFile != nil {
		tmp, err := os.CreateTemp("", "dhi-mcp-*.json")
		if err != nil {
			stopAll()
			r.notice(trigger, e.m.ID, "IDE tools unavailable this turn: "+err.Error())
			return nil
		}
		if _, err := tmp.WriteString(e.cli.MCPConfigFile(endpoint)); err != nil {
			_ = tmp.Close()
			_ = os.Remove(tmp.Name())
			stopAll()
			return nil
		}
		_ = tmp.Close()
		path := tmp.Name()
		sess.configPath = path
		sess.stop = func() {
			_ = os.Remove(path)
			stopAll()
		}
	}
	return sess
}

// compositeHandler merges DHI's own tools with the bridged third-party
// MCP servers: listing both, routing by the mcp__ prefix.
type compositeHandler struct {
	dhi    mcp.Handler
	bridge mcp.Handler
}

func (c compositeHandler) ProtocolVersion() string { return mcp.ProtocolVersion }

func (c compositeHandler) ServerInfo() (string, string) { return "dhi", "1" }

func (c compositeHandler) Tools() []mcp.ToolInfo {
	var out []mcp.ToolInfo
	if c.dhi != nil {
		out = append(out, c.dhi.Tools()...)
	}
	if c.bridge != nil {
		out = append(out, c.bridge.Tools()...)
	}
	return out
}

func (c compositeHandler) CallTool(ctx context.Context, name string, args json.RawMessage) (string, bool, error) {
	if strings.HasPrefix(name, mcpbridge.ToolPrefix) && c.bridge != nil {
		return c.bridge.CallTool(ctx, name, args)
	}
	if c.dhi != nil {
		return c.dhi.CallTool(ctx, name, args)
	}
	return "unknown tool " + name, true, nil
}

// cliPrompt is the level-0 prompt (see cliPromptAt).
func (r *Runtime) cliPrompt(ctx context.Context, e *entry, trigger bus.Message, wfText string) (prompt, system string) {
	return r.cliPromptAt(ctx, e, trigger, wfText, 0, nil, false)
}

// cliPromptAt flattens the trigger, its task card and its conversation
// into a headless CLI prompt and assembles the system block. Order is
// stable → volatile (F-065): persona, grounding, standards, conventions
// and workflow change rarely and lead, so the CLI's prompt cache keeps
// its prefix; memory and KB hits change per turn and trail. level picks
// the compaction limits (0 normal, 1 after a context overflow). ctx
// drives the KB search (bounded retrieval, not part of the turn timeout).
//
// ep is the turn's episode (nil = none). resumed means the CLI continues
// its own session, which already holds the earlier conversation: only
// messages after ep.SeenID are replayed. A fresh spawn of an ongoing
// episode carries the episode summary instead.
func (r *Runtime) cliPromptAt(ctx context.Context, e *entry, trigger bus.Message, wfText string, level int, ep *episode.Episode, resumed bool) (prompt, system string) {
	lim := limitsAt(level)
	// F-027: the effective persona comes from the behaviour composer —
	// manifest system + role template + attached skills — with the
	// runtime-owned layers appended after it, exactly as the Settings
	// preview renders (system, then standards, then conventions).
	role, skills := behavior.Resolve(e.m, r.lib())
	persona := behavior.ResolvePersona(e.m, r.lib())
	wsName := filepath.Base(r.cfg.WS.Root)
	identity := behavior.Compose(behavior.Input{
		AgentID:        e.m.ID,
		Workspace:      wsName,
		ManifestSystem: e.m.System,
		Role:           role,
		Persona:        persona,
		Skills:         skills,
	})
	var members []string
	for _, m := range r.cfg.WS.Members() {
		members = append(members, m.Name)
	}
	grounding := "\n\nFiles are addressed as <member>/<rel-path>. Members: " + strings.Join(members, ", ")
	grounding += "\nThe reserved workspace dotdir is addressed as .dhi/<rel-path>; ideation artifacts belong under .dhi/sessions/<session>/<file>."
	grounding += "\nBefore changing code outside a task worktree, call work_branch: DHI makes the card, branch and worktree and continues you there. Never commit on the default branch."
	grounding += "\nIf the sandbox blocks you (\"Operation not permitted\"), call permission_request for exactly what you need; do not give up or work around it."
	// Stable sections first (they cache), volatile after (F-065); each
	// carries its priority for the prompt budget (F-078).
	sys := []section{
		{name: "identity", text: identity, prio: 1, floor: 1500},
	}
	if pb := r.projectBlock(); pb != "" {
		sys = append(sys, section{name: "project", text: "\n\n" + pb, prio: 1, floor: 800})
	}
	sys = append(sys, section{name: "grounding", text: grounding + "\n\n" + r.tuning().Principal.Voice(), prio: 0})
	if r.cfg.Standards {
		sys = append(sys, section{name: "standards", text: "\n\n" + standards.Resolve(r.cfg.WS.Root, e.m.ID, r.teamLookup()), prio: 2, floor: 600})
	}
	if r.cfg.Conventions != nil {
		if cv := r.cfg.Conventions(); cv != nil {
			sys = append(sys, section{name: "conventions", text: "\n\n" + cv.Guidance(), prio: 2, floor: 400})
		}
	}
	if wfText != "" {
		sys = append(sys, section{name: "workflow", text: "\n\n" + wfText, prio: 2, floor: 600})
	}
	sys = append(sys,
		section{name: "lessons", text: r.lessonsBlock(e.m.ID, r.lessonQuery(trigger), lim), prio: 4, floor: 300},
		section{name: "memory", text: r.memoryBlock(e.m.ID, lim), prio: 5, floor: 200},
		section{name: "forge", text: r.forgeHint(e.m.ID), prio: 6},
		section{name: "knowledge", text: r.knowledgeBlock(ctx, trigger.Text, lim), prio: 6},
	)
	if level > 0 {
		sys = append(sys, section{name: "compacted", prio: 0,
			text: "\n\n(Context was compacted after the previous attempt overflowed: older history, journal and knowledge were trimmed.)"})
	}

	text := strings.TrimSpace(stripMention(trigger.Text, e.m.ID))
	if text == "" {
		text = trigger.Text
	}
	pr := []section{
		// The human's ask is never trimmed: everything else makes room.
		{name: "trigger", text: text, prio: 0, floor: runeLen(text)},
		{name: "task", text: r.taskBlock(e.m.ID, trigger, lim), prio: 1, floor: 1500},
		{name: "handoff", text: r.handoffBrief(e.m.ID, trigger), prio: 2, floor: 600},
	}
	var seen int64
	if ep != nil && resumed {
		seen = ep.SeenID
	} else if ep != nil {
		pr = append(pr, section{name: "episode", text: ep.Render(), prio: 3, floor: 300})
	}
	// The history's header and brief of dropped messages stay whole; its
	// messages give way oldest first.
	histHead, histBody := r.historyBlock(e.m.ID, trigger, lim, seen)
	pr = append(pr,
		section{name: "history-head", text: histHead, prio: 3, floor: min(runeLen(histHead), 1400)},
		section{name: "history", text: histBody, prio: 3, floor: 800, tail: true})

	// One budget for both halves: the system part is fitted in what the
	// prompt part leaves, the prompt part in what the system part leaves.
	budget := int(float64(r.tuning().memoryTuning().PromptTokens*runesPerToken) * compactionFactor[min(max(level, 0), len(compactionFactor)-1)])
	all := fitSections(append(append([]section{}, sys...), pr...), budget)
	return joinSections(all[len(sys):]), joinSections(all[:len(sys)])
}

// memoryBlock renders the agent's persistent context (M14 P1): the
// journal tail and the notes file, within lim. Unavailable memory
// degrades with a named line inside the block — the agent knows its
// memory is dark, never silent.
func (r *Runtime) memoryBlock(agentID string, lim promptLimits) string {
	if r.cfg.Memory == nil {
		return ""
	}
	var b strings.Builder
	b.WriteString("\n\nYour memory (persistent across turns):")
	journal, err := r.cfg.Memory.Journal(agentID, lim.journal)
	if err != nil {
		b.WriteString("\n- journal unavailable: " + err.Error())
	} else if len(journal) > 0 {
		b.WriteString("\nRecent journal:")
		for _, en := range journal {
			b.WriteString("\n- [" + en.Kind + "] " + truncateRunes(en.Text, lim.journalRunes))
		}
	} else {
		b.WriteString("\n- (journal empty — record durable lessons as you learn them)")
	}
	notes, err := r.cfg.Memory.ReadNotes(agentID)
	if err != nil {
		b.WriteString("\n- notes unavailable: " + err.Error())
	} else if strings.TrimSpace(notes) != "" {
		// Head and tail (F-078): what was added last is visible too.
		b.WriteString("\nNotes:\n" + shrinkRunes(strings.TrimSpace(notes), lim.notes, false))
	}
	if digest, err := r.cfg.Memory.Digest(agentID); err == nil && strings.TrimSpace(digest) != "" && lim.journal > 0 {
		// The rolled-up journal: older memory in brief, newest last.
		b.WriteString("\nOlder journal (digest):\n" + shrinkRunes(strings.TrimSpace(digest), lim.notes/2, true))
	}
	return b.String()
}

// knowledgeBlock retrieves KB entries relevant to the trigger (M14 P1).
// Search failure degrades with a named line; no hits render nothing.
func (r *Runtime) knowledgeBlock(ctx context.Context, query string, lim promptLimits) string {
	if r.cfg.Knowledge == nil || strings.TrimSpace(query) == "" {
		return ""
	}
	hits, err := r.cfg.Knowledge.Search(ctx, query, lim.kbHits)
	if err != nil {
		return "\n\nKnowledge base unavailable: " + err.Error()
	}
	if len(hits) == 0 {
		return ""
	}
	var b strings.Builder
	b.WriteString("\n\nRelevant knowledge base entries:")
	for _, h := range hits {
		b.WriteString("\n- " + truncateRunes(h.Snippet, lim.kbRunes))
	}
	return b.String()
}

// cliWorkdir picks the run cwd: the task card's first changeset worktree
// when the trigger is bound to one, else the workspace root. CLI runs
// happen in-repo like every other DHI turn (ADR-0012 contract shared).
func (r *Runtime) cliWorkdir(trigger bus.Message) string {
	if t, ok := r.boundTask(trigger); ok && len(t.ChangeSets) > 0 {
		return filepath.Join(r.cfg.WS.Root, t.ChangeSets[0].Path)
	}
	return r.cfg.WS.Root
}

// boundTask is the card bound to the trigger's thread — matched on the
// thread root, so a top-level trigger bound to a card (an autopilot DM)
// finds it exactly as recordRun does.
func (r *Runtime) boundTask(trigger bus.Message) (tasks.Task, bool) {
	if r.cfg.Tasks == nil {
		return tasks.Task{}, false
	}
	return r.cfg.Tasks.FindByThread(trigger.Channel, bus.ThreadOf(trigger))
}

// cliEnv assembles the spawn environment: the hermetic base (toolchain
// PATH etc.) extended by the CLI's EXACT declared pass-through. Nothing
// else crosses (ADR-0012 §4) — an auditable, fixed set. TMPDIR and the
// CLI's own temp vars point at the agent's private dir inside the
// sandbox's writable roots (F-086): the host's /tmp and /var/folders
// are not writable there.
func (r *Runtime) cliEnv(c *clirun.CLI, agentID, mcpConfig string) []string {
	env := append([]string(nil), r.cfg.CLIEnv...)
	for _, k := range c.EnvPass {
		if v, ok := os.LookupEnv(k); ok {
			env = append(env, k+"="+v)
		}
	}
	if dir := r.agentTmp(agentID); dir != "" {
		env = setEnv(env, "TMPDIR="+dir)
		for _, k := range c.TempEnv {
			env = setEnv(env, k+"="+dir)
		}
	}
	// A per-turn MCP config must override any pass-through value of the
	// same key: the turn's loopback endpoint, never the user's.
	for _, kv := range c.MCPEnv(mcpConfig) {
		env = setEnv(env, kv)
	}
	return env
}

// agentTmp is the agent's private temp dir, <ws>/.dhi/tmp/<agent>
// (0700: claude refuses a temp base other users can read). "" when it
// cannot be made — the CLI then keeps the inherited TMPDIR and any
// sandbox denial surfaces as the run's own error.
func (r *Runtime) agentTmp(agentID string) string {
	if r.cfg.WS == nil || agentID == "" {
		return ""
	}
	dir := filepath.Join(r.cfg.WS.Root, workspace.DirTmp, agentID)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return ""
	}
	return dir
}

// setEnv replaces every KEY= entry with kv (last write wins, one entry).
func setEnv(env []string, kv string) []string {
	key := kv[:strings.IndexByte(kv, '=')+1]
	kept := env[:0]
	for _, e := range env {
		if !strings.HasPrefix(e, key) {
			kept = append(kept, e)
		}
	}
	return append(kept, kv)
}

// cliEvent forwards non-final transcript events into the trigger thread.
func (r *Runtime) cliEvent(agentID string, trigger bus.Message, ev clirun.StreamEvent) {
	switch ev.Kind {
	case clirun.EventProgress, clirun.EventCommand, clirun.EventError:
		_, _ = r.cfg.Bus.Post(bus.Message{
			Channel: trigger.Channel, Thread: trigger.Thread,
			Author: agentID, Text: ev.Detail, Kind: bus.KindStream,
		})
	}
}

// recordRun persists the run onto the bound task card. Best-effort: the
// transcript already lives on the bus, so a failure here loses only the
// card's history column. The trigger's own thread root is used so a
// top-level trigger (e.g. an autopilot DM, bus.ThreadOf = its own id)
// still matches a card bound to that message.
func (r *Runtime) recordRun(trigger bus.Message, run tasks.Run) {
	if r.cfg.Tasks == nil {
		return
	}
	t, ok := r.boundTask(trigger)
	if !ok {
		return
	}
	_ = r.cfg.Tasks.RecordRun(t.Slug, run)
}

func runID(t time.Time) string { return "run-" + t.Format("20060102-150405.000") }

func cliErrText(err error) string {
	var ee *exec.ExitError
	if errors.As(err, &ee) {
		return fmt.Sprintf("exit %d", ee.ExitCode())
	}
	return err.Error()
}

// exitCode extracts the CLI process exit code, 0 when the process
// finished cleanly or the wait error carries none (F-014 `exit` field).
func exitCode(err error) int {
	var ee *exec.ExitError
	if errors.As(err, &ee) {
		return ee.ExitCode()
	}
	return 0
}

func truncateRunes(s string, n int) string {
	r := []rune(s)
	if len(r) <= n {
		return s
	}
	return string(r[:n-1]) + "…"
}

// excludeFromGit appends a worktree-relative path to the repo's
// info/exclude (the worktree's gitdir) so the agent's commits never
// capture a per-turn file, returning a cleanup that removes the line.
// Best-effort: on any failure it returns a no-op rather than blocking
// the turn.
func excludeFromGit(worktree, rel string) func() {
	noop := func() {}
	gitDir := filepath.Join(worktree, ".git")
	if fi, err := os.Stat(gitDir); err == nil && !fi.IsDir() {
		b, err := os.ReadFile(gitDir)
		if err != nil {
			return noop
		}
		const prefix = "gitdir:"
		line := strings.TrimSpace(string(b))
		if !strings.HasPrefix(line, prefix) {
			return noop
		}
		gd := strings.TrimSpace(strings.TrimPrefix(line, prefix))
		if !filepath.IsAbs(gd) {
			gd = filepath.Join(worktree, gd)
		}
		gitDir = gd
	}
	ex := filepath.Join(gitDir, "info", "exclude")
	pattern := "/" + filepath.ToSlash(rel)
	_ = os.MkdirAll(filepath.Dir(ex), 0o755)
	if orig, err := os.ReadFile(ex); err == nil && strings.Contains(string(orig), pattern) {
		return noop
	}
	f, err := os.OpenFile(ex, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o644)
	if err != nil {
		return noop
	}
	_, _ = f.WriteString(pattern + "\n")
	_ = f.Close()
	return func() {
		cur, err := os.ReadFile(ex)
		if err != nil {
			return
		}
		var kept []string
		for _, l := range strings.Split(string(cur), "\n") {
			if strings.TrimSpace(l) != pattern {
				kept = append(kept, l)
			}
		}
		_ = os.WriteFile(ex, []byte(strings.Join(kept, "\n")), 0o644)
	}
}
