package editor

import (
	"context"
	"fmt"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/drjzlyan/dhi/internal/dap"
	"github.com/drjzlyan/dhi/internal/textbuf"
	"github.com/drjzlyan/dhi/internal/tui/kit"
	"github.com/drjzlyan/dhi/internal/tui/theme"
	"github.com/drjzlyan/dhi/internal/tutorial"
)

// Debugging (F-039). Breakpoints are toggled per line (`:break`); `:debug`
// launches the current file's package under the adapter (delve over DAP).
// A stop jumps the editor to the top frame, marks it in the gutter and
// opens a panel with the stack, locals and output. Breakpoints are
// in-memory for the session of the editor (they do not follow edits).

// dapStarter creates the adapter connection; a seam so tests can run the
// whole flow against a scripted adapter.
type dapStarter func(ctx context.Context, dir string, env []string) (*dap.Client, error)

type dbgState struct {
	client   *dap.Client
	sess     *dap.Session
	starting bool
	frame    int // selected frame in the panel
	st       dap.State
	lastStop int // StopSeq already revealed (output pings must not re-open the panel)
}

// debug message kinds ride teaMsg (see editor.go).
type dbgStarted struct {
	client *dap.Client
	sess   *dap.Session
	err    error
}

// toggleBreakpoint flips a breakpoint at the cursor line of the active
// buffer and returns the status message.
func (m *Model) toggleBreakpoint() string {
	e := m.active()
	if e == nil {
		return "break: open a file first"
	}
	if !strings.HasSuffix(e.Path(), ".go") {
		return "break: only Go files can be debugged"
	}
	path, line := e.Path(), e.Buffer().Cursor().Line+1
	lines := m.breakpoints[path]
	idx := sort.SearchInts(lines, line)
	var msg string
	if idx < len(lines) && lines[idx] == line {
		lines = append(lines[:idx], lines[idx+1:]...)
		msg = fmt.Sprintf("breakpoint removed at %s:%d", filepath.Base(path), line)
	} else {
		lines = append(lines, 0)
		copy(lines[idx+1:], lines[idx:])
		lines[idx] = line
		msg = fmt.Sprintf("breakpoint set at %s:%d", filepath.Base(path), line)
	}
	if m.breakpoints == nil {
		m.breakpoints = map[string][]int{}
	}
	if len(lines) == 0 {
		delete(m.breakpoints, path)
	} else {
		m.breakpoints[path] = lines
	}
	m.syncBreakpoints(path)
	return msg
}

// syncBreakpoints pushes one file's breakpoints to a live session.
func (m *Model) syncBreakpoints(path string) {
	if m.dbg == nil || m.dbg.sess == nil {
		return
	}
	sess, lines := m.dbg.sess, append([]int(nil), m.breakpoints[path]...)
	ch := m.termMsgs
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		bps, err := sess.SetBreakpoints(ctx, path, lines)
		note := ""
		switch {
		case err != nil:
			note = "break: " + err.Error()
		default:
			for _, b := range bps {
				if !b.Verified {
					note = fmt.Sprintf("break: line %d not bound (%s)", b.Line, nonEmptyStr(b.Message, "no code there"))
				}
			}
		}
		if note != "" {
			ch <- teaMsg{kind: lspMsgNote, note: note}
		}
	}()
}

func nonEmptyStr(s, d string) string {
	if strings.TrimSpace(s) == "" {
		return d
	}
	return s
}

// debugCommand handles the :break/:debug family. It returns the status
// message and whether cmd was a debug command.
func (m *Model) debugCommand(cmd string) (string, bool) {
	f := strings.Fields(cmd)
	if len(f) == 0 {
		return "", false
	}
	switch f[0] {
	case "break":
		return m.toggleBreakpoint(), true
	case "debug":
		return m.startDebug(), true
	case "cont", "next", "step", "out":
		return m.debugStep(f[0]), true
	case "stop":
		return m.stopDebug(), true
	case "eval":
		return m.debugEval(strings.TrimSpace(strings.TrimPrefix(cmd, "eval"))), true
	}
	return "", false
}

func (m *Model) startDebug() string {
	if m.dbg != nil {
		return "debug: a session is already running (:stop ends it)"
	}
	e := m.active()
	if e == nil {
		return "debug: open a file first"
	}
	if !strings.HasSuffix(e.Path(), ".go") {
		return "debug: only Go packages can be debugged"
	}
	root, _, ok := m.repoRel(e.Path())
	if !ok {
		return "debug: the file is not inside a workspace member"
	}
	start := m.dapStart
	if start == nil {
		start = func(ctx context.Context, dir string, env []string) (*dap.Client, error) {
			return dap.StartDelve(ctx, dir, env)
		}
	}
	bps := map[string][]int{}
	for p, ls := range m.breakpoints {
		bps[p] = append([]int(nil), ls...)
	}
	cfg := dap.LaunchConfig{Mode: "debug", Program: filepath.Dir(e.Path()), Cwd: root}
	env, ch := m.termEnv, m.termMsgs
	m.dbg = &dbgState{starting: true}
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute) // includes the compile
		defer cancel()
		c, err := start(ctx, root, env)
		if err != nil {
			ch <- teaMsg{kind: dbgMsgStarted, dbgStart: &dbgStarted{err: err}}
			return
		}
		sess, err := dap.Start(ctx, c, cfg, bps)
		if err != nil {
			_ = c.Close()
			ch <- teaMsg{kind: dbgMsgStarted, dbgStart: &dbgStarted{err: err}}
			return
		}
		ch <- teaMsg{kind: dbgMsgStarted, dbgStart: &dbgStarted{client: c, sess: sess}}
		for {
			select {
			case <-sess.Updates():
				ch <- teaMsg{kind: dbgMsgUpdate}
			case <-sess.Done():
				ch <- teaMsg{kind: dbgMsgUpdate}
				return
			}
		}
	}()
	return "starting debugger…"
}

// applyDebugStarted folds the handshake result into the model.
func (m *Model) applyDebugStarted(s *dbgStarted) {
	if s == nil || m.dbg == nil {
		if s != nil && s.client != nil {
			_ = s.client.Close() // session was cancelled while starting
		}
		return
	}
	if s.err != nil {
		m.dbg = nil
		if e := m.active(); e != nil {
			e.SetMessage("debug: " + s.err.Error())
		}
		return
	}
	m.dbg.starting, m.dbg.client, m.dbg.sess = false, s.client, s.sess
	if e := m.active(); e != nil {
		e.SetMessage("debugger started — running to the first breakpoint")
	}
	m.act(tutorial.EvEditorDebug)
}

// applyDebugUpdate reacts to a state change from the session.
func (m *Model) applyDebugUpdate() {
	d := m.dbg
	if d == nil || d.sess == nil {
		return
	}
	d.st = d.sess.State()
	switch {
	case d.st.Exited:
		msg := fmt.Sprintf("program exited (code %d)", d.st.ExitCode)
		if d.st.Err != "" {
			msg = "debug: " + d.st.Err
		}
		if m.mode == modeDebug {
			m.mode = modeNav
		}
		if d.client != nil {
			go d.client.Close()
		}
		m.dbg = nil
		if e := m.active(); e != nil {
			e.SetMessage(msg)
		}
	case d.st.Stopped:
		if d.st.StopSeq != d.lastStop {
			d.lastStop = d.st.StopSeq
			if len(d.st.Frames) > 0 {
				m.revealFrame(d.st.Frames[0])
			}
			m.mode = modeDebug
			d.frame = 0
		}
	default: // running again
		if m.mode == modeDebug {
			m.mode = modeNav
		}
	}
}

// revealFrame opens the frame's file and puts the cursor on its line.
func (m *Model) revealFrame(f dap.Frame) {
	if f.Path == "" {
		return
	}
	if m.tabFor(f.Path) == nil && m.OpenPaths(f.Path) == 0 {
		return
	}
	for i, t := range m.bufs {
		if t.path == f.Path {
			m.activeTab = i
		}
	}
	if e := m.active(); e != nil && f.Line > 0 {
		e.Buffer().SetCursor(textbuf.Pos{Line: f.Line - 1, Col: 0})
	}
	m.bufFocus = true
}

func (m *Model) debugStep(which string) string {
	d := m.dbg
	if d == nil || d.sess == nil {
		return "debug: no session — :debug starts one"
	}
	if !d.st.Stopped {
		return "debug: the program is running (not stopped)"
	}
	sess, ch := d.sess, m.termMsgs
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
		defer cancel()
		var err error
		switch which {
		case "cont":
			err = sess.Continue(ctx)
		case "next":
			err = sess.Next(ctx)
		case "step":
			err = sess.StepIn(ctx)
		case "out":
			err = sess.StepOut(ctx)
		}
		if err != nil {
			ch <- teaMsg{kind: lspMsgNote, note: "debug: " + err.Error()}
		}
	}()
	return which + "…"
}

func (m *Model) stopDebug() string {
	d := m.dbg
	if d == nil {
		return "debug: no session"
	}
	m.dbg = nil
	if m.mode == modeDebug {
		m.mode = modeNav
	}
	if d.sess != nil {
		go func() {
			ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
			defer cancel()
			_ = d.sess.Stop(ctx)
		}()
	}
	return "debug session stopped"
}

func (m *Model) debugEval(expr string) string {
	d := m.dbg
	if d == nil || d.sess == nil || !d.st.Stopped {
		return "eval: needs a stopped debug session"
	}
	if expr == "" {
		return "usage: :eval <expression>"
	}
	sess, ch := d.sess, m.termMsgs
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		out, err := sess.Evaluate(ctx, expr)
		note := fmt.Sprintf("%s = %s", expr, out)
		if err != nil {
			note = "eval: " + err.Error()
		}
		ch <- teaMsg{kind: lspMsgNote, note: note}
	}()
	return "evaluating…"
}

func (m *Model) handleDebugKey(key string) bool {
	d := m.dbg
	if d == nil {
		m.mode = modeNav
		return false
	}
	switch key {
	case "esc", "q":
		m.mode = modeNav
	case "c":
		m.noteActive(m.debugStep("cont"))
	case "n":
		m.noteActive(m.debugStep("next"))
	case "s":
		m.noteActive(m.debugStep("step"))
	case "o":
		m.noteActive(m.debugStep("out"))
	case "x":
		m.noteActive(m.stopDebug())
	case "down", "j":
		if d.frame < len(d.st.Frames)-1 {
			d.frame++
		}
	case "up", "k":
		if d.frame > 0 {
			d.frame--
		}
	case "enter":
		if d.frame < len(d.st.Frames) {
			m.revealFrame(d.st.Frames[d.frame])
			m.mode = modeNav
		}
	}
	return true
}

func (m *Model) noteActive(msg string) {
	if e := m.active(); e != nil && msg != "" {
		e.SetMessage(msg)
	}
}

// debugGutter returns the gutter glyph for a debug marker on line l
// (0-based) of path: the stop line wins over a breakpoint.
func (m *Model) debugGutter(path string, l int) (string, bool) {
	if d := m.dbg; d != nil && d.st.Stopped && len(d.st.Frames) > 0 {
		if f := d.st.Frames[0]; f.Path == path && f.Line-1 == l {
			return theme.SuccessText().Render("▶"), true
		}
	}
	for _, bl := range m.breakpoints[path] {
		if bl-1 == l {
			return theme.DangerText().Render(theme.GlyphDot), true
		}
	}
	return "", false
}

// DebugState describes the session for an agent (the debug_state tool).
func (m *Model) DebugState() (string, error) {
	d := m.dbg
	if d == nil {
		return "no debug session", nil
	}
	if d.sess == nil {
		return "debugger starting", nil
	}
	st := d.sess.State()
	var b strings.Builder
	switch {
	case st.Exited:
		fmt.Fprintf(&b, "exited (code %d)\n", st.ExitCode)
	case st.Stopped:
		fmt.Fprintf(&b, "stopped: %s (thread %d)\n", st.Reason, st.ThreadID)
	default:
		b.WriteString("running\n")
	}
	for i, f := range st.Frames {
		p := f.Path
		if v, err := m.ws.VPathFor(f.Path); err == nil {
			p = v.String()
		}
		fmt.Fprintf(&b, "#%d %s %s:%d\n", i, f.Name, p, f.Line)
	}
	if len(st.Locals) > 0 {
		b.WriteString("locals:\n")
		for _, v := range st.Locals {
			fmt.Fprintf(&b, "  %s %s = %s\n", v.Name, v.Type, v.Value)
		}
	}
	if n := len(st.Output); n > 0 {
		from := n - 10
		if from < 0 {
			from = 0
		}
		b.WriteString("output (last lines):\n")
		for _, l := range st.Output[from:] {
			b.WriteString("  " + l + "\n")
		}
	}
	return b.String(), nil
}

func (m *Model) debugView() string {
	d := m.dbg
	st := d.st
	title := "debugger"
	if st.Reason != "" {
		title += " — " + st.Reason
	}
	var body []string
	body = append(body, theme.TextDim().Render("STACK"))
	for i, f := range st.Frames {
		row := fmt.Sprintf("%s  %s:%d", f.Name, m.shortPath(f.Path), f.Line)
		if i == d.frame {
			body = append(body, theme.Chip().Render(" "+row+" "))
		} else {
			body = append(body, "  "+row)
		}
	}
	body = append(body, "", theme.TextDim().Render("LOCALS"))
	if len(st.Locals) == 0 {
		body = append(body, theme.TextDim().Render("  (none)"))
	}
	for _, v := range st.Locals {
		body = append(body, truncateRunes(fmt.Sprintf("  %s %s = %s", v.Name, theme.TextDim().Render(v.Type), v.Value), 84))
	}
	if n := len(st.Output); n > 0 {
		body = append(body, "", theme.TextDim().Render("OUTPUT"))
		from := n - 6
		if from < 0 {
			from = 0
		}
		for _, l := range st.Output[from:] {
			body = append(body, "  "+truncateRunes(l, 84))
		}
	}
	box := kit.NewPanel(title, true)
	box.SetContent(body...)
	box.Width = 90
	box.Height = min(len(body)+2, m.height)
	hint := theme.Hint().Render("c continue · n next · s step in · o step out · x stop · enter go to frame · esc hide (ctrl+d)")
	return kit.Center(joinV(box.View(), "", hint), m.width, m.height)
}
