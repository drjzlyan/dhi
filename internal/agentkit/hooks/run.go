package hooks

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os/exec"
	"strings"
	"sync"
	"time"

	"github.com/drjzlyan/dhi/internal/agentkit/scopes"
	"github.com/drjzlyan/dhi/internal/agentkit/untrusted"
	"github.com/drjzlyan/dhi/internal/sandbox"
)

// NativePrefix is how a CLI spells DHI's own served tools in its native
// hook matchers (claude: mcp__dhi__run).
const NativePrefix = "mcp__dhi__"

// Named errors (ADR-0011): each failure says what happened and to which hook.
var (
	ErrNoExec  = errors.New("this agent has no exec scope")
	ErrTimeout = errors.New("timed out")
)

// maxOutput caps what one hook may hand back.
const maxOutput = 64 << 10

// Input is the event a hook receives as JSON on stdin.
type Input struct {
	Event        Event
	Tool         string          // DHI tool name (tool events)
	ToolInput    json.RawMessage // the call's arguments (tool events)
	ToolResponse string          // PostToolUse: the result text
	Prompt       string          // UserPromptSubmit: the human's message; Stop: the outcome
	Source       string          // SessionStart: "startup"
}

// JSON renders the vendor-shaped event: hook_event_name, session_id, cwd,
// and per event tool_name (the vendor's name for the tool; the DHI name
// rides beside it), tool_input, tool_response, prompt, source.
func (in Input) JSON(session, cwd string) []byte {
	m := map[string]any{
		"hook_event_name": string(in.Event),
		"session_id":      session,
		"cwd":             cwd,
	}
	if in.Event.IsTool() {
		m["tool_name"] = VendorName(in.Tool)
		m["dhi_tool_name"] = in.Tool
		m["tool_input"] = vendorInput(in.Tool, in.ToolInput)
		if in.Event == PostToolUse {
			m["tool_response"] = in.ToolResponse
		}
	}
	switch in.Event {
	case UserPromptSubmit:
		m["prompt"] = in.Prompt
	case Stop:
		m["stop_hook_active"] = false
		m["outcome"] = in.Prompt
	case SessionStart:
		m["source"] = in.Source
	}
	b, _ := json.Marshal(m)
	return b
}

// vendorInput adds the vendor's field names beside DHI's own, so a script
// written for Bash's "command" or Write's "file_path" finds them.
func vendorInput(tool string, raw json.RawMessage) any {
	var m map[string]any
	if json.Unmarshal(raw, &m) != nil || m == nil {
		return json.RawMessage(`{}`)
	}
	if p, ok := m["path"].(string); ok {
		if _, has := m["file_path"]; !has {
			m["file_path"] = p
		}
	}
	if tool == "run" {
		if prog, ok := m["program"].(string); ok {
			parts := []string{prog}
			if args, ok := m["args"].([]any); ok {
				for _, a := range args {
					if s, ok := a.(string); ok {
						parts = append(parts, s)
					}
				}
			}
			m["command"] = strings.Join(parts, " ")
		}
	}
	return m
}

// ExecRequest is one hook process to run.
type ExecRequest struct {
	Dir          string
	Argv         []string
	Stdin        []byte
	Env          []string
	AllowNetwork bool
}

// ExecResult is what the process did. Exit is -1 when it never ran.
type ExecResult struct {
	Stdout, Stderr string
	Exit           int
}

// ExecFunc runs a request. The deadline is on ctx.
type ExecFunc func(ctx context.Context, req ExecRequest) (ExecResult, error)

// SandboxExec runs hooks inside the OS sandbox sb, the same wrapping the
// `run` tool uses. A sandbox that cannot deny the network refuses a
// request that must not have it (no silent widening).
func SandboxExec(sb sandbox.Sandbox) ExecFunc {
	return func(ctx context.Context, req ExecRequest) (ExecResult, error) {
		argv := req.Argv
		var err error
		if np, ok := sb.(sandbox.NetworkPolicy); ok {
			argv, err = np.WrapNetwork(argv, req.AllowNetwork)
		} else if !req.AllowNetwork {
			err = errors.New("network denied by scope but no OS sandbox can enforce it")
		} else {
			argv, err = sb.Wrap(argv)
		}
		if err != nil {
			return ExecResult{Exit: -1}, err
		}
		cmd := exec.CommandContext(ctx, argv[0], argv[1:]...)
		cmd.Dir = req.Dir
		cmd.Env = req.Env
		cmd.Stdin = bytes.NewReader(req.Stdin)
		var out, errb limitedBuffer
		cmd.Stdout, cmd.Stderr = &out, &errb
		cmd.WaitDelay = 500 * time.Millisecond
		runErr := cmd.Run()
		res := ExecResult{Stdout: out.String(), Stderr: errb.String()}
		var ee *exec.ExitError
		switch {
		case runErr == nil:
		case errors.As(runErr, &ee):
			res.Exit = ee.ExitCode()
			if ctx.Err() != nil {
				return res, ctx.Err()
			}
			return res, nil
		default:
			res.Exit = -1
			if ctx.Err() != nil {
				return res, ctx.Err()
			}
			return res, runErr
		}
		return res, nil
	}
}

type limitedBuffer struct{ bytes.Buffer }

func (b *limitedBuffer) Write(p []byte) (int, error) {
	if room := maxOutput - b.Len(); room > 0 {
		b.Buffer.Write(p[:min(len(p), room)])
	}
	return len(p), nil
}

// Kind classifies a record.
type Kind string

// Record kinds.
const (
	Allowed Kind = "allow"
	Denied  Kind = "deny"
	Failed  Kind = "error"
	Skipped Kind = "skip"
)

// Record is one line for the run transcript.
type Record struct {
	Plugin  string
	Event   Event
	Tool    string
	Command string
	Kind    Kind
	Detail  string
}

// String renders the transcript line.
func (r Record) String() string {
	what := string(r.Event)
	if r.Tool != "" {
		what += " " + r.Tool
	}
	s := fmt.Sprintf("hook %s [%s] %s", r.Plugin, what, r.Kind)
	if r.Detail != "" {
		s += ": " + r.Detail
	}
	return s
}

// Result is what firing an event produced.
type Result struct {
	Blocked bool
	By      string // the plugin that blocked
	Reason  string // its stderr, fenced
	Context string // stdout meant for the prompt, fenced
	Records []Record
}

// Runner fires one turn's hooks. Zero Plugins fires nothing.
type Runner struct {
	Plugins []Plugin
	Scopes  scopes.Set
	Exec    ExecFunc
	Dir     string   // working directory for the commands
	Env     []string // the environment they get (the user's, like `run`)
	Session string
	// Native is true when the CLI was handed the tool hooks through its own
	// per-run hook input: it fires them for its native tools, and DHI skips
	// any hook the CLI would also match on DHI's served tools, so none
	// fires twice. The boundary events then stay with DHI.
	Native bool
	// Record receives every record as it happens (the run transcript).
	Record func(Record)

	mu      sync.Mutex
	skipped map[string]bool
}

// Active reports whether any hook could fire for the event.
func (r *Runner) Active(ev Event) bool {
	if r == nil {
		return false
	}
	for _, p := range r.Plugins {
		for _, h := range p.Set.Hooks {
			if h.Event == ev {
				return true
			}
		}
	}
	return false
}

// Fire runs every matching hook in plugin order. A deny on Pre or
// UserPromptSubmit stops the rest; otherwise all matching hooks run.
func (r *Runner) Fire(ctx context.Context, in Input) Result {
	var res Result
	if r == nil {
		return res
	}
	emit := func(rec Record) {
		res.Records = append(res.Records, rec)
		if r.Record != nil {
			r.mu.Lock()
			r.Record(rec)
			r.mu.Unlock()
		}
	}
	type match struct {
		plugin Plugin
		hook   Hook
	}
	var todo []match
	for _, p := range r.Plugins {
		for _, h := range p.Set.Hooks {
			if h.Event != in.Event {
				continue
			}
			if in.Event.IsTool() {
				if !MatchTool(h.Matcher, in.Tool) {
					continue
				}
				if r.Native && NativeMatch(h.Matcher, NativePrefix+in.Tool) {
					continue // the CLI fires it for this tool already
				}
			}
			todo = append(todo, match{p, h})
		}
	}
	if len(todo) == 0 {
		return res
	}
	if r.Scopes.EffectFor(scopes.Exec) == scopes.Deny {
		// Said once per plugin and event per turn, not on every tool call.
		key := todo[0].plugin.Name + "/" + string(in.Event)
		r.mu.Lock()
		seen := r.skipped[key]
		if r.skipped == nil {
			r.skipped = map[string]bool{}
		}
		r.skipped[key] = true
		r.mu.Unlock()
		if !seen {
			emit(Record{Plugin: todo[0].plugin.Name, Event: in.Event, Kind: Skipped,
				Detail: fmt.Sprintf("hooks not run: %v (plugin hooks need the exec scope)", ErrNoExec)})
		}
		return res
	}
	stdin := in.JSON(r.Session, r.Dir)
	allowNet := r.Scopes.EffectFor(scopes.Network) == scopes.Auto
	for _, m := range todo {
		if ctx.Err() != nil {
			break
		}
		rec := Record{Plugin: m.plugin.Name, Event: in.Event, Tool: in.Tool, Command: m.hook.Command}
		hctx, cancel := context.WithTimeout(ctx, m.hook.Timeout)
		exe := r.Exec
		if exe == nil {
			exe = func(context.Context, ExecRequest) (ExecResult, error) {
				return ExecResult{Exit: -1}, errors.New("no sandboxed executor wired")
			}
		}
		er, err := exe(hctx, ExecRequest{
			Dir: r.Dir, Argv: []string{"/bin/sh", "-c", m.hook.Command}, Stdin: stdin,
			Env: r.envFor(m.plugin), AllowNetwork: allowNet,
		})
		timedOut := errors.Is(hctx.Err(), context.DeadlineExceeded)
		cancel()
		switch {
		case timedOut:
			rec.Kind, rec.Detail = Failed, fmt.Sprintf("%v after %s; the hook was stopped (%s)", ErrTimeout, m.hook.Timeout, clip(m.hook.Command))
		case err != nil:
			rec.Kind, rec.Detail = Failed, fmt.Sprintf("could not run: %v (%s)", err, clip(m.hook.Command))
		case er.Exit == 2:
			reason := strings.TrimSpace(er.Stderr)
			if reason == "" {
				reason = "blocked (exit 2, no message)"
			}
			rec.Kind, rec.Detail = Denied, clip(reason)
			fenced := untrusted.Wrap("hook:"+m.plugin.Name, clip(reason))
			if in.Event == PostToolUse {
				res.Context = joinBlock(res.Context, fenced)
			}
			if !res.Blocked {
				res.Blocked, res.By, res.Reason = true, m.plugin.Name, fenced
			}
		case er.Exit != 0:
			rec.Kind, rec.Detail = Failed, fmt.Sprintf("exited %d (%s)%s", er.Exit, clip(m.hook.Command), tail(er.Stderr))
		default:
			rec.Kind = Allowed
			if out := strings.TrimSpace(er.Stdout); out != "" && (in.Event == UserPromptSubmit || in.Event == SessionStart) {
				res.Context = joinBlock(res.Context, untrusted.Wrap("hook:"+m.plugin.Name+":"+string(in.Event), out))
			}
		}
		emit(rec)
		if rec.Kind == Denied && (in.Event == PreToolUse || in.Event == UserPromptSubmit) {
			break
		}
	}
	return res
}

func (r *Runner) envFor(p Plugin) []string {
	env := append([]string(nil), r.Env...)
	return append(env,
		"CLAUDE_PLUGIN_ROOT="+p.Dir, "DHI_PLUGIN_ROOT="+p.Dir,
		"CLAUDE_PROJECT_DIR="+r.Dir, "DHI_HOOK_EVENT_SOURCE=dhi")
}

func joinBlock(a, b string) string {
	if a == "" {
		return b
	}
	return a + "\n" + b
}

func clip(s string) string {
	s = strings.TrimSpace(s)
	if len([]rune(s)) > 300 {
		return string([]rune(s)[:299]) + "…"
	}
	return s
}

func tail(stderr string) string {
	if s := strings.TrimSpace(stderr); s != "" {
		return ": " + clip(s)
	}
	return ""
}
