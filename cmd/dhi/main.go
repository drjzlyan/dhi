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
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"charm.land/bubbletea/v2"

	"github.com/drjzlyan/dhi/internal/agentkit/bus"
	"github.com/drjzlyan/dhi/internal/agentkit/clirun"
	"github.com/drjzlyan/dhi/internal/agentkit/knowledge"
	"github.com/drjzlyan/dhi/internal/agentkit/library"
	"github.com/drjzlyan/dhi/internal/agentkit/manifest"
	"github.com/drjzlyan/dhi/internal/agentkit/memory"
	agentkitOrg "github.com/drjzlyan/dhi/internal/agentkit/org"
	agentkitRuntime "github.com/drjzlyan/dhi/internal/agentkit/runtime"
	"github.com/drjzlyan/dhi/internal/agentkit/tools"
	"github.com/drjzlyan/dhi/internal/autopilot"
	"github.com/drjzlyan/dhi/internal/boot"
	"github.com/drjzlyan/dhi/internal/doctor"
	"github.com/drjzlyan/dhi/internal/gitcore"
	"github.com/drjzlyan/dhi/internal/ideation"
	"github.com/drjzlyan/dhi/internal/review"
	"github.com/drjzlyan/dhi/internal/sandbox"
	"github.com/drjzlyan/dhi/internal/search"
	"github.com/drjzlyan/dhi/internal/settings"
	"github.com/drjzlyan/dhi/internal/tasks"
	"github.com/drjzlyan/dhi/internal/toolchain"
	"github.com/drjzlyan/dhi/internal/tui/app"
	"github.com/drjzlyan/dhi/internal/tui/surfaces/bootgate"
	"github.com/drjzlyan/dhi/internal/tui/surfaces/bootstrap"
	"github.com/drjzlyan/dhi/internal/tui/surfaces/editor"
	ideator "github.com/drjzlyan/dhi/internal/tui/surfaces/ideator"
	reviewer "github.com/drjzlyan/dhi/internal/tui/surfaces/reviewer"
	settingsview "github.com/drjzlyan/dhi/internal/tui/surfaces/settings"
	wsview "github.com/drjzlyan/dhi/internal/tui/surfaces/workspace"
	"github.com/drjzlyan/dhi/internal/unread"
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
	cwd, err := os.Getwd()
	if err != nil {
		fmt.Fprintln(os.Stderr, "dhi:", err)
		os.Exit(1)
	}
	userCfg, _ := settings.DefaultUserPath()
	toolRoot := ""
	if root, terr := toolchain.DefaultRoot(); terr == nil {
		toolRoot = root
	}

	// The boot audit resolves everything up front (F-011 / ADR-0011):
	// proceed, offer confirmation-gated installs, or block with named
	// reasons. No fallbacks live below this line.
	decision := boot.Audit(boot.Input{
		CWD:      cwd,
		ToolRoot: toolRoot,
		UserCfg:  userCfg,
		GOOS:     runtime.GOOS,
		LookPath: exec.LookPath,
	})

	// Surfaces are built against the validated state (a blocked boot
	// never renders them; the gate owns the body until quit).
	ws, _ := workspace.Load(cwd) // audit-guaranteed: nil ⇒ not a workspace

	wsCfg := ""
	if ws != nil {
		wsCfg = filepath.Join(ws.Root, workspace.DHIDir, "config.toml")
	}
	cfg, cfgErr := settings.Load(userCfg, wsCfg)
	if cfgErr != nil && decision.Block == "" {
		// Unreachable via the audit; refuse rather than guess.
		fmt.Fprintln(os.Stderr, "dhi:", cfgErr)
		os.Exit(1)
	}
	cfg.Apply()
	savePath := userCfg
	if ws != nil {
		savePath = wsCfg // nearest file wins for persistence
	}

	var edOpts []editor.Option
	var rgSearcher search.Searcher
	var termEnv []string
	var identityFn gitcore.IdentityFunc
	var gitRunner *gitcore.Runner
	if toolRoot != "" {
		mgr := toolchain.New(toolRoot)
		// Terminal sessions run with DHI's hermetic PATH. When the
		// toolchain is absent the drawer REFUSES to open a session
		// naming the fix — it never leaks the host PATH (ADR-0011).
		termEnv = mgr.Env(nil)
		edOpts = append(edOpts, editor.WithTermEnv(termEnv))
		// F-029: one identity resolver, read from the user's git config
		// (host path) through the hermetic git binary. It is nil when the
		// toolchain is absent, which makes every commit path refuse by
		// name rather than invent an author.
		identityFn = func(ctx context.Context) (gitcore.Identity, error) {
			return gitcore.ResolveIdentity(ctx, gitcore.NewRunner(mgr.GitBin(), mgr.GitIdentityEnv(nil)))
		}
		edOpts = append(edOpts, editor.WithIdentity(identityFn))
		// Hermetic git runner backs the served git_diff tool (M15 P1);
		// absent git leaves it nil and the tool refuses by name.
		if r, err := gitcore.ResolveRunner(mgr); err == nil {
			gitRunner = r
		}
		if _, err := os.Stat(filepath.Join(toolRoot, "bin", "rg")); err == nil {
			rgSearcher = search.Ripgrep{Bin: filepath.Join(toolRoot, "bin", "rg")}
		}
	}
	if rgSearcher == nil {
		rgSearcher = search.Refused{Reason: "search unavailable: rg shim not installed — run bootstrap (dhi doctor shows status)"}
	}
	edOpts = append(edOpts, editor.WithSearcher(rgSearcher))

	// Message bus + tasks store exist for every workspace; the worktree
	// seam lights up only when the hermetic git shim is installed.
	var messageBus *bus.Bus
	var agentRT *agentkitRuntime.Runtime
	var taskStore *tasks.Store
	var reviewSvc *review.Service
	var sessionStore *ideation.Store
	var unreadStore *unread.Store
	if ws != nil {
		messageBus = openBus(ws)
		if ts, err := tasks.Open(ws); err == nil {
			taskStore = ts
			taskStore.SetIdentity(identityFn)
			wireTaskSeam(ws, ts)
		}
		reviewSvc = openReviewService(ws)
		if ss, err := ideation.Open(ws); err == nil {
			sessionStore = ss
		} else {
			fmt.Fprintln(os.Stderr, "dhi: session store:", err)
		}
		// Read-mark store (F-017): one instance shared by the workspace
		// CHANNELS rail and the editor chat sidebar; a failed open leaves
		// it nil and doctor reports the reason.
		if messageBus != nil {
			if us, err := unread.Open(ws, messageBus); err != nil {
				fmt.Fprintln(os.Stderr, "dhi: unread store:", err)
			} else {
				unreadStore = us
			}
		}
		// Agent runtime (F-007): lights up only when a roster exists
		// under .dhi/agents/. Guards carry the audited OS-sandbox
		// adapter (nil here is impossible: the audit blocked first).
		if messageBus != nil {
			agentRT = newAgentRuntime(ws, messageBus, decision.Sandbox, termEnv, cfg.Engine, gitRunner, identityFn, taskStore, reviewSvc, rgSearcher)
			if agentRT != nil {
				edOpts = append(edOpts, editor.WithChat(agentRT))
			}
		}
	}

	// Agent roster changes go live without a restart (F-018): the
	// reload seam re-reads .dhi/agents and swaps the runtime atomically
	// (failures keep the previous roster and name the manifest error).
	reloadRoster := func() error {
		if ws == nil {
			return fmt.Errorf("not inside a workspace")
		}
		if agentRT == nil {
			return fmt.Errorf("agent runtime unavailable — changes apply on next launch")
		}
		roster, err := agentkitOrg.LoadRoster(ws)
		if err != nil {
			return err
		}
		return agentRT.Reload(roster)
	}
	var wsAuto *autopilot.Store // one store shared by workspace + settings
	var settingsDeps settingsview.Deps
	var cliRegistry *clirun.Registry
	if ws != nil {
		company, oerr := agentkitOrg.Load(ws.Root)
		if oerr == nil {
			// One autopilot store shared by the workspace (execution:
			// catch-up + ticks, ADR-0014 §5) and Settings (management UI).
			autoStore, aerr := autopilot.Open(ws)
			if aerr != nil {
				fmt.Fprintln(os.Stderr, "dhi: autopilot store:", aerr)
			} else {
				wsAuto = autoStore
			}
			cliRegistry = clirun.NewRegistry(exec.LookPath)
			settingsDeps = settingsview.Deps{
				WS:         ws,
				Org:        company,
				CLIs:       cliRegistry.Names(),
				Detect:     cliRegistry.Detect,
				Library:    library.Open(ws),
				Reload:     reloadRoster,
				Autopilots: autoStore,
				Bus:        messageBus,
				Runtime:    agentRT,
				Tasks:      taskStore,
			}
		}
	}
	var appRef *app.App
	var approvals *tools.Approvals
	if agentRT != nil {
		approvals = agentRT.Approvals()
	}
	// The chat sidebar shares the read-mark store (F-017): one read
	// state, two surfaces.
	if unreadStore != nil {
		edOpts = append(edOpts, editor.WithUnread(unreadStore))
	}
	a := app.New(version.Version,
		wsview.New(version.Version, ws, wsview.Deps{
			Bus:        messageBus,
			Runtime:    agentRT,
			Tasks:      taskStore,
			Roster:     agentRT,
			ReviewSvc:  reviewSvc,
			Approvals:  approvals,
			Unread:     unreadStore,
			Autopilots: wsAuto,
			OpenChat: func() bool {
				if appRef == nil {
					return false
				}
				return appRef.FocusEditorChat()
			},
			OpenReview: func(id string) bool {
				if appRef == nil {
					return false
				}
				return appRef.SelectReviewer(id)
			},
			OpenEditor: func(paths []string) bool {
				if appRef == nil {
					return false
				}
				return appRef.OpenInEditor(paths)
			},
		}),
		editor.New(version.Version, ws, edOpts...),
		ideator.New(version.Version, ws, ideator.Deps{
			Store: sessionStore,
			Bus:   messageBus,
			Crew:  agentRT,
		}),
		reviewer.New(version.Version, ws, reviewer.Deps{
			Service: reviewSvc,
			Bus:     messageBus,
			Crew:    agentRT,
			Tasks:   taskStore,
			OpenInEditor: func(paths []string) bool {
				if appRef == nil {
					return false
				}
				return appRef.OpenInEditor(paths)
			},
		}),
		settingsview.New(cfg, savePath, settingsDeps),
	)
	appRef = a

	// Gates, in strict order: a block never releases; missing pieces
	// offer the confirmation-gated install; first-run (no lockfile)
	// keeps the classic full bootstrap.
	switch {
	case decision.Block != "":
		a.SetGate(bootgate.New(version.Version, decision, nil))
	case len(decision.Offer) > 0:
		a.SetGate(bootgate.New(version.Version, decision, toolchain.New(toolRoot)))
	case needsBootstrap(toolRoot):
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

// openReviewService builds the review orchestration layer: TOML store
// always; worktree seam + diff runner light up with the hermetic git
// shim; PR inputs additionally need the host gh CLI.
func openReviewService(ws *workspace.Workspace) *review.Service {
	st, err := review.Open(ws)
	if err != nil {
		fmt.Fprintln(os.Stderr, "dhi: review store:", err)
		return nil
	}
	// gh is hermetic (ADR-0011): the shim path lights PR flows; absent
	// shim → the seam refuses with the named fix, never a host lookup.
	ghShim := ""
	if root, rerr := toolchain.DefaultRoot(); rerr == nil {
		if _, serr := os.Stat(filepath.Join(root, "bin", "gh")); serr == nil {
			ghShim = filepath.Join(root, "bin", "gh")
		}
	}
	gh := review.NewGHCLI(ghShim)
	svc := review.NewService(ws, st, nil, gh)
	svc.SetTokenFn(func(ctx context.Context) (string, error) {
		return gh.AuthToken(ctx)
	})
	root, err := toolchain.DefaultRoot()
	if err != nil {
		return svc
	}
	runner, rerr := gitcore.ResolveRunner(toolchain.New(root))
	if rerr != nil {
		return svc // pre-release: shim absent; diffs degrade visibly
	}
	svc = review.NewService(ws, st, runner, gh)
	svc.SetTokenFn(func(ctx context.Context) (string, error) {
		return gh.AuthToken(ctx)
	})
	st.SetWorktreeSeam(
		func(id, member, startpoint string) (string, error) {
			mem, ok := ws.Member(member)
			if !ok {
				return "", fmt.Errorf("unknown member %q", member)
			}
			rel := filepath.Join(review.Dir, id, member)
			dst := filepath.Join(ws.Root, rel)
			ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
			defer cancel()
			branch := "review/" + id
			if err := runner.WorktreeAdd(ctx, mem.Path, dst, branch, startpoint); err != nil {
				return "", err
			}
			return rel, nil
		},
		func(id, relPath string) error {
			mem, ok := ws.Member(filepath.Base(relPath))
			if !ok {
				return fmt.Errorf("member %q no longer registered", filepath.Base(relPath))
			}
			ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
			defer cancel()
			if err := runner.WorktreeRemove(ctx, mem.Path,
				filepath.Join(ws.Root, relPath), false); err != nil {
				return err
			}
			return runner.Prune(ctx, mem.Path)
		},
	)
	return svc
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
// ride along when their sidecar files parse; broken ones degrade. Agent
// memory + the knowledge base join the turn loop (M14 P1): persistent
// context in, review-gated contributions out.
func newAgentRuntime(ws *workspace.Workspace, b *bus.Bus, sb sandbox.Sandbox, cliEnv []string, defaultEngine string, gitRunner *gitcore.Runner, identityFn gitcore.IdentityFunc, taskStore *tasks.Store, reviewSvc *review.Service, kbSearcher search.Searcher) *agentkitRuntime.Runtime {
	roster, err := manifest.LoadDir(filepath.Join(ws.Root, workspace.DirAgents))
	if err != nil {
		fmt.Fprintln(os.Stderr, "dhi: agent roster:", err)
		return nil
	}
	if len(roster) == 0 {
		return nil
	}
	company, err := agentkitOrg.Load(ws.Root)
	if err != nil {
		fmt.Fprintln(os.Stderr, "dhi: org registry:", err)
	}
	var memStore *memory.Store
	var kbStore knowledge.KnowledgeStore
	if mem, kbErr := knowledge.Open(ws, knowledge.Review, kbSearcher); kbErr == nil {
		kbStore = mem
	} else {
		fmt.Fprintln(os.Stderr, "dhi: knowledge base:", kbErr)
	}
	memStore = memory.Open(ws)
	rt, err := agentkitRuntime.New(agentkitRuntime.Config{
		WS:        ws,
		Bus:       b,
		Approvals: tools.NewApprovals(),
		// Host agent CLIs (F-013/ADR-0012/0013): DHI ships no model
		// engine of its own; every rostered agent thinks through one of
		// these, resolved on the host PATH. Absent CLIs refuse roster
		// agents that declare them, and doctor names the installation —
		// never a fallback.
		CLIs: clirun.NewRegistry(exec.LookPath),
		// DefaultEngine is the workspace default (ADR-0019): an agent
		// that omits `engine` inherits it; with none set the agent
		// refuses by name at build time.
		DefaultEngine: defaultEngine,
		CLIEnv:        cliEnv,
		Tasks:         taskStore,
		Org:           company,
		Standards:     true,
		Sandbox:       sb,
		Memory:        memStore,
		Knowledge:     kbStore,
		Search:        kbSearcher,
		// F-020 pr_open: the review service opens task PRs; gh missing
		// refuses by name at dispatch.
		PR: func(ctx context.Context, member, branch, title, base string) (string, error) {
			if reviewSvc == nil {
				return "", fmt.Errorf("review service unavailable")
			}
			meta, err := reviewSvc.CreatePRForBranch(ctx, member, branch, title, base)
			if err != nil {
				return "", err
			}
			return fmt.Sprintf("PR #%d %s", meta.Number, meta.URL), nil
		},
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
		_, _ = os.Stdout.Write(data)
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
