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
      — package removed wholesale by ADR-0013 (2026-09-09); turns run
      through host CLIs via `internal/agentkit/clirun`.
- [x] Namespaced VPath tools behind path-jail policies (read/write/list/
      search; Ask decisions park in an approvals queue consumed by the
      sidebar); MCP client stdio+http bridged into the same guarded
      registry as `mcp__<server>__<tool>` (`internal/agentkit/tools`,
      `internal/mcp`)
      — tool registry removed (ADR-0013); approvals queue + `internal/mcp`
      retained (IDE-tool bridge is the forward path).
- [x] Message bus (channels/DMs/threads, mention-triggered turns via the
      turn engine, JSONL persistence + replay) (`internal/agentkit/bus`,
      `internal/jsonl`, `internal/agentkit/runtime`)
- [x] Per-agent memory (journal.jsonl + notes.md) + shared KB w/ rg
      retrieval behind `KnowledgeStore`, review/auto contribution policy
      (`internal/agentkit/memory`, `internal/agentkit/knowledge`)
- [x] Editor chat sidebar: ctrl+a right panel, roster channels (#general +
      per-agent DMs), apply-suggestion→buffer (^f), keyboard approvals
      y/n (`surfaces/editor/chat.go`); doctor roster/env checks

## M4 — **Workspace full** (F-003) ✅ (2026-08-25)

Phased delivery; each phase landed on green `make verify`. Design
decisions: ADR-0009 (hermetic minimal git for worktrees), layered
coding standards (guidance-only, defaults→team→agent, injected at
prompt assembly). Deferred to M7: unread markers, richer transcript
rendering, Settings-section integration of standards editing.

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
- [x] Fully automated pin pipeline: GPG-verified build (kernel.org
      detached sig vs pinned release-key fingerprint) → release publish
      → auto-PR re-hashing uploads into `registry/manifest.json`
      (`scripts/pin-git-manifest.py`, sidecar-checked, other tools
      untouched) — human work reduced to dispatch + merge
- [x] **Done:** `release-git` for v2.55.0 dispatched, pin PR merged,
      shim locked (`dhi doctor` reports hermetic git v2.55.0)

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

### P5 — Inspection + attach-points ✅ (2026-08-25)

- [x] `internal/agentkit/profile`: per-agent aggregation over
      independent sources (roster manifest, org teams, task store,
      bus activity timeline newest-first, memory journal+notes,
      KB contributions via new Store.ContributionsBy, layered
      standards block); every source degrades independently
- [x] INSPECT section (seventh pane): roster list with one-line
      summaries; enter/v expands the profile — identity+current work,
      model/tools/system, teams, activity, memory, KB, standards
- [x] Attach-point seams: `profile.Roster` (AgentIDs+Manifest) and
      wsview `turnHandler` both satisfied by *runtime.Runtime;
      editor chat + channels floor consume only these narrow
      interfaces, so M5 Reviewer / M6 Ideator plug in without churn

## M5 — **Reviewer full** (F-005) ✅ (2026-08-26)

Phased delivery P1–P6; each phase landed on green `make verify`.
Design decisions: PR objects fetched via go-git (`refs/pull/N/head` →
`refs/dhi/pr/N`) while metadata/diff/posting ride the host `gh` CLI
(doctor warns when absent); every review gets a dedicated worktree at
`.dhi/reviews/<id>/<member>` on a `review/<id>` branch so reviewing
never dirties working copies.

- [x] Diff engine (`internal/gitdiff`): unified-diff parser →
      GitHub-style file/hunk/line model + side-by-side pairing;
      golden fixtures; live-git round-trip validated
- [x] Review domain (`internal/review`): targets branch|worktree|pr,
      injectable worktree seam (gitcore wiring in cmd), gh seam,
      go-git Fetch/RemoteURL/Branches additions, TOML cards under
      `.dhi/reviews/` (threads, comments, viewed marks, pending batch,
      bus-thread correlation), orchestration service with raw-patch +
      parsed-diff production
- [x] Reviewer surface replacing the placeholder: REVIEWS · FILES ·
      DIFF panes, BOTH layouts (unified/side-by-side toggle `\`,
      auto-stacked columns, tab-expanded width math), hunk/file jumps,
      viewed marks, new-review modal (branch/worktree/PR)
- [x] Comments UI: line/hunk/file-level composer (`c`), thread
      drill-down (`t`) with reply/edit/delete/resolve, pending batch
      submit (`s`); drafts mutable, submitted immutable
- [x] Agent participation: @mentions dispatch through bus+runtime and
      replies mirror into threads live (MockProvider e2e per F-005);
      complete-agent-review mode (`A`) posts the diff to the review
      channel for a full pass; unmatched replies become file-level
      threads authored by the agent
- [x] Completion flows: post to PR via gh with agent attribution (`P`),
      dispatch fixer task bound to the SAME review worktree via new
      tasks.RecordChangeSet (`F`), open-in-editor handoff
      (`editor.OpenPaths` + `app.OpenInEditor`, key `e`); doctor gh check

### M5.x — PR round-trip addendum ✅ (2026-08-26)

- [x] Push + PR creation (`gitcore.Push/IsDirty`, `GH.CreatePR`,
      `Service.CreatePRForBranch/CreatePR`): pushes the worktree branch
      (go-git network path; `gh auth token` → BasicAuth) and opens a PR,
      refusing visibly on dirty trees; review cards link back
      (`Kind=pr`, `PRNumber`, `PRURL`) which lights up diff view, invites
      and posting. Reviewer key `C`; task cards gain `p` with additive
      `pr_number`/`pr_url` fields shown in card detail
- [x] Comment import + sync (`GH.ReviewComments/IssueComments`,
      `Service.ImportComments`): reply-chains map to threads anchored by
      file/line/side (outdated positions → `(remote)` file-level);
      append-only merge keyed by new `Comment.RemoteID` /
      `Thread.RemoteRoot` — local drafts never touched. Auto-import on
      opening a PR-backed review + manual `R`; FILES header badge shows
      `N remote · synced HH:MM`

### M5.x — agent-work-on-PR addendum (R1–R4) ✅ (2026-08-28)

- [x] R1 — git tools for agents (`git_commit`/`git_push` with GitRunner
      seam, write-gated approvals, manifest BuiltinTools allowlist)
- [x] R2 — manual commit/push on task cards (`c` message prompt, `u` push
      confirm; `tasks.Store.Commit/PushBranch`; fTaskCommit/fTaskPush modals)
- [x] R3 — fixer binds PR head branch (`Target.HeadBranch` persisted;
      dispatchFixer uses the PR head branch for PR-backed reviews so the
      fixing agent edits the branch that feeds the PR)
- [x] R4 — ownership-split posting (`GH.PostReviewComment`;
      `Service.PublishThreads`): own PRs (head branch exists locally) →
      real threaded review comments, line-anchored (`side`/`line`) with
      `in_reply_to` replies; external PRs → one consolidated issue
      comment containing only unresolved/actionable threads, pending
      drafts skipped and `_DHI agent @_` attribution stripped — nothing
      reveals agent identity on others' PRs (gh posts as the user).
      `Comment` gained `Side`/`Line`; `Posted` marked after publish

## M6 — **Ideator full** (F-004) ✅ (2026-09-01)

Design decisions: ADR-0010 (reserved `.dhi` vpath pseudo-member jailed
like member roots so agents `write` artifacts under
`.dhi/sessions/<slug>/` through manifest policies; per-session implicit
bus channels; content-hash-keyed artifact statuses so a fresh revision
flips back to draft). Deferred to M7: diagram/SVG preview, export to
repo paths / MCP issue trackers.

- [x] Sessions: invited agent set + implicit session channel +
      artifact folder — `internal/ideation` TOML cards under
      `.dhi/sessions/` (strict decode, malformed-card warnings,
      Subscribe pings; `.dhi/sessions` reserved in the workspace schema)
- [x] Read-only artifact tree + preview (markdown first via
      `internal/preview`, raw fallback; memoized renders) with
      draft→reviewed→approved/rejected statuses persisted per artifact
- [x] Rejection→revision loop back to the authoring agent (notes modal
      → session channel post @author → crew dispatch; authorship
      claimed from agent chatter); CHAT section with @mention dispatch;
      doctor `sessions/store` suite

## M7 — Hardening & polish ✅ (2026-09-08)

- [x] Rich LSP features (F-009, 2026-09-02): hover (`K`), rename (`gr`
      prompt → server WorkspaceEdit), code actions (`ga`, quickfix
      popup off cursor-line diagnostics), `workspace/applyEdit`
      routing into open buffers (bottom-up, one undo group), and the
      M2 diagnostics-clear gap fixed (publishes carry their path).
      Deferred: prepareRename validation, references/definition nav,
      auto-open-and-apply for closed files, hover markdown styling
- [x] Hardening + perf (F-010, 2026-09-02): OS-sandbox adapters on by
      default (seatbelt on darwin, bubblewrap on linux)
      behind `sandbox.Select`, injected into every agent guard from
      cmd/dhi via `runtime.Config.Sandbox`; `security.sandbox`
      setting (auto|off) is the escape hatch; doctor `sandbox/adapter`
      check. Perf: pre-lowered fuzzy index (20 017 → 16 allocs per
      keystroke at 20k paths), early-exit scoring, benchmarks for the
      editor hot paths, tab-strip overflow elision. Deferred: MCP
      stdio spawn wrapping (awaits first MCP consumer), ro-root
      policy differentiation
- [x] No silent fallbacks (F-011, 2026-09-02; ADR-0011): strict boot
      audit (`internal/boot` decision matrix: sandbox helper /
      workspace config / settings / lockfile are hard requirements),
      bootgate surface (block screen never releases; confirm-first
      install delegates to bootstrap + gopls source-build), settings
      strict (sanitize deleted; unknown keys/values refuse naming
      file+key), `term.Start` never leaks the host env, refused
      capabilities surface at use (search.Refused, drawer refusal,
      one-time LSP notice), standards refuse turns on malformed docs,
      tasks/sessions   malformed cards + gh shim + sandbox doctor rows
      now Fail. gh hermetic: `review.GHCLI` shim-bound + pin-gh.yml /
      pin-gh-manifest.py pipeline — **done (PR #3, gh v2.100.0,
      shim locked)**: digests CI-computed and cross-checked locally.
- [x] Animation polish + reduced-motion (F-012, 2026-09-08): `reduced_motion`
      setting (strict, layered, live) → `theme.Motion` switch; bootstrap
      spinner goes static (`GlyphBusy`, clock off) when reduced; view
      transitions fade in (2×100 ms, message-driven) on surface switch
      and gate release, instant when reduced. Goldens unchanged (fade is
      styling-only)

## M8 — Roster any agent *(Multica-inspired)*

Status: complete (P0–P4 landed 2026-09-09; wave-3 live-verify pending
installs).
Design decisions: ADR-0012 (host agent CLIs — user-owned, declared
env, OS sandbox is the boundary, runs not turns; the one named
exception to ADR-0005's hermetic rule), ADR-0013 (in-house engine
removed; CLI runtimes are the ONLY kind — `runtime` required and
CLI-only, approvals queue + manifest tools/policy retained). DHI
keeps its identity: single binary, local-first, hermetic DHI-owned
toolchain, no server/daemon.

- [x] P0 — specs + ADR (F-013…F-016, ADR-0012, 2026-09-08)
- [x] P1 — CLI runtimes (F-013): `internal/agentkit/clirun` registry
      (adapter = argv builder + stream parser + cost extractor +
      declared env pass-through, all fixture-tested); manifest
      `runtime` key (required, strict CLI enum); sandbox-wrapped
      headless spawn in the task worktree; transcript → bus thread;
      `[[run]]` records on task cards; timeout + retry policy;
      doctor `runtime/<cli>` rows.
      Wave 1 (LANDED): claude adapter — in-house engine removed by
      ADR-0013, `runtime=""|"anthropic"` gone, tests script fixture
      CLI stubs, `.dhi/agents/dev.toml` on claude.
      Wave 2 (LANDED 2026-09-09): codex (0.147.0) + opencode (1.18.25)
      adapters live-verified; executor gains retry loop (30s backoff,
      ms-compressed in tests via injected clock) + persisted
      `runs/<run>-<attempt>.jsonl` transcripts (F-013 step 4).
      Wave 3 (LANDED fixture-first 2026-09-09): cursor-agent, copilot,
      gemini adapters + fixtures to the documented contracts; live-verify
      checklists recorded in each adapter file — `Tested` stays empty
      until a real run fills them, so doctor treats a detected version
      as untested (FAIL), never a guess
- [x] P2 — run observability (F-014, 2026-09-09): uniform `cli:<name>`
      run schema (`[[run]]` gains `exit` + declared `cost:` marker,
      strict decode refuses unknown status with the value named);
      `tasks/runs` rollup math (per-task + per-agent: ok/fail/timeout,
      token sums excluding `-1` with a partial marker, cost over costed
      runs only); INSPECT profile RUNS subsection (totals + last 5
      runs); run-replay pane (`r` on a card, `e` on an INSPECT agent —
      transcript jsonl chronological, wrapped, scrollable, named
      "transcript unavailable at <path>" refusal on missing files);
      task-detail runs suffix (`N runs · $cost` / `cost partial`);
      doctor `runs/store` row (line-precise warnings) wired into the
      JSON report; 3 goldens
- [x] P3 — autopilots (F-015): `.dhi/autopilots/` cards (strict),
      due-on-launch catch-up (one missed run, no backfill) +
      in-session interval ticks, AUTOPILOTS pane — landed 2026-09-09:
      8th pane (secAutopilots) with `n` new (schema-validated form),
      `e` arm/pause, `r` run now, `x` remove-with-confirm, `o` last
      transcript; `internal/autopilot` strict store (`ParseSchedule`
      names bad values; duplicate-create refused; unknown-key refusal)
      with pure `Due`/`Next` table-tested math (interval from first
      run, daily weekly-anchor, `weekly` dow wrap); Store keeps
      armSeq roster (`strings.Join(ids)`), cards validated before
      write; docs-only run = readiness probe (not advisory);
      **execution** = `dm:<agent>` post + `[autopilot <slug>] <prompt>`
      tagged bus message through the same runtime seam as @-mentions
      (same timeout/retry/review gate), success-only `MarkRan` (crash
      never double-runs catch-up), dangling agent → named refusal, never
      marked; **ticks**: interval cards arm `tea.Tick(NextArm)` re-arms
      on `autopilotTickMsg`; paused/absent store = nil (no chain);
      catch-up runs due set in slug order at launch (workspace Init);
      doctor `autopilots` row (malformed card → Fail naming slugs,
      dangling agent → Warn, line-precise); 2 goldens
- [x] P4 — inbox (F-016): pure aggregation (approvals / unreplied
      @-mentions / failed runs / in-review tasks), INBOX pane with
      jump-to-owner, `!N` statusline marker — landed 2026-09-09:
      `internal/inbox` pure `Build(apprs, bus, tasks)` (severity
      approval > run_failed > in_review > mention, then oldest-first,
      table-tested; no state written); mention rule = a message @-ing
      the human ("you") with no later "you" message in its bus thread
      (replied → gone next render; unread DM mention stays); INBOX is
      the 9th `[`/`]` pane, `enter`/`o` jumps to the owner (editor
      chat approvals, CHANNELS thread at the mention, run-replay pane,
      Reviewer card) via narrow injected seams that degrade to a named
      visible hint when absent; `!N` statusline segment appears iff
      N > 0 and is recomputed per frame so it clears on resolution;
      3 goldens

## M9 — True unread *(read marks the human can feel)* ✅ (2026-09-09)

Status: complete (P0–P3 landed 2026-09-09). Closes the M4-deferred
unread marker that F-016 only approximated with a stateless mention
rule. User decisions (2026-09-09): Slack-style read watermarks; items
= all unreplied agent messages addressed to me + @you mentions (never
agent-to-agent chatter); scope = CHANNELS rail + editor chat + INBOX;
snooze included.

- [x] P0 — spec (F-017-true-unread.md, 2026-09-09)
- [x] P1 — `internal/unread` store (strict `.dhi/unread.json`,
      monotonic `MarkRead`, fresh-install seeding, subscribe pump) +
      `AddressedToHuman` predicate + `inbox.Build` mention→
      agent_message swap + doctor `unread/store` row — landed
      2026-09-09 (f7bc4e1)
- [x] P2 — read-on-open/post wiring (workspace CHANNELS + editor chat
      shared store) + `●`/`●N` rail markers (both surfaces) — landed
      2026-09-09: one store opened in cmd/dhi, injected into the
      workspace Deps + editor `WithUnread`; chatPane `onRead` seam
      fires on channel switch, thread open (`t`), inbox mention jump
      (`openAt` marks the thread scope) and post; Scan treats a
      top-level message whose thread was opened as read (jump truly
      resolves the row); `●`/`●N` markers on the CHANNELS rail and the
      editor chat header (dot = 1, ●N otherwise, theme danger);
      `unread.Counts` per frame via `syncUnread` in View
- [x] P3 — snooze: `z` preset form, dimmed rows, `!N` exclusion,
      30s tick chain for expiries; goldens — landed 2026-09-09:
      `z` opens a toggle form with presets 15m/1h/4h/tomorrow 09:00
      (pure `parseSnoozePreset`, day-wrap aware, unknown names the
      value); `z` again/`u` unsnoozes; snooze applies to agent messages
      only (the store schema keys on channel+messageID — other kinds
      refuse by name); snoozed rows stay visible dimmed with
      "snoozed until HH:MM" (cross-midnight: "HH:MM Mon d"), enter
      refuses the jump naming the expiry; `AttentionCount` + rail count
      exclude parked items so `!N` clears; single 30s `snoozeTickMsg`
      chain armed in Init/after snooze re-arms while snoozes are
      pending (deterministic via explicit ticks); INBOX golden

## M10 — Agent lifecycle & full IDE parity *(the user's crew goal)* ✅ (2026-09-09)

Status: complete (P0–P3 landed 2026-09-09). User goal: create agents in
Settings, add agents from GitHub, and give agents the IDE tools for
everything — editing, reading files, suggesting changes, chatting in
channels and with the user — to complete work end-to-end. Decisions
(2026-09-09): one GitHub flow (manifest or pack, auto-detected); full
agent CRUD in Settings; full IDE parity via the audited tool seam.

- [x] P0 — specs (F-018/F-019/F-020, 2026-09-09); survey found the
      dead seam: `runtime.Reload` has no callers — roster changes do
      not go live until restart (F-018 Part C fixes it)
- [x] P1 — F-018: Settings AGENTS section (full CRUD via org crew
      ops, strict manifest forms) + the live-reload pump — landed
      2026-09-09: Settings gains `[`/`]` sections (CONFIG · AGENTS);
      create/edit forms validate via the strict `Marshal`→`Parse`
      round-trip (id immutable on edit, unknown tool/runtime named),
      writes go through `org.CreateAgent/UpdateAgent/ArchiveAgent/
      RestoreAgent` + NEW `DeleteAgent` (removes active OR archived
      copy; active `x` archives first — the soft path is the default);
      every successful crew op drives the injected reload seam
      (`LoadRoster` → `runtime.Reload`, atomic, failure keeps the
      previous roster); live-reload contract proven by a runtime test
      (new manifest on disk → @mention routes without restart)
- [x] P2 — F-019: one add-from-source flow (git URL or path;
      pack.toml → pack install, else manifest import; validate-all-
      before-write; named skips) in Settings — landed 2026-09-09:
      `g` on AGENTS opens the source form (busy while in flight, async
      outcome via the surface's event channel); `#sub/path` fragment
      scopes the import; pack.toml delegates to the provenance-tracked
      pack install; bare manifests strict-parse ALL before the first
      write (one bad file refuses the batch naming file + reason);
      duplicate ids skip named (`scout (already exists)`); reload seam
      drives the imported agents live
- [x] P3 — F-020: parity matrix + toolbridge (DHI-namespaced tool
      calls from CLI adapters → tasks/PR actions, allowlist-gated,
      approvals for mutating ops) — landed 2026-09-09: `internal/
      agentkit/toolbridge` executes ```dhi-action blocks from the
      turn's final message (one code path for all six adapters — the
      stream-interception draft was a documented deviation: no
      structured tool args in the neutral event model, no mid-turn
      result channel for run-to-completion CLIs); builtins gain
      task_create/task_status/task_assign/pr_open (strict args, unknown
      keys named); manifest allowlist gates every action; mutating ops
      cross `tools.Approvals.Ask` (the human's y/n); results and
      refusals post to the trigger thread so the agent sees the
      outcome next turn; system prompt carries the block contract
      exactly when the allowlist includes bridge actions; pr_open
      resolves the card's changeset through the review service (gh
      missing refuses by name); runtime tests cover end-to-end
      create/refuse/approve + prompt contract

## M11 — Dashboard floor & Settings management *(IA restructure)* ✅ (2026-09-10)

Status: complete (P0–P5 landed 2026-09-10). User goal: workspace = a
JIRA-like dashboard (board + notifications + team chat + repos);
agent/team management lives in Settings; UI mirrors JIRA/Slack; theme
gets continuous borders, section background shades, working dialogs.
Design decisions: ADR-0014 (workspace IA restructure — four sections,
management in Settings, autopilot execution stays on the boot view).

- [x] P0 — specs (F-021/F-022/F-023/F-024) + ADR-0014, 2026-09-10
- [x] P1 — F-024 foundation: kit Panel top-edge corner fix (every
      titled panel's top row was one column short — 42 goldens
      regenerated deliberately); theme bg tokens (BgInset/BgOverlay +
      InsetBg/OverlayDim/ElevatedBg/Chip/DialogEdge helpers);
      kit.Modal + kit.Overlay (dimmed backdrop, veil-padded, styled
      clip via new ansi.Clip) + kit.Form + kit.Columns
- [x] P2 — F-023: Settings TEAMS · PACKS · STANDARDS · AUTOPILOTS
      sections (kit.Form dialogs over dimmed backdrops; run-now
      through the DM seam with named dangling-agent refusal) + agent
      profile modal in AGENTS; one shared autopilot.Store wired from
      main (execution stays on the workspace)
- [x] P3 — F-021: workspace → INBOX · BOARD · CHANNELS · REPOS
      (section enum 9 → 4, BOARD is the landing section); kanban lanes
      with per-lane cursors + JIRA-style fact pane; focus follows the
      card on status change; `o` jumps the bound thread/assignee DM to
      the floor; INBOX jump seams retargeted; ORG/PACKS/STANDARDS/
      INSPECT/AUTOPILOTS panes deleted
- [x] P4 — F-022: CHANNELS Slack floor — vertical rail (CHANNELS +
      DIRECT MESSAGES groups, unread dots, tab-focus rail nav),
      transcript + composer center, right context pane (thread beside
      the transcript; `v` agent profile via profile.Build); narrow
      (<100 cols) keeps the inline drill-down; openAt/watermark
      contracts preserved
- [x] P5 — F-024 adoption sweep: workspace/settings/ideator/reviewer
      dialogs + the app help overlay all render through kit.Modal +
      kit.Overlay (dimmed backdrop, centered box); the four duplicated
      stackOver/overlayCentered/dimLines implementations deleted;
      rail/sidebar inset shades; final golden regeneration + closeout

## M12 — Coherent UX *(bottom chrome, shaded zones, responsive)*

Status: in progress (P0 landed 2026-09-10). User goal: responsive,
consistent UI that uses all available space; keymap instructions at the
BOTTOM on a distinct chrome background (never the focus); different
background shades per zone; best-practice UX across the IDE.
Decisions: F-025 (bottom chrome bar, left-rail Settings, contextual
statusline, full-width narrow stacks).

- [x] P0 — spec (F-025), 2026-09-10
- [ ] P1 — kit foundation: theme.BgChrome; kit.HintBar; kit.Rail;
      shared breakpoints; kit.Column lane accent
- [ ] P2 — contextual statusline (mode chip + surface › zone +
      per-surface hints via interface assertions)
- [ ] P3 — Settings left-rail IA + HintBar
- [ ] P4 — Workspace: keymaps to pane foot, board lane chips +
      ElevatedBg detail, compact full-width stack
- [ ] P5 — Editor/Ideator/Reviewer: shaded rails, hints to foot,
      zone shades
- [ ] P6 — full golden regen + closeout docs
