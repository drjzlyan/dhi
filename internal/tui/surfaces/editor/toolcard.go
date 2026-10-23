package editor

import (
	"context"
	"strings"

	"github.com/drjzlyan/dhi/internal/term"
	"github.com/drjzlyan/dhi/internal/tui/kit"
	"github.com/drjzlyan/dhi/internal/tui/theme"
	"github.com/drjzlyan/dhi/internal/vt"
)

// HostTools finds and installs the programs the editor uses on the user's
// own system (F-110). cmd injects internal/hosttools; the editor never
// installs anything itself — it shows the exact command and, on yes, runs
// it in a terminal tab the user watches.
type HostTools interface {
	Find(tool string) (string, bool)
	Plan(tool string) ToolPlan
	// Refresh re-reads the user's shell environment (slow: never call it
	// on the UI loop).
	Refresh()
}

// ToolPlan is what the install card shows for one tool.
type ToolPlan struct {
	Tool    string
	Purpose string   // what DHI uses it for
	Argv    []string // the install command; nil when nothing here can install it
	Sudo    bool     // the command asks for an administrator password
	Doc     string   // where the vendor documents installing it
	Note    string   // extra context (e.g. a version constraint)
}

// Command is the install command as the user would type it.
func (p ToolPlan) Command() string { return strings.Join(p.Argv, " ") }

// WithHostTools enables install cards for missing tools.
func WithHostTools(t HostTools) Option {
	return func(m *Model) { m.tools = t }
}

// AskInstall raises the install card for tool when it is still missing
// (an agent waiting on bwrap, F-113); false when it cannot.
func (m *Model) AskInstall(tool string) bool {
	if m.tools == nil {
		return false
	}
	if _, ok := m.tools.Find(tool); ok {
		return true
	}
	return m.askInstall(tool)
}

// askInstall raises the install card for tool (no-op without HostTools).
func (m *Model) askInstall(tool string) bool {
	if m.tools == nil {
		return false
	}
	p := m.tools.Plan(tool)
	m.toolCard = &p
	return true
}

// handleToolCardKey is the card's focus trap: y runs the command in a
// terminal tab, c copies it, n/esc closes.
func (m *Model) handleToolCardKey(key string) bool {
	p := *m.toolCard
	switch key {
	case "y", "enter":
		if len(p.Argv) == 0 {
			return true
		}
		m.toolCard = nil
		m.runToolInstall(p)
	case "c":
		text := p.Command()
		if text == "" {
			text = p.Doc
		}
		m.queue(kit.Copy(text))
		m.toolCard = nil
		m.note("copied: " + text)
	case "n", "esc", "q":
		m.toolCard = nil
	}
	return true
}

// note shows a one-line message on the active buffer, if any.
func (m *Model) note(msg string) {
	if e := m.active(); e != nil {
		e.SetMessage(msg)
	}
}

// toolCardView renders the card over the editor body.
func (m *Model) toolCardView(base string) string {
	p := m.toolCard
	dim, accent := theme.TextMuted(), theme.AccentText()
	lines := append(kit.WrapWords("DHI uses "+p.Tool+" for "+p.Purpose+".", 70), "It is not installed.", "")
	if len(p.Argv) > 0 {
		lines = append(lines, "Install it with your package manager:", "  "+accent.Render(p.Command()))
		if p.Sudo {
			lines = append(lines, dim.Render("  it asks for your administrator password in the terminal"))
		}
	} else {
		lines = append(lines, "No package manager here installs it. See:", "  "+accent.Render(p.Doc))
	}
	if p.Note != "" {
		lines = append(lines, "", dim.Render(p.Note))
	}
	lines = append(lines, "")
	if len(p.Argv) > 0 {
		lines = append(lines, dim.Render("y run it in a terminal tab · c copy · n not now"))
	} else {
		lines = append(lines, dim.Render("c copy the link · n close"))
	}
	box := kit.Modal{Title: "install " + p.Tool, Lines: lines, Width: min(maxInt(m.width-8, 30), 76)}
	return kit.Overlay(strings.Split(base, "\n"), box.View(), m.width, m.height)
}

// runToolInstall runs an approved install command in a new drawer tab.
// When it exits, DHI looks for the tool again (re-reading the shell
// environment only when the PATH it has misses it) and says what happened.
func (m *Model) runToolInstall(p ToolPlan) {
	dir := m.installDir()
	m.drawerOpen, m.termFocus = true, true
	ctx, cancel := context.WithCancel(context.Background())
	sess, err := term.Start(ctx, term.Options{Dir: dir, Label: "install " + p.Tool, Env: m.env(), Command: p.Argv})
	t := &termTab{sess: sess, dir: dir, screen: vt.New(scrollbackCap), tool: p.Tool}
	t.screen.Feed([]byte("\x1b[2m$ " + p.Command() + "\x1b[0m\r\n"))
	m.terms = append(m.terms, t)
	m.cancelTerms = append(m.cancelTerms, cancel)
	m.activeTerm = len(m.terms) - 1
	if err != nil {
		t.exited = true
		t.screen.Feed([]byte("\x1b[31m" + err.Error() + "\x1b[0m\r\n"))
		return
	}
	m.syncTermSize()
	go m.pumpTerm(len(m.terms)-1, sess, m.termMsgs)
}

// installDir is where install tabs start: the workspace root.
func (m *Model) installDir() string {
	if m.ws != nil && m.ws.Root != "" {
		return m.ws.Root
	}
	if len(m.members) > 0 {
		return m.members[0].path
	}
	return "."
}

// toolInstallDone re-detects a tool after its install tab exited (off the
// UI loop) and reports the outcome as a message.
func (m *Model) toolInstallDone(tool string, code int) {
	tools, ch := m.tools, m.termMsgs
	if tools == nil || ch == nil {
		return
	}
	go func() {
		_, ok := tools.Find(tool)
		if !ok {
			tools.Refresh() // a new PATH entry (a fresh nvm, ~/go/bin) shows up in a re-read shell
			_, ok = tools.Find(tool)
		}
		msg := teaMsg{kind: toolMsgInstalled, lang: tool, installOK: ok}
		switch {
		case ok:
			msg.note = tool + " is installed"
		case code > 0:
			msg.note = "the install of " + tool + " failed (exit " + itoa(code) + ") — see its terminal tab"
		default:
			msg.note = "the install finished but " + tool + " is still not on your PATH — check your shell profile"
		}
		ch <- msg
	}()
}

// applyToolInstalled retries what needed the tool.
func (m *Model) applyToolInstalled(msg teaMsg) {
	m.note(msg.note)
	if !msg.installOK {
		return
	}
	for _, l := range m.langs.All() {
		if l.Install.Tool == msg.lang {
			delete(m.lspNoted, l.ID)
		}
	}
	if e := m.active(); e != nil && e.Path() != "" {
		m.lspOpenDoc(e.Path(), e.Buffer().Text()) // a fresh server starts for the open file
	}
}

// installCommand handles :install <tool> — the install card for any host
// tool DHI knows (bwrap for agents, gh for pull requests, a debugger).
func (m *Model) installCommand(cmd string) (string, bool) {
	f := strings.Fields(cmd)
	if len(f) == 0 || f[0] != "install" {
		return "", false
	}
	if len(f) != 2 {
		return "install: name a tool, e.g. :install rg", true
	}
	if m.tools == nil {
		return "install: unavailable here", true
	}
	if p, ok := m.tools.Find(f[1]); ok {
		return f[1] + " is already installed (" + p + ")", true
	}
	if p := m.tools.Plan(f[1]); p.Purpose == "" || (len(p.Argv) == 0 && p.Doc == "") {
		return "install: DHI does not know " + f[1], true
	}
	m.askInstall(f[1])
	return "", true
}
