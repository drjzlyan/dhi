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
	"time"

	"github.com/drjzlyan/dhi/internal/agentkit/bus"
	"github.com/drjzlyan/dhi/internal/agentkit/clirun"
	"github.com/drjzlyan/dhi/internal/agentkit/standards"
	"github.com/drjzlyan/dhi/internal/tasks"
)

// cliTurn executes one full turn of a CLI-runtime agent (F-013,
// ADR-0012): flat prompt → sandbox-wrapped headless spawn in the task
// worktree → streamed transcript (posted to the trigger thread + one
// persisted JSONL per attempt) → finalize → run record on the task card
// + final reply in the thread. A failed or timed-out attempt retries per
// the manifest's retries budget (step 7), backing off between spawns.
// The manifest's `runtime` key is the exec authorization; the OS sandbox
// — from the same Guard seam every exec uses — is the boundary.
func (r *Runtime) cliTurn(ctx context.Context, e *entry, trigger bus.Message) error {
	prompt, system := r.cliPrompt(e, trigger)
	workdir := r.cliWorkdir(trigger)

	if e.m.Timeout > 0 {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, e.m.Timeout)
		defer cancel()
	}

	var lastErr error
	for attempt := 0; ; attempt++ {
		run := r.cliSpawnOnce(ctx, e, trigger, prompt, system, workdir, attempt)
		r.recordRun(trigger, run)

		if run.Status == tasks.RunOK {
			if run.Summary != "" {
				_, _ = r.cfg.Bus.Post(bus.Message{
					Channel: trigger.Channel, Thread: trigger.Thread,
					Author: e.m.ID, Text: run.Summary,
				})
			}
			return nil
		}
		lastErr = fmt.Errorf("runtime: %s: run %s: %s", e.m.ID, run.ID, run.Error)
		if attempt >= e.m.Retries {
			return lastErr
		}
		_, _ = r.cfg.Bus.Post(bus.Message{
			Channel: trigger.Channel, Thread: trigger.Thread,
			Author: e.m.ID,
			Text:   fmt.Sprintf("retrying (attempt %d/%d) in %s…", attempt+1, e.m.Retries, cliRetryBackoff),
		})
		select {
		case <-time.After(cliRetryBackoff):
		case <-ctx.Done():
			return fmt.Errorf("runtime: %s: %w", e.m.ID, ctx.Err())
		}
	}
}

// cliRetryBackoff is the wait between retry spawns (F-013 step 7 says
// 30s in production; tests compress it to milliseconds via this var).
var cliRetryBackoff = 30 * time.Second

// cliSpawnOnce runs a single attempt: spawn the wrapped CLI, stream the
// transcript into the trigger thread, persist the event JSONL, finalize,
// and return the run record.
func (r *Runtime) cliSpawnOnce(ctx context.Context, e *entry, trigger bus.Message, prompt, system, workdir string, attempt int) tasks.Run {
	started := time.Now().UTC()
	run := tasks.Run{
		ID: runID(started), Agent: e.m.ID,
		Runtime: "cli:" + e.m.Runtime, Model: e.m.Model, Attempt: attempt,
		Started: started, Finished: time.Now().UTC(),
		TokensIn: -1, TokensOut: -1,
	}

	argv := e.cli.BuildArgs(clirun.RunInput{
		Prompt: prompt, System: system,
		Model: e.m.Model, Workdir: workdir,
	})
	wrapped, err := e.guard.Sandbox.Wrap(argv)
	if err != nil {
		run.Status = tasks.RunError
		run.Error = fmt.Sprintf("wrap: %v", err)
		return run
	}

	cmd := exec.CommandContext(ctx, e.cliPath, wrapped...)
	cmd.Dir = workdir
	cmd.Env = r.cliEnv(e.cli)
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		run.Status = tasks.RunError
		run.Error = fmt.Sprintf("stdout: %v", err)
		return run
	}
	if err := cmd.Start(); err != nil {
		run.Status = tasks.RunError
		run.Error = fmt.Sprintf("spawn %s: %v", e.cli.Bin, err)
		return run
	}

	// Stream the transcript into the trigger's thread as it happens, so
	// long CLI runs stay visible; the terminal line is kept for
	// Finalize. Events are also persisted (F-013 step 4) for the P2
	// run-replay surface.
	var final string
	var events []clirun.StreamEvent
	for ev := range e.cli.ParseStream(stdout) {
		events = append(events, ev)
		r.cliEvent(e.m.ID, trigger, ev)
		if ev.Kind == clirun.EventFinal {
			final = ev.Detail
		}
	}
	waitErr := cmd.Wait()
	run.Finished = time.Now().UTC()
	run.Exit = exitCode(waitErr)

	switch {
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
	case waitErr != nil:
		run.Status = tasks.RunError
		run.Error = cliErrText(waitErr)
	default:
		run.Status = tasks.RunError
		run.Error = "CLI stream ended without a final event"
	}

	run.Transcript = r.saveTranscript(events, run)
	return run
}

// saveTranscript persists one attempt's events as JSONL under
// .dhi/agents/<id>/runs/<run-id>-<attempt>.jsonl (F-013 step 4). It is
// best-effort: the thread already carries the live stream, so a failure
// here only loses the durable replay file.
func (r *Runtime) saveTranscript(events []clirun.StreamEvent, run tasks.Run) string {
	if len(events) == 0 {
		return ""
	}
	dir := filepath.Join(r.cfg.WS.Root, ".dhi", "agents", run.Agent, "runs")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return ""
	}
	p := filepath.Join(dir, fmt.Sprintf("%s-%d.jsonl", run.ID, run.Attempt))
	f, err := os.Create(p)
	if err != nil {
		return ""
	}
	defer f.Close()
	enc := json.NewEncoder(f)
	for _, ev := range events {
		if ev.Detail == "" {
			continue
		}
		if err := enc.Encode(struct {
			Kind   string `json:"kind"`
			Detail string `json:"detail"`
		}{ev.Kind, ev.Detail}); err != nil {
			return ""
		}
	}
	return p
}

// cliPrompt flattens the trigger + recent history into a headless CLI
// prompt and assembles the system block (grounding + standards), mirroring
// the in-house prompt() but as text a CLI accepts.
func (r *Runtime) cliPrompt(e *entry, trigger bus.Message) (prompt, system string) {
	system = strings.TrimSpace(e.m.System)
	var members []string
	for _, m := range r.cfg.WS.Members() {
		members = append(members, m.Name)
	}
	grounding := "\n\nFiles are addressed as <member>/<rel-path>. Members: " + strings.Join(members, ", ")
	grounding += "\nThe reserved workspace dotdir is addressed as .dhi/<rel-path>; ideation artifacts belong under .dhi/sessions/<session>/<file>."
	system += grounding
	if r.cfg.Standards {
		system += "\n\n" + standards.Resolve(r.cfg.WS.Root, e.m.ID, r.teamLookup())
	}

	var b strings.Builder
	text := strings.TrimSpace(stripMention(trigger.Text, e.m.ID))
	if text == "" {
		text = trigger.Text
	}
	b.WriteString(text)

	history := r.cfg.Bus.History(trigger.Channel, 0)
	if n := len(history); n > historyWindow {
		history = history[n-historyWindow:]
	}
	var earlier []string
	for _, m := range history {
		if m.ID == trigger.ID || m.Author == e.m.ID {
			continue // the trigger itself, and our own drafts, add no context
		}
		prefix := m.Author
		if m.Author == "you" {
			prefix = "you (the human)"
		}
		earlier = append(earlier, prefix+": "+truncateRunes(m.Text, 500))
	}
	if len(earlier) > 0 {
		b.WriteString("\n\nEarlier in this thread:\n- " + strings.Join(earlier, "\n- "))
	}
	return b.String(), system
}

// cliWorkdir picks the run cwd: the task card's first changeset worktree
// when the trigger is bound to one, else the workspace root. CLI runs
// happen in-repo like every other DHI turn (ADR-0012 contract shared).
func (r *Runtime) cliWorkdir(trigger bus.Message) string {
	if r.cfg.Tasks != nil {
		if t, ok := r.cfg.Tasks.FindByThread(trigger.Channel, trigger.Thread); ok && len(t.ChangeSets) > 0 {
			return filepath.Join(r.cfg.WS.Root, t.ChangeSets[0].Path)
		}
	}
	return r.cfg.WS.Root
}

// cliEnv assembles the spawn environment: the hermetic base (toolchain
// PATH etc.) extended by the CLI's EXACT declared pass-through. Nothing
// else crosses (ADR-0012 §4) — an auditable, fixed set.
func (r *Runtime) cliEnv(c *clirun.CLI) []string {
	env := append([]string(nil), r.cfg.CLIEnv...)
	for _, k := range c.EnvPass {
		if v, ok := os.LookupEnv(k); ok {
			env = append(env, k+"="+v)
		}
	}
	return env
}

// cliEvent forwards non-final transcript events into the trigger thread.
func (r *Runtime) cliEvent(agentID string, trigger bus.Message, ev clirun.StreamEvent) {
	switch ev.Kind {
	case clirun.EventProgress, clirun.EventCommand, clirun.EventError:
		_, _ = r.cfg.Bus.Post(bus.Message{
			Channel: trigger.Channel, Thread: trigger.Thread,
			Author: agentID, Text: ev.Detail,
		})
	}
}

// recordRun persists the run onto the bound task card. Best-effort: the
// transcript already lives on the bus, so a failure here loses only the
// card's history column.
func (r *Runtime) recordRun(trigger bus.Message, run tasks.Run) {
	if r.cfg.Tasks == nil {
		return
	}
	t, ok := r.cfg.Tasks.FindByThread(trigger.Channel, trigger.Thread)
	if !ok {
		return
	}
	_ = r.cfg.Tasks.RecordRun(t.Slug, run)
}

func runID(t time.Time) string { return "run-" + t.Format("20060102-150405.000") }

func cliErrText(err error) string {
	var ee *exec.ExitError
	if errors.As(err, &ee) {
		return fmt.Sprintf("exit %d", ee.ProcessState.ExitCode())
	}
	return err.Error()
}

// exitCode extracts the CLI process exit code, 0 when the process
// finished cleanly or the wait error carries none (F-014 `exit` field).
func exitCode(err error) int {
	var ee *exec.ExitError
	if errors.As(err, &ee) {
		return ee.ProcessState.ExitCode()
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
