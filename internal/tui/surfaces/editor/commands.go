package editor

import (
	"path/filepath"

	"charm.land/bubbletea/v2"

	"github.com/drjzlyan/dhi/internal/agentkit/skills"
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
	add("Find and open a file", "/", m.openFind)
	if m.chat != nil && m.chat.rt != nil { // G-X3: user-invocable skills
		list := m.chat.skillList()
		for _, k := range skills.Commands(list) {
			k := k
			add("Skill /"+k.Slug+" — "+k.Description, "chat", func() { m.chat.fillSkill(k.Slug) })
		}
		if n := skills.ShadowNote(list); n != "" {
			add(n, "", func() {})
		}
	}
	if m.cps != nil {
		add("Undo the last agent turn…", ":undo-turn", func() {
			if msg := m.openUndoTurn(); msg != "" {
				m.toast = &kit.Toast{Kind: kit.ToastInfo, Title: "Undo turn", Detail: msg}
			}
		})
	}
	if len(m.closed) > 0 {
		add("Reopen closed file", ":reopen", func() {
			if msg := m.reopenClosed(); msg != "" && m.active() != nil {
				m.active().SetMessage(msg)
			}
		})
	}
	for _, p := range m.recentShown(5) {
		p := p
		add("Open recent: "+m.relToWorkspace(p), "", func() { m.open(&node{kind: nodeFile, path: p, name: filepath.Base(p)}) })
	}
	if m.ws != nil && m.start.NewTask != nil {
		add("New task", "n", m.start.NewTask)
	}
	if m.searcher != nil {
		add("Search the workspace", "s", m.openSearch)
	}
	add("Run a task…", ":task", func() {
		if e := m.active(); e != nil {
			m.ExecEx(e, "task")
		} else if msg, _ := m.taskCommand("task"); msg != "" {
			m.toast = &kit.Toast{Kind: kit.ToastInfo, Title: "Tasks", Detail: msg}
		}
	})
	if m.trust != nil {
		title, ex := "Trust this folder…", "trust"
		if m.trust.Trusted(m.trustRoot(m.activeDir())) {
			title, ex = "Stop trusting this folder (restricted mode)", "untrust"
		}
		add(title, ":"+ex, func() {
			msg, _ := m.trustCommand(ex)
			m.toast = &kit.Toast{Kind: kit.ToastInfo, Title: "Trust", Detail: msg}
		})
	}
	add("Toggle terminal", "ctrl+t", m.ToggleDrawer)
	if len(m.terms) > 0 && m.chat != nil {
		add("Ask an agent about the terminal output", "", func() {
			msg := m.askAboutTerminal()
			m.toast = &kit.Toast{Kind: kit.ToastInfo, Title: "Terminal", Detail: msg}
		})
	}
	add("Toggle git panel", "ctrl+j", m.ToggleGitPanel)
	if m.chat != nil {
		add("Ask an agent (open chat)", "ctrl+a", m.chat.Focus)
	} else if m.ws != nil {
		add("Ask an agent (pick a team first)", "ctrl+a", m.askWithoutTeam)
	}
	if e := m.active(); e != nil {
		add("Save file", ":w", func() { // direct: typing ":w" landed as text in insert mode (F-116)
			if err := e.Save(); err != nil {
				e.SetMessage(err.Error())
			} else {
				e.SetMessage(`"` + m.relToWorkspace(e.Path()) + `" written`)
			}
		})
		if p := e.Path(); p != "" { // F-103: paths for a PR, a chat, a shell
			out = append(out,
				surfaces.Command{Group: "Editor", Title: "Copy file path", Run: func() tea.Cmd { return kit.Copy(p) }},
				surfaces.Command{Group: "Editor", Title: "Copy relative path", Run: func() tea.Cmd { return kit.Copy(m.relToWorkspace(p)) }})
		}
		add("Format document", ":fmt", ex("fmt"))
		add("Change the selection with an agent…", "ctrl+k", func() { m.startAgentEdit() })
		if _, ok := m.diagAtLine(e.Path(), e.Buffer().Cursor().Line); ok {
			add("Fix this problem with an agent", ":fix", ex("fix"))
		}
		add("Organize imports", ":organize", ex("organize"))
		add("Local history of this file", ":localhistory", ex("localhistory"))
		add("Commit graph of the branches", ":graph", ex("graph"))
		add("Go to symbol", ":sym", ex("sym"))
		out = append(out, surfaces.Command{Group: "Editor", Title: "Go to symbol in workspace", Hint: ":wsym", Run: func() tea.Cmd {
			ex("wsym")()
			return m.TakeCmd()
		}})
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
