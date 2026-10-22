package evals

import (
	"context"
	"fmt"
	"github.com/drjzlyan/dhi/internal/agentkit/library"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync/atomic"
	"time"

	"github.com/drjzlyan/dhi/internal/agentkit/budget"
	"github.com/drjzlyan/dhi/internal/agentkit/bus"
	"github.com/drjzlyan/dhi/internal/agentkit/clirun"
	"github.com/drjzlyan/dhi/internal/agentkit/episode"
	"github.com/drjzlyan/dhi/internal/agentkit/knowledge"
	"github.com/drjzlyan/dhi/internal/agentkit/manifest"
	"github.com/drjzlyan/dhi/internal/agentkit/memory"
	agentruntime "github.com/drjzlyan/dhi/internal/agentkit/runtime"
	"github.com/drjzlyan/dhi/internal/agentkit/tools"
	"github.com/drjzlyan/dhi/internal/sandbox"
	"github.com/drjzlyan/dhi/internal/tasks"
	"github.com/drjzlyan/dhi/internal/workspace"
)

// Config is what both arms share, so the comparison is like for like:
// the same CLI, model, environment and (lack of) OS sandbox.
type Config struct {
	CLI     string                       // registry name, e.g. "claude"
	Model   string                       // "" = the CLI's default
	Look    func(string) (string, error) // CLI binary lookup (exec.LookPath live)
	Env     []string                     // base env; each CLI's pass-through is added
	Timeout time.Duration                // per run; 0 = none
	Retries int                          // DHI arm retries (the raw CLI has none)
	Now     func() time.Time             // result stamp; nil = time.Now
}

func (c Config) now() time.Time {
	if c.Now != nil {
		return c.Now()
	}
	return time.Now()
}

func (c Config) cli() (*clirun.CLI, string, error) {
	reg := clirun.NewRegistry(c.Look)
	cli, ok := reg.Get(c.CLI)
	if !ok {
		return nil, "", fmt.Errorf("evals: unknown CLI %q (valid: %s)", c.CLI, reg.ValidRuntimeNames())
	}
	path, err := reg.Path(c.CLI)
	if err != nil {
		return nil, "", fmt.Errorf("evals: %s: %w", c.CLI, err)
	}
	return cli, path, nil
}

// env is the base env plus the CLI's declared pass-through — the same
// set the runtime hands an agent spawn (ADR-0012 §4).
func (c Config) env(cli *clirun.CLI) []string {
	env := append([]string(nil), c.Env...)
	for _, k := range cli.EnvPass {
		if v, ok := os.LookupEnv(k); ok {
			env = append(env, k+"="+v)
		}
	}
	return env
}

func (c Config) ctx() (context.Context, context.CancelFunc) {
	if c.Timeout > 0 {
		return context.WithTimeout(context.Background(), c.Timeout)
	}
	return context.WithCancel(context.Background())
}

// RunRaw runs the task through the bare CLI headless in a fresh copy:
// the prompt alone, no DHI system block, tools, memory or retry. Each
// follow-up resumes the CLI's own session where the adapter supports it
// — what a person driving the CLI by hand would do.
func RunRaw(c Config, t Task, root string) Result {
	r := Result{At: c.now(), Task: t.Name, Arm: "raw", CLI: c.CLI, Model: c.Model, TokensIn: -1, TokensOut: -1}
	cli, path, err := c.cli()
	if err != nil {
		r.Err = err.Error()
		return r
	}
	if err := t.Stage(root); err != nil {
		r.Err = "stage: " + err.Error()
		return r
	}
	ctx, cancel := c.ctx()
	defer cancel()
	start := time.Now()
	session := ""
	for _, prompt := range append([]string{t.Prompt}, t.Followups...) {
		in := clirun.RunInput{Prompt: prompt, Model: c.Model, Workdir: root}
		if cli.ResumeOK {
			in.Resume = session
		}
		u, err := rawSpawn(ctx, cli, path, c.env(cli), root, in)
		r.Attempts++
		if err != nil {
			r.Err = err.Error()
			break
		}
		r.add(u)
		session = u.SessionID
	}
	r.WallMs = time.Since(start).Milliseconds()
	return score(t, root, r)
}

// rawSpawn runs one headless CLI invocation and returns its usage.
func rawSpawn(ctx context.Context, cli *clirun.CLI, path string, env []string, dir string, in clirun.RunInput) (clirun.Usage, error) {
	cmd := exec.CommandContext(ctx, path, cli.BuildArgs(in)...)
	cmd.Dir = dir
	cmd.Env = env
	cmd.WaitDelay = 2 * time.Second // see runtime.cliSpawnOnce
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return clirun.Usage{}, err
	}
	if err := cmd.Start(); err != nil {
		return clirun.Usage{}, err
	}
	var final string
	for ev := range cli.ParseStream(stdout) {
		if ev.Kind == clirun.EventFinal {
			final = ev.Detail
		}
	}
	waitErr := cmd.Wait()
	switch {
	case final != "":
		_, u, ferr := cli.Finalize(final)
		return u, ferr
	case waitErr != nil:
		return clirun.Usage{}, waitErr
	}
	return clirun.Usage{}, fmt.Errorf("CLI stream ended without a final event")
}

// add folds one spawn's usage into the result.
func (r *Result) add(u clirun.Usage) {
	if u.TokensIn >= 0 {
		r.TokensIn = max(r.TokensIn, 0) + u.TokensIn
	}
	if u.TokensOut >= 0 {
		r.TokensOut = max(r.TokensOut, 0) + u.TokensOut
	}
	r.CacheRead += u.CacheRead
	r.CacheWrite += u.CacheWrite
	r.CostUSD += u.CostUSD
	r.HasCost = r.HasCost || u.HasCost
}

// evalAgent is the DHI arm's roster agent: a generalist with the IDE's
// file and memory tools.
const evalAgent = "evaluator"

func evalManifest(cli, model string) string {
	if model == "" {
		model = manifest.ModelDefault
	}
	return `schema = 1
name = "Evaluator"
model = ` + strconv.Quote(model) + `
runtime = ` + strconv.Quote(cli) + `
system = "You are a careful senior engineer. Complete the task fully, then reply with a one-paragraph summary."
tools = ["read", "list", "glob", "write", "patch", "workspace_search", "memory_append", "kb_search"]
policy_json = """{"rules":[{"op":"read","path":"**","effect":"allow"},{"op":"write","path":"**","effect":"allow"}]}"""
`
}

// RunDHI runs the task through DHI's runtime with the full harness:
// composed system block, standards, workflow, memory, KB, a bound task
// card, served IDE tools and failure-aware retry. Approvals are allowed
// automatically and counted (Asks) — the autonomy an unattended task
// needs, measured.
func RunDHI(c Config, t Task, root string) Result {
	r := Result{At: c.now(), Task: t.Name, Arm: "dhi", CLI: c.CLI, Model: c.Model, TokensIn: -1, TokensOut: -1}
	fail := func(stage string, err error) Result {
		r.Err = stage + ": " + err.Error()
		return r
	}
	if err := t.Stage(root); err != nil {
		return fail("stage", err)
	}
	if err := workspace.Create(root, Member); err != nil {
		return fail("workspace", err)
	}
	if err := setMemberVerify(root, t.Verify); err != nil {
		return fail("workspace", err)
	}
	ws, err := workspace.Load(root)
	if err != nil {
		return fail("workspace", err)
	}
	b, err := bus.Open(ws)
	if err != nil {
		return fail("bus", err)
	}
	st, err := tasks.Open(ws)
	if err != nil {
		return fail("tasks", err)
	}
	st.SetAttach(gitWorktreeAttach(ws), nil)
	kb, err := knowledge.Open(ws, knowledge.Review, nil)
	if err != nil {
		return fail("knowledge", err)
	}
	m, err := manifest.Parse(evalAgent, []byte(evalManifest(c.CLI, c.Model)))
	if err != nil {
		return fail("manifest", err)
	}
	m.Retries = c.Retries
	// DHI_EVAL_SKILL assigns one built-in skill (ADR-0043), to check the
	// agent loads it on demand from the catalog; SkillUse reports it.
	skill := os.Getenv("DHI_EVAL_SKILL")
	if skill != "" {
		dir := filepath.Join(root, ".dhi", "builtin-skills")
		if err := library.MaterializeBuiltins(dir); err != nil {
			return fail("skills", err)
		}
		library.BuiltinSkillsDir = dir
		defer func() { library.BuiltinSkillsDir = "" }()
		m.Skills = []string{skill}
	}

	var asks atomic.Int32
	ap := tools.NewApprovals()
	ap.OnRequest = func(a *tools.Approval) {
		asks.Add(1)
		go ap.Resolve(a.ID, true)
	}
	rt, err := agentruntime.New(agentruntime.Config{
		WS: ws, Bus: b, Approvals: ap,
		CLIs:      clirun.NewRegistry(c.Look),
		CLIEnv:    c.Env,
		Sandbox:   sandbox.Noop{},
		Tasks:     st,
		Standards: true,
		Workflows: true,
		Memory:    memory.Open(ws),
		Knowledge: kb,
		Episodes:  episode.Open(ws.Root),
		Spend:     budget.Open(ws.Root),
		// The app's goal-loop seams (F-067): DHI provisions the task
		// worktree and verifies the work itself.
		Run:       execRunner{env: c.Env},
		Tuning:    func() agentruntime.Tuning { return evalTuning(t) },
		Provision: func(slug, member string) error { return st.Attach(slug, member, "task/"+slug, "") },
	}, []*manifest.Agent{m})
	if err != nil {
		return fail("runtime", err)
	}

	trig, err := b.Post(bus.Message{Channel: "#general", Author: bus.Human, Text: "@" + evalAgent + " " + t.Prompt})
	if err != nil {
		return fail("post", err)
	}
	if err := st.Create("eval-"+t.Name, firstLine(t.Prompt), evalAgent, ""); err != nil {
		return fail("task", err)
	}
	if err := st.BindThread("eval-"+t.Name, trig.Channel, trig.ID); err != nil {
		return fail("task", err)
	}

	ctx, cancel := c.ctx()
	defer cancel()
	start := time.Now()
	turnErr := rt.Turn(ctx, evalAgent, trig)
	for _, f := range t.Followups {
		if turnErr != nil {
			break
		}
		next, err := b.Post(bus.Message{Channel: trig.Channel, Thread: trig.ID, Author: bus.Human, Text: "@" + evalAgent + " " + f})
		if err != nil {
			return fail("post", err)
		}
		turnErr = rt.Turn(ctx, evalAgent, next)
	}
	r.WallMs = time.Since(start).Milliseconds()
	if turnErr != nil {
		r.Err = turnErr.Error()
	}
	r.Asks = int(asks.Load())
	memberDir := filepath.Join(root, Member)
	if card, ok := st.Get("eval-" + t.Name); ok {
		if len(card.ChangeSets) > 0 {
			memberDir = filepath.Join(root, card.ChangeSets[0].Path) // the work is on the task branch
		}
		r.Attempts = len(card.Runs)
		rl := tasks.RollupRuns(card.Runs)
		if rl.TokRuns > 0 {
			r.TokensIn, r.TokensOut = rl.TokensIn, rl.TokensOut
		}
		r.CacheRead, r.CacheWrite = rl.CacheRead, rl.CacheWrite
		r.CostUSD, r.HasCost = rl.CostSum, rl.Costed > 0
	}
	if skill != "" {
		r.SkillUse = skillUse(root, skill)
	}
	if ep, ok, err := episode.Open(root).Get(evalAgent, episode.Key("eval-"+t.Name, "", 0)); err == nil && ok {
		r.VerifyRounds, r.Verify = ep.Iterations, ep.Verify
	}
	return scoreIn(t, root, memberDir, r)
}

// evalTuning is the runtime's defaults with the fixture's round cap.
func evalTuning(t Task) agentruntime.Tuning {
	tu := agentruntime.Tuning{Resume: true, Autonomy: "envelope", MaxIterations: 3, Reflect: true}
	if t.MaxIterations > 0 {
		tu.MaxIterations = t.MaxIterations
	}
	return tu
}

// setMemberVerify writes the fixture's verify override onto the member
// in workspace.toml (F-076's per-member `verify`).
func setMemberVerify(root string, argv []string) error {
	if len(argv) == 0 {
		return nil
	}
	p := filepath.Join(root, workspace.ConfigFile)
	b, err := os.ReadFile(p)
	if err != nil {
		return err
	}
	q := make([]string, len(argv))
	for i, a := range argv {
		q[i] = strconv.Quote(a)
	}
	line := "verify = [" + strings.Join(q, ", ") + "]\n"
	s := string(b)
	head := "[members." + Member + "]\n"
	i := strings.Index(s, head)
	if i < 0 {
		return fmt.Errorf("evals: no [members.%s] in %s", Member, p)
	}
	end := i + len(head)
	if j := strings.Index(s[end:], "\n"); j >= 0 {
		end += j + 1 // after the path line
	}
	return os.WriteFile(p, []byte(s[:end]+line+s[end:]), 0o644)
}

// execRunner is the eval's stand-in for the app's sandboxed `run` seam:
// the command in dir, with the arm's env. Dev-only harness code.
type execRunner struct{ env []string }

func (e execRunner) Run(ctx context.Context, dir string, argv []string, _ bool) (string, error) {
	cmd := exec.CommandContext(ctx, argv[0], argv[1:]...)
	cmd.Dir = dir
	cmd.Env = e.env
	cmd.WaitDelay = 2 * time.Second
	out, err := cmd.CombinedOutput()
	return string(out), err
}

// gitWorktreeAttach mirrors the app's task worktree seam with git.
func gitWorktreeAttach(ws *workspace.Workspace) tasks.AttachFn {
	return func(slug, member, branch, startpoint string) (string, error) {
		mem, ok := ws.Member(member)
		if !ok {
			return "", fmt.Errorf("unknown member %q", member)
		}
		rel := filepath.Join(".dhi", "worktrees", slug, member)
		args := []string{"-C", mem.Path, "worktree", "add", "-b", branch, filepath.Join(ws.Root, rel)}
		if startpoint != "" {
			args = append(args, startpoint)
		}
		if out, err := exec.Command("git", args...).CombinedOutput(); err != nil {
			return "", fmt.Errorf("git worktree add: %v: %s", err, strings.TrimSpace(string(out)))
		}
		return rel, nil
	}
}

func firstLine(s string) string {
	s, _, _ = strings.Cut(strings.TrimSpace(s), "\n")
	if len(s) > 60 {
		s = s[:59] + "…"
	}
	return s
}

// ArmOrder alternates which arm runs first on successive runs, so the
// CLI's own warm prompt cache (left by whichever arm ran just before)
// does not consistently favour one side.
func ArmOrder(turn int) []func(Config, Task, string) Result {
	if turn%2 == 0 {
		return []func(Config, Task, string) Result{RunDHI, RunRaw}
	}
	return []func(Config, Task, string) Result{RunRaw, RunDHI}
}

// skillUse reports how the agent took up its skill: "read" (it read the
// SKILL.md), "activated" (skill_activate), or "unused". It scans what
// DHI recorded of the run (transcripts, traces) for either.
func skillUse(root, skill string) string {
	read, activated := false, false
	_ = filepath.WalkDir(filepath.Join(root, ".dhi"), func(p string, d os.DirEntry, err error) error {
		if err != nil || d.IsDir() || strings.Contains(p, "builtin-skills") {
			return nil
		}
		b, err := os.ReadFile(p)
		if err != nil {
			return nil
		}
		text := string(b)
		if strings.Contains(text, skill+"/SKILL.md") {
			read = true
		}
		if strings.Contains(text, "skill_activate") && strings.Contains(text, skill) {
			activated = true
		}
		return nil
	})
	switch {
	case read:
		return "read"
	case activated:
		return "activated"
	}
	return "unused"
}
