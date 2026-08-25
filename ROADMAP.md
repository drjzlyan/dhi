# DHI ROADMAP

Status legend: `[ ]` planned · `[~]` in progress · `[x]` done.
Update this file **and** [STATE.md](STATE.md) at the end of every session.

## Product shape — five views

```
1 Workspace (boot) › 2 Editor › 3 Ideator › 4 Reviewer › 5 Settings
```

Feature specs: [F-003](docs/features/F-003-workspace.md) Workspace ·
[F-002](docs/features/F-002-editor.md) Editor ·
[F-004](docs/features/F-004-ideator.md) Ideator ·
[F-005](docs/features/F-005-reviewer.md) Reviewer ·
[F-006](docs/features/F-006-settings.md) Settings

---

## M0 — Skeleton & design system ✅ (2026-08-23)

- [x] Repo scaffold `github.com/drjzlyan/dhi`, Go 1.26, Bubble Tea/Lip Gloss v2 (`charm.land`)
- [x] Theme token system (`internal/tui/theme`) + raw-color lint enforcement test
- [x] Kit primitives: Panel, Tabs, StatusLine, List, Center (`internal/tui/kit`)
- [x] App shell: surface registry, router, global keys, help overlay
- [x] Golden-file snapshot harness (`internal/testutil/golden`, ANSI-stripped)
- [x] CI (vet + race tests + golangci-lint), Makefile, docs/ADRs 0001–0007
- [x] Five-view IA alignment: Workspace boot view w/ centered brand hero
      (`internal/tui/branding`), placeholders for all views; Home removed

## M1 — Hermetic toolchain & workspace domain *(foundation for every view)*

- [x] `toolchain.Manager`: resolve → download → sha256 verify → extract → activate → lockfile
- [x] Registry manifest embedded in binary; production pins for ripgrep 15.2.0,
      uv 0.12.5, node 24.19.0 LTS (darwin/arm64 + linux/amd64); git excluded
      per ADR-0008 (go-git). Live-artifact smoke test behind DHI_SMOKE_NET=1
- [x] XDG-isolated prefix `~/.local/share/dhi`; shim links + `Manager.Env()`
      PATH seam for child processes only
- [x] Animated Bootstrap surface (event-driven, deterministic in tests,
      reuses `branding`) + shell gate wiring on first run (`app.SetGate`)
- [x] `dhi doctor [--json]` check suite shared with in-app health panel
      (`internal/doctor`; human + JSON output wired in `cmd/dhi`)
- [x] Path-jail sandbox + permission policy engine (`internal/sandbox`) + OS-sandbox seam
- [x] Workspace domain: multi-repo model, `.dhi/workspace.toml`, VPath resolver;
      `.dhi/` dir-schema reservation for agents/memory/knowledge/channels/tasks
- [x] Tests: httptest fixture server, tamper cases, doctor JSON assertions

## M2 — **Editor core** (F-002) + Settings skeleton (F-006)

Status: complete (2026-08-23). Deferred to later milestones: syntax
highlighting, multi-tab terminal extras, worktree ops (M5), rich LSP
features (M7).
- [x] Multi-repo nav tree grouped by member repo; fuzzy find; ripgrep
      fan-out search (`surfaces/editor`, `internal/fuzzy`, `internal/search`;
      rg runs from the hermetic shim, no host fallback)
- [x] Modal editor MVP: normal/insert/visual/command, motions, d/c/y,
      `:w :q :wq :e` (`internal/textbuf`, TUI-free) + multi-buffer tabs
      with :bn/:bp/:b switching and a tab strip
- [x] PTY terminal drawer: one cwd-pinned tab per member repo (+ alt+n
      extra tabs), DHI toolchain PATH via Manager.Env; ANSI-stripped
      scrollback MVP (full VT emulation deferred to M7 polish)
- [~] Markdown preview (GitHub-style via glamour; ctrl+g on .md buffers,
      live re-render on edit)
- [x] Git view MVP (go-git, ADR-0008): status/stage/unstage/commit +
      log in a ctrl+j bottom panel; per-buffer repo selection
- [~] LSP foundation: minimal stdio JSON-RPC client (`internal/lsp`),
  servers resolved via toolchain shims; didOpen/didChange, diagnostics
  in gutter + title chip, ctrl+space completion popup. Go pinned in the
  registry (go1.27.0, digests cross-checked vs go.dev/dl) and gopls
  builds hermetically from source through it (`BuildInstall`, live
  smoke `DHI_SMOKE_BUILD=1`) since upstream ships no binaries
  (golang/go#79066); bootstrap auto-build + richer features track later
- [~] Settings skeleton: typed TOML schema, defaults<user<workspace
      precedence, unknown-key doctor warnings, live theme switch
      (`internal/settings`, real Settings view); keybinding overrides +
      remaining sections track later milestones

## M3 — Agent runtime → chat sidebar in Editor

Status: complete (2026-08-24). Deferred to later milestones: marketplace
pack installs + org UI (M4), embedding retrieval (ADR-0007 seam), richer
streaming render in sidebar (M7 polish).

- [x] `agentkit` manifest spec + validation; Provider iface (Anthropic
      SSE adapter, hand-rolled, httptest-verified + scripted Mock sharing
      one conformance suite) (`internal/agentkit/manifest`,
      `internal/agentkit/provider`)
- [x] Namespaced VPath tools behind path-jail policies (read/write/list/
      search; Ask decisions park in an approvals queue consumed by the
      sidebar); MCP client stdio+http bridged into the same guarded
      registry as `mcp__<server>__<tool>` (`internal/agentkit/tools`,
      `internal/mcp`)
- [x] Message bus (channels/DMs/threads, mention-triggered turns via the
      turn engine, JSONL persistence + replay) (`internal/agentkit/bus`,
      `internal/jsonl`, `internal/agentkit/runtime`)
- [x] Per-agent memory (journal.jsonl + notes.md) + shared KB w/ rg
      retrieval behind `KnowledgeStore`, review/auto contribution policy
      (`internal/agentkit/memory`, `internal/agentkit/knowledge`)
- [x] Editor chat sidebar: ctrl+a right panel, roster channels (#general +
      per-agent DMs), apply-suggestion→buffer (^f), keyboard approvals
      y/n (`surfaces/editor/chat.go`); doctor roster/env checks

## M4 — **Workspace full** (F-003)

Phased delivery; each phase lands on green `make verify`. Design
decisions: ADR-0009 (hermetic minimal git for worktrees), layered
coding standards (guidance-only, defaults→team→agent, injected at
prompt assembly).

### P0 — Hermetic git spike ✅ (2026-08-24)

- [x] ADR-0009: supersedes ADR-0008's git exclusion for worktree ops;
      go-git keeps clone/fetch/status/commit; CLI owns worktree lifecycle
- [x] `toolchain.Manager.GitEnv/GitBin/EnsureGitConfig`: shim-first PATH +
      hardening (`GIT_CONFIG_NOSYSTEM=1`, `GIT_TERMINAL_PROMPT=0`, managed
      global config w/ empty hooks dir); user terminals unaffected
- [x] `gitcore.Runner`: exec seam — Version, WorktreeAdd/List/Remove/
      Prune over porcelain; 2-min per-invocation timeout; no host fallback
- [x] Live smoke `DHI_SMOKE_GIT=1` round-trip validated on darwin/arm64
      against real git 2.55.0 built via `scripts/build-hermetic-git.sh`
      (4.2 MB binary, OS-provided libs only)
- [x] Doctor git suite (shim/version-pin agreement) wired into `Run()`
- [x] `.github/workflows/release-git.yml`: builds artifacts from pinned
      upstream source (NO_CURL NO_EXPAT NO_GETTEXT NO_PERL NO_TCLTK)
- [ ] **Release checklist:** dispatch release-git, publish artifacts as
      GitHub release `hermetic-git-v2.55.0`, cross-check source digest
      (kernel.org tarball sha256 recorded in session notes), flip the
      `git` entry into `registry/manifest.json` (supply-chain review)
      — until then doctor degrades visibly per ADR-0005

### P1 — Member management ✅ (2026-08-24)

- [x] `internal/workspace`: roster guarded by RWMutex (`Members()`
      snapshot), atomic `Save` (relative paths preserved under root),
      `AddMember`/`RemoveMember`/`RenameMember` persisting before
      visibility; last-member invariant; change events via `Subscribe`
- [x] Live re-resolution: editor watches roster changes — tree roots,
      search roots, fuzzy index rebuild; buffers/terminal sessions of
      removed members close; no restart
- [x] Workspace view: members pane (`a` add local-path-or-git-URL,
      `r` rename, `d` remove-with-confirm; working trees never deleted);
      add-by-URL clones async via go-git into `<root>/<name>` with
      half-clone cleanup; remaining sections render as dim roadmap rows
- [x] `gitcore.Clone` (in-process, ADR-0008/0009: network stays go-git)

### P2 — Org + marketplace + coding standards *(services landed; UI next)*

- [x] F-008 spec (`docs/features/F-008-marketplace.md`)
- [x] `.dhi/org.toml` sidecar registry (teams, leads; strict decode;
      atomic persist-before-commit; change subscriptions)
      — `internal/agentkit/org`
- [x] Agent CRUD: manifest Marshal/WriteFile validate-on-write round-trip;
      archive = move to `.dhi/agents/.archived/` (LoadDir skips dirs);
      restore; `internal/agentkit/org` crew ops + tests
- [x] `Runtime.Reload(roster)` — rebuild entries then swap under lock,
      in-flight turns finish on old entries; `Changes()` ping; editor
      chat sidebar refreshes channels on reload
- [x] Marketplace packs: pack.toml v1 (strict), local-path + git installs
      (go-git clone → temp → validate-all-then-install), same-pack
      idempotent update, cross-pack conflict refusal, provenance in
      `.dhi/marketplace.json`, uninstall-exactly-recorded
      — `internal/agentkit/pack`
- [x] Coding standards: built-ins → workspace → team(s) → agent layers
      (extend|replace), fresh-per-turn resolution, injected after
      grounding in `runtime.prompt()` behind Config.Standards+Org,
      write API validates slugs, doctor suite warns on parse failures +
      dangling team/agent refs — `internal/agentkit/standards`
- [x] UI (Workspace view): `[`/`]` section switcher over MEMBERS · ORG ·
      PACKS · STANDARDS; team create/edit/delete modals (lead, CSV
      membership); agent create form + archive confirm + restore;
      pack install modal (async, path|git) with uninstall confirm and
      provenance listing; standards editors per layer (CSV rules,
      extend/replace toggle via ←/→) with effective-block preview
      (`v`) resolving builtins+layers for any agent id
      — Settings-section integration deferred (single-host surface is
      the Workspace view; revisit if a second consumer appears)

### P3 — Channels UI (Slack floor) ✅ (2026-08-25)

- [x] CHANNELS section on the Workspace view (fifth `[`/`]` pane):
      rail = #general seeded + `#<team>` channels from the org registry
      + sorted DMs per rostered agent; selection survives rail rebuilds
- [x] Transcript with message cursor, word-wrap, thread drill-down
      (`t` opens root+replies view, `c` back; bus keeps threaded rows
      out of the top-level channel by design)
- [x] Composer (`i` focus, esc blur) posting as "you"; mentions/DMs
      route through a narrow `turnHandler` seam satisfied by
      *runtime.Runtime — posting works crew-less too (bus now created
      for every workspace in cmd/dhi, shared with the runtime)
- [x] Task-card refs: deferred to P4 where cards exist; transcript tags
      threaded replies with ↳ today

### P4 — Tasks ↔ ChangeSets + kanban ✅ (2026-08-25)

- [x] `internal/tasks`: per-card TOML under reserved `.dhi/tasks/<slug>.toml`
      (title/status backlog|active|in-review|done/assignee/team/thread
      binding/[[changeset]] records); strict decode, atomic persist-
      before-commit, Subscribe pings, malformed cards → store warnings
- [x] Worktree binding behind an injectable AttachFn/DetachFn seam:
      cmd/dhi wires gitcore.Runner (worktree add at
      `.dhi/tasks/<slug>/<member>` on `task/<slug>` branches, safe
      remove + prune); pre-registry-flip installs degrade with a visible
      "seam unavailable" error; card removal never deletes worktrees
- [x] TASKS section UI (sixth pane): grouped flow-order list with
      selected-card detail line (changesets, thread ref), n new / s
      cycle status / a assign / w attach / t bind-thread / x remove
- [x] Doctor tasks suite: malformed cards + dangling assignee/team/
      member refs warned by name

### P5 — Inspection + attach-points

- [ ] Per-agent profile: manifest inventory, current task/worktree
      state, activity timeline (bus + journal), read-only memory, KB
      contributions, pending approvals, effective standards preview
- [ ] Narrow Roster/invite interface replacing editor's concrete
      `*runtime.Runtime` dependency so M5/M6 attach-points plug in

## M5 — **Reviewer full** (F-005)

- [ ] PR/worktree input → auto review-worktree; diff UI (files, hunks, side-by-side)
- [ ] Line/hunk comment threads; pending-review batching; viewed marks
- [ ] Agent invites per-line/thread + complete-agent-review mode
- [ ] Completion: post to PR via `gh` | dispatch fixing agent | open-in-editor handoff

## M6 — **Ideator full** (F-004)

- [ ] Sessions: invited agent set + thread + artifact folder
- [ ] Read-only artifact tree + preview (markdown first); approve/reject flow
- [ ] Rejection→revision loop back to authoring agent; export later via MCP

## M7 — Hardening & polish

- [ ] Rich LSP features (hover, rename, refactor, code actions)
- [ ] Performance passes (large repos, many buffers), OS-sandbox adapters on by default
- [ ] Animation polish across bootstrap/transitions; reduced-motion honored everywhere
