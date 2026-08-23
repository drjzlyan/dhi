// Command dhi launches the DHI IDE. The binary intentionally exposes no
// project-management subcommands; everything happens inside the TUI
// (see ADR-0004). `dhi doctor` arrives with the toolchain milestone (M1).
//
// Five views: Workspace (boot) · Editor · Ideator · Reviewer · Settings.
package main

import (
	"fmt"
	"os"

	"charm.land/bubbletea/v2"

	"github.com/drjzlyan/dhi/internal/tui/app"
	"github.com/drjzlyan/dhi/internal/tui/surfaces/placeholder"
	"github.com/drjzlyan/dhi/internal/tui/surfaces/workspace"
	"github.com/drjzlyan/dhi/internal/version"
)

func main() {
	a := app.New(version.Version,
		workspace.New(version.Version),
		placeholder.New("editor", "Editor", "M2",
			"Files · modal buffers · repo-tabbed terminal · git view · chat sidebar · preview."),
		placeholder.New("ideator", "Ideator", "M6",
			"Ideation sessions: artifact navigation, preview, approval — no editing."),
		placeholder.New("reviewer", "Reviewer", "M5",
			"PR & worktree review: GitHub-style diffs, line comments, agent dispatch."),
		placeholder.New("settings", "Settings", "M2+",
			"Everything configurable: theme, keys, terminal, LSP, agents, sandbox."),
	)

	p := tea.NewProgram(a)
	if _, err := p.Run(); err != nil {
		fmt.Fprintln(os.Stderr, "dhi:", err)
		os.Exit(1)
	}
}
