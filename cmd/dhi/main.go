// Command dhi launches the DHI IDE. Per ADR-0004 the CLI surface is
// minimal: no project-management subcommands — everything happens inside
// the TUI. The one exception is `dhi doctor [--json]`, which audits the
// hermetic install and workspace health.
//
// Five views: Workspace (boot) · Editor · Ideator · Reviewer · Settings.
package main

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"charm.land/bubbletea/v2"

	"github.com/drjzlyan/dhi/internal/doctor"
	"github.com/drjzlyan/dhi/internal/search"
	"github.com/drjzlyan/dhi/internal/settings"
	"github.com/drjzlyan/dhi/internal/toolchain"
	"github.com/drjzlyan/dhi/internal/tui/app"
	"github.com/drjzlyan/dhi/internal/tui/surfaces/bootstrap"
	"github.com/drjzlyan/dhi/internal/tui/surfaces/editor"
	"github.com/drjzlyan/dhi/internal/tui/surfaces/placeholder"
	settingsview "github.com/drjzlyan/dhi/internal/tui/surfaces/settings"
	wsview "github.com/drjzlyan/dhi/internal/tui/surfaces/workspace"
	"github.com/drjzlyan/dhi/internal/version"
	"github.com/drjzlyan/dhi/internal/workspace"
)

func main() {
	if len(os.Args) > 1 {
		switch os.Args[1] {
		case "doctor":
			os.Exit(runDoctor(os.Args[2:]))
		default:
			fmt.Fprintf(os.Stderr, "dhi: unknown command %q\n", os.Args[1])
			fmt.Fprintln(os.Stderr, "usage: dhi [doctor [--json]]")
			os.Exit(2)
		}
	}
	runTUI()
}

func runTUI() {
	var ws *workspace.Workspace
	if cwd, err := os.Getwd(); err == nil {
		ws, _ = workspace.Load(cwd) // not a workspace → empty-state editor
	}

	// Settings: defaults < user config < workspace config; theme applies
	// before any surface renders.
	userCfg, _ := settings.DefaultUserPath()
	wsCfg := ""
	if ws != nil {
		wsCfg = filepath.Join(ws.Root, workspace.DHIDir, "config.toml")
	}
	cfg, cfgErr := settings.Load(userCfg, wsCfg)
	if cfgErr != nil {
		fmt.Fprintln(os.Stderr, "dhi:", cfgErr)
	}
	cfg.Apply()
	savePath := userCfg
	if ws != nil {
		savePath = wsCfg // nearest file wins for persistence
	}

	var edOpts []editor.Option
	if root, err := toolchain.DefaultRoot(); err == nil {
		mgr := toolchain.New(root)
		// Terminal sessions run with DHI's hermetic PATH; search uses
		// the shim rg. Capabilities stay off when the toolchain is not
		// installed rather than falling back to host tools (ADR-0005).
		edOpts = append(edOpts, editor.WithTermEnv(mgr.Env(nil)))
		if _, err := os.Stat(filepath.Join(root, "bin", "rg")); err == nil {
			edOpts = append(edOpts, editor.WithSearcher(search.Ripgrep{
				Bin: filepath.Join(root, "bin", "rg"),
			}))
		}
	}

	a := app.New(version.Version,
		wsview.New(version.Version),
		editor.New(version.Version, ws, edOpts...),
		placeholder.New("ideator", "Ideator", "M6",
			"Ideation sessions: artifact navigation, preview, approval — no editing."),
		placeholder.New("reviewer", "Reviewer", "M5",
			"PR & worktree review: GitHub-style diffs, line comments, agent dispatch."),
		settingsview.New(cfg, savePath),
	)

	if cfgErr == nil && needsBootstrap(toolchainRoot()) {
		mgr := toolchain.New(toolchainRoot())
		// DHI_REGISTRY overrides the embedded manifest with a remote one
		// (loopback http allowed) for testing the pipeline end-to-end.
		a.SetGate(bootstrap.New(version.Version, mgr, os.Getenv("DHI_REGISTRY")))
	}

	p := tea.NewProgram(a)
	if _, err := p.Run(); err != nil {
		fmt.Fprintln(os.Stderr, "dhi:", err)
		os.Exit(1)
	}
}

// toolchainRoot resolves the hermetic prefix, falling back to a
// temp-dir-safe empty result when home cannot be located.
func toolchainRoot() string {
	root, err := toolchain.DefaultRoot()
	if err != nil {
		return filepath.Join(os.TempDir(), "dhi-unavailable")
	}
	return root
}

// needsBootstrap reports whether the hermetic prefix has never been
// installed (no lockfile). A corrupt lockfile boots normally; doctor
// surfaces it as a failure.
func needsBootstrap(root string) bool {
	_, err := os.Stat(filepath.Join(root, "lock.json"))
	return os.IsNotExist(err)
}

// runDoctor executes the shared check suite and prints a report.
// Exit codes: 0 healthy, 1 unhealthy, 2 usage error.
func runDoctor(args []string) int {
	asJSON := false
	for _, arg := range args {
		switch arg {
		case "--json":
			asJSON = true
		default:
			fmt.Fprintf(os.Stderr, "dhi doctor: unknown flag %q\n", arg)
			return 2
		}
	}

	toolRoot, err := toolchain.DefaultRoot()
	if err != nil {
		fmt.Fprintln(os.Stderr, "dhi doctor:", err)
		return 1
	}
	cwd, err := os.Getwd()
	if err != nil {
		fmt.Fprintln(os.Stderr, "dhi doctor:", err)
		return 1
	}

	report := doctor.Run(toolRoot, cwd)
	if asJSON {
		data, err := report.JSON()
		if err != nil {
			fmt.Fprintln(os.Stderr, "dhi doctor:", err)
			return 1
		}
		os.Stdout.Write(data)
	} else {
		fmt.Println("dhi doctor")
		fmt.Println(strings.Repeat("─", 40))
		for _, c := range report.Checks {
			line := fmt.Sprintf("%-5s %-22s %s", string(c.Status), c.Name, c.Detail)
			fmt.Println(strings.TrimRight(line, " "))
		}
		status := "unhealthy"
		if report.Healthy {
			status = "healthy"
		}
		fmt.Printf("\n%s (%d check(s))\n", status, len(report.Checks))
	}
	if !report.Healthy {
		return 1
	}
	return 0
}
