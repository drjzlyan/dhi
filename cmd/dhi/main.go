// Command dhi launches the DHI IDE. Per ADR-0004 the CLI surface is
// minimal: no project-management subcommands — everything happens inside
// the TUI. The one exception is `dhi doctor [--json]`, which audits the
// hermetic install and workspace health.
//
// Five views: Workspace (boot) · Editor · Ideator · Reviewer · Settings.
package main

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"charm.land/bubbletea/v2"

	"github.com/drjzlyan/dhi/internal/agentkit/bus"
	"github.com/drjzlyan/dhi/internal/agentkit/manifest"
	agentkitOrg "github.com/drjzlyan/dhi/internal/agentkit/org"
	"github.com/drjzlyan/dhi/internal/agentkit/provider"
	"github.com/drjzlyan/dhi/internal/agentkit/runtime"
	"github.com/drjzlyan/dhi/internal/agentkit/tools"
	"github.com/drjzlyan/dhi/internal/doctor"
	"github.com/drjzlyan/dhi/internal/gitcore"
	"github.com/drjzlyan/dhi/internal/search"
	"github.com/drjzlyan/dhi/internal/settings"
	"github.com/drjzlyan/dhi/internal/tasks"
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
	var rgSearcher search.Searcher
	if root, err := toolchain.DefaultRoot(); err == nil {
		mgr := toolchain.New(root)
		// Terminal sessions run with DHI's hermetic PATH; search uses
		// the shim rg. Capabilities stay off when the toolchain is not
		// installed rather than falling back to host tools (ADR-0005).
		edOpts = append(edOpts, editor.WithTermEnv(mgr.Env(nil)))
		if _, err := os.Stat(filepath.Join(root, "bin", "rg")); err == nil {
			rgSearcher = search.Ripgrep{Bin: filepath.Join(root, "bin", "rg")}
			edOpts = append(edOpts, editor.WithSearcher(rgSearcher))
		}
	}

	// Message bus + tasks store exist for every workspace; the worktree
	// seam lights up only when the hermetic git shim is installed.
	var messageBus *bus.Bus
	var agentRT *runtime.Runtime
	var taskStore *tasks.Store
	if ws != nil {
		messageBus = openBus(ws)
		if ts, err := tasks.Open(ws); err == nil {
			taskStore = ts
			wireTaskSeam(ws, ts)
		}
		// Agent runtime (F-007): lights up only when a roster exists
		// under .dhi/agents/. A missing API key surfaces at first turn,
		// not boot; doctor warns about it.
		if messageBus != nil {
			agentRT = newAgentRuntime(ws, messageBus, rgSearcher)
			if agentRT != nil {
				edOpts = append(edOpts, editor.WithChat(agentRT))
			}
		}
	}

	a := app.New(version.Version,
		wsview.New(version.Version, ws, wsview.Deps{
			Bus:     messageBus,
			Runtime: agentRT,
			Tasks:   taskStore,
			Roster:  agentRT,
		}),
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

// wireTaskSeam connects task ChangeSets to hermetic-git worktrees when
// the shim exists; without it, attaching reports a visible error.
func wireTaskSeam(ws *workspace.Workspace, ts *tasks.Store) {
	root, err := toolchain.DefaultRoot()
	if err != nil {
		return
	}
	runner, err := gitcore.ResolveRunner(toolchain.New(root))
	if err != nil {
		return // pre-release: shim absent; doctor explains
	}
	ts.SetAttach(
		func(slug, member, branch, startpoint string) (string, error) {
			mem, ok := ws.Member(member)
			if !ok {
				return "", fmt.Errorf("unknown member %q", member)
			}
			if branch == "" {
				branch = "task/" + slug
			}
			rel := filepath.Join(tasks.Dir, slug, member)
			dst := filepath.Join(ws.Root, rel)
			ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
			defer cancel()
			if err := runner.WorktreeAdd(ctx, mem.Path, dst, branch, startpoint); err != nil {
				return "", err
			}
			return rel, nil
		},
		func(slug, relPath string) error {
			ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
			defer cancel()
			member := filepath.Base(relPath)
			if mem, ok := ws.Member(member); ok {
				if err := runner.WorktreeRemove(ctx, mem.Path,
					filepath.Join(ws.Root, relPath), false); err != nil {
					// Dirty trees refuse removal; surface and keep both.
					return err
				}
				return runner.Prune(ctx, mem.Path)
			}
			return fmt.Errorf("member %q no longer registered", member)
		},
	)
}

// openBus loads the workspace message store; nil disables CHANNELS
// (errors are reported but never block boot).
func openBus(ws *workspace.Workspace) *bus.Bus {
	b, err := bus.Open(ws)
	if err != nil {
		fmt.Fprintln(os.Stderr, "dhi: message bus:", err)
		return nil
	}
	return b
}

// newAgentRuntime wires the turn engine onto an existing bus; nil means
// no crew (no roster, or a broken one). Org + layered coding standards
// ride along when their sidecar files parse; broken ones degrade.
func newAgentRuntime(ws *workspace.Workspace, b *bus.Bus, srch search.Searcher) *runtime.Runtime {
	roster, err := manifest.LoadDir(filepath.Join(ws.Root, workspace.DirAgents))
	if err != nil {
		fmt.Fprintln(os.Stderr, "dhi: agent roster:", err)
		return nil
	}
	if len(roster) == 0 {
		return nil
	}
	envVar := "ANTHROPIC_API_KEY"
	for _, a := range roster {
		if a.EnvVar != "" {
			envVar = a.EnvVar
			break
		}
	}
	company, err := agentkitOrg.Load(ws.Root)
	if err != nil {
		fmt.Fprintln(os.Stderr, "dhi: org registry:", err)
	}
	rt, err := runtime.New(runtime.Config{
		WS:        ws,
		Bus:       b,
		Approvals: tools.NewApprovals(),
		Searcher:  srch,
		Provider:  provider.NewAnthropic("", os.Getenv(envVar)),
		Org:       company,
		Standards: true,
	}, roster)
	if err != nil {
		fmt.Fprintln(os.Stderr, "dhi: agent runtime:", err)
		return nil
	}
	return rt
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
