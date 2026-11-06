package editor

import (
	"context"
	"fmt"
	"github.com/drjzlyan/dhi/internal/textbuf"
	"io"
	"path/filepath"

	xansi "github.com/charmbracelet/x/ansi"

	"github.com/drjzlyan/dhi/internal/term"
	"github.com/drjzlyan/dhi/internal/tui/kit"
	"github.com/drjzlyan/dhi/internal/tui/theme"
	"github.com/drjzlyan/dhi/internal/vt"
)

const (
	gitPanelHeight = 12
	drawerHeight   = 10
)

// termTab is one drawer tab bound to a working directory.
type termTab struct {
	id     int // stable across closes: output is routed by it (F-121)
	sess   *term.Session
	dir    string
	screen *vt.Screen // emulated terminal sized to the pane (F-061)
	exited bool
	tool   string       // an approved install of this host tool runs here (F-110)
	scroll int          // rows scrolled back into history (0 = live) (F-121)
	task   *taskCapture // a task run's output, scanned for problems on exit (F-146)
	rebase bool         // an interactive rebase step runs here; its exit is checked (F-165)

	jumpRow int  // absolute row of the prompt alt+k / alt+j last chose (F-157)
	hasJump bool // jumpRow is valid until the view moves otherwise
}

// ToggleDrawer opens/closes/focus-blurs the terminal drawer (ctrl+t):
// closed -> open+focus -> blurred (session kept alive) -> closed.
func (m *Model) ToggleDrawer() {
	switch {
	case !m.drawerOpen:
		m.drawerOpen = true
		m.ensureTermTabs()
		m.termFocus = true
		m.syncTermSize()
	case m.termFocus:
		m.termFocus = false
	default:
		m.drawerOpen = false
		m.termFocus = false
	}
}

// ensureTermTabs lazily creates one cwd-pinned tab per member repo.
func (m *Model) ensureTermTabs() {
	if len(m.members) == 0 || len(m.terms) > 0 {
		return
	}
	for _, mem := range m.members {
		m.newTermTab(mem.path, mem.name)
	}
}

// newTermTab starts a session; errors render inline as an exited tab.
func (m *Model) newTermTab(dir, label string) {
	ctx, cancel := context.WithCancel(context.Background())
	sess, err := term.Start(ctx, term.Options{Dir: dir, Label: label, Env: m.env()})
	t := &termTab{sess: sess, dir: dir, screen: vt.New(m.editorPrefs().Scrollback)}
	// A program copying through OSC 52 (tmux, vim over SSH) reaches the
	// system clipboard (F-103); Feed runs on the UI loop, so no lock.
	t.screen.OnClipboard(func(text string) { m.termCopy = text })
	m.addTerm(t, cancel)
	if err != nil {
		t.exited = true
		t.screen.Feed([]byte("\x1b[31m" + err.Error() + "\x1b[0m\n"))
		return
	}
	// The emulator answers the program's terminal queries (cursor
	// position, attributes); those replies are the program's input.
	go func() { _, _ = io.Copy(writerFunc(sess.Write), t.screen.Replies()) }()
	m.syncTermSize()
	go m.pumpTerm(t.id, sess, m.termMsgs)
}

// writerFunc adapts a write method to io.Writer.
type writerFunc func([]byte) error

func (f writerFunc) Write(p []byte) (int, error) {
	if err := f(p); err != nil {
		return 0, err
	}
	return len(p), nil
}

// termGeom is the terminal's cell size: the drawer pane, or the whole
// body while a full-screen program holds the alternate screen.
func (m *Model) termGeom(full bool) (cols, rows int) {
	if full {
		return maxInt(m.width-4, 10), maxInt(m.height-3, 3) // edges + hint footer
	}
	h := min(drawerHeight, maxInt(m.height/3, 4))
	return maxInt(m.width-4, 16), maxInt(h-2, 1)
}

// syncTermSize keeps every live session's PTY and emulator at the size
// it is shown at (they must agree, or programs draw for the wrong box).
func (m *Model) syncTermSize() {
	for i, t := range m.terms {
		if t.exited || t.sess == nil {
			continue
		}
		cols, rows := m.termGeom(i == m.activeTerm && t.screen.AltScreen())
		if w := m.paneWidth(i); w > 0 && (i != m.activeTerm || !t.screen.AltScreen()) {
			cols = w // a split pane gets its own PTY width (F-141)
		}
		if w, h := t.screen.Size(); w == cols && h == rows {
			continue
		}
		t.screen.Resize(cols, rows)
		_ = t.sess.Resize(cols, rows)
	}
}

// TakesCtrlC implements surfaces.CtrlCTaker: a focused live terminal
// gets ctrl+c (interrupt), not the shell's quit.
// A buffer in insert, visual or command mode takes it too: ctrl+c leaves
// the mode as in vim (F-115), it never quits with the edit in flight.
func (m *Model) TakesCtrlC() bool {
	t := m.activeTermTab()
	if m.drawerOpen && m.termFocus && t != nil && !t.exited {
		return true
	}
	return m.bufCtrlCIsEsc()
}

// bufCtrlCIsEsc reports a focused buffer outside normal mode.
func (m *Model) bufCtrlCIsEsc() bool {
	if m.ws == nil || !m.bufFocus || (m.chat != nil && m.chat.open && m.chat.focus) || (m.gitOpen && m.gitFocus) {
		return false
	}
	e := m.active()
	return e != nil && e.Mode() != textbuf.ModeNormal
}

// TakesAltDigits implements surfaces.AltDigitTaker: a focused terminal
// keeps alt+1..9 for its own tabs.
func (m *Model) TakesAltDigits() bool { return m.drawerOpen && m.termFocus }

// fullScreenTerm reports a focused drawer whose program is full-screen:
// the terminal then takes the whole body (vim needs more than 10 rows).
func (m *Model) fullScreenTerm() bool {
	t := m.activeTermTab()
	return m.drawerOpen && t != nil && !t.exited && t.screen.AltScreen()
}

// pumpTerm bridges session output into Update messages.
func (m *Model) pumpTerm(idx int, sess *term.Session, msgs chan<- teaMsg) {
	for chunk := range sess.Out {
		msgs <- teaMsg{kind: termMsgOut, tab: idx, chunk: chunk}
	}
	msgs <- teaMsg{kind: termMsgClosed, tab: idx}
}

// takeTermCopy returns, once, what a terminal program last copied.
func (m *Model) takeTermCopy() string {
	c := m.termCopy
	m.termCopy = ""
	return c
}

// termIndex finds the tab with id (-1 when it was closed).
func (m *Model) termIndex(id int) int {
	for i, t := range m.terms {
		if t.id == id {
			return i
		}
	}
	return -1
}

// addTerm registers a new tab with the next id.
func (m *Model) addTerm(t *termTab, cancel context.CancelFunc) {
	m.termSeq++
	t.id = m.termSeq
	m.terms = append(m.terms, t)
	m.cancelTerms = append(m.cancelTerms, cancel)
}

func (m *Model) ingestTermChunk(id int, chunk []byte) {
	idx := m.termIndex(id)
	if idx < 0 {
		return
	}
	t := m.terms[idx]
	if t.task != nil {
		t.task.add(chunk)
	}
	wasAlt := t.screen.AltScreen()
	t.screen.Feed(chunk)
	if t.screen.AltScreen() != wasAlt {
		m.syncTermSize() // a full-screen program started or ended
		if t.screen.AltScreen() {
			m.termFocus = true // it needs the keyboard
		}
	}
}

func (m *Model) termExited(id int) {
	if idx := m.termIndex(id); idx >= 0 && !m.terms[idx].exited {
		t := m.terms[idx]
		t.exited = true
		code := -1
		if t.sess != nil {
			code = t.sess.ExitCode()
		}
		t.screen.Feed([]byte("\r\n\x1b[2m[process exited" + exitNote(code) + "]\x1b[0m"))
		t.screen.Close()
		if t.tool != "" {
			m.toolInstallDone(t.tool, code)
		}
		if t.task != nil {
			m.taskFinished(t.task, code)
		}
		if t.rebase {
			m.rebaseStepDone(t.dir)
		}
	}
}

func exitNote(code int) string {
	if code < 0 {
		return ""
	}
	return " with status " + itoa(code)
}

// drawerView renders the bottom terminal panel (F-064): it spans the
// full width, the edge lights up in the surface accent while focused,
// tabs are pills in the title, and the keys sit in the panel's own hint
// footer. The scrollback keeps SGR colors and honors cursor moves
// (F-026 P5); rows clip to the pane width with styles intact.
func (m *Model) drawerView() string {
	h := min(drawerHeight, maxInt(m.height/3, 4))
	cols, rows := m.termGeom(false)
	body := m.termBody(cols, rows)
	if m.termSplitActive() && !m.fullScreenTerm() {
		body = m.splitBody(rows)
	}
	panel := kit.NewPanel("terminal"+termStrip(m), m.termFocus)
	panel.SetContent(body...)
	panel.Width = maxInt(m.width, 20)
	panel.Height = h + 1
	return panel.View()
}

// fullTermView is the terminal over the whole body while a full-screen
// program runs; it returns to the drawer when the program exits.
func (m *Model) fullTermView() string {
	cols, rows := m.termGeom(true)
	panel := kit.NewPanel("terminal · full screen"+termStrip(m), m.termFocus)
	panel.SetContent(m.termBody(cols, rows)...)
	panel.Width, panel.Height = m.width, m.height
	return panel.View()
}

// termBody renders the active screen with the cursor cell shown while
// the terminal has focus.
func (m *Model) termBody(cols, rows int) []string {
	t := m.activeTermTab()
	if t == nil {
		return make([]string, rows)
	}
	return m.termBodyOf(t, cols, rows, m.termFocus)
}

// termBodyOf renders one tab's screen; focused shows its cursor cell.
func (m *Model) termBodyOf(t *termTab, cols, rows int, focused bool) []string {
	body := t.screen.Lines(cols, rows)
	if t.scroll > 0 { // F-121: looking back through the scrollback
		all := append(t.screen.History(cols), body...)
		t.scroll = min(t.scroll, max(len(all)-rows, 0))
		end := len(all) - t.scroll
		start := max(end-rows, 0)
		view := append([]string(nil), all[start:end]...)
		markFailed(t, view, t.screen.RowBase()-len(all)+len(body)+start, cols)
		if len(view) > 0 {
			view[len(view)-1] = theme.Hint().Render(fmt.Sprintf("↑ %d lines back · shift+pgdown or any key returns", t.scroll))
		}
		return view
	}
	markFailed(t, body, t.screen.RowBase(), cols)
	if t.exited || !focused {
		return body
	}
	x, y := t.screen.Cursor()
	if y < 0 || y >= len(body) || x < 0 || x >= cols {
		return body
	}
	line := body[y]
	cell := xansi.Strip(xansi.Cut(line, x, x+1))
	if cell == "" {
		cell = " "
	}
	body[y] = xansi.Truncate(line, x, "") + "\x1b[0m" + cursorStyle.Render(cell) + xansi.TruncateLeft(line, x+1, "")
	return body
}

// termStrip renders the drawer's tabs as pills (F-064): the active tab
// in the surface accent with a live dot, the rest quiet, exited tabs
// marked ✗. A single tab still shows its name so the drawer says where
// the shell is.
func termStrip(m *Model) string {
	if len(m.terms) == 0 {
		return ""
	}
	out := " "
	for i, t := range m.terms {
		label := filepath.Base(t.dir) // refused start; error shows in-body
		if t.sess != nil {
			label = t.sess.Label()
		}
		switch {
		case i == m.activeTerm && t.exited:
			out += kit.Pill(theme.GlyphCross+" "+label, theme.Current.TextDim)
		case i == m.activeTerm:
			out += kit.Pill(theme.GlyphDot+" "+label, theme.SurfaceAccent())
		case t.exited:
			out += theme.TextMuted().Render(" " + theme.GlyphCross + " " + label + " ")
		default:
			out += theme.Hint().Render(" " + label + " ")
		}
	}
	return out
}

// closeTerm stops and removes tab i; the drawer closes with the last one.
func (m *Model) closeTerm(i int) {
	if i < 0 || i >= len(m.terms) {
		return
	}
	if m.cancelTerms[i] != nil {
		m.cancelTerms[i]()
	}
	if id := m.terms[i].id; id == m.termSplit[0] || id == m.termSplit[1] {
		m.termSplit = [2]int{} // closing either pane ends the split
	}
	m.terms = append(m.terms[:i], m.terms[i+1:]...)
	m.cancelTerms = append(m.cancelTerms[:i], m.cancelTerms[i+1:]...)
	if m.activeTerm >= len(m.terms) {
		m.activeTerm = max(len(m.terms)-1, 0)
	}
	if len(m.terms) == 0 {
		m.drawerOpen, m.termFocus = false, false
	}
}

// restartTerm replaces an exited tab with a fresh shell in its folder.
func (m *Model) restartTerm(i int) {
	if i < 0 || i >= len(m.terms) || !m.terms[i].exited {
		return
	}
	dir := m.terms[i].dir
	label := filepath.Base(dir)
	if s := m.terms[i].sess; s != nil {
		label = s.Label()
	}
	m.closeTerm(i)
	m.newTermTab(dir, label)
	// keep its place in the strip
	last := len(m.terms) - 1
	t, c := m.terms[last], m.cancelTerms[last]
	copy(m.terms[i+1:], m.terms[i:last])
	copy(m.cancelTerms[i+1:], m.cancelTerms[i:last])
	m.terms[i], m.cancelTerms[i] = t, c
	m.activeTerm = i
	m.drawerOpen, m.termFocus = true, true
}
