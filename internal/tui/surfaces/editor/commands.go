package editor

import (
	"path/filepath"

	"charm.land/bubbletea/v2"

	"github.com/drjzlyan/dhi/internal/tui/kit"
	"github.com/drjzlyan/dhi/internal/tui/surfaces"
)

// Commands implements surfaces.CommandProvider (F-041): the palette lists
// what the editor can do *now* — buffer-bound actions appear only with a
// buffer open, debugger actions only with a session, and so on — so the
// palette doubles as discoverable documentation of the `:` commands.
func (m *Model) Commands() []surfaces.Command {
	var out []surfaces.Command
	add := func(title, hint string, run func()) {
		out = append(out, surfaces.Command{Group: "Editor", Title: title, Hint: hint, Run: func() tea.Cmd {
			run()
			return nil
		}})
	}
	ex := func(cmd string) func() {
		return func() {
			if e := m.active(); e != nil {
				if !m.ExecEx(e, cmd) {
					e.SetMessage("unknown command :" + cmd)
				}
			}
		}
	}
	add("Find file", "/", m.openFind)
	if m.searcher != nil {
		add("Search the workspace", "s", m.openSearch)
	}
	add("Toggle terminal", "ctrl+t", m.ToggleDrawer)
	add("Toggle git panel", "ctrl+j", m.ToggleGitPanel)
	if m.chat != nil {
		add("Toggle crew chat", "ctrl+a", m.chat.Toggle)
	}
	if e := m.active(); e != nil {
		add("Save file", ":w", func() { e.Key(":"); e.Key("w"); e.Key("enter") })
		if p := e.Path(); p != "" { // F-103: paths for a PR, a chat, a shell
			out = append(out,
				surfaces.Command{Group: "Editor", Title: "Copy file path", Run: func() tea.Cmd { return kit.Copy(p) }},
				surfaces.Command{Group: "Editor", Title: "Copy relative path", Run: func() tea.Cmd { return kit.Copy(m.relToWorkspace(p)) }})
		}
		add("Format document", ":fmt", ex("fmt"))
		add("Go to symbol", ":sym", ex("sym"))
		add("Run tests in this package", ":test", ex("test"))
		add("Run all tests in this repo", ":test all", ex("test all"))
		add("Toggle breakpoint on this line", ":break", ex("break"))
		if m.dbg == nil {
			add("Start debugging", ":debug", ex("debug"))
		}
		if m.chat != nil && m.chat.rt != nil && m.pairAgent == "" {
			for _, id := range m.chat.agents {
				id := id
				add("Pair with @"+id, ":pair "+id, ex("pair "+id))
			}
		}
		if m.pairAgent != "" {
			add("End pairing with @"+m.pairAgent, ":unpair", ex("unpair"))
		}
	}
	if m.dbg != nil {
		add("Stop debugging", ":stop", ex("stop"))
		if m.dbg.st.Stopped {
			add("Continue", ":cont", ex("cont"))
			add("Step over", ":next", ex("next"))
			add("Step into", ":step", ex("step"))
			add("Step out", ":out", ex("out"))
			add("Show debugger panel", "ctrl+d", func() { m.mode = modeDebug })
		}
	}
	if len(m.proposals) > 0 {
		add("Review agent suggestions", "ctrl+y", func() { m.reviewOpen = true })
	}
	return out
}

// relToWorkspace is p relative to the workspace root (p itself when it
// lies outside or there is no workspace).
func (m *Model) relToWorkspace(p string) string {
	if m.ws == nil {
		return p
	}
	if r, err := filepath.Rel(m.ws.Root, p); err == nil && !filepath.IsAbs(r) && r != ".." && !hasDotDotPrefix(r) {
		return r
	}
	return p
}

func hasDotDotPrefix(r string) bool { return len(r) >= 3 && r[:3] == ".."+string(filepath.Separator) }
