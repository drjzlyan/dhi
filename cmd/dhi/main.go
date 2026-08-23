// Command dhi launches the DHI IDE. The binary intentionally exposes no
// project-management subcommands; everything happens inside the TUI
// (see ADR-0004). `dhi doctor` arrives with the toolchain milestone (M1).
package main

import (
	"fmt"
	"os"

	"charm.land/bubbletea/v2"

	"github.com/drjzlyan/dhi/internal/tui/app"
	"github.com/drjzlyan/dhi/internal/tui/surfaces/home"
	"github.com/drjzlyan/dhi/internal/tui/surfaces/placeholder"
	"github.com/drjzlyan/dhi/internal/version"
)

func main() {
	a := app.New(version.Version,
		home.New(version.Version),
		placeholder.New("editor", "Editor", "M2", "Modal (vim-inspired) editing over workspace files."),
		placeholder.New("files", "Files", "M2", "Multi-repo navigation tree with fuzzy find."),
		placeholder.New("term", "Term", "M2", "PTY terminal panel, one tab per member repo."),
		placeholder.New("trees", "Trees", "M2", "Worktree-first git control: create, switch, stage, commit."),
		placeholder.New("agents", "Agents", "M3", "Roster of agents: skills, memory, presence, chat."),
		placeholder.New("tasks", "Tasks", "M4", "Team channels and task threads with agent assignment."),
		placeholder.New("review", "Review", "M5", "Worktree-based review: diffs, threads, agent review."),
		placeholder.New("market", "Market", "M6", "Browse and install agent packs from path or git."),
	)

	p := tea.NewProgram(a)
	if _, err := p.Run(); err != nil {
		fmt.Fprintln(os.Stderr, "dhi:", err)
		os.Exit(1)
	}
}
