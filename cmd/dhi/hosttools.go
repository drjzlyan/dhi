package main

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"runtime"

	tea "charm.land/bubbletea/v2"

	"github.com/drjzlyan/dhi/internal/gitcore"
	"github.com/drjzlyan/dhi/internal/hostenv"
	"github.com/drjzlyan/dhi/internal/hosttools"
	"github.com/drjzlyan/dhi/internal/setup"
	"github.com/drjzlyan/dhi/internal/tui/surfaces/editor"
	"github.com/drjzlyan/dhi/internal/tui/surfaces/wizard"
	"github.com/drjzlyan/dhi/internal/workspace"
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
		return editor.ToolPlan{Tool: tool}
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

// userGit is the user's own git with their environment (F-112); nil when
// git is not installed.
func userGit() *gitcore.Runner {
	r, err := gitcore.ResolveRunner(hostLook, hostenv.Env())
	if err != nil {
		return nil
	}
	return r
}

// userIdentity reads the user's git identity from their own config.
func userIdentity(ctx context.Context) (gitcore.Identity, error) {
	r := userGit()
	if r == nil {
		return gitcore.Identity{}, errors.New("git is not installed")
	}
	return gitcore.ResolveIdentity(ctx, r)
}

// setUserIdentity writes the identity to the user's ~/.gitconfig, after
// they confirmed it (F-083, F-112).
func setUserIdentity(ctx context.Context, id gitcore.Identity) error {
	r := userGit()
	if r == nil {
		return errors.New("git is not installed")
	}
	return gitcore.SetIdentity(ctx, r, id)
}

// setWorkspaceIdentity keeps the identity to root's member repos (each
// one's .git/config) and remembers it, so members added later get it from
// a repair (F-112). ~/.gitconfig is untouched.
func setWorkspaceIdentity(root string) func(ctx context.Context, id gitcore.Identity) error {
	return func(ctx context.Context, id gitcore.Identity) error {
		r := userGit()
		if r == nil {
			return errors.New("git is not installed")
		}
		w, err := workspace.Load(root)
		if err != nil {
			return err
		}
		for _, m := range w.Members() {
			if err := gitcore.SetLocalIdentity(ctx, r, m.Path, id); err != nil {
				return fmt.Errorf("%s: %w", m.Name, err)
			}
		}
		p := setup.WorkspaceStatePath(root)
		st, _ := setup.LoadState(p)
		st.LocalIdentity = &setup.Identity{Name: id.Name, Email: id.Email}
		return setup.SaveState(p, st)
	}
}

// setupTools are the tools the setup step lists: the core ones whole
// features need, then the language servers and the debugger.
var setupTools = []string{"git", "rg", "gh", "bwrap", "gopls", "typescript-language-server", "pyright",
	"bash-language-server", "yaml-language-server", "vscode-langservers-extracted", "rust-analyzer", "dlv"}

// toolRows is the tools step's view of the user's system (F-114).
func toolRows() []wizard.ToolRow {
	var rows []wizard.ToolRow
	for _, name := range setupTools {
		t, ok := hosttools.Get(name)
		if !ok || !t.ForThisOS(runtime.GOOS) {
			continue
		}
		row := wizard.ToolRow{Name: t.Name, Purpose: t.Purpose, Core: name == "git" || name == "rg" || name == "gh" || name == "bwrap"}
		if p, found := hosttools.Find(t); found {
			row.Version = p
		} else if in, ok := hosttools.Recipe(t); ok {
			row.Install, row.CanRun = in.String(), true
		} else {
			row.Install = t.Doc
		}
		rows = append(rows, row)
	}
	return rows
}

// pathOf is env's PATH.
func pathOf(env []string) string {
	p, _ := hostenv.Lookup(env, "PATH")
	return p
}
