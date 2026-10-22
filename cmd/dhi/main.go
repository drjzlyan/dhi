// Command dhi launches the DHI IDE. Per ADR-0004 the CLI surface is
// minimal: no project-management subcommands — everything happens inside
// the TUI. The one exception is `dhi doctor [--json]`, which audits the
// hermetic install and workspace health.
//
// Five views: Workspace (boot) · Editor · Ideator · Reviewer · Settings.
package main

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"os/exec"
	osuser "os/user"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/drjzlyan/dhi/internal/agentkit/brief"
	"github.com/drjzlyan/dhi/internal/agentkit/budget"
	"github.com/drjzlyan/dhi/internal/agentkit/bus"
	"github.com/drjzlyan/dhi/internal/agentkit/catalog"
	"github.com/drjzlyan/dhi/internal/agentkit/clirun"
	"github.com/drjzlyan/dhi/internal/agentkit/dhitools"
	"github.com/drjzlyan/dhi/internal/agentkit/episode"
	"github.com/drjzlyan/dhi/internal/agentkit/forge"
	"github.com/drjzlyan/dhi/internal/agentkit/knowledge"
	"github.com/drjzlyan/dhi/internal/agentkit/lessons"
	"github.com/drjzlyan/dhi/internal/agentkit/library"
	"github.com/drjzlyan/dhi/internal/agentkit/manifest"
	"github.com/drjzlyan/dhi/internal/agentkit/mcpbridge"
	"github.com/drjzlyan/dhi/internal/agentkit/mcpserver"
	"github.com/drjzlyan/dhi/internal/agentkit/memory"
	agentkitOrg "github.com/drjzlyan/dhi/internal/agentkit/org"
	"github.com/drjzlyan/dhi/internal/agentkit/registry"
	agentkitRuntime "github.com/drjzlyan/dhi/internal/agentkit/runtime"
	"github.com/drjzlyan/dhi/internal/agentkit/scopes"
	"github.com/drjzlyan/dhi/internal/agentkit/setprop"
	"github.com/drjzlyan/dhi/internal/agentkit/skillprop"
	"github.com/drjzlyan/dhi/internal/agentkit/standards"
	"github.com/drjzlyan/dhi/internal/agentkit/starter"
	"github.com/drjzlyan/dhi/internal/agentkit/tools"
	"github.com/drjzlyan/dhi/internal/autopilot"
	"github.com/drjzlyan/dhi/internal/boot"
	"github.com/drjzlyan/dhi/internal/conventions"
	"github.com/drjzlyan/dhi/internal/credstore"
	"github.com/drjzlyan/dhi/internal/doctor"
	"github.com/drjzlyan/dhi/internal/gitcore"
	"github.com/drjzlyan/dhi/internal/ideation"
	"github.com/drjzlyan/dhi/internal/langserver"
	"github.com/drjzlyan/dhi/internal/lsp"
	"github.com/drjzlyan/dhi/internal/redact"
	"github.com/drjzlyan/dhi/internal/repair"
	"github.com/drjzlyan/dhi/internal/review"
	"github.com/drjzlyan/dhi/internal/sandbox"
	"github.com/drjzlyan/dhi/internal/search"
	"github.com/drjzlyan/dhi/internal/settings"
	"github.com/drjzlyan/dhi/internal/setup"
	"github.com/drjzlyan/dhi/internal/tasks"
	"github.com/drjzlyan/dhi/internal/toolchain"
	"github.com/drjzlyan/dhi/internal/tui/app"
	"github.com/drjzlyan/dhi/internal/tui/kit"
	"github.com/drjzlyan/dhi/internal/tui/surfaces/bootgate"
	"github.com/drjzlyan/dhi/internal/tui/surfaces/bootstrap"
	"github.com/drjzlyan/dhi/internal/tui/surfaces/editor"
	"github.com/drjzlyan/dhi/internal/tui/surfaces/gatechain"
	ideator "github.com/drjzlyan/dhi/internal/tui/surfaces/ideator"
	"github.com/drjzlyan/dhi/internal/tui/surfaces/repairgate"
	reviewer "github.com/drjzlyan/dhi/internal/tui/surfaces/reviewer"
	settingsview "github.com/drjzlyan/dhi/internal/tui/surfaces/settings"
	"github.com/drjzlyan/dhi/internal/tui/surfaces/wizard"
	wsview "github.com/drjzlyan/dhi/internal/tui/surfaces/workspace"
	"github.com/drjzlyan/dhi/internal/tutorial"
	"github.com/drjzlyan/dhi/internal/unread"
	"github.com/drjzlyan/dhi/internal/version"
	"github.com/drjzlyan/dhi/internal/workspace"
	"github.com/drjzlyan/dhi/internal/worktrees"
)

func main() {
	// F-090/F-091: your own instructions, skills and registry, read in
	// every workspace.
	if cred, err := credstore.DefaultPath(); err == nil {
		cfgDir := filepath.Dir(cred)
		standards.UserPath = filepath.Join(cfgDir, "standards.toml")
		library.UserSkillsDir = filepath.Join(cfgDir, "skills")
		registry.UserDir = filepath.Join(cfgDir, "registry")
	}
	if len(os.Args) > 1 {
		switch os.Args[1] {
		case "doctor":
			os.Exit(runDoctor(os.Args[2:]))
		case "version", "--version", "-v":
			fmt.Println(version.String())
			return
		default:
			fmt.Fprintf(os.Stderr, "dhi: unknown command %q\n", os.Args[1])
			fmt.Fprintln(os.Stderr, "usage: dhi [doctor [--json] | version]")
			os.Exit(2)
		}
	}
	// A relaunch is a fresh process image's worth of state: services read
	// the workspace, settings and toolchain once at launch, so when setup
	// or the bootstrap changed any of them we start over rather than
	// hot-swap (F-043).
	for runTUI() {
	}
}

// runTUI runs one session and reports whether the program should be
// started again.
func runTUI() (relaunch bool) {
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

	// Coding CLIs resolve from PATH first, then DHI's managed folder
	// (ADR-0027); every consumer shares the one lookup.
	cliLook := clirun.ManagedLook(toolRoot, exec.LookPath)
	var toolBin []string // tool shims for bridged MCP servers (uvx/npx)
	mcpHome := ""        // their private HOME, inside the sandbox's writable prefix
	if toolRoot != "" {
		toolBin = []string{toolchain.New(toolRoot).ShimDir()}
		mcpHome = filepath.Join(toolRoot, "mcp-home")
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
		ReadOnly: []string{library.UserSkillsDir}, // F-091: your skills' scripts
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
	kit.SetKeyMap(cfg.Keys)                       // [keys] remaps (F-058): the shell translates, the chrome shows them
	kit.MacKeys = runtime.GOOS == "darwin"        // hints spell alt as ⌥ (F-101)
	kit.SetOSClipboard(toolchain.CopyToClipboard) // copy works where OSC 52 does not (ADR-0042)
	// Conventions are re-read when their files change, so Settings edits and
	// hand edits of .dhi/conventions.toml govern branch names, commits, PRs and
	// the agents' prompts without a restart (F-052).
	convLive := settings.LiveConventions(userCfg, wsCfg, cfg.Conventions)
	savePath := userCfg
	if ws != nil {
		savePath = wsCfg // nearest file wins for persistence
	}

	var edOpts []editor.Option
	// The editor's language table: built-ins plus [editor.languages]
	// overrides. Problems are surfaced by `dhi doctor`, never fatal here.
	langs, _ := langserver.Resolve(cfg.Editor.LanguageOverrides())
	edOpts = append(edOpts, editor.WithLanguages(langs))
	var rgSearcher search.Searcher
	var termEnv []string
	var agentEnv []string // termEnv + redirected build caches (F-076)
	var identityFn gitcore.IdentityFunc
	var gitRunner *gitcore.Runner
	var runRunner dhitools.CommandRunner
	// Workspace capability layer (F-030 P2); settings validates the
	// names/effects, so a parse miss leaves the value out (never a grant).
	wsScopes := scopes.Set{}
	for name, eff := range cfg.Scopes {
		sc, err1 := scopes.ParseScope(name)
		e, err2 := scopes.ParseEffect(eff)
		if err1 == nil && err2 == nil {
			wsScopes[sc] = e
		}
	}

	// editorBridge routes agent editor actions onto the UI loop
	// (ADR-0023); its program is attached once the TUI is created.
	editorBridge := &editorBridge{ws: ws}
	if toolRoot != "" {
		mgr := toolchain.New(toolRoot)
		// Terminal sessions run with DHI's hermetic PATH. When the
		// toolchain is absent the drawer REFUSES to open a session
		// naming the fix — it never leaks the host PATH (ADR-0011).
		termEnv = mgr.Env(nil)
		edOpts = append(edOpts, editor.WithTermEnv(termEnv))
		// Language servers run from the same hermetic shim dir and env.
		// Shut down on every return — the relaunch loop re-enters runTUI,
		// so a leak here would stack one gopls per setup relaunch.
		lspMgr := lsp.NewManager(mgr.ShimDir(), termEnv)
		lspMgr.SetManagedRoot(filepath.Join(toolRoot, "lsp"))
		defer lspMgr.ShutdownAll()
		edOpts = append(edOpts, editor.WithLSP(lspMgr),
			// Only ever called after the user confirmed `:lsp install` (F-050).
			editor.WithLSPInstaller(func(ctx context.Context, prefix string, pkgs []string) error {
				_, err := mgr.NPMInstall(ctx, prefix, pkgs[0], pkgs[1:]...)
				return err
			}))
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
		// Agents and DHI's own verify build with caches redirected into
		// the toolchain's writable folders (F-076); terminals keep home.
		agentEnv = append(append(append([]string(nil), termEnv...), mgr.BuildEnv()...), mgr.ToolEnv()...)
		runRunner = execRunner{env: runTmpEnv(agentEnv, ws), timeout: 5 * time.Minute, sandbox: guardWorkspace(decision.Sandbox, ws)}
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
	var mcpStore *mcpserver.Store
	if ws != nil {
		messageBus = openBus(ws)
		if ts, err := tasks.Open(ws); err == nil {
			taskStore = ts
			taskStore.SetIdentity(identityFn)
			wireTaskSeam(ws, ts, convLive.Get, decision.Worktrees)
		}
		reviewSvc = openReviewService(ws, func() conventions.Config { return convLive.Get() }, decision.Worktrees)
		if reviewSvc != nil && taskStore != nil {
			reviewSvc.SetPRVarsFunc(func(branch string) map[string]string {
				for _, tk := range taskStore.List() {
					for _, cs := range tk.ChangeSets {
						if cs.Branch == branch {
							return taskVars(taskStore, convLive.Get(), tk.Slug, cs.Member)
						}
					}
				}
				return nil
			})
		}
		mcpStore = mcpserver.Open(ws.Root)
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
			agentRT = newAgentRuntime(ws, messageBus, guardWorkspace(decision.Sandbox, ws), agentEnv, cfg.Engine, gitRunner, identityFn, sessionStore, runRunner, editorBridge, wsScopes, taskStore, reviewSvc, rgSearcher, mcpStore, convLive.Source(), cliLook, toolBin, mcpHome, decision.Worktrees,
				liveTuning(userCfg, wsCfg, cfg, identityFn))
			if agentRT != nil {
				// F-071: a held sub-card starts the moment its blockers are
				// done, whoever moved them.
				go agentRT.WatchTasks(context.Background())
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
			cliRegistry = clirun.NewRegistry(cliLook)
			settingsDeps = settingsview.Deps{
				WS:         ws,
				Org:        company,
				CLIs:       cliRegistry.Names(),
				Detect:     cliRegistry.Detect,
				Library:    library.Open(ws),
				Reload:     reloadRoster,
				Autopilots: autoStore,
				Bus:        messageBus,
				Tasks:      taskStore,
				Registry:   registry.New(ws),
				RunScript:  runRunner,
			}
			if agentRT != nil {
				// Only a live runtime: a typed-nil one inside the interface
				// would pass a nil check and panic on Handle.
				settingsDeps.Runtime = agentRT
				settingsDeps.Grants = agentRT.Approvals() // F-067 remembered grants
			}
			// F-068 BRAIN: what the crew learned; s shares a lesson to the KB
			// under its review policy.
			if credStore != nil {
				settingsDeps.Secrets = credStore // F-089 SECRETS: names only
			}
			settingsDeps.Lessons = lessons.Open(ws.Root)
			settingsDeps.Memory = memory.Open(ws)
			settingsDeps.Forge = forge.Open(ws.Root)          // F-069 PROPOSALS
			settingsDeps.SkillProps = skillprop.Open(ws.Root) // F-091 suggested skills
			settingsDeps.SetProps = setprop.Open(ws.Root)     // F-070 settings proposals
			settingsDeps.UserConfig, settingsDeps.WorkspaceConfig = userCfg, wsCfg
			settingsDeps.LearnConventions = func() conventions.Inference { return learnConventions(gitRunner, memberDirs(ws)) }
			// F-083: the git identity row reads, suggests and sets through
			// the same hermetic git as the wizard.
			settingsDeps.Identity = identityFn
			if identityFn != nil {
				mgr := toolchain.New(toolRoot)
				idRunner := func() *gitcore.Runner { return gitcore.NewRunner(mgr.GitBin(), mgr.GitIdentityEnv(nil)) }
				settingsDeps.SetIdentity = func(ctx context.Context, id gitcore.Identity) error {
					return gitcore.SetIdentity(ctx, idRunner(), id)
				}
				settingsDeps.SuggestIdentity = func(ctx context.Context) []gitcore.Suggestion {
					return suggestIdentity(ctx, idRunner(), memberDirs(ws), toolRoot)
				}
			}
			if kb, kerr := knowledge.Open(ws, knowledge.Review, rgSearcher); kerr == nil {
				settingsDeps.ShareLesson = func(l lessons.Lesson) (string, error) {
					author := l.Agent
					if author == "" {
						author = bus.Human
					}
					st, _, err := kb.Contribute(knowledge.Contribution{
						Title: truncateTitle(l.Text), Body: l.Text, Tags: append([]string{"lesson"}, l.Tags...),
						Author: author, Importance: 3,
					})
					return string(st), err
				}
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
	// The start page's actions that live outside the editor (F-097). The
	// setup checklist (F-098) is bound after the wizard wiring below.
	var checklist []kit.ChecklistItem
	var runStep func(id string)
	edOpts = append(edOpts, editor.WithStart(editor.StartDeps{
		Checklist: func() []kit.ChecklistItem { return checklist },
		RunStep: func(id string) {
			if runStep != nil {
				runStep(id)
			}
		},
		StartTour: func() {
			if appRef != nil {
				appRef.StartLesson("tour")
			}
		},
		NewTask: func() {
			if appRef != nil {
				appRef.OpenNewTask()
			}
		},
	}))
	// The Editor is view 1 (ADR-0041): DHI opens where you write code.
	a := app.New(version.Version,
		editor.New(version.Version, ws, edOpts...),
		wsview.New(version.Version, ws, wsview.Deps{
			Episodes:     openEpisodes(ws), // F-067 card pane: plan, verify, spend
			WorktreeHint: worktreeHint(decision.Worktrees),
			Bus:          messageBus,
			Runtime:      agentRT,
			Tasks:        taskStore,
			Roster:       agentRT,
			ReviewSvc:    reviewSvc,
			Approvals:    approvals,
			Secrets:      secretSaver(),
			Unread:       unreadStore,
			Autopilots:   wsAuto,
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
			Working:      agentRT.Working,
			AgentWorking: agentRT.AgentWorking,
			BranchFor: func(slug string) string {
				c := convLive.Get()
				b, err := conventions.ExpandBranch(c.Branch.Task, taskVars(taskStore, c, slug, ""))
				if err != nil {
					return ""
				}
				return b
			},
			CommitPrefix: func(slug string) string {
				c := convLive.Get()
				if c.Commit.Subject == "" || strings.Contains(c.Commit.Subject, "{type}") {
					return "" // a commit type is the author's call
				}
				v := taskVars(taskStore, c, slug, "")
				v["subject"] = ""
				return conventions.Render(c.Commit.Subject, v)
			},
			OpenWorktree: func(label, dir string) bool {
				if appRef == nil {
					return false
				}
				return appRef.OpenWorktreeInEditor(label, dir)
			},
			Sessions: sessionStore,
			OpenIdeator: func(id int) bool {
				if appRef == nil {
					return false
				}
				return appRef.SelectProposal(id)
			},
		}),
		ideator.New(version.Version, ws, ideator.Deps{
			Store: sessionStore,
			Bus:   messageBus,
			Crew:  agentRT,
			Tasks: taskStore,
			OpenInEditor: func(paths []string) bool {
				if appRef == nil {
					return false
				}
				return appRef.OpenInEditor(paths)
			},
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
	if agentRT != nil {
		a.SetActivity(agentRT.ActiveCount)
	}

	// Tutorials (F-048): lessons in the palette; progress lives in the user
	// setup state, so a finished lesson shows a check everywhere.
	startLesson := func(slug string) {
		if t, ok := tutorial.Get(slug); ok && appRef != nil {
			appRef.StartTutorial(t)
		}
	}
	a.SetTutorials(tutorial.All(), app.TutorialHooks{
		Done: func(slug string) bool {
			p, err := setup.UserStatePath()
			if err != nil {
				return false
			}
			st, _ := setup.LoadState(p)
			return st.Has("tutorial:" + slug)
		},
		OnEnd: func(slug string, finished bool) {
			if !finished {
				return
			}
			if p, err := setup.UserStatePath(); err == nil {
				st, _ := setup.LoadState(p)
				st.Mark("tutorial:"+slug, time.Now())
				_ = setup.SaveState(p, st)
			}
		},
	})
	// A tour queued by a wizard that relaunched starts now.
	if p, err := tourMarker(); err == nil {
		if _, serr := os.Stat(p); serr == nil {
			_ = os.Remove(p)
			startLesson("tour")
		}
	}

	// Setup wizard (F-043). It auto-runs for a first-time user only; every
	// other launch reaches it from the palette. Each run gets a fresh Env
	// so a palette re-run starts from the state on disk.
	var wizards []*wizard.Model
	newWizard := func() *wizard.Model {
		env := newSetupEnv(cwd, ws, userCfg, toolRoot, cfg.Conventions, cfg.Engine, identityFn, cliLook,
			func() { startLesson("tour") })
		w := wizard.New(env, loadSetupState(ws), wizard.DefaultSteps(env, version.Version)...)
		wizards = append(wizards, w)
		return w
	}
	if decision.Block == "" {
		a.SetSetupGate(func() app.Gate { return newWizard() })
	}
	if !cfg.ThemeSet {
		// No theme chosen anywhere: match the terminal (light terminals
		// get the light theme) instead of forcing dark on everyone.
		a.SetBackgroundHook(cfg.FollowBackground)
	}
	// Setup is a checklist, not a gate (ADR-0041, F-098): the start page
	// and the status pill list the steps that apply; each runs on its own.
	// Applies can be costly (CLI detection), so the list is computed at
	// launch and after each step, never per frame.
	refreshChecklist := func() {
		if decision.Block != "" {
			checklist = nil
			return
		}
		env := newSetupEnv(cwd, ws, userCfg, toolRoot, cfg.Conventions, cfg.Engine, identityFn, cliLook,
			func() { startLesson("tour") })
		checklist = wizard.Checklist(env, version.Version, loadSetupState(ws))
	}
	refreshChecklist()
	runStep = func(id string) {
		if decision.Block != "" || appRef == nil {
			return
		}
		env := newSetupEnv(cwd, ws, userCfg, toolRoot, cfg.Conventions, cfg.Engine, identityFn, cliLook,
			func() { startLesson("tour") })
		before := pendingCount(checklist)
		w := wizard.Single(env, version.Version, loadSetupState(ws), id, func() {
			refreshChecklist()
			if before > 0 && pendingCount(checklist) == 0 {
				appRef.Toast(kit.Toast{Kind: kit.ToastSuccess, Title: "You're set up", Detail: "redo any step from the start page"})
			}
		})
		if w == nil {
			// Done, and its step no longer applies here (the workspace
			// exists): say so instead of ignoring the click.
			refreshChecklist()
			appRef.Toast(kit.Toast{Kind: kit.ToastInfo, Title: wizard.ChecklistTitle(id) + " is already set up",
				Detail: "change it in Settings (5)"})
			return
		}
		wizards = append(wizards, w)
		appRef.QueueGate(w)
	}
	a.SetSetup(func() []kit.ChecklistItem { return checklist }, runStep)

	// Gates, in strict order: a block never releases; missing pieces
	// offer the confirmation-gated install; first-run (no lockfile)
	// keeps the classic full bootstrap. The setup wizard follows, then a
	// tail that relaunches when the toolchain changed under the services.
	var gates []gatechain.Gate
	switch {
	case decision.Block != "":
		gates = append(gates, bootgate.New(version.Version, decision, nil))
	case len(decision.Offer) > 0:
		gates = append(gates, bootgate.New(version.Version, decision, toolchain.New(toolRoot)))
	case needsBootstrap(toolRoot):
		mgr := toolchain.New(toolchainRoot())
		// DHI_REGISTRY overrides the embedded manifest with a remote one
		// (loopback http allowed) for testing the pipeline end-to-end; that
		// path stays the unattended classic bootstrap.
		if os.Getenv("DHI_REGISTRY") != "" {
			gates = append(gates, bootstrap.New(version.Version, mgr, os.Getenv("DHI_REGISTRY")))
			break
		}
		// A real first run asks before downloading ~150 MB: what, how big,
		// where it goes (F-044). Skipping is allowed; capabilities then
		// refuse by name until it is installed.
		first := decision
		first.FirstRun = true
		first.OfferRoot = mgr.Root()
		first.Offer, first.OfferBytes, first.OfferTotal = boot.FirstRunOffer()
		if len(first.Offer) == 0 {
			gates = append(gates, bootstrap.New(version.Version, mgr, ""))
			break
		}
		gates = append(gates, bootgate.New(version.Version, first, mgr))
	}
	// Self-healing (F-085, ADR-0038): safe repairs run now and are
	// reported; the rest wait on the user's yes in the same card.
	if decision.Block == "" && ws != nil {
		if g := repairGate(ws, gitRunner); g != nil {
			gates = append(gates, g)
		}
	}
	toolsAtLaunch := toolsFingerprint(toolRoot)
	var tail *gatechain.RelaunchGate
	if decision.Block == "" && len(gates) > 0 {
		tail = gatechain.Relaunch(func() bool { return toolsFingerprint(toolRoot) != toolsAtLaunch })
		gates = append(gates, tail)
	}
	switch len(gates) {
	case 0:
	case 1:
		a.SetGate(gates[0])
	default:
		a.SetGate(gatechain.New(gates...))
	}

	p := tea.NewProgram(a)
	editorBridge.prog = p
	if _, err := p.Run(); err != nil {
		fmt.Fprintln(os.Stderr, "dhi:", err)
		os.Exit(1)
	}
	for _, w := range wizards {
		if w.NeedsRelaunch() {
			return true
		}
	}
	return tail != nil && tail.NeedsRelaunch()
}

// tourMarker is the file a relaunching wizard leaves to say "start the
// tour next launch".
func tourMarker() (string, error) {
	p, err := setup.UserStatePath()
	if err != nil {
		return "", err
	}
	return filepath.Join(filepath.Dir(p), "tour.pending"), nil
}

// toolsFingerprint captures which hermetic tools exist: the services read
// them once at launch (git runner, ripgrep searcher, terminal env), so a
// change means they are stale.
func toolsFingerprint(root string) string {
	if root == "" {
		return ""
	}
	var b strings.Builder
	for _, rel := range []string{"git", "rg", "go", "gopls", "dlv"} {
		if _, err := os.Stat(filepath.Join(root, "bin", rel)); err == nil {
			b.WriteString(rel + ";")
		}
	}
	return b.String()
}

// pendingCount counts checklist items still to do.
func pendingCount(items []kit.ChecklistItem) int {
	n := 0
	for _, it := range items {
		if it.State == kit.ItemPending {
			n++
		}
	}
	return n
}

// loadSetupState reads the progress the wizard resumes from: your steps
// from the user state, the workspace's own steps from its state only, so
// a step answered (or skipped) in one workspace is still owed in another.
func loadSetupState(ws *workspace.Workspace) setup.State {
	var user, here setup.State
	if p, err := setup.UserStatePath(); err == nil {
		user, _ = setup.LoadState(p)
	}
	if ws != nil {
		here, _ = setup.LoadState(setup.WorkspaceStatePath(ws.Root))
	}
	return setup.Scoped(user, here)
}

// newSetupEnv wires the wizard to real services. Funcs stay nil when the
// capability is absent (no toolchain root → no git), and the step then
// explains the manual fix instead of guessing.
func newSetupEnv(cwd string, ws *workspace.Workspace, userCfg, toolRoot string,
	conv conventions.Config, engine string, identityFn gitcore.IdentityFunc,
	cliLook func(string) (string, error), startTour func()) *wizard.Env {
	env := &wizard.Env{
		Version: version.Version, CWD: cwd,
		Discover: setup.DiscoverMembers, InitWorkspace: setup.InitWorkspace,
		Identity: identityFn, Conventions: conv, Engine: engine,
	}
	if ws != nil {
		env.Root = ws.Root
	}
	if toolRoot != "" {
		mgr := toolchain.New(toolRoot)
		env.SetIdentity = func(ctx context.Context, id gitcore.Identity) error {
			return gitcore.SetIdentity(ctx, gitcore.NewRunner(mgr.GitBin(), mgr.GitIdentityEnv(nil)), id)
		}
		env.SuggestIdentity = func(ctx context.Context) []gitcore.Suggestion {
			dirs := []string{cwd}
			if env.Root != "" {
				if w, err := workspace.Load(env.Root); err == nil {
					dirs = memberDirs(w)
				}
			}
			return suggestIdentity(ctx, gitcore.NewRunner(mgr.GitBin(), mgr.GitIdentityEnv(nil)), dirs, toolRoot)
		}
	}
	// You step (F-081): the profile is personal, so it lives in the user config.
	if c, err := settings.Load(userCfg, ""); err == nil {
		env.Profile = c.Profile
	}
	env.ProfileSaved = settings.LayerHas(userCfg, "profile")
	if userCfg != "" {
		env.SaveProfile = func(p settings.Profile) error { return settings.SaveProfile(userCfg, p) }
	}
	env.SaveConventions = func(root string, c conventions.Config) error {
		path := settings.ConventionsPath(userCfg)
		if root != "" {
			path = filepath.Join(root, workspace.DHIDir, settings.ConventionsFile)
		}
		return settings.SaveConventions(path, c)
	}
	// Team step: everything resolves the workspace from env.Root at call
	// time, because the wizard itself may have just created it.
	env.RosterCount = func() int {
		if env.Root == "" {
			return 0
		}
		roster, err := manifest.LoadDir(filepath.Join(env.Root, workspace.DirAgents))
		if err != nil {
			return 1 // a broken roster is not an empty one: don't offer to add to it
		}
		return len(roster)
	}
	// Detection asks every CLI for its version (seconds, live): the CLI and
	// team steps share one answer per run, refreshed after an install.
	var detected map[string]string
	detect := func() map[string]string {
		if detected == nil {
			detected = clirun.NewRegistry(cliLook).Detect()
		}
		return detected
	}
	env.DetectCLIs = detect
	env.CLIPrefix = func(name string) string { return clirun.ManagedDir(toolRoot, name) }
	env.CLIStatus = func() []wizard.CLIRow {
		reg := clirun.NewRegistry(cliLook)
		detected := detect()
		var rows []wizard.CLIRow
		for _, c := range reg.All() {
			row := wizard.CLIRow{Name: c.Name, Version: detected[c.Name], Tested: c.Tested}
			row.Plan, _ = clirun.PlanFor(c.Name)
			if row.Version != "" {
				row.Verdict, row.Why = clirun.Assess(c.Tested, row.Version)
			}
			rows = append(rows, row)
		}
		return rows
	}
	if toolRoot != "" {
		mgr := toolchain.New(toolRoot)
		env.InstallCLI = func(ctx context.Context, name string) error {
			plan, ok := clirun.PlanFor(name)
			if !ok || plan.Method != clirun.MethodNPM {
				return fmt.Errorf("%s is not installed by DHI (guided manual install)", name)
			}
			_, err := mgr.NPMInstall(ctx, clirun.ManagedDir(toolRoot, name), plan.Package)
			detected = nil // the next look sees what was installed
			return err
		}
	}
	env.ApplyTeam = func(slug string) (starter.Result, error) {
		tpl, ok := starter.Get(slug)
		if !ok {
			return starter.Result{}, fmt.Errorf("unknown starter team %q", slug)
		}
		w, err := workspace.Load(env.Root)
		if err != nil {
			return starter.Result{}, err
		}
		o, err := agentkitOrg.Load(env.Root)
		if err != nil {
			return starter.Result{}, err
		}
		return starter.Apply(w, o, library.Open(w), tpl)
	}
	env.LearnConventions = func() conventions.Inference {
		if env.Root == "" {
			return conventions.Inference{}
		}
		w, err := workspace.Load(env.Root)
		if err != nil {
			return conventions.Inference{}
		}
		var r *gitcore.Runner
		if toolRoot != "" {
			r, _ = gitcore.ResolveRunner(toolchain.New(toolRoot))
		}
		return learnConventions(r, memberDirs(w))
	}
	env.DetectBrief = func() brief.Brief {
		var dirs []string
		if w, err := workspace.Load(env.Root); err == nil {
			for _, mb := range w.Members() {
				dirs = append(dirs, mb.Path)
			}
		}
		return brief.Detect(env.Root, dirs)
	}
	env.SaveBrief = func(b brief.Brief) error { return brief.Save(env.Root, b) }
	env.HasBrief = func() bool { _, ok, err := brief.Load(env.Root); return ok || err != nil }
	env.ComposeTeam = func(c starter.Counts) ([]starter.Result, error) {
		w, err := workspace.Load(env.Root)
		if err != nil {
			return nil, err
		}
		o, err := agentkitOrg.Load(env.Root)
		if err != nil {
			return nil, err
		}
		opt := composeOptions(w, o)
		opt.Persona = env.Profile.Principal("", "").Persona() // how you read sets their voice
		tpls, err := starter.Compose(c, opt)
		if err != nil {
			return nil, err
		}
		return starter.ApplyAll(w, o, library.Open(w), tpls)
	}
	// Integrations step: credentials live in the user-level file, the card in
	// the workspace; both resolve env.Root at call time.
	env.CredentialsPath, _ = credstore.DefaultPath()
	env.IntegrationRows = func() []wizard.IntegrationRow {
		var rows []wizard.IntegrationRow
		for _, e := range catalog.Entries() {
			rows = append(rows, wizard.IntegrationRow{
				Entry: e, State: catalog.Status(env.Root, e, mcpbridge.DefaultLookup)})
		}
		return rows
	}
	env.SetupIntegration = func(slug string, inputs map[string]string, who string) ([]string, error) {
		e, ok := catalog.Get(slug)
		if !ok {
			return nil, fmt.Errorf("unknown integration %q", slug)
		}
		st, err := credstore.Open()
		if err != nil {
			return nil, err
		}
		if err := catalog.Install(env.Root, st, e, inputs); err != nil {
			return nil, err
		}
		w, err := workspace.Load(env.Root)
		if err != nil {
			return nil, err
		}
		o, err := agentkitOrg.Load(env.Root)
		if err != nil {
			return nil, err
		}
		roster, err := agentkitOrg.LoadRoster(w)
		if err != nil {
			return nil, err
		}
		var ids []string
		switch who {
		case "every employee":
			for _, a := range roster {
				ids = append(ids, a.ID)
			}
		case "the team lead only":
			for _, t := range o.Teams() { // teams are sorted; the first agent lead wins
				if t.Lead != "" && t.Lead != agentkitOrg.Human {
					ids = append(ids, t.Lead)
					break
				}
			}
		}
		if len(ids) == 0 {
			return nil, nil
		}
		return catalog.Enable(w, o, e, ids)
	}
	// Tour: begin in this process when the wizard does not relaunch, else
	// leave a marker for the next launch.
	env.QueueTour = func() error {
		p, err := tourMarker()
		if err != nil {
			return err
		}
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			return err
		}
		return os.WriteFile(p, []byte("tour\n"), 0o644)
	}
	env.StartTour = startTour
	// The default engine is your CLI, so it is saved for every workspace
	// (F-090): a new workspace starts with it, and this one's own copy is
	// dropped so yours governs. Settings can still pin a workspace.
	env.SetEngine = func(engine string) error {
		c, err := settings.Load(userCfg, "")
		if err != nil {
			return err
		}
		before := settings.Snapshot(c)
		c.Engine = engine
		if _, err := settings.SaveChanged(userCfg, before, c); err != nil {
			return err
		}
		if env.Root != "" {
			return settings.RemoveKeys(filepath.Join(env.Root, workspace.DHIDir, "config.toml"), []string{"engine"})
		}
		return nil
	}
	// F-090: the instructions step — yours (every workspace) or this
	// workspace's standing instructions.
	if standards.UserPath != "" {
		env.Instructions = func() (mine, here []string) {
			mine, _ = standards.LoadUser()
			if env.Root != "" {
				if snap, err := standards.Inspect(env.Root); err == nil {
					here = snap.Workspace
				}
			}
			return mine, here
		}
		env.SaveInstructions = func(lines []string, everywhere bool) error {
			if everywhere || env.Root == "" {
				return standards.SaveUser(lines)
			}
			snap, err := standards.Inspect(env.Root)
			if err != nil {
				return err
			}
			return standards.Save(env.Root, lines, snap.Teams, snap.Agents)
		}
	}
	env.Persist = func(root string, st setup.State) error {
		var firstErr error
		if p, err := setup.UserStatePath(); err == nil {
			firstErr = setup.SaveState(p, st)
		}
		if root != "" {
			if err := setup.SaveState(setup.WorkspaceStatePath(root), st); err != nil && firstErr == nil {
				firstErr = err
			}
		}
		return firstErr
	}
	return env
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
func openReviewService(ws *workspace.Workspace, conv func() conventions.Config, wt worktrees.Layout) *review.Service {
	prBody := func() string { return conv().PR.Body }
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
	svc.SetPRBodyFunc(prBody)
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
	svc.SetPRBodyFunc(prBody)
	svc.SetTokenFn(func(ctx context.Context) (string, error) {
		return gh.AuthToken(ctx)
	})
	st.SetWorktreeSeam(
		func(id, member, startpoint string) (string, error) {
			mem, ok := ws.Member(member)
			if !ok {
				return "", fmt.Errorf("unknown member %q", member)
			}
			rel, dst := wt.Review(id, member)
			ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
			defer cancel()
			branch, err := conventions.ExpandBranch(conv().Branch.Review, branchVars(id))
			if err != nil {
				return "", err
			}
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
func wireTaskSeam(ws *workspace.Workspace, ts *tasks.Store, conv func() conventions.Config, wt worktrees.Layout) {
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
			c := conv()
			vars := taskVars(ts, c, slug, member)
			if branch == "" {
				var err error
				if branch, err = conventions.ExpandBranch(c.Branch.Task, vars); err != nil {
					return "", err
				}
			}
			sub, err := conventions.ExpandWorktree(c.Worktree.Task, vars)
			if err != nil {
				return "", err
			}
			rel, dst := wt.TaskAt(sub)
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
	b.Redact = secretRedactor.Func()
	return b
}

// credStore holds the secrets agents request by name (F-089) and the
// integration credentials (F-047); nil when the user config dir cannot
// be located.
var credStore = func() *credstore.Store {
	st, err := credstore.Open()
	if err != nil {
		return nil
	}
	return st
}()

// secretStore and secretSaver hand credStore to the runtime and the UI
// as interfaces, nil (not a typed nil) when there is no store.
func secretStore() agentkitRuntime.SecretStore {
	if credStore == nil {
		return nil
	}
	return credStore
}

func secretSaver() wsview.SecretSaver {
	if credStore == nil {
		return nil
	}
	return credStore
}

// secretRedactor keeps stored credential values out of chat, tool
// results, run records and transcripts (F-088). It re-reads the store
// when the file changes, so a secret added mid-session is covered.
var secretRedactor = func() *redact.Redactor {
	if credStore == nil {
		return nil
	}
	return redact.New(credStore.Values, credStore.Stamp)
}()

// newAgentRuntime wires the turn engine onto an existing bus; nil means
// no crew (no roster, or a broken one). Org + layered coding standards
// ride along when their sidecar files parse; broken ones degrade. Agent
// memory + the knowledge base join the turn loop (M14 P1): persistent
// context in, review-gated contributions out.
// editorBridge implements dhitools.EditorAPI (ADR-0023): it resolves
// VPaths through the workspace jail, sends an app.EditorRequest onto the
// UI loop, and blocks for the reply (or ctx). A nil program refuses.
type editorBridge struct {
	ws   *workspace.Workspace
	prog interface{ Send(tea.Msg) }
}

func (b *editorBridge) resolve(paths []string) ([]string, error) {
	if b.ws == nil {
		return nil, fmt.Errorf("editor unavailable (no workspace)")
	}
	out := make([]string, 0, len(paths))
	for _, p := range paths {
		v, err := workspace.ParseVPath(p)
		if err != nil {
			return nil, err
		}
		abs, err := b.ws.Resolve(v)
		if err != nil {
			return nil, err
		}
		out = append(out, abs)
	}
	return out, nil
}

func (b *editorBridge) request(ctx context.Context, req app.EditorRequest) (app.EditorReply, error) {
	if b.prog == nil {
		return app.EditorReply{}, fmt.Errorf("editor unavailable (UI not started)")
	}
	req.Reply = make(chan app.EditorReply, 1)
	b.prog.Send(req)
	select {
	case r := <-req.Reply:
		if r.Err != "" {
			return r, fmt.Errorf("%s", r.Err)
		}
		return r, nil
	case <-ctx.Done():
		return app.EditorReply{}, ctx.Err()
	}
}

func (b *editorBridge) Open(ctx context.Context, paths []string) error {
	abs, err := b.resolve(paths)
	if err != nil {
		return err
	}
	_, err = b.request(ctx, app.EditorRequest{Op: "open", Paths: abs})
	return err
}

func (b *editorBridge) Reveal(ctx context.Context, path string) error {
	abs, err := b.resolve([]string{path})
	if err != nil {
		return err
	}
	_, err = b.request(ctx, app.EditorRequest{Op: "reveal", Paths: abs})
	return err
}

func (b *editorBridge) Apply(ctx context.Context, path, old, new string, all bool) error {
	abs, err := b.resolve([]string{path})
	if err != nil {
		return err
	}
	if len(abs) == 0 {
		return fmt.Errorf("path is required")
	}
	_, err = b.request(ctx, app.EditorRequest{Op: "apply", Path: abs[0], Old: old, New: new, All: all})
	return err
}

// Context and Propose make the bridge a dhitools.PairAPI (F-038).
func (b *editorBridge) Context(ctx context.Context) (string, error) {
	r, err := b.request(ctx, app.EditorRequest{Op: "context"})
	return r.Text, err
}

func (b *editorBridge) Propose(ctx context.Context, path, old, new, note, from string) error {
	abs, err := b.resolve([]string{path})
	if err != nil {
		return err
	}
	if len(abs) == 0 {
		return fmt.Errorf("path is required")
	}
	_, err = b.request(ctx, app.EditorRequest{Op: "propose", Path: abs[0], Old: old, New: new, Note: note, From: from})
	return err
}

// DebugState makes the bridge a dhitools.DebugAPI (F-039).
func (b *editorBridge) DebugState(ctx context.Context) (string, error) {
	r, err := b.request(ctx, app.EditorRequest{Op: "debug"})
	return r.Text, err
}

func (b *editorBridge) LSP(ctx context.Context, op, path string, line, col int, arg string) (string, error) {
	abs, err := b.resolve([]string{path})
	if err != nil {
		return "", err
	}
	if len(abs) == 0 {
		return "", fmt.Errorf("path is required")
	}
	r, err := b.request(ctx, app.EditorRequest{Op: "lsp", LSPOp: op, Path: abs[0], Line: line, Col: col, Arg: arg})
	return r.Text, err
}

// execRunner implements dhitools.CommandRunner: a hermetic-env child
// process (no shell) with a wall-clock timeout and capped output. The
// allowlist policy lives in dhitools; this only executes vetted argv.
type execRunner struct {
	env     []string
	timeout time.Duration
	sandbox sandbox.Sandbox
}

// runTmpEnv points the run/verify runner's TMPDIR at <ws>/.dhi/tmp/run:
// the host's temp dir is outside the sandbox's writable roots, so
// compilers and test runners would fail on their first temp file (F-086).
func runTmpEnv(env []string, ws *workspace.Workspace) []string {
	if ws == nil {
		return env
	}
	dir := filepath.Join(ws.Root, workspace.DirTmp, "run")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return env
	}
	out := make([]string, 0, len(env)+1)
	for _, kv := range env {
		if !strings.HasPrefix(kv, "TMPDIR=") {
			out = append(out, kv)
		}
	}
	return append(out, "TMPDIR="+dir)
}

func (r execRunner) Run(ctx context.Context, dir string, argv []string, allowNetwork bool) (string, error) {
	return r.RunEnv(ctx, dir, argv, allowNetwork, nil)
}

// RunEnv runs with extra env: the secrets the calling agent was granted
// (F-089), for this command only.
func (r execRunner) RunEnv(ctx context.Context, dir string, argv []string, allowNetwork bool, extra []string) (string, error) {
	ctx, cancel := context.WithTimeout(ctx, r.timeout)
	defer cancel()
	// F-030 P2: DHI-served children are network-denied by default via the
	// OS sandbox; a Noop/absent adapter cannot deny, so we refuse rather
	// than silently leak (network is a declared scope).
	if !allowNetwork {
		np, ok := r.sandbox.(sandbox.NetworkPolicy)
		if !ok {
			return "", fmt.Errorf("run: network denied by scope but no OS sandbox can enforce it")
		}
		wrapped, werr := np.WrapNetwork(argv, false)
		if werr != nil {
			return "", werr
		}
		argv = wrapped
	}
	cmd := exec.CommandContext(ctx, argv[0], argv[1:]...)
	cmd.Dir = dir
	cmd.Env = append(append([]string(nil), r.env...), extra...)
	cmd.WaitDelay = 500 * time.Millisecond
	var buf bytes.Buffer
	cmd.Stdout, cmd.Stderr = &buf, &buf
	err := cmd.Run()
	out := buf.String()
	if len(out) > 1<<20 {
		out = out[:1<<20] + "\n(output truncated)\n"
	}
	return out, err
}

// settingCheck validates an agent's proposed settings change against the
// full layer stack (F-070), on the workspace layer unless "user".
func settingCheck(ws *workspace.Workspace) dhitools.SettingCheck {
	userCfg, _ := settings.DefaultUserPath()
	wsCfg := filepath.Join(ws.Root, workspace.DHIDir, "config.toml")
	return func(key, value, layer string) (string, error) {
		target := wsCfg
		if layer == setprop.LayerUser {
			if userCfg == "" {
				return "", fmt.Errorf("no user config path on this system")
			}
			target = userCfg
		}
		return settings.CheckChange(userCfg, wsCfg, target, key, value)
	}
}

// truncateTitle makes a KB title from a lesson (first 60 runes).
func truncateTitle(s string) string {
	if r := []rune(s); len(r) > 60 {
		return string(r[:59]) + "…"
	}
	return s
}

// guardWorkspace makes DHI's control files read-only to every wrapped
// child (agent CLIs, the run/verify runner, forged scripts): manifests,
// forged tools, grants, config, budgets, proposals and memory under .dhi/
// are written by DHI itself, so an agent cannot approve its own tool,
// grant itself a scope or raise its own budget. The trees agents
// legitimately write — task worktrees and ideation sessions — stay
// writable. An adapter without guard support is used as is.
func guardWorkspace(sb sandbox.Sandbox, ws *workspace.Workspace) sandbox.Sandbox {
	g, ok := sb.(sandbox.Guarder)
	if !ok || ws == nil {
		return sb
	}
	dhi := filepath.Join(ws.Root, workspace.DHIDir)
	// Task and review worktrees are subdirectories of .dhi/tasks and
	// .dhi/reviews, beside the card / review files agents must not edit.
	reopen := []string{
		filepath.Join(ws.Root, worktrees.TasksDir) + "/*",
		filepath.Join(ws.Root, worktrees.ReviewsDir) + "/*",
		filepath.Join(dhi, "sessions"),
		filepath.Join(ws.Root, workspace.DirTmp), // private TMPDIRs (F-086)
	}
	for _, d := range reopen {
		_ = os.MkdirAll(strings.TrimSuffix(d, "/*"), 0o755) // bubblewrap binds only existing paths
	}
	guard := []string{dhi}
	// F-086: the default branch moves only through DHI or you. An agent's
	// own `git commit` on main fails, so work lands on a task branch.
	for _, m := range ws.Members() {
		gitcore.LooseDefaultRefs(m.Path) // bubblewrap guards only files that exist
		guard = append(guard, gitcore.DefaultBranchRefs(m.Path)...)
	}
	guarded, err := g.WithGuarded(guard, reopen)
	if err != nil {
		fmt.Fprintln(os.Stderr, "dhi: guarding .dhi/:", err)
		return sb
	}
	return guarded
}

// openEpisodes is the read side of the agents' episodes for the board
// (nil outside a workspace).
func openEpisodes(ws *workspace.Workspace) *episode.Store {
	if ws == nil {
		return nil
	}
	return episode.Open(ws.Root)
}

// liveTuning re-reads [agents] and [budgets] for every turn (F-066), so a
// Settings edit governs the next spawn without a restart. A broken edit
// keeps the boot values; doctor and the Settings view name the problem.
func liveTuning(userCfg, wsCfg string, boot settings.Config, identityFn gitcore.IdentityFunc) func() agentkitRuntime.Tuning {
	// The principal's name comes from git (F-081); resolved once it
	// succeeds, so a turn never waits on git config.
	var (
		idMu sync.Mutex
		id   gitcore.Identity
	)
	identity := func() gitcore.Identity {
		idMu.Lock()
		defer idMu.Unlock()
		if id.Name == "" && identityFn != nil {
			ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
			if got, err := identityFn(ctx); err == nil {
				id = got
			}
			cancel()
		}
		return id
	}
	return func() agentkitRuntime.Tuning {
		c, err := settings.Load(userCfg, wsCfg)
		if err != nil {
			c = boot
		}
		who := identity()
		return agentkitRuntime.Tuning{
			Principal:     c.Profile.Principal(who.Name, who.Email),
			MaxTurns:      c.Agents.MaxTurns,
			Resume:        c.Agents.Resume,
			Autonomy:      c.Agents.Autonomy,
			MaxIterations: c.Agents.MaxIterations,
			Stall:         time.Duration(c.Agents.StallSeconds) * time.Second,
			Reflect:       c.Agents.Reflect,
			ReflectModel:  c.Agents.ReflectModel,
			TracePrompts:  c.Agents.TracePrompts,
			Memory: agentkitRuntime.MemoryTuning{
				PromptTokens: c.Memory.PromptTokens, SessionTokens: c.Memory.SessionTokens,
				JournalMax: c.Memory.JournalMax, LessonsMax: c.Memory.LessonsMax,
				PinnedMax: c.Memory.PinnedMax, DecayDays: c.Memory.DecayDays,
			},
			Budgets: budget.Limits{
				TaskUSD: c.Budgets.TaskUSD, TaskTokens: c.Budgets.TaskTokens,
				AgentDayUSD: c.Budgets.AgentDayUSD, WorkspaceDayUSD: c.Budgets.WorkspaceDayUSD,
			},
		}
	}
}

func newAgentRuntime(ws *workspace.Workspace, b *bus.Bus, sb sandbox.Sandbox, cliEnv []string, defaultEngine string, gitRunner *gitcore.Runner, identityFn gitcore.IdentityFunc, sessionStore *ideation.Store, runRunner dhitools.CommandRunner, editor dhitools.EditorAPI, workspaceScopes scopes.Set, taskStore *tasks.Store, reviewSvc *review.Service, kbSearcher search.Searcher, mcpStore *mcpserver.Store, conv conventions.Source, cliLook func(string) (string, error), toolBin []string, mcpHome string, wt worktrees.Layout, tune func() agentkitRuntime.Tuning) *agentkitRuntime.Runtime {
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
	// F-067: "allow always" survives a restart. A malformed grants file is
	// named and ignored (every scope asks again) rather than trusted.
	// F-086: agents' private temp dirs only grow; drop what nothing has
	// used for a day.
	agentkitRuntime.CleanTmp(ws.Root, 24*time.Hour, time.Now())
	approvals := tools.NewApprovals()
	if err := approvals.Persist(filepath.Join(ws.Root, workspace.DHIDir, "grants.json")); err != nil {
		fmt.Fprintln(os.Stderr, "dhi:", err)
	}
	// F-087: grants given "everywhere" live beside the credentials, in
	// the user config every workspace reads.
	if cred, err := credstore.DefaultPath(); err == nil {
		if err := approvals.PersistUser(filepath.Join(filepath.Dir(cred), "grants.json")); err != nil {
			fmt.Fprintln(os.Stderr, "dhi:", err)
		}
	}
	rt, err := agentkitRuntime.New(agentkitRuntime.Config{
		WS:        ws,
		Bus:       b,
		Approvals: approvals,
		Redact:    secretRedactor.Func(),
		PushGuard: pushGuard(ws),
		Secrets:   secretStore(),
		// Host agent CLIs (F-013/ADR-0012/0013): DHI ships no model
		// engine of its own; every rostered agent thinks through one of
		// these, resolved on the host PATH. Absent CLIs refuse roster
		// agents that declare them, and doctor names the installation —
		// never a fallback.
		CLIs: clirun.NewRegistry(cliLook),
		// DefaultEngine is the workspace default (ADR-0019): an agent
		// that omits `engine` inherits it; with none set the agent
		// refuses by name at build time.
		DefaultEngine: defaultEngine,
		CLIEnv:        cliEnv,
		Tasks:         taskStore,
		Org:           company,
		Standards:     true,
		Conventions:   conv,
		ExtraRoots:    worktreeRoots(wt),
		ToolBin:       toolBin,
		MCPHome:       mcpHome,
		Workflows:     true,
		Sandbox:       sb,
		Memory:        memStore,
		Knowledge:     kbStore,
		Search:        kbSearcher,
		// M15 P1 tool seams: the hermetic git runner (git_diff), the F-029
		// identity resolver (git_commit), the shared ideation store
		// (ideation_*), and the allowlisted command runner (`run`). A nil
		// seam makes its tool refuse by name — never a host fallback.
		Git:             gitRunner,
		Identity:        identityFn,
		Sessions:        sessionStore,
		MCPServers:      mcpStore,
		Run:             runRunner,
		Editor:          editor,
		WorkspaceScopes: workspaceScopes,
		// F-066: per-task / per-thread episodes (CLI session resume) and
		// the spend ledger budgets are checked against.
		Episodes: episode.Open(ws.Root),
		Spend:    budget.Open(ws.Root),
		Tuning:   tune,
		Lessons:  lessons.Open(ws.Root), // F-068: learned rules, retrieval + reflection
		Forge:    forge.Open(ws.Root),   // F-069: agent-made tools, active once approved
		// F-070: agents propose settings changes; the human applies them.
		SetProps:     setprop.Open(ws.Root),
		SettingCheck: settingCheck(ws),
		// F-091: employees find and suggest skills from the DHI registry;
		// DHI verifies the suggestion and the human installs it.
		SkillSearch: func(q string) ([]dhitools.SkillHit, error) {
			es, err := registry.New(ws).Search(q)
			var out []dhitools.SkillHit
			for _, e := range es {
				out = append(out, dhitools.SkillHit{Name: e.Name, Version: e.Version, Description: e.Description})
			}
			return out, err
		},
		SkillSuggest: func(ctx context.Context, agent, entry, reason string) (string, error) {
			p, err := skillprop.Review(ctx, registry.New(ws), entry)
			if err != nil {
				return "", err
			}
			p.Agent, p.Reason = agent, reason
			if _, err := skillprop.Open(ws.Root).Propose(p); err != nil {
				return "", err
			}
			what := fmt.Sprintf("%d skill(s), %s", len(p.Skills), p.Trust())
			if p.HasScripts() {
				what += ", ships a script"
			}
			return what, nil
		},
		// F-067: a task turn without a worktree gets one on the convention
		// branch before the agent starts, so the work lands where DHI can
		// verify it and open the PR.
		Toolchain: optionalToolchain(),
		Provision: func(slug, member string) error {
			if taskStore == nil {
				return fmt.Errorf("task store unavailable")
			}
			c := conventions.Defaults()
			if conv != nil {
				if cc := conv(); cc != nil {
					c = *cc
				}
			}
			branch, err := conventions.ExpandBranch(c.Branch.Task, taskVars(taskStore, c, slug, member))
			if err != nil {
				return err
			}
			return taskStore.Attach(slug, member, branch, "")
		},
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

// branchVars supplies the placeholders a branch pattern may use:
// {slug}/{id} (the task slug or review id), {user} (the OS account, as
// a ref-safe token) and {date} (YYYYMMDD).
func branchVars(id string) map[string]string {
	user := "user"
	if u, err := osuser.Current(); err == nil && u.Username != "" {
		user = strings.ToLower(u.Username)
	}
	return map[string]string{
		"slug": id, "id": id, "user": user, "ticket": id, "member": "",
		"date": time.Now().Format("20060102"),
	}
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

// worktreeRoots lists the extra path-jail roots for an external worktree base.
func worktreeRoots(wt worktrees.Layout) []string {
	if !wt.External() {
		return nil
	}
	return []string{wt.Base()}
}

// worktreeHint names where task worktrees land for the attach form; "" keeps
// the in-workspace default text.
func worktreeHint(wt worktrees.Layout) string {
	if !wt.External() {
		return ""
	}
	return filepath.Join(wt.Base(), "tasks", "<slug>", "<member>")
}

// taskVars supplies a task's naming placeholders (F-075): the branch
// vars plus {member}, {title}, {assignee} and {ticket} — the card's ticket
// key found by the ticket pattern in its slug, title, epic or labels,
// else the slug itself.
func taskVars(ts *tasks.Store, c conventions.Config, slug, member string) map[string]string {
	v := branchVars(slug)
	v["member"] = member
	if ts != nil {
		if tk, ok := ts.Get(slug); ok {
			v["title"], v["assignee"] = tk.Title, tk.Assignee
			if t := conventions.TicketFor(c.Commit.TicketPattern, append([]string{tk.Slug, tk.Title, tk.Epic}, tk.Labels...)...); t != "" {
				v["ticket"] = t
			}
		}
	}
	return v
}

// toolchainSeam adapts the toolchain manager to the runtime's optional-
// toolchain seam (F-076); installs are serialised.
type toolchainSeam struct {
	mgr *toolchain.Manager
	mu  sync.Mutex
}

func (t *toolchainSeam) Optional(name string) (int64, bool) {
	mf, err := toolchain.Embedded()
	if err != nil {
		return 0, false
	}
	tool, ok := mf.Tools[name]
	if !ok || !tool.Optional {
		return 0, false
	}
	return tool.Platforms[toolchain.PlatformKey()].Size, true
}

func (t *toolchainSeam) Installed(name string) bool { return t.mgr.Installed(name) }

func (t *toolchainSeam) Install(ctx context.Context, name string) error {
	t.mu.Lock()
	defer t.mu.Unlock()
	if t.mgr.Installed(name) {
		return nil // another verify installed it while this one waited
	}
	return t.mgr.InstallEmbedded(ctx, []string{name})
}

// optionalToolchain is the runtime's install seam, nil without a prefix.
func optionalToolchain() agentkitRuntime.Toolchain {
	root, err := toolchain.DefaultRoot()
	if err != nil {
		return nil
	}
	return &toolchainSeam{mgr: toolchain.New(root)}
}

// composeOptions marks every roster id (archived included) and team slug
// as taken, so composed employees never collide with existing ones (F-079).
func composeOptions(w *workspace.Workspace, o *agentkitOrg.Org) starter.Options {
	taken := map[string]bool{}
	if roster, err := agentkitOrg.LoadRoster(w); err == nil {
		for _, a := range roster {
			taken[a.ID] = true
		}
	}
	for _, id := range o.Archived(w) {
		taken[id] = true
	}
	return starter.Options{
		AgentTaken: func(id string) bool { return taken[id] },
		TeamTaken:  func(slug string) bool { _, ok := o.Team(slug); return ok },
	}
}

// memberDirs lists the workspace's member repositories.
func memberDirs(ws *workspace.Workspace) []string {
	var dirs []string
	for _, mb := range ws.Members() {
		dirs = append(dirs, mb.Path)
	}
	return dirs
}

// learnConventions infers commit and PR style from the members' recent
// history (hermetic git) and their PR templates (F-082). A nil runner
// learns from the templates only.
func learnConventions(r *gitcore.Runner, dirs []string) conventions.Inference {
	var subjects []string
	if r != nil {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		for _, d := range dirs {
			subjects = append(subjects, r.Subjects(ctx, d, 50)...)
		}
	}
	inf := conventions.InferCommits(subjects)
	for _, d := range dirs {
		if t := conventions.PRTemplate(d); t != "" {
			inf.PRBody = t
			inf.Evidence = append(inf.Evidence, "PR descriptions use "+filepath.Base(d)+"'s pull request template")
			break
		}
	}
	return inf
}

// suggestIdentity gathers git identity candidates (F-083): the repos'
// config and history, the signed-in GitHub account (managed gh only),
// and the computer account's full name.
func suggestIdentity(ctx context.Context, r *gitcore.Runner, dirs []string, toolRoot string) []gitcore.Suggestion {
	var extra []gitcore.Suggestion
	if shim := filepath.Join(toolRoot, "bin", "gh"); toolRoot != "" {
		if _, err := os.Stat(shim); err == nil {
			gctx, cancel := context.WithTimeout(ctx, 4*time.Second)
			if u, err := review.NewGHCLI(shim).User(gctx); err == nil {
				email := u.Email
				if email == "" {
					email = u.NoReplyEmail()
				}
				extra = append(extra, gitcore.Suggestion{Identity: gitcore.Identity{Name: u.Name, Email: email},
					Source: "your GitHub account (" + u.Login + ")"})
			}
			cancel()
		}
	}
	if u, err := osuser.Current(); err == nil && strings.TrimSpace(u.Name) != "" {
		extra = append(extra, gitcore.Suggestion{Identity: gitcore.Identity{Name: u.Name}, Source: "your computer account"})
	}
	return gitcore.SuggestIdentity(ctx, r, dirs, extra...)
}

// repairGate finds what DHI can fix in the workspace, applies the safe
// repairs, and returns the card that reports them and asks about the
// rest; nil when there is nothing to say (F-085).
func repairGate(ws *workspace.Workspace, git *gitcore.Runner) *repairgate.Model {
	in := repair.Input{WS: ws, Library: library.Open(ws), UserConfig: settingsUserPath()}
	if o, err := agentkitOrg.Load(ws.Root); err == nil {
		in.Org = o
	}
	if git != nil {
		in.Ignored = func(rel string) bool {
			ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
			defer cancel()
			_, _, err := git.Run(ctx, ws.Root, "check-ignore", "-q", rel)
			return err == nil
		}
	}
	rs := repair.Find(in)
	var ask []repair.Repair
	for _, r := range rs {
		if !r.Safe {
			ask = append(ask, r)
		}
	}
	g := repairgate.New(version.Version, repair.Run(rs, true), ask)
	if !g.Needed() {
		return nil
	}
	return g
}

// settingsUserPath is your config.toml ("" when it cannot be located).
func settingsUserPath() string {
	p, err := settings.DefaultUserPath()
	if err != nil {
		return ""
	}
	return p
}

// pushGuard installs the pre-push hook that keeps agents off a remote's
// default branch (F-086) under .dhi/hooks, where agents cannot write it.
// "" when it cannot be written (the push then goes unguarded, as before).
func pushGuard(ws *workspace.Workspace) string {
	p := filepath.Join(ws.Root, workspace.DHIDir, "hooks", "dhi-pre-push")
	if err := gitcore.WritePushGuard(p); err != nil {
		fmt.Fprintln(os.Stderr, "dhi: push guard:", err)
		return ""
	}
	return p
}
