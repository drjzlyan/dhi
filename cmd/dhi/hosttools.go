package main

import (
	"os"
	"os/exec"

	tea "charm.land/bubbletea/v2"

	"github.com/drjzlyan/dhi/internal/hostenv"
	"github.com/drjzlyan/dhi/internal/hosttools"
	"github.com/drjzlyan/dhi/internal/tui/surfaces/editor"
)

// hostLook resolves a program the way the user's shell would, plus the
// folders language installers use (F-110): a known tool through
// hosttools, anything else on the resolved PATH.
func hostLook(name string) (string, error) {
	if t, ok := hosttools.Get(name); ok {
		if p, found := hosttools.Find(t); found {
			return p, nil
		}
	}
	return hostenv.LookPath(name)
}

// hostFind is hostLook as (path, found).
func hostFind(name string) (string, bool) {
	p, err := hostLook(name)
	return p, err == nil
}

// reloadEnv re-reads the user's shell environment and makes it DHI's own,
// so a tool an install just put on PATH is found (F-109).
func reloadEnv() {
	hostenv.Reload()
	hostenv.Apply()
}

// editorTools adapts hosttools to the editor's install card.
type editorTools struct{}

func (editorTools) Find(tool string) (string, bool) { return hostFind(tool) }

func (editorTools) Refresh() { reloadEnv() }

func (editorTools) Plan(tool string) editor.ToolPlan {
	t, ok := hosttools.Get(tool)
	if !ok {
		return editor.ToolPlan{Tool: tool, Purpose: "this feature"}
	}
	p := editor.ToolPlan{Tool: t.Name, Purpose: t.Purpose, Doc: t.Doc}
	if in, ok := hosttools.Recipe(t); ok {
		p.Argv, p.Sudo = in.Argv, in.Sudo
	}
	if t.Name == "typescript-language-server" {
		p.Note = "It needs TypeScript 5 (TypeScript 7 has no tsserver): your project's node_modules/typescript, or a global typescript@5."
	}
	return p
}

// cliInstall is the package-manager command that installs a coding CLI.
func cliInstall(name string) (string, bool) {
	t, ok := hosttools.Get(name)
	if !ok {
		return "", false
	}
	in, ok := hosttools.Recipe(t)
	if !ok {
		return "", false
	}
	return in.String(), true
}

// runInstall runs an approved install in the user's own terminal: DHI
// steps aside (tea.ExecProcess), the user sees the output and answers any
// prompt (sudo), and DHI comes back after they press enter.
func runInstall(name string, done func(error) tea.Msg) tea.Cmd {
	t, ok := hosttools.Get(name)
	if !ok {
		return func() tea.Msg { return done(exec.ErrNotFound) }
	}
	in, ok := hosttools.Recipe(t)
	if !ok {
		return func() tea.Msg { return done(exec.ErrNotFound) }
	}
	script := `printf '\n$ %s\n\n' "$*"; "$@"; s=$?; printf '\n[exit %s — press enter to return to DHI] ' "$s"; read _; exit $s`
	cmd := exec.Command("/bin/sh", append([]string{"-c", script, "sh"}, in.Argv...)...)
	cmd.Env = hostenv.Env()
	if wd, err := os.Getwd(); err == nil {
		cmd.Dir = wd
	}
	return tea.ExecProcess(cmd, func(err error) tea.Msg {
		if err == nil {
			if _, found := hosttools.Find(t); !found {
				reloadEnv()
			}
		}
		return done(err)
	})
}
