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

- [ ] Workspace management: add/remove/rename member repos from the UI
      (local path or git clone); atomic `.dhi/workspace.toml` rewrite;
      live re-resolution without restart
- [ ] Org: create/edit/archive agents, teams, leads; marketplace packs install (path/git)
- [ ] Channels UI (#general, team channels, DMs) + threads + task cards
- [ ] Task↔ChangeSet binding (per-repo worktrees); kanban statuses
- [ ] Inspection dashboards: current work, activity, private memory, KB contributions
- [ ] Attach-points: invite any rostered agent into Editor/Ideator/Reviewer sessions

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
