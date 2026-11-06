// Command dhi launches the DHI IDE. Per ADR-0004 the CLI surface is
// minimal: no project-management subcommands — everything happens inside
// the TUI. The one exception is `dhi doctor [--json]`, which audits the
// tools DHI finds on your system and workspace health.
//
// Five views: Workspace (boot) · Editor · Ideator · Reviewer · Settings.
package main

import (
	"bytes"
	"context"
	"fmt"
	"github.com/drjzlyan/dhi/internal/checkpoint"
	"github.com/drjzlyan/dhi/internal/editshim"
	"github.com/drjzlyan/dhi/internal/hostenv"
	"github.com/drjzlyan/dhi/internal/hosttools"
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
	"github.com/drjzlyan/dhi/internal/agentkit/clirun"
	"github.com/drjzlyan/dhi/internal/agentkit/dhitools"
	"github.com/drjzlyan/dhi/internal/agentkit/episode"
	"github.com/drjzlyan/dhi/internal/agentkit/forge"
	"github.com/drjzlyan/dhi/internal/agentkit/knowledge"
	"github.com/drjzlyan/dhi/internal/agentkit/lessons"
	"github.com/drjzlyan/dhi/internal/agentkit/library"
	"github.com/drjzlyan/dhi/internal/agentkit/manifest"
	"github.com/drjzlyan/dhi/internal/agentkit/mcpserver"
	"github.com/drjzlyan/dhi/internal/agentkit/memory"
	agentkitOrg "github.com/drjzlyan/dhi/internal/agentkit/org"
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
	"github.com/drjzlyan/dhi/internal/trust"
	"github.com/drjzlyan/dhi/internal/tui/app"
	"github.com/drjzlyan/dhi/internal/tui/kit"
	"github.com/drjzlyan/dhi/internal/tui/surfaces"
	"github.com/drjzlyan/dhi/internal/tui/surfaces/bootgate"
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
	// F-165: started as the editor of a program DHI launched (an interactive
	// rebase), DHI only hands the file to the running instance. This is not a
	// subcommand: the link in DHI's private temp folder names the binary.
	if editshim.IsShim(os.Args[0]) {
		os.Exit(editshim.RunShim(os.Args[1:], os.Getenv, func(s string) { fmt.Fprintln(os.Stderr, s) }))
	}
	// F-090/F-091: your own instructions, skills and registry, read in
	// every workspace.
	if cred, err := credstore.DefaultPath(); err == nil {
		cfgDir := filepath.Dir(cred)
		standards.UserPath = filepath.Join(cfgDir, "standards.toml")
		setupExtensionPaths(cfgDir) // ADR-0043: skills, MCP servers, installs
	}
	// F-109: the environment as the user's login shell sets it up (nvm,
	// Homebrew, asdf…) becomes DHI's own, so every child — and doctor —
	// sees what the user's terminal sees.
	if os.Getenv("DHI_RESOLVING_ENV") == "" { // never recurse from an rc file that runs dhi
		hostenv.Apply()
	}
	if len(os.Args) > 1 {
		switch os.Args[1] {
		case "doctor":
			os.Exit(runDoctor(os.Args[2:]))
		case "version", "--version", "-v":
			fmt.Println(version.String())
			return
		case "help", "--help", "-h":
			fmt.Println("usage: dhi [doctor [--json] | version | help]")
			fmt.Println()
			fmt.Println("  dhi            open the IDE in the current folder")
			fmt.Println("  dhi doctor     check this machine and workspace (--json for scripts)")
			fmt.Println("  dhi version    print the build")
			return
		default:
			fmt.Fprintf(os.Stderr, "dhi: unknown command %q\n", os.Args[1])
			fmt.Fprintln(os.Stderr, "usage: dhi [doctor [--json] | version | help]")
			os.Exit(2)
		}
	}
	// A relaunch is a fresh process image's worth of state: services read
	// the workspace and settings once at launch, so when setup changed
	// them we start over rather than hot-swap (F-043).
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

	// Coding CLIs resolve on the user's own PATH (F-110); every consumer
	// shares the one lookup.
	cliLook := hostLook

	// The boot audit resolves everything up front (F-011 / ADR-0011):
	// proceed, or block with named reasons.
	decision := boot.Audit(boot.Input{
		CWD:      cwd,
		UserCfg:  userCfg,
		GOOS:     runtime.GOOS,
		LookPath: exec.LookPath,
		PATH:     hostenv.Get("PATH"),
		Getenv:   hostenv.Get,
		// Skills agents read (ADR-0043): yours, other tools' and the
		// built-ins written out for them.
		ReadOnly: nonEmpty(library.UserSkillsDir, library.AgentsSkillsDir, library.BuiltinSkillsDir),
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
	// F-180 (ADR-0048): every agent gets a visible default fallback engine
	// from the other installed CLIs, in clirun.FallbackOrder. An agent that
	// inherits the workspace engine is measured against that default.
	agentkitOrg.DefaultFallback = func(primary string) (string, string) {
		if primary == "" {
			if name, err := manifest.ParseEngine(cfg.Engine); err == nil {
				primary = name
			}
		}
		return clirun.PickFallback(primary, clirun.NewRegistry(cliLook).Installed())
	}

	kit.SetKeyMap(cfg.Keys)                       // [keys] remaps (F-058): the shell translates, the chrome shows them
	kit.MacKeys = runtime.GOOS == "darwin"        // hints spell alt as ⌥ (F-101)
	kit.SetOSClipboard(hosttools.CopyToClipboard) // copy works where OSC 52 does not (ADR-0042)
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
	liveCfg := settings.Live(userCfg, wsCfg, cfg)
	edOpts = append(edOpts, editor.WithPrefs(func() editor.Prefs { // F-116: Settings apply live
		c := liveCfg()
		return editor.Prefs{TabWidth: c.Editor.TabWidth, LineNumbers: c.Editor.LineNumbers, Wrap: c.Editor.Wrap, Autosave: c.Editor.Autosave, Blame: c.Editor.Blame, InlayHints: c.Editor.InlayHints, SemanticTokens: c.Editor.SemanticTokens, Breadcrumbs: c.Editor.Breadcrumbs, StickyScroll: c.Editor.StickyScroll, InlineCompletion: c.Editor.InlineCompletion, RestoreSession: c.Editor.RestoreSession, Scrollback: c.Terminal.Scrollback}
	}))
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
	// Terminals, tests, the debugger and agents start from the user's own
	// environment (F-109, ADR-0044); agents keep their private TMPDIR.
	termEnv = hostenv.Env()
	agentEnv = termEnv
	edOpts = append(edOpts, editor.WithTermEnvFunc(hostenv.Env), editor.WithHostTools(editorTools{}))
	// Language servers come from the user's system (F-110); a missing one
	// raises the install card. Shut down on every return — the relaunch
	// loop re-enters runTUI, so a leak would stack one gopls per relaunch.
	lspMgr := lsp.NewManager(hostFind, hostenv.Env)
	defer lspMgr.ShutdownAll()
	edOpts = append(edOpts, editor.WithLSP(lspMgr))
	// Restricted mode (F-148): a folder runs servers, formatters, tasks, tests
	// and the debugger only once the user trusted it (kept beside the user
	// config, never in the repo).
	edOpts = append(edOpts, editor.WithTrust(userTrust()))
	// The user's ripgrep, looked up at each search; missing → install card.
	rgSearcher = search.Host{Look: hostFind}
	// F-029: one identity resolver, read from the user's own git config.
	// Without git it errors, so every commit path refuses by name rather
	// than invent an author.
	identityFn = userIdentity
	edOpts = append(edOpts, editor.WithIdentity(identityFn))
	// The user's git backs the served git_diff tool (M15 P1); without git
	// it is nil and the tool refuses by name.
	gitRunner = userGit()
	// Clone, fetch and push go through the user's git: their ssh keys,
	// agent, credential helpers and insteadOf rules (F-112).
	gitcore.SetNetwork(gitRunner)
	runRunner = execRunner{envFn: func() []string { return runTmpEnv(hostenv.Env(), ws) }, timeout: 5 * time.Minute, sandbox: guardWorkspace(decision.Sandbox, ws)}
	edOpts = append(edOpts, editor.WithSearcher(rgSearcher))
	if gitRunner != nil {
		edOpts = append(edOpts, editor.WithGit(gitRunner)) // F-121: diff, hunks, branches, blame
	}
	if cps := workspaceCheckpoints(ws, gitRunner); cps != nil {
		edOpts = append(edOpts, editor.WithCheckpoints(cps)) // F-120: undo an agent turn
	}

	// Message bus + tasks store exist for every workspace; the worktree
	// seam lights up only when git is installed.
	var messageBus *bus.Bus
	var agentRT *agentkitRuntime.Runtime
	// F-182: the AI parts of a branch, commit or PR nobody's agent is
	// writing are written by the default agent, once the team is up.
	writer := func(ctx context.Context, req conventions.GenRequest) (map[string]string, error) {
		if agentRT == nil {
			return nil, fmt.Errorf("no agents yet to write the AI parts ({ai:…}) — ctrl+a sets up a team, or write them yourself")
		}
		return agentRT.Writer()(ctx, req)
	}
	var taskStore *tasks.Store
	var reviewSvc *review.Service
	var sessionStore *ideation.Store
	var unreadStore *unread.Store
	var mcpStore *mcpserver.Store
	// F-182: the commit buffer starts from the team's shape for the
	// branch it commits on, and :w writes the {ai:…} parts left in it.
	edOpts = append(edOpts, editor.WithCommitConventions(editor.CommitConventions{
		Draft: func(dir string) string {
			branch := ""
			if r, err := gitcore.Open(dir); err == nil {
				branch, _ = r.CurrentBranch()
			}
			c := convLive.Get()
			return c.Commit.Draft(varsForBranch(taskStore, c, branch))
		},
		Write: func(ctx context.Context, dir, text string) (string, error) {
			diff, _ := gitDiffFor(ctx, dir, true)
			return writeCommit(ctx, writer, text, diff)
		},
	}))
	if ws != nil {
		messageBus = openBus(ws)
		if ts, err := tasks.Open(ws); err == nil {
			taskStore = ts
			taskStore.SetIdentity(identityFn)
			wireTaskSeam(ws, ts, convLive.Get, decision.Worktrees, writer)
		}
		reviewSvc = openReviewService(ws, func() conventions.Config { return convLive.Get() }, decision.Worktrees)
		if reviewSvc != nil {
			// F-182: one symbol source for every PR, task branch or not.
			reviewSvc.SetPRVarsFunc(func(branch string) map[string]string {
				return varsForBranch(taskStore, convLive.Get(), branch)
			})
			reviewSvc.SetGenerator(writer)
		}
		mcpStore = mcpserver.OpenWith(ws.Root, ws.Members())
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
			// Existing agents without a recorded fallback get the default
			// written into their manifests before the roster loads.
			if changed, ferr := agentkitOrg.EnsureFallbacks(ws); ferr != nil {
				fmt.Fprintln(os.Stderr, "dhi: fallback engines:", ferr)
			} else if len(changed) > 0 {
				fmt.Fprintln(os.Stderr, "dhi: default fallback engine set for:", strings.Join(changed, ", "))
			}
			agentRT = newAgentRuntime(ws, messageBus, guardWorkspace(decision.Sandbox, ws), agentEnv, cfg.Engine, gitRunner, identityFn, sessionStore, runRunner, editorBridge, wsScopes, taskStore, reviewSvc, rgSearcher, mcpStore, convLive.Source(), cliLook, decision.Worktrees,
				liveTuning(userCfg, wsCfg, cfg, identityFn), writer)
			if agentRT != nil {
				// F-071: a held sub-card starts the moment its blockers are
				// done, whoever moved them.
				go agentRT.WatchTasks(context.Background())
				edOpts = append(edOpts, editor.WithChat(agentRT))
				if sv := secretSaver(); sv != nil {
					edOpts = append(edOpts, editor.WithSecrets(sv)) // F-104: answer secret asks here too
				}
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
		// Settings loads everything else when .dhi/org.toml is broken and
		// names the error (F-116: it used to lose most sections silently).
		{
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
				CLIs:       cliRegistry.Names(),
				Detect:     cliRegistry.Detect,
				Library:    library.Open(ws),
				Reload:     reloadRoster,
				Autopilots: autoStore,
				Bus:        messageBus,
				Tasks:      taskStore,
				Ext:        newExtensions(ws.Root),
			}
			if oerr == nil {
				settingsDeps.Org = company
			} else {
				settingsDeps.OrgErr = oerr.Error()
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
			// the user's git, like the wizard.
			settingsDeps.Identity = identityFn
			settingsDeps.SetIdentity = setUserIdentity
			settingsDeps.SetIdentityLocal = setWorkspaceIdentity(ws.Root)
			settingsDeps.SuggestIdentity = func(ctx context.Context) []gitcore.Suggestion {
				return suggestIdentity(ctx, userGit(), memberDirs(ws))
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
	var resetSetup func() error // F-108: clear step records, run setup again
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
		OpenSetup: func() {
			if appRef != nil {
				appRef.OpenSettingsSection("SETUP")
			}
		},
	}))
	// Settings → SETUP (F-108): every step, re-runnable, and a full reset.
	settingsDeps.Setup = func() []kit.ChecklistItem { return checklist }
	settingsDeps.RunSetup = func(id string) {
		if runStep != nil {
			runStep(id)
		}
	}
	settingsDeps.ResetSetup = func() error {
		if resetSetup == nil {
			return fmt.Errorf("setup is not available here")
		}
		return resetSetup()
	}
	settingsSurface := settingsview.New(cfg, savePath, settingsDeps)
	// crewRT is agentRT as an interface that is nil when there is no
	// runtime: a typed-nil *Runtime would pass the surfaces' nil checks
	// (F-115: Ideator n, Reviewer A, channel posts crashed with no roster).
	var crewRT liveCrew
	if agentRT != nil {
		crewRT = agentRT
	}
	// The Editor is view 1 (ADR-0041): DHI opens where you write code.
	a := app.New(version.Version,
		editor.New(version.Version, ws, edOpts...),
		wsview.New(version.Version, ws, wsview.Deps{
			Episodes: openEpisodes(ws), // F-067 card pane: plan, verify, spend
			// F-127: the chat's context meter measures against the live budgets.
			MemoryLimits: func() (int, int) { m := liveCfg().Memory; return m.PromptTokens, m.SessionTokens },
			WorktreeHint: worktreeHint(decision.Worktrees),
			Bus:          messageBus,
			Runtime:      crewRT,
			Tasks:        taskStore,
			Roster:       crewRT,
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
			// F-182: the card's own branch once it has one; before that the
			// template with the task's symbols and any AI part as an
			// {ai:…} marker, which attaching writes.
			BranchFor: func(slug string) string {
				if taskStore != nil {
					if tk, ok := taskStore.Get(slug); ok && len(tk.ChangeSets) > 0 {
						return tk.ChangeSets[0].Branch
					}
				}
				c := convLive.Get()
				return conventions.Prefill(c.Branch.Task, taskVars(taskStore, c, slug, ""))
			},
			CommitPrefix: func(slug string) string {
				c := convLive.Get()
				branch := ""
				if taskStore != nil {
					if tk, ok := taskStore.Get(slug); ok && len(tk.ChangeSets) > 0 {
						branch = tk.ChangeSets[0].Branch
					}
				}
				// One line you type at the end of: the subject up to its first
				// AI part ("PROJ-7: "), never a marker you would type after.
				subject, _, _ := strings.Cut(c.Commit.Draft(varsForBranch(taskStore, c, branch)), "\n")
				if i := strings.Index(subject, "{ai"); i >= 0 {
					subject = subject[:i]
				}
				return subject
			},
			WriteCommit: func(ctx context.Context, slug, text string) (string, error) {
				if len(conventions.Markers(text)) == 0 && convLive.Get().Commit.BodyFormat == "" {
					return text, nil
				}
				c := convLive.Get()
				var diff strings.Builder
				if taskStore != nil {
					if tk, ok := taskStore.Get(slug); ok {
						for _, cs := range tk.ChangeSets {
							if d, ok := gitDiffFor(ctx, filepath.Join(ws.Root, cs.Path), false); ok {
								fmt.Fprintf(&diff, "%s:\n%s\n", cs.Member, d)
							}
						}
						if len(tk.ChangeSets) > 0 && !strings.Contains(strings.TrimSpace(text), "\n") {
							// The board takes one line; the body format follows it.
							if f := c.Commit.BodyFormat; f != "" {
								text += "\n\n" + conventions.Prefill(f, varsForBranch(taskStore, c, tk.ChangeSets[0].Branch))
							}
						}
					}
				}
				return writeCommit(ctx, writer, text, diff.String())
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
			Crew:  crewRT,
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
			Crew:    crewRT,
			Tasks:   taskStore,
			OpenInEditor: func(paths []string) bool {
				if appRef == nil {
					return false
				}
				return appRef.OpenInEditor(paths)
			},
		}),
		settingsSurface,
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
				if err := setup.SaveState(p, st); err != nil && appRef != nil {
					appRef.Toast(kit.Toast{Kind: kit.ToastWarning, Title: "Lesson progress not saved", Detail: err.Error()})
				}
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
		env := newSetupEnv(cwd, ws, userCfg, cfg.Conventions, cfg.Engine, identityFn, cliLook,
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
		env := newSetupEnv(cwd, ws, userCfg, cfg.Conventions, cfg.Engine, identityFn, cliLook,
			func() { startLesson("tour") })
		checklist = wizard.Checklist(env, version.Version, loadSetupState(ws))
	}
	refreshChecklist()
	resetSetup = func() error {
		if decision.Block != "" || appRef == nil {
			return fmt.Errorf("setup is blocked: %s", decision.Block)
		}
		st := loadSetupState(ws)
		st.Steps, st.Skipped, st.Finished = nil, nil, false
		root := ""
		if ws != nil {
			root = ws.Root
		}
		if p, err := setup.UserStatePath(); err == nil {
			if err := setup.SaveState(p, st); err != nil {
				return err
			}
		}
		if root != "" {
			if err := setup.SaveState(setup.WorkspaceStatePath(root), st); err != nil {
				return err
			}
		}
		refreshChecklist()
		appRef.QueueGate(newWizard())
		return nil
	}
	runStep = func(id string) {
		if decision.Block != "" || appRef == nil {
			return
		}
		env := newSetupEnv(cwd, ws, userCfg, cfg.Conventions, cfg.Engine, identityFn, cliLook,
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
				Detail: "change it in Settings"})
			return
		}
		wizards = append(wizards, w)
		appRef.QueueGate(w)
	}
	a.SetSetup(func() []kit.ChecklistItem { return checklist }, runStep)
	// A tool installed in another terminal, or a changed shell profile,
	// reaches DHI without a restart (F-109).
	for _, name := range settings.ThemeNames() { // saved like the Settings row (F-116)
		name := name
		a.AddCommand(surfaces.Command{Group: "Theme", Title: name, Run: func() tea.Cmd {
			note := settingsSurface.SetTheme(name)
			a.Toast(kit.Toast{Kind: kit.ToastInfo, Title: "Theme " + name, Detail: note})
			return nil
		}})
	}
	a.AddCommand(surfaces.Command{Group: "Tools", Title: "Reload shell environment", Run: func() tea.Cmd {
		return func() tea.Msg {
			s := hostenv.Reload()
			hostenv.Apply()
			if s.Err != nil {
				return app.ToastMsg{Kind: kit.ToastWarning, Title: "Shell environment not read", Detail: s.Err.Error()}
			}
			return app.ToastMsg{Kind: kit.ToastSuccess, Title: "Shell environment reloaded", Detail: "from " + s.Shell}
		}
	}})

	// Gates, in strict order: a block never releases; then the repairs.
	var gates []gatechain.Gate
	if decision.Block != "" {
		gates = append(gates, bootgate.New(version.Version, decision))
	}
	// Self-healing (F-085, ADR-0038): safe repairs run now and are
	// reported; the rest wait on the user's yes in the same card.
	if decision.Block == "" && ws != nil {
		if g := repairGate(ws, gitRunner, decision.Sandbox); g != nil {
			gates = append(gates, g)
		}
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
	uiSend = p.Send // background sign-ins and installs raise toasts (F-106)
	if _, err := p.Run(); err != nil {
		fmt.Fprintln(os.Stderr, "dhi:", err)
		os.Exit(1)
	}
	for _, w := range wizards {
		if w.NeedsRelaunch() {
			return true
		}
	}
	return false
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

// newSetupEnv wires the wizard to real services.
func newSetupEnv(cwd string, ws *workspace.Workspace, userCfg string,
	conv conventions.Config, engine string, identityFn gitcore.IdentityFunc,
	cliLook func(string) (string, error), startTour func()) *wizard.Env {
	env := &wizard.Env{
		Version: version.Version, CWD: cwd,
		Discover: setup.DiscoverMembers, InitWorkspace: initTrustedWorkspace,
		Identity: identityFn, Conventions: conv, Engine: engine,
	}
	if ws != nil {
		env.Root = ws.Root
	}
	env.SetIdentity = setUserIdentity
	env.SetIdentityLocal = func(ctx context.Context, id gitcore.Identity) error {
		if env.Root == "" {
			return fmt.Errorf("no workspace yet")
		}
		return setWorkspaceIdentity(env.Root)(ctx, id)
	}
	env.SuggestIdentity = func(ctx context.Context) []gitcore.Suggestion {
		dirs := []string{cwd}
		if env.Root != "" {
			if w, err := workspace.Load(env.Root); err == nil {
				dirs = memberDirs(w)
			}
		}
		return suggestIdentity(ctx, userGit(), dirs)
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
	// team steps share one answer per run, refreshed after an install. The
	// steps call it from commands, so the lock is held across DetectAll: a
	// second caller waits for the first answer instead of probing again.
	var (
		detectMu sync.Mutex
		detected map[string]clirun.Probe
	)
	probe := func() map[string]clirun.Probe {
		detectMu.Lock()
		defer detectMu.Unlock()
		if detected == nil {
			detected = clirun.NewRegistry(cliLook).DetectAll()
		}
		return detected
	}
	env.DetectCLIs = func() map[string]string {
		out := map[string]string{}
		for n, p := range probe() {
			out[n] = p.Version
		}
		return out
	}
	env.CLIStatus = func() []wizard.CLIRow {
		reg := clirun.NewRegistry(cliLook)
		detected := probe()
		var rows []wizard.CLIRow
		for _, c := range reg.All() {
			p := detected[c.Name]
			row := wizard.CLIRow{Name: c.Name, Version: p.Version, Tested: c.Tested, Path: p.Path}
			if p.Err != nil {
				row.ProbeErr = p.Err.Error()
			}
			row.Plan, _ = clirun.PlanFor(c.Name)
			if row.Version != "" {
				row.Verdict, row.Why = clirun.Assess(c.Tested, row.Version)
			}
			rows = append(rows, row)
		}
		return rows
	}
	env.ToolStatus = toolRows
	// Installs use the user's package manager, in their terminal, after
	// they saw the exact command (F-110).
	env.CLIInstall = cliInstall
	env.RunInstall = func(name string, done func(error) tea.Msg) tea.Cmd {
		return runInstall(name, func(err error) tea.Msg {
			detectMu.Lock()
			detected = nil // the next look sees what was installed
			detectMu.Unlock()
			return done(err)
		})
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
		return learnConventions(userGit(), memberDirs(w))
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
	env.LoadBrief = func() (brief.Brief, bool) {
		b, ok, err := brief.Load(env.Root)
		return b, ok && err == nil
	}
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
	// Integrations step (ADR-0043): the vendors' official plugins from the
	// marketplace, given to the bots they fit.
	env.IntegrationRows = func() []wizard.IntegrationRow { return integrationRows(env.Root) }
	env.SetupIntegration = func(ctx context.Context, plugin, who string) (string, error) {
		return setupIntegration(ctx, env.Root, plugin, who)
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

// openReviewService builds the review orchestration layer: TOML store
// always; worktree seam + diff runner light up with the user's git; PR
// inputs additionally need their gh.
func openReviewService(ws *workspace.Workspace, conv func() conventions.Config, wt worktrees.Layout) *review.Service {
	prBody := func() string { return conv().PR.Body }
	st, err := review.Open(ws)
	if err != nil {
		fmt.Fprintln(os.Stderr, "dhi: review store:", err)
		return nil
	}
	// The user's own gh and its login light PR flows (F-110); without it
	// the seam refuses by name.
	ghBin, _ := hostLook("gh")
	gh := review.NewGHCLI(ghBin)
	svc := review.NewService(ws, st, nil, gh)
	prTitle := func() string { return conv().PR.Title }
	reviewBranch := func(id string) (string, error) { return conventions.ExpandBranch(conv().Branch.Review, branchVars(id)) }
	svc.SetPRBodyFunc(prBody)
	svc.SetPRTitleFunc(prTitle)
	svc.SetReviewBranchFunc(reviewBranch)
	runner := userGit()
	if runner == nil {
		return svc // no git: diffs degrade visibly
	}
	svc = review.NewService(ws, st, runner, gh)
	svc.SetPRBodyFunc(prBody)
	svc.SetPRTitleFunc(prTitle)
	svc.SetReviewBranchFunc(reviewBranch)
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

// wireTaskSeam connects task ChangeSets to git worktrees when git is
// installed; without it, attaching reports a visible error.
func wireTaskSeam(ws *workspace.Workspace, ts *tasks.Store, conv func() conventions.Config, wt worktrees.Layout, gen conventions.Generator) {
	runner := userGit()
	if runner == nil {
		return // no git; doctor explains
	}
	ts.SetAttach(
		func(slug, member, branch, startpoint string) (string, error) {
			mem, ok := ws.Member(member)
			if !ok {
				return "", fmt.Errorf("unknown member %q", member)
			}
			c := conv()
			vars := taskVars(ts, c, slug, member)
			ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
			defer cancel()
			var err error
			// A prefilled name may still hold symbols only known now ({member}).
			branch = conventions.RenderKnown(branch, vars)
			switch {
			case branch == "":
				if branch, err = nameTaskBranch(ctx, c, ts, slug, member, nil, gen); err != nil {
					return "", err
				}
			case len(conventions.Markers(branch)) > 0:
				// F-182: a prefilled name whose {ai:…} parts the human kept.
				if gen == nil {
					return "", fmt.Errorf("branch %q has AI parts and no agent can write them: name it yourself", branch)
				}
				got, gerr := gen(ctx, conventions.GenRequest{What: "git branch name", Shape: conventions.Shape(branch, nil),
					Slots: conventions.Markers(branch), Context: taskContext(ts, slug) + "\nWrite each part as a few lower-case words; DHI joins them with dashes."})
				if gerr != nil {
					return "", fmt.Errorf("naming the branch: %w", gerr)
				}
				if branch, err = conventions.FillBranchMarkers(branch, got); err != nil {
					return "", err
				}
			}
			sub, err := conventions.ExpandWorktree(c.Worktree.Task, vars)
			if err != nil {
				return "", err
			}
			rel, dst := wt.TaskAt(sub)
			wctx, wcancel := context.WithTimeout(context.Background(), 2*time.Minute)
			defer wcancel()
			if err := runner.WorktreeAdd(wctx, mem.Path, dst, branch, startpoint); err != nil {
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
	// A workspace-wide symbol query names no file (F-145).
	if op == "symbols_workspace" && path == "" {
		r, err := b.request(ctx, app.EditorRequest{Op: "lsp", LSPOp: op, Arg: arg})
		return r.Text, err
	}
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

// execRunner implements dhitools.CommandRunner: a child process (no
// shell) in the user's environment with a wall-clock timeout and capped output. The
// allowlist policy lives in dhitools; this only executes vetted argv.
type execRunner struct {
	env     []string
	envFn   func() []string // read per run when set (F-109)
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
	home, _ := os.UserHomeDir()
	set := append([]string{"TMPDIR=" + dir}, sandbox.CacheFallbackEnv(home, runtime.GOOS,
		func(k string) string { v, _ := hostenv.Lookup(env, k); return v },
		filepath.Join(ws.Root, workspace.DirCache))...)
	out := append([]string(nil), env...)
	for _, kv := range set {
		k, _, _ := strings.Cut(kv, "=")
		out = hostenv.With(out, k, strings.TrimPrefix(kv, k+"="))
	}
	return out
}

// WithGrants implements dhitools.GrantRunner: a runner whose sandbox
// also admits one agent's granted write roots and program folders.
func (r execRunner) WithGrants(write, exec []string) dhitools.CommandRunner {
	sb := r.sandbox
	if re, ok := sb.(sandbox.RootExtender); ok && len(write) > 0 {
		if ext, err := re.WithExtraRoots(write); err == nil {
			sb = ext
		}
	}
	if re, ok := sb.(sandbox.ReadOnlyExtender); ok && len(exec) > 0 {
		if ext, err := re.WithExtraReadOnly(exec); err == nil {
			sb = ext
		}
	}
	r.sandbox = sb
	return r
}

func (r execRunner) Run(ctx context.Context, dir string, argv []string, allowNetwork bool) (string, error) {
	return r.RunEnv(ctx, dir, argv, allowNetwork, nil)
}

// RunEnv runs with extra env: the secrets the calling agent was granted
// (F-089), for this command only.
func (r execRunner) RunEnv(ctx context.Context, dir string, argv []string, allowNetwork bool, extra []string) (string, error) {
	ctx, cancel := context.WithTimeout(ctx, r.timeout)
	defer cancel()
	base := r.env
	if r.envFn != nil {
		base = r.envFn()
		// The folders on the current PATH run, a reloaded one included.
		home, _ := os.UserHomeDir()
		if roots := sandbox.ExecRoots(pathOf(base), home); len(roots) > 0 {
			if re, ok := r.sandbox.(sandbox.ReadOnlyExtender); ok {
				if ext, err := re.WithExtraReadOnly(roots); err == nil {
					r.sandbox = ext
				}
			}
		}
	}
	// Every run is inside the OS sandbox (F-113): writes stay in the
	// workspace and the package caches whether or not network is allowed.
	// Network is a declared scope (F-030 P2); a Noop adapter (the user's
	// explicit opt-out) cannot deny it, so a denied run refuses.
	if np, ok := r.sandbox.(sandbox.NetworkPolicy); ok {
		wrapped, werr := np.WrapNetwork(argv, allowNetwork)
		if werr != nil {
			return "", werr
		}
		argv = wrapped
	} else if !allowNetwork {
		return "", fmt.Errorf("run: network denied by scope but no OS sandbox can enforce it")
	}
	cmd := exec.CommandContext(ctx, argv[0], argv[1:]...)
	cmd.Dir = dir
	cmd.Env = append(append([]string(nil), base...), extra...)
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
	// bubblewrap guards only ref files that exist: a packed-only default
	// branch gets a loose copy from the default-ref repair, after a yes.
	for _, m := range ws.Members() {
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
			Principal:      c.Profile.Principal(who.Name, who.Email),
			MaxTurns:       c.Agents.MaxTurns,
			Resume:         c.Agents.Resume,
			Autonomy:       c.Agents.Autonomy,
			AllowNativeWeb: c.Agents.NativeWeb == settings.WebAllow,
			MaxIterations:  c.Agents.MaxIterations,
			Stall:          time.Duration(c.Agents.StallSeconds) * time.Second,
			Reflect:        c.Agents.Reflect,
			ReflectModel:   c.Agents.ReflectModel,
			TracePrompts:   c.Agents.TracePrompts,
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

// liveCrew is what the views need from the agent runtime.
type liveCrew interface {
	Handle(ctx context.Context, msg bus.Message)
	AgentIDs() []string
	Manifest(id string) (*manifest.Agent, bool)
	CancelChannel(channel string) int
}

func newAgentRuntime(ws *workspace.Workspace, b *bus.Bus, sb sandbox.Sandbox, cliEnv []string, defaultEngine string, gitRunner *gitcore.Runner, identityFn gitcore.IdentityFunc, sessionStore *ideation.Store, runRunner dhitools.CommandRunner, editor dhitools.EditorAPI, workspaceScopes scopes.Set, taskStore *tasks.Store, reviewSvc *review.Service, kbSearcher search.Searcher, mcpStore *mcpserver.Store, conv conventions.Source, cliLook func(string) (string, error), wt worktrees.Layout, tune func() agentkitRuntime.Tuning, gen conventions.Generator) *agentkitRuntime.Runtime {
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
		CLIEnvFunc:    hostenv.Env, // "Reload shell environment" reaches the next spawn
		// A turn waiting for bubblewrap opens its install card (F-113).
		OnHelperMissing: func() {
			if b, ok := editor.(*editorBridge); ok {
				go func() { _, _ = b.request(context.Background(), app.EditorRequest{Op: "install", Arg: "bwrap"}) }()
			}
		},
		Tasks:       taskStore,
		Org:         company,
		Standards:   true,
		Conventions: conv,
		ExtraRoots:  worktreeRoots(wt),
		// Bridged MCP servers get a private HOME inside .dhi, which the
		// sandbox lets them write (F-109).
		MCPHome:   filepath.Join(ws.Root, workspace.DHIDir, "mcp-home"),
		Workflows: true,
		Sandbox:   sb,
		Memory:    memStore,
		Knowledge: kbStore,
		Search:    kbSearcher,
		// M15 P1 tool seams: the user's git runner (git_diff), the F-029
		// identity resolver (git_commit), the shared ideation store
		// (ideation_*), and the allowlisted command runner (`run`). A nil
		// seam makes its tool refuse by name — never a host fallback.
		Git:         gitRunner,
		Checkpoints: workspaceCheckpoints(ws, gitRunner), // F-120: undo a chat turn
		Identity:    identityFn,
		Sessions:    sessionStore,
		MCPServers:  mcpStore,
		MCPLoad: func() *mcpserver.Store { // ADR-0043: servers added in Settings reach the next run
			return mcpserver.OpenWith(ws.Root, ws.Members())
		},
		HooksLoad:       enabledHooks(ws.Root), // F-179: plugin hooks the user turned on
		Run:             runRunner,
		Editor:          editor,
		WorkspaceScopes: workspaceScopes,
		// F-066: per-task / per-thread episodes (CLI session resume) and
		// the spend ledger budgets are checked against.
		Episodes: episode.Open(ws.Root),
		Spend:    budget.Open(ws.Root),
		Tuning:   tune,
		OnFailover: func(agent, from, to, why string) { // F-180: never silent
			if uiSend != nil {
				uiSend(app.ToastMsg{Kind: kit.ToastWarning, Title: agent + " switched engine: " + from + " to " + to,
					Detail: why + "; the fallback engine set for this agent took over"})
			}
		},
		OnRun:   otelOnRun(),           // F-164: optional trace export, off without OTEL_* env
		Lessons: lessons.Open(ws.Root), // F-068: learned rules, retrieval + reflection
		Forge:   forge.Open(ws.Root),   // F-069: agent-made tools, active once approved
		// F-070: agents propose settings changes; the human applies them.
		SetProps:     setprop.Open(ws.Root),
		SettingCheck: settingCheck(ws),
		// F-091 (ADR-0043): employees find skills in the plugin marketplace
		// or on GitHub and suggest them; DHI pins the commit and the human
		// installs it.
		SkillSearch:  skillSearch,
		SkillSuggest: skillSuggest(ws.Root),
		AllBots:      allBots(ws.Root), // F-107: given to every bot, new ones too
		MCPAuth:      mcpAuth,          // F-106: sign in with the vendor's own login
		// F-067: a task turn without a worktree gets one on the convention
		// branch before the agent starts, so the work lands where DHI can
		// verify it and open the PR.
		Toolchain: hostToolchain{},
		Provision: func(ctx context.Context, slug, member string, fill map[string]string) error {
			if taskStore == nil {
				return fmt.Errorf("task store unavailable")
			}
			c := conventions.Defaults()
			if conv != nil {
				if cc := conv(); cc != nil {
					c = *cc
				}
			}
			// F-182: the agent's fill (work_branch), else the default agent
			// writes the branch name's AI parts before the turn starts.
			branch, err := nameTaskBranch(ctx, c, taskStore, slug, member, fill, gen)
			if err != nil {
				return err
			}
			return taskStore.Attach(slug, member, branch, "")
		},
		ConventionVars: func(branch string) map[string]string {
			c := conventions.Defaults()
			if conv != nil {
				if cc := conv(); cc != nil {
					c = *cc
				}
			}
			return varsForBranch(taskStore, c, branch)
		},
		// F-020 pr_open: the review service opens task PRs; gh missing
		// refuses by name at dispatch. The agent wrote the AI parts
		// (fill); DHI never writes them behind its back.
		PR: func(ctx context.Context, req dhitools.PRRequest) (string, error) {
			if reviewSvc == nil {
				return "", fmt.Errorf("review service unavailable")
			}
			meta, err := reviewSvc.OpenPR(ctx, review.PRRequest{Member: req.Member, Branch: req.Branch,
				Title: req.Title, Base: req.Base, Fill: req.Fill})
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

	cwd, err := os.Getwd()
	if err != nil {
		fmt.Fprintln(os.Stderr, "dhi doctor:", err)
		return 1
	}

	report := doctor.Run(cwd)
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

// hostToolchain is the runtime's host-tool seam (F-110): verify names a
// missing toolchain and its install command; agents never install.
type hostToolchain struct{}

func (hostToolchain) Missing(name string) (string, bool) {
	t, ok := hosttools.Get(name)
	if !ok {
		return "", false
	}
	if _, found := hosttools.Find(t); found {
		return "", false
	}
	return hosttools.Missing(t), true
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
// workspaceCheckpoints is the undo store for chat turns (F-120); nil
// without git.
func workspaceCheckpoints(ws *workspace.Workspace, git *gitcore.Runner) *checkpoint.Store {
	if ws == nil || git == nil {
		return nil
	}
	cpMu.Lock() // one store per workspace: the runtime records, the editor undoes
	defer cpMu.Unlock()
	if s, ok := cpStores[ws.Root]; ok {
		return s
	}
	s := checkpoint.Open(ws.Root, git.WithTimeout(30*time.Second), func() []checkpoint.Member {
		var out []checkpoint.Member
		for _, mb := range ws.Members() {
			out = append(out, checkpoint.Member{Name: mb.Name, Path: mb.Path})
		}
		return out
	})
	cpStores[ws.Root] = s
	return s
}

var (
	cpMu     sync.Mutex
	cpStores = map[string]*checkpoint.Store{}
)

func memberDirs(ws *workspace.Workspace) []string {
	var dirs []string
	for _, mb := range ws.Members() {
		dirs = append(dirs, mb.Path)
	}
	return dirs
}

// learnConventions infers commit and PR style from the members' recent
// history (git) and their PR templates (F-082). A nil runner
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
func suggestIdentity(ctx context.Context, r *gitcore.Runner, dirs []string) []gitcore.Suggestion {
	var extra []gitcore.Suggestion
	if gh, err := hostLook("gh"); err == nil {
		{
			gctx, cancel := context.WithTimeout(ctx, 4*time.Second)
			if u, err := review.NewGHCLI(gh).User(gctx); err == nil {
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
func repairGate(ws *workspace.Workspace, git *gitcore.Runner, sb sandbox.Sandbox) *repairgate.Model {
	in := repair.Input{WS: ws, Library: library.Open(ws), UserConfig: settingsUserPath(), LegacyToolchain: hosttools.LegacyToolchainDir()}
	_, in.GuardRefs = sb.(*sandbox.Bubblewrap)
	if o, err := agentkitOrg.Load(ws.Root); err == nil {
		in.Org = o
	}
	if st, err := credstore.Open(); err == nil {
		in.Creds = st
	}
	if st, err := setup.LoadState(setup.WorkspaceStatePath(ws.Root)); err == nil && st.LocalIdentity != nil && git != nil {
		id := gitcore.Identity{Name: st.LocalIdentity.Name, Email: st.LocalIdentity.Email}
		in.LocalIdentity = st.LocalIdentity
		in.SetLocalIdentity = func(dir string) error {
			ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
			defer cancel()
			return gitcore.SetLocalIdentity(ctx, git, dir, id)
		}
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

// nonEmpty drops empty paths.
func nonEmpty(paths ...string) []string {
	var out []string
	for _, p := range paths {
		if p != "" {
			out = append(out, p)
		}
	}
	return out
}

// userTrust opens the user's trust record. When its folder cannot be
// located the store has no path, so nothing is trusted and trusting fails
// with a message (ADR-0011) instead of running repo code unasked.
func userTrust() *trust.Store {
	p, _ := trust.DefaultPath()
	return trust.New(p)
}

// initTrustedWorkspace is the setup wizard's workspace creation: a
// workspace the user just made on purpose counts as trusted (F-148).
func initTrustedWorkspace(root string, members []setup.MemberSpec) ([]setup.MemberSpec, error) {
	made, err := setup.InitWorkspace(root, members)
	if err != nil {
		return nil, err
	}
	st := userTrust()
	if err := st.Trust(root); err != nil {
		return nil, fmt.Errorf("workspace created, but trusting it failed: %w", err)
	}
	return made, nil
}
