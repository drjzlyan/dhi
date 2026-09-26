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

	"github.com/drjzlyan/dhi/internal/agentkit/behavior"
	"github.com/drjzlyan/dhi/internal/agentkit/bus"
	"github.com/drjzlyan/dhi/internal/agentkit/clirun"
	"github.com/drjzlyan/dhi/internal/agentkit/dhitools"
	"github.com/drjzlyan/dhi/internal/agentkit/standards"
	"github.com/drjzlyan/dhi/internal/agentkit/toolbridge"
	"github.com/drjzlyan/dhi/internal/agentkit/tools"
	"github.com/drjzlyan/dhi/internal/mcp"
	"github.com/drjzlyan/dhi/internal/sandbox"
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
	// The KB retrieval rides the turn context but must not inherit a
	// timeout that would cut the run short: bounded by the caller's
	// context here, before the manifest timeout arms (the timeout
	// governs the CLI run, not context assembly).
	serve := r.serveTools(e, trigger)
	prompt, system := r.cliPrompt(ctx, e, trigger, serve != nil)
	workdir := r.cliWorkdir(trigger)
	mcpConfig, mcpEndpoint := "", ""
	if serve != nil {
		defer serve.stop()
		mcpConfig, mcpEndpoint = serve.configPath, serve.endpoint
	}

	if e.m.Timeout > 0 {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, e.m.Timeout)
		defer cancel()
	}

	var lastErr error
	for attempt := 0; ; attempt++ {
		run := r.cliSpawnOnce(ctx, e, trigger, prompt, system, workdir, attempt, mcpConfig, mcpEndpoint)
		r.recordRun(trigger, run)

		if run.Status == tasks.RunOK {
			if run.Summary != "" {
				_, _ = r.cfg.Bus.Post(bus.Message{
					Channel: trigger.Channel, Thread: trigger.Thread,
					Author: e.m.ID, Text: run.Summary,
				})
			}
			r.dispatchActions(ctx, e, trigger, run.Summary)
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
func (r *Runtime) cliSpawnOnce(ctx context.Context, e *entry, trigger bus.Message, prompt, system, workdir string, attempt int, mcpConfig, mcpEndpoint string) tasks.Run {
	started := time.Now().UTC()
	run := tasks.Run{
		ID: runID(started), Agent: e.m.ID,
		Runtime: "cli:" + e.m.Runtime, Model: e.m.Model, Attempt: attempt,
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
			return run
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
		Model: e.m.Model, Workdir: workdir, Stdin: stdin,
		MCPConfig: mcpConfig, MCPURL: mcpEndpoint,
	})...)
	wrapped, err := e.guard.Sandbox.Wrap(argv)
	if err != nil {
		run.Status = tasks.RunError
		run.Error = fmt.Sprintf("wrap: %v", err)
		return run
	}

	cmd := exec.CommandContext(ctx, wrapped[0], wrapped[1:]...)
	cmd.Dir = workdir
	cmd.Env = r.cliEnv(e.cli, mcpConfig)
	if stdin != "" {
		cmd.Stdin = strings.NewReader(stdin)
	}
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

// serveSession is one per-turn IDE-tools server (nil when the agent
// allowlist carries no served tools, or the adapter has no verified
// MCP wiring — the dhi-action fallback stays for those).
type serveSession struct {
	configPath string // temp MCP config file ("" for inline adapters)
	endpoint   string // the loopback URL the adapter must reach
	stop       func()
}

// serveTools starts the loopback IDE-tools endpoint for agents whose
// allowlist includes served tools on an MCP-capable adapter. The
// endpoint lives exactly as long as the turn (ADR-0017: no daemon);
// the temp config dies with it.
func (r *Runtime) serveTools(e *entry, trigger bus.Message) *serveSession {
	if r.cfg.WS == nil || e.cli == nil || !e.cli.MCPWired() {
		return nil
	}
	any := false
	for _, t := range e.m.Tools {
		if dhitools.Serves(t) {
			any = true
			break
		}
	}
	if !any {
		return nil
	}
	handler := dhitools.Deps{
		Agent:     e.m,
		Tasks:     r.cfg.Tasks,
		KB:        r.cfg.Knowledge,
		Memory:    r.cfg.Memory,
		Bus:       r.cfg.Bus,
		WS:        r.cfg.WS,
		Search:    r.cfg.Search,
		Approvals: r.cfg.Approvals,
		Git:       r.cfg.Git,
		Identity:  r.cfg.Identity,
		Sessions:  r.cfg.Sessions,
		Editor:    r.cfg.Editor,
		Scopes:    r.agentScopes(e.m),
		Channel:   trigger.Channel,
		Thread:    trigger.Thread,
		Workdir:   r.cliWorkdir(trigger),
	}.Handler()
	if len(handler.Tools()) == 0 {
		return nil
	}
	endpoint, stop, err := mcp.ServeLoopback(handler)
	if err != nil {
		// Named degrade, never silent: the turn proceeds on the
		// dhi-action fallback and the thread sees why (F-011).
		_, _ = r.cfg.Bus.Post(bus.Message{
			Channel: trigger.Channel, Thread: trigger.Thread,
			Author: e.m.ID,
			Text:   "IDE tools unavailable this turn: " + err.Error(),
		})
		return nil
	}
	sess := &serveSession{endpoint: endpoint, stop: stop}
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
					stop()
				}
				return sess
			}
		}
		stop()
		_, _ = r.cfg.Bus.Post(bus.Message{
			Channel: trigger.Channel, Thread: trigger.Thread,
			Author: e.m.ID,
			Text:   "IDE tools unavailable this turn: cannot write " + e.cli.MCPProjectFile,
		})
		return nil
	}
	// File-delivered adapters get a temp config; inline adapters
	// (codex) take the endpoint straight on argv.
	if e.cli.MCPConfigFile != nil {
		tmp, err := os.CreateTemp("", "dhi-mcp-*.json")
		if err != nil {
			stop()
			_, _ = r.cfg.Bus.Post(bus.Message{
				Channel: trigger.Channel, Thread: trigger.Thread,
				Author: e.m.ID,
				Text:   "IDE tools unavailable this turn: " + err.Error(),
			})
			return nil
		}
		if _, err := tmp.WriteString(e.cli.MCPConfigFile(endpoint)); err != nil {
			_ = tmp.Close()
			_ = os.Remove(tmp.Name())
			stop()
			return nil
		}
		_ = tmp.Close()
		path := tmp.Name()
		sess.configPath = path
		sess.stop = func() {
			_ = os.Remove(path)
			stop()
		}
	}
	return sess
}

// cliPrompt flattens the trigger + recent history into a headless CLI
// prompt and assembles the system block (grounding + memory + KB hits +
// standards), mirroring the in-house prompt() but as text a CLI accepts.
// ctx drives the KB search (bounded retrieval, not part of the turn
// timeout). mcp=true suppresses the dhi-action advertising — the MCP
// tools/list carries the contract for those adapters (F-028).
func (r *Runtime) cliPrompt(ctx context.Context, e *entry, trigger bus.Message, mcp bool) (prompt, system string) {
	// F-027: the effective persona comes from the behaviour composer —
	// manifest system + role template + attached skills — with the
	// runtime-owned layers (grounding, actions, memory, KB, standards)
	// appended after it, exactly as the Settings preview renders.
	role, skills := behavior.Resolve(e.m, r.lib())
	wsName := filepath.Base(r.cfg.WS.Root)
	system = behavior.Compose(behavior.Input{
		AgentID:        e.m.ID,
		Workspace:      wsName,
		ManifestSystem: e.m.System,
		Role:           role,
		Skills:         skills,
	})
	var members []string
	for _, m := range r.cfg.WS.Members() {
		members = append(members, m.Name)
	}
	grounding := "\n\nFiles are addressed as <member>/<rel-path>. Members: " + strings.Join(members, ", ")
	grounding += "\nThe reserved workspace dotdir is addressed as .dhi/<rel-path>; ideation artifacts belong under .dhi/sessions/<session>/<file>."
	system += grounding
	if !mcp {
		if actions := r.allowedActions(e); len(actions) > 0 {
			system += "\n\nDHI actions: end your reply with a fenced ```dhi-action block to request one. " +
				"Shape: {\"action\": \"<name>\", \"args\": {...}}. Valid names: " +
				strings.Join(actions, ", ") + ". Each runs after the human approves it; " +
				"the result arrives as a reply in this thread."
		}
	}
	system += r.memoryBlock(e.m.ID)
	system += r.knowledgeBlock(ctx, trigger.Text)
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

// memoryBlock renders the agent's persistent context (M14 P1): the
// journal tail and the notes file. Unavailable memory degrades with a
// named line inside the block — the agent knows its memory is dark,
// never silent.
func (r *Runtime) memoryBlock(agentID string) string {
	if r.cfg.Memory == nil {
		return ""
	}
	var b strings.Builder
	b.WriteString("\n\nYour memory (persistent across turns):")
	journal, err := r.cfg.Memory.Journal(agentID, 8)
	if err != nil {
		b.WriteString("\n- journal unavailable: " + err.Error())
	} else if len(journal) > 0 {
		b.WriteString("\nRecent journal:")
		for _, en := range journal {
			b.WriteString("\n- [" + en.Kind + "] " + truncateRunes(en.Text, 200))
		}
	} else {
		b.WriteString("\n- (journal empty — record durable lessons as you learn them)")
	}
	notes, err := r.cfg.Memory.ReadNotes(agentID)
	if err != nil {
		b.WriteString("\n- notes unavailable: " + err.Error())
	} else if strings.TrimSpace(notes) != "" {
		b.WriteString("\nNotes:\n" + truncateRunes(strings.TrimSpace(notes), 1500))
	}
	return b.String()
}

// knowledgeBlock retrieves KB entries relevant to the trigger (M14 P1).
// Search failure degrades with a named line; no hits render nothing.
func (r *Runtime) knowledgeBlock(ctx context.Context, query string) string {
	if r.cfg.Knowledge == nil || strings.TrimSpace(query) == "" {
		return ""
	}
	hits, err := r.cfg.Knowledge.Search(ctx, query, 3)
	if err != nil {
		return "\n\nKnowledge base unavailable: " + err.Error()
	}
	if len(hits) == 0 {
		return ""
	}
	var b strings.Builder
	b.WriteString("\n\nRelevant knowledge base entries:")
	for _, h := range hits {
		b.WriteString("\n- " + truncateRunes(h.Snippet, 300))
	}
	return b.String()
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
func (r *Runtime) cliEnv(c *clirun.CLI, mcpConfig string) []string {
	env := append([]string(nil), r.cfg.CLIEnv...)
	for _, k := range c.EnvPass {
		if v, ok := os.LookupEnv(k); ok {
			env = append(env, k+"="+v)
		}
	}
	// A per-turn MCP config must override any pass-through value of the
	// same key: the turn's loopback endpoint, never the user's.
	for _, kv := range c.MCPEnv(mcpConfig) {
		key := kv[:strings.IndexByte(kv, '=')]
		kept := env[:0]
		for _, e := range env {
			if !strings.HasPrefix(e, key+"=") {
				kept = append(kept, e)
			}
		}
		env = append(kept, kv)
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

// dispatchActions runs the F-020 toolbridge: dhi-action blocks in the
// agent's final message execute against the real stores (allowlist +
// approvals gated), and every outcome — result or named refusal — is
// posted to the trigger thread so the agent sees it on its next turn.
func (r *Runtime) dispatchActions(ctx context.Context, e *entry, trigger bus.Message, summary string) {
	reqs, parseErrs := toolbridge.ParseActions(summary)
	if len(reqs) == 0 && len(parseErrs) == 0 {
		return
	}
	b := r.bridge(e)
	post := func(text string) {
		_, _ = r.cfg.Bus.Post(bus.Message{
			Channel: trigger.Channel, Thread: trigger.Thread,
			Author: e.m.ID, Text: text,
		})
	}
	for _, perr := range parseErrs {
		post("dhi-action refused: " + perr.Error())
	}
	for _, req := range reqs {
		res, err := b.Dispatch(ctx, e.m.ID, req)
		if err != nil {
			post("dhi-action refused: " + err.Error())
			continue
		}
		post(res)
	}
}

// bridge wires the toolbridge seams from the runtime config: the
// manifest allowlist gates every action, approvals park mutating ops
// on the human's y/n seam, and the PR seam (when wired) opens PRs.
func (r *Runtime) bridge(e *entry) *toolbridge.Bridge {
	allow := map[string]bool{}
	for _, t := range e.m.Tools {
		allow[t] = true
	}
	var approvals *tools.Approvals
	if r.cfg.Approvals != nil {
		approvals = r.cfg.Approvals
	}
	b := &toolbridge.Bridge{
		Tasks:  r.cfg.Tasks,
		OpenPR: r.cfg.PR,
		Allow:  func(agentID, action string) bool { return allow[action] },
	}
	if approvals != nil {
		b.Approve = func(ctx context.Context, agentID, detail string) error {
			return approvals.Ask(ctx, agentID, sandbox.OpExec, detail,
				"agent-requested DHI action")
		}
	}
	return b
}

// allowedActions lists the bridge actions the manifest allows, in
// documentation order — the prompt-side contract (agents can only
// request what their tools allowlist).
func (r *Runtime) allowedActions(e *entry) []string {
	allow := map[string]bool{}
	for _, t := range e.m.Tools {
		allow[t] = true
	}
	var out []string
	for _, a := range toolbridge.All() {
		if allow[a] {
			out = append(out, a)
		}
	}
	return out
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
