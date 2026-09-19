package editor

import (
	"context"
	"github.com/drjzlyan/dhi/internal/term"
	"github.com/drjzlyan/dhi/internal/tui/kit"
	"github.com/drjzlyan/dhi/internal/tui/theme"
	"github.com/drjzlyan/dhi/internal/vt"
	"path/filepath"
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
	screen *vt.Screen // ANSI-aware scrollback (F-026 P5)
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
	go m.pumpTerm(len(m.terms)-1, sess, m.termMsgs)
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
	m.terms[idx].screen.Feed(chunk)
}

func (m *Model) termExited(idx int) {
	if idx >= 0 && idx < len(m.terms) && !m.terms[idx].exited {
		m.terms[idx].exited = true
		m.terms[idx].screen.Feed([]byte("\x1b[2m[process exited]\x1b[0m\n"))
	}
}

// drawerView renders the bottom terminal panel. The scrollback keeps
// SGR colors and honors cursor moves (F-026 P5); rows clip to the
// pane width with styles intact.
func (m *Model) drawerView() string {
	h := min(drawerHeight, maxInt(m.height/3, 4))
	var body []string
	if m.activeTerm < len(m.terms) {
		t := m.terms[m.activeTerm]
		inner := maxInt(m.width-railWidth-5, 20)
		body = t.screen.Lines(inner, h-2)
		if !t.exited {
			// Live-cell prompt marker: fresh rows get their own "_",
			// the running line carries it at the cursor.
			if t.screen.Pending() {
				body = append(body, "_")
			} else if len(body) > 0 {
				body[len(body)-1] += "_"
			}
		}
	}
	for len(body) < h-2 {
		body = append(body, "")
	}
	focusMark := ""
	if m.termFocus {
		focusMark = " " + theme.Brand().Render(theme.GlyphDot)
	}
	panel := kit.NewPanel("terminal"+focusMark+termStrip(m), false)
	panel.SetContent(body...)
	panel.Width = maxInt(m.width-railWidth-1, 20)
	panel.Height = h
	hint := "ctrl+t blur/close · alt+1..9 switch · alt+n new tab"
	return panel.View() + "\n" + theme.Hint().Render(hint)
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
