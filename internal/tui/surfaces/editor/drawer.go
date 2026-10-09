package editor

import (
	"context"
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
	scrollbackCap  = 1000
)

// termTab is one drawer tab bound to a working directory.
type termTab struct {
	sess   *term.Session
	dir    string
	screen *vt.Screen // emulated terminal sized to the pane (F-061)
	exited bool
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
	sess, err := term.Start(ctx, term.Options{Dir: dir, Label: label, Env: m.termEnv})
	t := &termTab{sess: sess, dir: dir, screen: vt.New(scrollbackCap)}
	m.terms = append(m.terms, t)
	m.cancelTerms = append(m.cancelTerms, cancel)
	if err != nil {
		t.exited = true
		t.screen.Feed([]byte("\x1b[31m" + err.Error() + "\x1b[0m\n"))
		return
	}
	// The emulator answers the program's terminal queries (cursor
	// position, attributes); those replies are the program's input.
	go func() { _, _ = io.Copy(writerFunc(sess.Write), t.screen.Replies()) }()
	m.syncTermSize()
	go m.pumpTerm(len(m.terms)-1, sess, m.termMsgs)
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
		return maxInt(m.width-4, 10), maxInt(m.height-3, 3)
	}
	h := min(drawerHeight, maxInt(m.height/3, 4))
	return maxInt(m.width-m.railW()-5, 20), maxInt(h-2, 1)
}

// syncTermSize keeps every live session's PTY and emulator at the size
// it is shown at (they must agree, or programs draw for the wrong box).
func (m *Model) syncTermSize() {
	for i, t := range m.terms {
		if t.exited || t.sess == nil {
			continue
		}
		cols, rows := m.termGeom(i == m.activeTerm && t.screen.AltScreen())
		if w, h := t.screen.Size(); w == cols && h == rows {
			continue
		}
		t.screen.Resize(cols, rows)
		_ = t.sess.Resize(cols, rows)
	}
}

// TakesCtrlC implements surfaces.CtrlCTaker: a focused live terminal
// gets ctrl+c (interrupt), not the shell's quit.
func (m *Model) TakesCtrlC() bool {
	t := m.activeTermTab()
	return m.drawerOpen && m.termFocus && t != nil && !t.exited
}

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

func (m *Model) ingestTermChunk(idx int, chunk []byte) {
	if idx < 0 || idx >= len(m.terms) {
		return
	}
	t := m.terms[idx]
	wasAlt := t.screen.AltScreen()
	t.screen.Feed(chunk)
	if t.screen.AltScreen() != wasAlt {
		m.syncTermSize() // a full-screen program started or ended
		if t.screen.AltScreen() {
			m.termFocus = true // it needs the keyboard
		}
	}
}

func (m *Model) termExited(idx int) {
	if idx >= 0 && idx < len(m.terms) && !m.terms[idx].exited {
		m.terms[idx].exited = true
		m.terms[idx].screen.Feed([]byte("\r\n\x1b[2m[process exited]\x1b[0m"))
		m.terms[idx].screen.Close()
	}
}

// drawerView renders the bottom terminal panel. The scrollback keeps
// SGR colors and honors cursor moves (F-026 P5); rows clip to the
// pane width with styles intact.
func (m *Model) drawerView() string {
	h := min(drawerHeight, maxInt(m.height/3, 4))
	cols, rows := m.termGeom(false)
	body := m.termBody(cols, rows)
	focusMark := ""
	if m.termFocus {
		focusMark = " " + theme.Brand().Render(theme.GlyphDot)
	}
	panel := kit.NewPanel("terminal"+focusMark+termStrip(m), false)
	panel.SetContent(body...)
	panel.Width = maxInt(m.width-m.railW()-1, 20)
	panel.Height = h
	hint := "ctrl+t blur/close · alt+1..9 switch · alt+n new tab · ctrl+q quits DHI"
	return panel.View() + "\n" + theme.Hint().Render(kit.ClipEllipsis(hint, m.width))
}

// fullTermView is the terminal over the whole body while a full-screen
// program runs; it returns to the drawer when the program exits.
func (m *Model) fullTermView() string {
	cols, rows := m.termGeom(true)
	panel := kit.NewPanel("terminal · full screen"+termStrip(m), m.termFocus)
	panel.SetContent(m.termBody(cols, rows)...)
	panel.Width, panel.Height = m.width, m.height-1
	hint := "ctrl+t blur · keys go to the program · ctrl+q quits DHI"
	return panel.View() + "\n" + theme.Hint().Render(kit.ClipEllipsis(hint, m.width))
}

// termBody renders the active screen with the cursor cell shown while
// the terminal has focus.
func (m *Model) termBody(cols, rows int) []string {
	t := m.activeTermTab()
	if t == nil {
		return make([]string, rows)
	}
	body := t.screen.Lines(cols, rows)
	if t.exited || !m.termFocus {
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

func termStrip(m *Model) string {
	if len(m.terms) <= 1 {
		return ""
	}
	out := "  "
	for i, t := range m.terms {
		label := ""
		if t.sess != nil {
			label = t.sess.Label()
		} else {
			label = filepath.Base(t.dir) // refused start; error shows in-body
		}
		switch {
		case i == m.activeTerm:
			out += theme.TabActive().Render("[" + label + "] ")
		case t.exited:
			// skip exited tabs in the strip
		default:
			out += theme.Hint().Render(label + " ")
		}
	}
	return out
}
