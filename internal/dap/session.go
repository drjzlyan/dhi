package dap

import (
	"context"
	"encoding/json"
	"fmt"
	"sync"
	"time"
)

// callTimeout bounds each ordinary adapter request.
const callTimeout = 10 * time.Second

// launchTimeout bounds the launch handshake. A "debug" launch compiles
// the program first (delve builds with optimizations off, so a cold build
// cache takes many seconds) and only then announces `initialized`.
var launchTimeout = 3 * time.Minute

// maxOutput caps the retained debuggee output lines.
const maxOutput = 500

// Frame is one stack frame.
type Frame struct {
	ID   int
	Name string
	Path string
	Line int // 1-based, as the adapter reports it
}

// Variable is one name/value in a scope.
type Variable struct {
	Name, Value, Type string
}

// Breakpoint is the adapter's verdict on one requested line.
type Breakpoint struct {
	Line     int
	Verified bool
	Message  string
}

// State is a snapshot of the debuggee.
type State struct {
	Started  bool // handshake done
	Stopped  bool // paused at a stop (frames/locals are current)
	StopSeq  int  // increments on every stop, even without a `continued` between
	Reason   string
	ThreadID int
	Frames   []Frame
	Locals   []Variable
	Output   []string
	Exited   bool
	ExitCode int
	Err      string // adapter-side failure worth showing
}

// LaunchConfig describes what to debug.
type LaunchConfig struct {
	Mode    string // "debug" (build+run a package), "test", "exec"
	Program string // package dir, test dir, or binary
	Args    []string
	Cwd     string
}

// Session drives one debug run.
type Session struct {
	c *Client

	mu      sync.Mutex
	st      State
	updates chan struct{} // coalesced "state changed" pings
	done    chan struct{}
}

// Start runs the handshake — initialize, launch, initialized event,
// breakpoints, configurationDone — and returns a live Session. bps maps
// absolute file paths to 1-based lines.
func Start(ctx context.Context, c *Client, cfg LaunchConfig, bps map[string][]int) (*Session, error) {
	s := &Session{c: c, updates: make(chan struct{}, 1), done: make(chan struct{})}

	// Events are consumed from the start so `initialized` is not missed;
	// the handshake waits on this channel before the loop takes over.
	initialized := make(chan struct{}, 1)
	go s.loop(initialized)

	actx, cancel := withTimeout(ctx, callTimeout)
	defer cancel()
	if err := c.Call(actx, "initialize", map[string]any{
		"clientID": "dhi", "adapterID": "dhi", "linesStartAt1": true, "columnsStartAt1": true,
		"pathFormat": "path", "supportsRunInTerminalRequest": false,
	}, nil); err != nil {
		return nil, err
	}
	// outputMode "remote" makes delve deliver the debuggee's stdout/stderr
	// as DAP `output` events (by default it writes them to its own stdout,
	// which nobody reads). Adapters that do not know it ignore it.
	args := map[string]any{"request": "launch", "mode": cfg.Mode, "program": cfg.Program, "outputMode": "remote"}
	if len(cfg.Args) > 0 {
		args["args"] = cfg.Args
	}
	if cfg.Cwd != "" {
		args["cwd"] = cfg.Cwd
	}
	launch, err := c.Send("launch", args)
	if err != nil {
		return nil, err
	}
	// The launch response normally arrives only after configurationDone,
	// but a launch that fails (cannot compile, cannot start the process)
	// answers immediately with an error and never sends `initialized` —
	// so watch for both rather than waiting out the timeout on a refusal.
	launchRes := make(chan error, 1)
	lctx, lcancel := withTimeout(ctx, launchTimeout)
	defer lcancel()
	go func() { launchRes <- launch.Wait(lctx, nil) }()
	var launchErr error
	launchResolved := false
	select {
	case <-initialized:
	case launchErr = <-launchRes:
		launchResolved = true
		if launchErr != nil {
			return nil, launchErr
		}
		// Launched without an `initialized` first: still wait for it.
		select {
		case <-initialized:
		case <-time.After(callTimeout):
			return nil, fmt.Errorf("dap: adapter launched but never sent the initialized event")
		case <-ctx.Done():
			return nil, ctx.Err()
		}
	case <-time.After(launchTimeout):
		return nil, fmt.Errorf("dap: adapter never sent the initialized event within %s (is the program still compiling?)", launchTimeout)
	case <-ctx.Done():
		return nil, ctx.Err()
	}
	for path, lines := range bps {
		if _, err := s.SetBreakpoints(ctx, path, lines); err != nil {
			return nil, err
		}
	}
	cctx, ccancel := withTimeout(ctx, launchTimeout)
	defer ccancel()
	if err := c.Call(cctx, "configurationDone", nil, nil); err != nil {
		return nil, err
	}
	if !launchResolved {
		select {
		case launchErr = <-launchRes:
		case <-cctx.Done():
			return nil, fmt.Errorf("dap: launch: %w", cctx.Err())
		}
		if launchErr != nil {
			return nil, launchErr
		}
	}
	s.mu.Lock()
	s.st.Started = true
	s.mu.Unlock()
	s.ping()
	return s, nil
}

// Updates pings (coalesced) whenever State changed.
func (s *Session) Updates() <-chan struct{} { return s.updates }

// Done closes when the event stream ends (adapter gone or terminated).
func (s *Session) Done() <-chan struct{} { return s.done }

func (s *Session) ping() {
	select {
	case s.updates <- struct{}{}:
	default:
	}
}

// State returns a copy of the current snapshot.
func (s *Session) State() State {
	s.mu.Lock()
	defer s.mu.Unlock()
	st := s.st
	st.Frames = append([]Frame(nil), s.st.Frames...)
	st.Locals = append([]Variable(nil), s.st.Locals...)
	st.Output = append([]string(nil), s.st.Output...)
	return st
}

func (s *Session) loop(initialized chan<- struct{}) {
	defer close(s.done)
	for ev := range s.c.Events() {
		switch ev.Name {
		case "initialized":
			select {
			case initialized <- struct{}{}:
			default:
			}
		case "stopped":
			var b struct {
				Reason   string `json:"reason"`
				ThreadID int    `json:"threadId"`
			}
			_ = json.Unmarshal(ev.Body, &b)
			s.onStopped(b.Reason, b.ThreadID)
		case "continued":
			s.mu.Lock()
			s.st.Stopped, s.st.Frames, s.st.Locals = false, nil, nil
			s.mu.Unlock()
			s.ping()
		case "output":
			var b struct {
				Output   string `json:"output"`
				Category string `json:"category"`
			}
			_ = json.Unmarshal(ev.Body, &b)
			if b.Category == "telemetry" || b.Output == "" {
				continue
			}
			s.mu.Lock()
			s.st.Output = append(s.st.Output, splitOutput(b.Output)...)
			if over := len(s.st.Output) - maxOutput; over > 0 {
				s.st.Output = append([]string(nil), s.st.Output[over:]...)
			}
			s.mu.Unlock()
			s.ping()
		case "exited":
			var b struct {
				ExitCode int `json:"exitCode"`
			}
			_ = json.Unmarshal(ev.Body, &b)
			s.mu.Lock()
			s.st.Exited, s.st.ExitCode, s.st.Stopped = true, b.ExitCode, false
			s.mu.Unlock()
			s.ping()
		case "terminated":
			s.mu.Lock()
			s.st.Exited, s.st.Stopped = true, false
			s.mu.Unlock()
			s.ping()
		}
	}
	s.mu.Lock()
	if !s.st.Exited {
		s.st.Exited, s.st.Stopped = true, false
		s.st.Err = "debug adapter connection closed"
	}
	s.mu.Unlock()
	s.ping()
}

func splitOutput(s string) []string {
	var out []string
	cur := ""
	for _, r := range s {
		if r == '\n' {
			out = append(out, cur)
			cur = ""
			continue
		}
		cur += string(r)
	}
	if cur != "" {
		out = append(out, cur)
	}
	return out
}

// onStopped refreshes frames and locals for a stop. A failure to fetch
// them is recorded, never hidden — the stop itself is still true.
func (s *Session) onStopped(reason string, thread int) {
	ctx, cancel := context.WithTimeout(context.Background(), callTimeout)
	defer cancel()
	st := State{Started: true, Stopped: true, Reason: reason, ThreadID: thread}
	s.mu.Lock()
	st.StopSeq = s.st.StopSeq + 1
	s.mu.Unlock()

	var stack struct {
		StackFrames []struct {
			ID     int    `json:"id"`
			Name   string `json:"name"`
			Line   int    `json:"line"`
			Source *struct {
				Path string `json:"path"`
			} `json:"source"`
		} `json:"stackFrames"`
	}
	if err := s.c.Call(ctx, "stackTrace", map[string]any{"threadId": thread, "levels": 20}, &stack); err != nil {
		st.Err = err.Error()
	}
	for _, f := range stack.StackFrames {
		fr := Frame{ID: f.ID, Name: f.Name, Line: f.Line}
		if f.Source != nil {
			fr.Path = f.Source.Path
		}
		st.Frames = append(st.Frames, fr)
	}
	if len(st.Frames) > 0 {
		st.Locals, st.Err = s.locals(ctx, st.Frames[0].ID, st.Err)
	}
	s.mu.Lock()
	st.Output, st.Exited = s.st.Output, s.st.Exited
	s.st = st
	s.mu.Unlock()
	s.ping()
}

func (s *Session) locals(ctx context.Context, frame int, prevErr string) ([]Variable, string) {
	var scopes struct {
		Scopes []struct {
			Name      string `json:"name"`
			Reference int    `json:"variablesReference"`
		} `json:"scopes"`
	}
	if err := s.c.Call(ctx, "scopes", map[string]any{"frameId": frame}, &scopes); err != nil {
		return nil, nonEmpty(prevErr, err.Error())
	}
	for _, sc := range scopes.Scopes {
		if sc.Name != "Locals" && sc.Name != "Local" && sc.Name != "Variables" {
			continue
		}
		var vars struct {
			Variables []struct {
				Name  string `json:"name"`
				Value string `json:"value"`
				Type  string `json:"type"`
			} `json:"variables"`
		}
		if err := s.c.Call(ctx, "variables", map[string]any{"variablesReference": sc.Reference}, &vars); err != nil {
			return nil, nonEmpty(prevErr, err.Error())
		}
		out := make([]Variable, 0, len(vars.Variables))
		for _, v := range vars.Variables {
			out = append(out, Variable{Name: v.Name, Value: v.Value, Type: v.Type})
		}
		return out, prevErr
	}
	return nil, prevErr
}

// SetBreakpoints replaces the breakpoints of one file (1-based lines).
func (s *Session) SetBreakpoints(ctx context.Context, path string, lines []int) ([]Breakpoint, error) {
	bp := make([]map[string]int, 0, len(lines))
	for _, l := range lines {
		bp = append(bp, map[string]int{"line": l})
	}
	var out struct {
		Breakpoints []struct {
			Verified bool   `json:"verified"`
			Line     int    `json:"line"`
			Message  string `json:"message"`
		} `json:"breakpoints"`
	}
	cctx, cancel := withTimeout(ctx, callTimeout)
	defer cancel()
	if err := s.c.Call(cctx, "setBreakpoints", map[string]any{
		"source": map[string]string{"path": path}, "breakpoints": bp,
	}, &out); err != nil {
		return nil, err
	}
	res := make([]Breakpoint, 0, len(out.Breakpoints))
	for _, b := range out.Breakpoints {
		res = append(res, Breakpoint{Line: b.Line, Verified: b.Verified, Message: b.Message})
	}
	return res, nil
}

func (s *Session) step(ctx context.Context, cmd string) error {
	st := s.State()
	if !st.Stopped {
		return fmt.Errorf("dap: %s: the program is running (not stopped)", cmd)
	}
	cctx, cancel := withTimeout(ctx, callTimeout)
	defer cancel()
	return s.c.Call(cctx, cmd, map[string]any{"threadId": st.ThreadID}, nil)
}

// Continue resumes the program.
func (s *Session) Continue(ctx context.Context) error { return s.step(ctx, "continue") }

// Next steps over.
func (s *Session) Next(ctx context.Context) error { return s.step(ctx, "next") }

// StepIn steps into a call.
func (s *Session) StepIn(ctx context.Context) error { return s.step(ctx, "stepIn") }

// StepOut runs to the caller.
func (s *Session) StepOut(ctx context.Context) error { return s.step(ctx, "stepOut") }

// Evaluate evaluates an expression in the top frame (only while stopped).
func (s *Session) Evaluate(ctx context.Context, expr string) (string, error) {
	st := s.State()
	if !st.Stopped || len(st.Frames) == 0 {
		return "", fmt.Errorf("dap: evaluate: the program is not stopped")
	}
	var out struct {
		Result string `json:"result"`
		Type   string `json:"type"`
	}
	cctx, cancel := withTimeout(ctx, callTimeout)
	defer cancel()
	if err := s.c.Call(cctx, "evaluate", map[string]any{
		"expression": expr, "frameId": st.Frames[0].ID, "context": "repl",
	}, &out); err != nil {
		return "", err
	}
	return out.Result, nil
}

// Stop ends the session and the debuggee, then closes the connection.
func (s *Session) Stop(ctx context.Context) error {
	cctx, cancel := withTimeout(ctx, callTimeout)
	defer cancel()
	err := s.c.Call(cctx, "disconnect", map[string]any{"terminateDebuggee": true}, nil)
	_ = s.c.Close()
	if err != nil && err != ErrClosed {
		return err
	}
	return nil
}
