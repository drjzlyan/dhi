# STATE — current position

Updated: 2026-10-04 (session 43: F-025 deferred item — per-pane
scrollbars, right-edge thumb. Roadmap M15–M20 and all follow-ons
complete; `make verify` green.)

### Session 43 (F-025 follow-on: per-pane scrollbars)

- `kit.Panel.SetScroll(Scroller)` paints a `█` thumb on the pane's right
  edge (track `│` on the border shade) when the window overflows the
  body budget; a fitted scroller leaves the plain edge. Shares glyphs
  with `Scroller.Scrollbar`; `kit.NewScroller(total, height, offset)`
  builds one from a surface-owned offset (offset is unexported).
- Reviewer: mainPane now sets the HintBar as a real `SetFooter` row (so
  the track spans exactly the body), and wires scrollbars for the DIFF
  viewport (`m.scroll`) and the run-transcript pane (`m.transcriptScroll`);
  `diffSegmentsView` is the shared wrap geometry for render + bar.
- Other surfaces (inbox/board/files) clip rather than scroll, so they
  have no window to paint yet — adoption is follow-on as panes gain
  offsets.
- Tests: kit right-edge thumb (present when overflowing, absent when
  fitted); reviewer DIFF fits→no bar / overflow→bar. `make verify` green.

### Session 42 (F-009 follow-on: prepareRename validation)

- `lsp.Client.PrepareRename(path, line, col) (Range, bool, error)`
  handles all three wire shapes (bare range, `{range,placeholder}`,
  `{defaultBehavior:true}`) and `null` (declines).
- Editor rename flow: `dispatchRename` now calls prepareRename in the
  async goroutine before `textDocument/rename`; a valid range anchors the
  edit (beats the client word guess), an explicit decline surfaces
  `lsp: not renameable here` (new `lspMsgNote`) and sends nothing, and
  an unsupported/erroring server degrades to the client range. The
  prompt still opens instantly off the client word.
- Tests: lsp PrepareRename shapes table; editor rename flow now exercises
  the prepareRename round-trip + a decline test (buffer untouched).
  `make verify` green.

### Session 41 (F-015 follow-on: per-autopilot task cards)

- Autopilot cards gained an opt-in `create_task` flag (strict TOML,
  default false; `SetCreateTask`), toggled in Settings → AUTOPILOTS
  with key `c` (row shows a `task` marker).
- When set, each run (workspace scheduled path *and* Settings run-now)
  creates a task card `auto-<slug>-<msgID>` and `BindThread`s it to the
  run's DM message, so the run's record lands on the card.
- `runtime.recordRun` now resolves the card via `bus.ThreadOf(trigger)`
  instead of `trigger.Thread`, so a **top-level** trigger (autopilot DM,
  thread 0 ⇒ its own id) matches the bound card. Backward compatible:
  threaded triggers are unchanged (`ThreadOf` returns `Thread`).
- Tests: autopilot `SetCreateTask` persistence; workspace create/bind +
  default-off; runtime `recordRun` top-level matching. Settings golden
  regenerated (hint line). `make verify` green.

### Session 40 (docs: fix stale statuses + close landed deferrals)

- Feature status headers that still read "planned"/"in progress" now
  reflect reality: F-003, F-006, F-008, F-013, F-018, F-019, F-027,
  F-030 (in progress → complete), F-031, F-032. No feature file reads
  planned/in-progress anymore.
- Deferred sections: struck-through + annotated the items that have
  since **landed** (F-010 MCP-stdio sandbox; F-016 true-unread &
  per-item snooze; F-026 LSP nav + ideator preview; F-028 fs tools;
  F-031/F-034 executable skill scripts; F-034 index signatures) and the
  items **closed (not planned)** (F-012 easings + terminal detection;
  F-016 external notifications; F-017 multi-human; F-020 MCP
  registries; F-025 tab badges + empty-state; F-029 per-agent
  identity; F-033 voice/video). The remaining deferred entries are the
  genuinely open/design items.

### Session 39 (fix a real Approvals data race)

- The intermittent editor `DATA RACE` was **production**, not test-only:
  `tools.Approvals.signal()` read the `changes` channel field while
  `Changes()` lazily created it, unsynchronised — a concurrent
  `AskScope` + `Changes()` mutated/read the same pointer. (It also made
  `TestChatApprovalFlow` hang past its deadline.)
- Fix: the channel is created once in `NewApprovals` and never
  reassigned, so `Changes()` returns it and `signal()` reads it
  lock-free (no `a.mu` — `Resolve` already holds it, so a locking
  `signal` would self-deadlock). Documented the constructor requirement.
- Reproduced with `editor -race -count=20`; after the fix 20 iterations
  are clean. Added `TestApprovalsConcurrentChangesAndAsk` (bounded
  concurrency under `-race`). `make verify` green.

### Session 38 (drop the deferred api engine kind — cleanup)

- Per decision, removed the never-built `engine = "api:<provider>"`
  path rather than leaving it deferred. Code: `manifest.ParseEngine` no
  longer special-cases `api:` (it is now a plain unknown-kind refusal);
  the `case "api"` branch + "not built yet" message are gone and the
  test asserts the unknown-kind refusal.
- Docs: new **ADR-0024** supersedes ADR-0019 §5's forwarding clause
  (ADR-0019 is otherwise untouched — append-only); ROADMAP drops the
  `api:<provider>` deferred bullet and the "only deferred item" status
  note; F-030 drops the Part D/acceptance/Deferred `api:` mentions;
  product.md states best-effort containment plainly with no future path.
- No remaining `api:` engine artifact in code or docs (ADR-0019/0024
  history excepted). `make verify` green.

### Session 37 (de-flake the async test waits)

- The long-standing "known flakes" (`TestCLIRuntimeEndToEnd`,
  `TestMemoryAndKBReachTheSystemBlock`, clirun `TestDetect`, editor
  `TestChatApprovalFlow`) were **tight test deadlines**, not product
  bugs: their helper waits (2–5 s) expired under `-race` + full-suite
  contention while a stub CLI subprocess was spawning.
- Introduced a shared `asyncTimeout = 30s` in the reviewer, runtime, and
  editor test packages and used it for `pumpCmd`/`waitReply`/chat
  outcome waits; raised the `runtime_clitest` drain to it; bumped the
  production `Registry.Detect` per-adapter probe 5s → 10s (a stub
  `--version` under load can exceed 5s; still bounded).
- `make verify` (gofmt + vet + race) is now green on **three consecutive
  full runs** — the flake caveats in earlier session notes no longer
  apply. (A one-off editor `DATA RACE` warning seen earlier did not
  reproduce across these runs; keep an eye out.)

### Session 36 (M19 follow-on — executable skill scripts)

- Skills gained an optional `script: <rel-path>` frontmatter key
  (relative to `.dhi/skills`, no `..`/absolute; `validScriptRel`).
  `library.Store.Script(slug)` resolves it, refusing builtin/absent/
  non-executable/escaping paths by name. Scripts run directly (shebang,
  no shell) under the sandbox.
- Human path: LIBRARY `r` on a skill runs its script (confirm → async
  through the injected runner, network denied) and shows stdout/err in a
  dialog. New Settings `Deps.RunScript` seam (wired from `runRunner`).
- Agent path: served `skill_run` tool (dhitools `Deps.Skills *library.Store`
  wired from `runtime.lib()`, scope `Exec`, approval-gated, network per
  the network scope). `runPlan` carries skill+args; argv = [script, ...].
- Tests: library resolution/frontmatter, dhitools skill_run
  (happy/denied/no-runner/no-library/unknown), settings run flow.
  Goldens regenerated (LIBRARY hint bar). `make verify` green.
- Docs: F-027 deferred item struck; LIBRARY hints add `r run script`.

### Session 35 (M19 follow-on — pack-sourced LIBRARY badges)

- Settings LIBRARY now badges pack-installed roles/skills with
  `pack:<name>` (from `pack.Installer.Records()` provenance), distinct
  from `builtin` and `local`. `libPackBadges` reads the marketplace
  records once per render; a malformed provenance simply yields no
  badge (the listing never fails). Tests cover the badge for a role and
  a skill from one installed pack.
- Docs: F-027 LIBRARY source column updated.
- Observed once under full-suite `-race` load (did not reproduce isolated
  over 3 runs, nor on the next full run): a `DATA RACE` warning +
  `TestChatApprovalFlow` timeout in `internal/tui/surfaces/editor`.
  Treat as an intermittent to watch; the editor package passes `-race`
  isolated.

### Session 34 (M19 follow-on — index signature verification)

- `internal/agentkit/registry/sign.go`: Ed25519 publisher signatures. A
  detached `index.toml.sig` (lowercase hex) covers the raw `index.toml`
  bytes; `ParsePublicKey`/`PublicKeyHex`/`VerifyIndex`/`SignIndex`.
  Pinned keys live in `.dhi/registry/trusted_keys`
  (`TrustedKeys`/`TrustKey`/`UntrustKey`; malformed pins refuse by name).
- Policy: with a key pinned, `Refresh` REQUIRES a verifying signature
  (unsigned source refuses by name) and the cached sig is re-verified on
  every `index()` read (tamper/legacy-unsigned cache refuses); with no
  key, digest-only mode (documented). Invalid signer/untrusted key
  refuses and caches nothing.
- Settings MARKETPLACE: `t` pins a publisher key (form), `T` clears
  (confirm); the section shows the trust mode.
- Tests: sign round-trip/refusals, trust idempotency, refresh policy,
  tampered/unsigned cache, marketplace pin/clear. `make verify` green.
- Docs: F-034 registry bullet updated.

### Session 33 (M18 follow-on — pending proposals in the inbox)

- `inbox.Build` gained a 4th source `props []ideation.Proposal`; new
  `Proposal` ItemKind (severity just below `approval`) with
  `ProposalID/Name/Caller`; row names the caller, name and mode, jump
  targets the Ideator.
- `ideator.Model.SelectProposal(id)` focuses SESSIONS on the pending
  proposal (false when decided/gone). `App.SelectProposal` routes it
  (mirrors `SelectReviewer`); workspace `Deps.Sessions` +
  `Deps.OpenIdeator` aggregate + jump (nil seam degrades with a named
  hint). `cmd/dhi` wires `sessionStore` + `appRef.SelectProposal`.
- Tests: inbox proposal item + rank, ideator `SelectProposal`,
  workspace proposal row + jump/degrade. `make verify` green.
- Docs: F-016 sources table gains `proposal` (+ the previously-undocu-
  mented `dependency` row) and the severity order.

### Session 32 (M15 P3a + P3b — antigravity MCP; dhi-action removed)

- `agy` reads MCP servers only from `<gemini_dir>/config/mcp_config.json`
  and takes the dir via the undocumented-but-working `--gemini_dir`
  flag (`agy --gemini_dir=<dir> mcp list` honours it; the flag parses
  before the subcommand, not after). It does NOT honour a plain HOME
  swap for auth: a mirror-HOME boots to "Authentication required" even
  with `~/.gemini/oauth_creds.json` symlinked, so HOME must stay real.
- New clirun seam `MCPGeminiDir func(baseDir, configBody) (dir, cleanup,
  err)` + `MCPGeminiDirFlag`; `RunInput.MCPGeminiDir`. Antigravity's
  helper `antigravityMCPGeminiDir` builds a per-turn mirror of the real
  `~/.gemini` (symlinks every entry except `config/mcp_config.json`,
  which carries DHI's loopback endpoint). The runtime writes the mirror
  under `<ws.Root>/.dhi/mcp/gemini-*` (admitted + gitignored) and passes
  `--gemini_dir=<mirror>`; cleanup removes it after the turn.
- `MCPOK` flipped for antigravity; `MCPWired` now accepts the dir path.
  All six adapters (claude/codex/opencode/cursor/copilot/antigravity)
  are MCP-wired. Live-verified: `DHI_LIVE_MCP=1 go test
  ./internal/agentkit/clirun/ -run TestLiveAntigravityMCP` — agy called
  DHI's served `echo_probe` (2026-09-30, agy 1.2.11).
- Tests: `TestAntigravityGeminiDirMirror` (symlink/mcp-file layout +
  cleanup), `TestAntigravityMCPArgs`, updated `TestMCPConfigWiring`
  (dir-delivery branch), `TestMCPCapabilityDeclared` (+antigravity),
  runtime `TestServeToolsGeminiDirForAntigravity` (replaces the obsolete
  non-MCP-fallback test — no adapter is non-MCP now), doctor
  `TestAgentToolsNoEngineWarns` (replaces the fallback-warns test).
- Gotcha: with every adapter MCP-capable, the "no verified MCP wiring"
  doctor/fallback test cases are unreachable via real adapters; the only
  remaining warn is "no engine and no workspace default".
- Known flakes unchanged. Untracked `.dhi/agents/dev/` — do NOT commit.
- **P3b done in the same session — `dhi-action` removed at parity:**
  `pr_open` is now a served dhitools tool (new `dhitools.PRSeam` in
  `Deps.PR`, scope `Push`, workflow `pr` gate + approval + per-changeset
  fan-out; task_create/status/assign were already served). Deleted the
  `internal/agentkit/toolbridge` package and the runtime's
  `dispatchActions`/`bridge`/`allowedActions`/`cliPrompt` advertising
  path (and the `mcp` param); `Config.PR` is now `dhitools.PRSeam`
  wired into `serveTools`. Updated doctor `agent-tools` wording and
  many "fallback" comments; removed the obsolete runtime action tests;
  added `TestPROpenTool`/`TestPROpenRefusals`. `make verify` green.

### Session 31 (M20 — depth & cohesion, F-035)

- **P0 board Jira depth** (`d9aa3f2`): task schema 2 adds
  `Labels/Priority/Epic/Due` (schema-1 cards load unchanged) with
  `SetLabels/SetPriority/SetEpic/SetDue/NormalizeLabels/PriorityRank/
  ValidPriority`; board keys `L`/`P`/`E`/`D`, `/` filter over
  title/slug/labels/epic/assignee, `space` mark + `M` bulk move + `C`
  clear; priority glyphs + metadata line in the card detail.
- **P1 channel depth** (`depth P1`): new `internal/agentkit/channelmeta`
  stores reactions/edits/pins over `.dhi/channels/meta.json` (atomic;
  malformed refuses by name) — the bus JSONL stays append-only. Chat
  pane gains `/` search, `+` reaction picker (tokens +1/ok/?/!),
  `e` edit of the human's own messages, `p` pin; rows render
  `(edited)`/`[+1]`/`pin` markers and the header shows the pinned count.
  Goldens regenerated (channel hints; board help).
- **P2 live work log** (`depth P2`): `Runtime.Working` tracks in-flight
  turns per thread (key `channel#root`, set in `Turn` around `cliTurn`
  AFTER the strict refusals). New workspace `Deps.Working` seam; the
  board shows a `●` working glyph on the bound card and a live
  "agent working — live in the thread" detail line. Progress/command/
  error events already streamed into the thread; replay stays durable.
- **P3 editor co-editing** (`depth P3`): agent `editor_apply_edit`
  reaches `editor.Model.ApplyReplace` (live buffer, one undo step);
  that path stamps the edit and the buffer title shows
  `● agent editing` for a 6s window (`agentEditWindow`), path-scoped —
  truthful, no fake state.
- Known flakes (pass isolated): `TestCLIRuntimeEndToEnd` (reviewer),
  `TestChatGrantAlways` (editor), `TestMemoryAndKBReachTheSystemBlock`,
  clirun `TestDetect`. Untracked `.dhi/agents/dev/` — do NOT commit.

### Session 30 (M19 — pack registry & MCP install, F-034)

- **P0 extended pack kinds**: pack schema 2 adds `roles`, `skills`,
  `standards`, `mcp_servers` (plus agents/workflows). All-before-write
  validation; conflicts refuse by name; provenance records every kind;
  uninstall removes exactly what it wrote. New `internal/agentkit/
  mcpserver` (`.dhi/mcp/<slug>.toml`, stdio/http, declared env NAMES
  only, declared origins) joins the reserved `.dhi` tree. Roles/skills
  validate via new `library.ParseRole/ParseSkill` + `WriteRole/
  WriteSkill`; standards fragments merge into the workspace layer by
  value. Settings PACKS shows per-kind counts.
- **P1 signed registry**: `internal/agentkit/registry` — index.toml pins
  a SHA-256 content digest per pack; `Refresh` clones via hermetic git &
  caches `.dhi/registry`; `Install` recomputes the canonical digest
  (path+content, `.git` ignored) and refuses by name on mismatch;
  `pack.InstallDir` installs a pre-resolved verified dir. Settings
  MARKETPLACE section (r refresh / f search / enter digest-verified
  install / v inspect); doctor `registry`, `packs/provenance`,
  `mcp/servers` rows.
- **P2 MCP under trust**: `internal/agentkit/mcpbridge` dials installed
  servers for a turn; per-agent allowlist via `mcp__<server>__<tool>`;
  stdio spawns through the OS sandbox (network only when origins are
  declared); credentials resolve by name from env / macOS keychain
  (never `.dhi/`); every call crosses network scope (deny-by-default)
  and approvals. `scopes.ToolScope` maps `mcp__*` → Network.
  `serveTools` composes DHI + bridge via a prefix-routing
  `compositeHandler`; `runtime.Config.MCPServers` wired from cmd/dhi.
- Known sabotages/flakes unchanged. Untracked `.dhi/agents/dev/` — do
  NOT commit.

### Session 29 (M18 — ideation round-table & canvas, F-033)

- **P0 session model + floor protocol** (`internal/ideation`): schema 2
  adds `Mode` (1:1|group|breakout), `Moderator`, `Parent`, `Turns`;
  schema-1 cards load as group (no mod/parent/turns). `CreateSession`/
  `CreateBreakout`/`SetMode`/`SetModerator`/`GrantFloor`/`ReleaseFloor`/
  `Breakouts`; `Remove` refuses a parent with breakouts; `Open` warns on
  a dangling breakout parent. A `proposals.toml` registry (skipped by
  card scan) records agent proposals: `Propose`/`Proposals`/
  `PendingProposals`/`Decision` (pending→accepted/declined, no
  resurrection). 6 new tests.
- **P1 tools + mermaid**: `session_read`, `artifact_create`,
  `artifact_edit`, `propose_session` added to `servedSlugs` + manifest
  `BuiltinTools`; scopes read/write/read; artifact writes are
  VPath-safe, approval-gated via Write, rescan + author-claim.
  `preview.IsMermaid`/`RenderMermaid` (deterministic ASCII outline:
  flowchart edges with node-label resolution + sequence messages),
  `RenderMermaid` wired into the canvas. Tests in dhitools + preview.
- **P2 surface**: IDEATOR is now SESSIONS · PARTICIPANTS · CANVAS ·
  TRANSCRIPT. SESSIONS lists pending proposals (`a` accept / `x`
  decline — accept opens; nothing opens otherwise) then sessions with
  mode chips + nested breakouts (`b`); PARTICIPANTS has the moderator
  slot + floor (`f` grant/dispatch, `m` moderator, `a` invite, `x`
  remove); CANVAS unifies artifact list + live preview + `e` open-in-
  editor seam; TRANSCRIPT stamps each message `#id` + floor header.
  Floor protocol in `agent.go`: human mention grants first participant,
  an addressed agent hands on, unaddressed returns to the moderator,
  bounded by `roundTableMaxTurns=24`; 1:1 auto-invites its sole agent.
  Goldens regenerated (sessions-proposal, participants, canvas-markdown,
  canvas-mermaid, transcript-composer; obsolete artifacts-list/
  preview-markdown/chat-composer removed).
- `cmd/dhi` wires `ideator.Deps.OpenInEditor` → `app.OpenInEditor`.
- Known flakes unchanged (TestCLIRuntimeEndToEnd in reviewer, and the
  runtime/clirun ones). Untracked `.dhi/agents/dev/` — do NOT commit.

### Session 28 (M17 — cross-project work)

- **P0 multi-changeset** (`tasks.Commit`/`PushBranch`, `toolbridge`
  prOpen): every changeset committed/pushed/PR'd on its own branch; a
  per-member failure is named and the others still proceed (no silent
  skip, no rollback).
- **P1 declared dependencies**: `workspace.toml [[dependency]]
  from/to/kind` (kind module|api|build) strict-decoded (bad kind,
  self-edge, missing endpoint, duplicate refuse); `Workspace.
  Dependencies/DanglingDependencies/DependentsOf/SetDependencies`;
  doctor `dependencies` row (dangling member WARN by name); the REPOS
  section renders the graph (`from → to (kind)`, missing member
  flagged). `saveConfig` preserves edges across roster edits.
- **P2 propagation proposals**: attaching a changeset seeds pending
  `tasks.Propagation` from `ws.DependentsOf`; inbox gains a
  `Dependency` kind; `a` accepts (creates `<origin>`-derived
  `dep-…` task in the target member and links it), `x` declines
  (creates nothing); re-seeding never resurrects a decided proposal.
- Known flakes unchanged (TestCLIRuntimeEndToEnd,
  TestMemoryAndKBReachTheSystemBlock, clirun TestDetect). Untracked
  `.dhi/agents/dev/` — do NOT commit.

## Where we are

**The planned roadmap M15–M20 is fully landed (M20 complete 2026-09-29,
M15 complete 2026-09-30); `make verify` green.** M15 engine inversion
(the host CLI is the engine; all six adapters serve DHI tools over the
loopback MCP endpoint; `dhi-action` removed), M18 ideation round-table,
M19 pack registry + MCP install, and M20 depth & cohesion (board depth,
channel depth, live work log, editor co-editing) all shipped. Beyond
M20 only the optional follow-ons and the deferred backlog remain
(below).

**M14 is complete (P0–P4); `make verify` green.** P4 shipped F-029:
`gitcore.ResolveIdentity` (one resolver, reads the user's git config
through the hermetic binary under the HOST config path via
`Manager.GitIdentityEnv`; named refusal `ErrIdentityUnset` when unset),
injected into `tasks.Store.SetIdentity` and `editor.WithIdentity` so
task-card + editor commits are authored by the user's real identity;
external-PR consolidated comments drop agent handles and the PR-body
DHI footer is gone; doctor gains an `identity` row (Warn when unset
naming the fix, OK naming the identity — never Fail on absence).

**The roadmap was re-based beyond M14.** A full design session locked
the "virtual workspace" north star and planned M15–M20 (see ROADMAP
"North star"): M15 engine inversion + IDE tool surface, M16 feature
workflows, M17 cross-project work, M18 ideation round-table, M19 pack
registry + MCP install, M20 depth & cohesion. New ADRs **0019–0022**
(0019 supersedes 0012/0013: DHI owns the loop, the host CLI is the
engine) and feature specs **F-030–F-035**.

**M15 P0 landed (engine inversion).** Manifest schema 3 adds
`engine = "cli:<name>"` (optional; empty = inherit the workspace
default) via `manifest.ParseEngine`/`EngineString`; `runtime` stays as
a synonym for schema 1/2 and refuses alongside `engine`; unknown engine
kinds refuse by name (ADR-0024 drops the `api:` path). Settings gained a strict `engine` default;
`runtime.Config.DefaultEngine` + `runtime.engineName` resolve the
effective engine, refusing by name when neither is set; doctor
`agent-tools` resolves the inherited engine. Adapter selectability
follows detection — claude/codex/opencode today; cursor/copilot/gemini
join once installed and MCP-verified. Next: M15 P1 (the IDE tool
catalog).

### Session 27 (M16 — feature workflows P0–P2)

- **P0 `internal/agentkit/workflow`**: strict TOML decode of
  `.dhi/workflows/<slug>.toml`; builtin `feature`
  (worktree_create → implement → test → commit → push → open_pr →
  review); gates warn|block|exception-approval; `CheckGate(seam,Progress)`
  with the ADR's exact hard blocks (worktree→commit, tests→PR);
  `Available`/`Load`/`Resolve`/`Render`. 15 tests.
- **P1 layering + injection**: manifest schema 5 `workflow = "<slug>"`
  (agent layer), org team `workflow` + `SetTeamWorkflow` (team layer),
  `.dhi/workflows.toml` `default` (workspace layer), builtin last;
  malformed refuses at `Turn`; the resolved workflow's guidance is
  appended to the CLI system block; the active slug is recorded on the
  task card.
- **P2 enforcement**: `dhitools.Deps.Gate` refuses `git_commit` with no
  worktree; `toolbridge.Bridge.Gate(taskSlug,"pr")` refuses a PR while
  tests haven't passed. Progress is DURABLE task state:
  `Progress.Worktree` from the card's changesets, `Progress.TestsPass`
  set when the served `run` observes a passing `go test` (OnRun →
  `tasks.SetTestsPass`). `doctor` gains a `workflows` row (malformed
  FAIL; dangling agent/team/task slug WARN).
- **P3 landed (all four M16 items complete)**:
  - exception-approval review bypass: completing a task (`task_status →
    done`) crosses the `review` step; the bypass routes through the
    approvals queue and is recorded as `tasks.Bypass{Step,Reason,At}`
    (declining refuses). `Progress.Reviewed` = any recorded bypass.
  - packs ship workflows: `pack.toml` `workflows = [...]`, validated
    with `workflow.Parse` before any file lands, written to
    `.dhi/workflows/`, provenance-tracked, removed on uninstall,
    conflict-refused against non-owned files.
  - Settings WORKFLOWS section: browse builtin/local, `n` authors a new
    definition via the strict form (`workflow.Save` validates), `d`
    toggles the workspace default, `v` previews steps. New goldens
    regenerated (rail entry).
  - board: `boardWorkflowLine` shows the task's active workflow + next
    enforced step (or complete/missing).
  - `workflow.Save`/`SaveDefault`/`NewDefinition`/`NextStep`/`Parse`
    write+inspect API added.
- `make verify` green (known flakes unchanged: TestCLIRuntimeEndToEnd,
  TestMemoryAndKBReachTheSystemBlock, clirun TestDetect). Untracked
  `.dhi/agents/dev/` — do NOT commit.

### Session 26 (M15 P3 — MCP tool serving; adapter swap)

- **Gemini adapter deleted** (deprecated); **antigravity (`agy`)**
  adapter added and live-verified on 1.2.11. `agy -p <prompt>
  --output-format stream-json --dangerously-skip-permissions`; events
  `init` / `step_update` (state, step_type user_input|agent_response|
  tool, text_delta, tool_info.parameters.CommandLine, usage) / `result`
  (status SUCCESS|…, response, usage). State root ~/.gemini/
  antigravity-cli (+ ~/.cache/antigravity). Registry now: antigravity,
  claude, codex, copilot, cursor-agent, opencode.
- **MCP loopback server was the real P3 blocker** (`internal/mcp`):
  POST-only + protocol 2024-11-05 made streamable-HTTP clients silently
  drop every served tool. Fixed: GET SSE stream held open for the
  session; POST replies SSE-framed when the client Accepts
  text/event-stream; ProtocolVersion `2025-11-25`; ToolInfo now emits
  lowercase keys (`name`/`description`/`inputSchema`); rpc `error`
  omitempty. Diagnosed by replaying opencode's real requests.
- **MCP delivery seam generalized** (`clirun.CLI`): `MCPConfigFile`
  (optional) + `MCPConfigEnv` + `MCPConfigArgs(configPath, endpoint)`;
  `RunInput.MCPURL`; runtime `serveTools` threads path+endpoint and
  writes a file only when the adapter declares one. Deliveries: claude
  file+argv (`--mcp-config … --strict-mcp-config`), opencode env file
  (`OPENCODE_CONFIG`, merges with the user's global config), codex
  inline argv (`-c mcp_servers.dhi.url="…"`).
- **Live-verified end-to-end** (agent really calls a served tool
  through DHI's loopback server): claude ✓, opencode ✓, codex ✓,
  cursor-agent ✓, copilot ✓. Repro: `DHI_LIVE_MCP=1 go test
  ./internal/agentkit/clirun/ -run
  'TestLive(Claude|OpenCode|Codex|Cursor|Copilot)MCP' -v` (real runs,
  real tokens). `MCPOK` flipped only for those five; antigravity stays
  on the fallback by decision.
- **cursor-agent**: parser was wrong vs its real stream (text nests
  under `message.content[]`; usage is camelCase `inputTokens`; thinking
  deltas on their own subtype) — fixed + live-verified (Tested
  2026.09.26); MCP delivered as a worktree `.cursor/mcp.json` +
  `--approve-mcps`, git-excluded via `excludeFromGit` (info/exclude,
  removed on stop — decision: worktree + git-exclude).
- **copilot**: parser/flags fixed against its real envelope (terminal
  is `result` with top-level `exitCode`; tools are
  `tool.execution_start`/`tool.execution_complete`; assistant text in
  `assistant.message`), live-verified 1.0.88; MCP via worktree
  `.mcp.json` + git-exclude + `--allow-all-tools --disable-builtin-mcps`
  (workspace `.mcp.json` IS loaded even though `copilot mcp list` does
  not surface it).
- **antigravity left on the fallback by decision (2026-09-26).** `agy`'s
  MCP list lives only in the user-global `~/.gemini/config/mcp_config.json`;
  project `.antigravitycli/*` is ignored and no env relocates it
  (`ANTIGRAVITY_APP_DATA_DIR`, `CASCADE_GLOBAL_CONFIG_OVERRIDE` tried).
  Wiring would require mutating the user's global config each turn →
  deferred; `MCPOK=false` by choice, not gap. Revisit with a mirror-HOME
  or transient-register design.
- Known flakes (pass isolated): `TestCLIRuntimeEndToEnd`,
  `TestMemoryAndKBReachTheSystemBlock`, clirun `TestDetect`.
- Untracked `.dhi/agents/dev/` exists — do NOT commit.

### Session 25 gotchas (planning re-base + M14 P4)

- **Identity reads use the HOST git env, not the hermetic one.** The
  managed hermetic config (`GitEnv`) has no `[user]` section by design
  (ADR-0009), so `ResolveIdentity` builds a Runner with
  `Manager.GitIdentityEnv(nil)` (= `Env(nil)`): the user's HOME/global
  config apply, the hermetic shim is still first on PATH. Never route
  identity reads through `ResolveRunner`/`GitEnv` — they'd report unset
  on a machine that has a perfectly good git config.
- **Doctor `identity` is Warn, not Fail, when unset.** The IDE boots and
  agents run; commit paths refuse at use with the SAME message
  (ADR-0011 "refused capability surfaces at use"). The F-029 spec's
  "fails when a commit path would refuse" is read as the use-time
  refusal, not the doctor row.
- **Commit paths refuse when the resolver is nil.** `tasks.Store.Commit`
  checks `identity == nil` BEFORE opening the repo, and the editor panel
  does the same — the refusal names `git config --global user.name`, so
  tests assert on that substring, not a generic error.
- **`fakeGH.CreatePR` now captures the body** (service_test) — the
  signature has a `body` param between title and base; the old fake
  ignored it. Any new GH fake must keep all six params.
- **External-PR bullets are now plain `- text`** (no `**author**:`); the
  reviewer external-publish test asserts `- **` is absent.
- **Planning re-base conventions**: ADRs append-only (0019 supersedes
  0012/0013, does not edit them); feature specs F-030–F-035 match the
  ROADMAP milestones M15–M20; product.md carries the vision/principles.
- Known flakes under full `make verify` remain: the CLI-stub e2e
  (`TestCLIRuntimeEndToEnd`), clirun `TestDetect`, and occasional
  runtime-package timeouts; all pass isolated with `-race` — rerun
  before treating as real.
- **Engine resolution precedence (M15 P0)**: `manifest.Agent.Runtime`
  is the resolved CLI name and is empty exactly when the engine is
  inherited. `runtime.engineName` is the single resolver (manifest
  engine → workspace default → named refusal); `buildEntry` and
  `extendSandboxForRoster` both go through it, so a roster with no
  engine metadata still admits sandbox roots correctly.
- **Marshal derives the engine pair**: callers may set only `Runtime`
  or only `Engine`; `Marshal` computes `wantEngine`/`wantRuntime` for
  the round-trip check, so partial structs still marshal (this bit
  org/pack/settings tests when it was first missed).
- **Settings `engine` is `omitempty`** so a default empty engine does
  not appear in saved config; validation is format-only
  (`cli:<slug>`), registration is checked by runtime/doctor.
- **Served-tool seams must be wired in `cmd/dhi`'s Config literal,
  not just in `serveTools`.** A scripted replace meant to add
  `Git`/`Identity`/`Sessions` to `newAgentRuntime`'s Config silently
  no-matched (different field alignment), so production served those
  tools with nil seams (silent refusal) while every unit test — which
  builds `Deps` directly — stayed green. When adding a served tool:
  edit the Config literal explicitly and grep it back; tests that build
  `Deps` by hand do NOT cover main's wiring.

### Session 24 gotchas (M14 P3)

- **Serving is in-process; approvals are why.** ADR-0017 named a
  `dhi __toolserve` stdio child, but the approvals queue is in-memory
  and the human answers it in the TUI — a child process would need its
  own queue. ADR-0018 supersedes that with `mcp.ServeLoopback` (HTTP on
  an ephemeral 127.0.0.1 port) started per turn. `mcp.ServeStdio` is
  kept in reserve for stdio-only adapters; no `dhi` subcommand exists.
- **The MCP HTTP client must accept `202`.** `HTTPHandler` answers
  notifications (`notifications/initialized`) with 202; the client's
  `write` originally treated any non-200 as an error and handshakes
  broke. 202 = accepted, nothing pending.
- **Nil `serveSession` must not be dereferenced.** `serveTools` returns
  nil when the allowlist has no served slug or the adapter lacks
  `MCPOK`; `cliTurn` now captures `mcpConfig` behind a nil guard
  instead of `defer serve.stop()` / `serve.configPath` (which panics).
- **`MCPOK` is the per-adapter gate.** Only claude declares verified
  MCP wiring today; codex/opencode/gemini/copilot/cursor ignore
  `MCPConfig` entirely and keep the `dhi-action` fallback. `MCPConfig`
  reaches argv only on claude (`--mcp-config … --strict-mcp-config`).
- **`dhi-action` is suppressed per turn when MCP serves.** `cliPrompt`
  takes an `mcp bool`; the bridge contract is advertised only when
  serving is off (avoids double contracts).
- **Served slugs are one source of truth.** `dhitools.Serves(name)` /
  `ServedTools()` drive both the runtime's serving decision and the
  doctor row; `memory_write_notes` was missing from `manifest.
  BuiltinTools` and had to be added (allowlist validation).
- **Doctor `agent-tools` is one row**: OK when idle or all interested
  agents are MCP-capable; Warn naming each agent on a non-MCP runtime.
- **Latent inbox-golden time bomb fixed.** `seedInbox` pinned `b.Now`
  but not `m.now`, so relative stamps drifted from "now" to "6d" a few
  days after the golden was written. Pin `m.now` to the same instant as
  `b.Now` in any fixture that renders relative time.


## Session 22 gotchas (M13)

- **ansi.Width skips escapes itself** — raw styled strings measure the
  same as stripped ones; kit.runeWidth delegates. ansi.Clip is strict
  (a wide rune that would overflow is not written).
- **kit.Modal content height = len(body)+3** (the shadow row). Scroll
  needs Height set; the appended busy/error row stays PINNED outside
  the scroll window. Rail foot is its OWN row (fixed-Height rails
  shrink the row budget; overflow scrolls with the cue riding the
  foot). List group rows are never the cursor — no selectable row in
  the travel direction means the cursor STAYS PUT.
- **kit.Form in-value cursor**: NewTextField prefill sets cur=len —
  DIRECT `.Value = x` writes leave cur at 0 (settings editAgentForm
  reconstructs fields for this reason). Backspace deletes BEFORE the
  cursor; paste arrives as the composed key "paste:<text>".
- **kit.Transcript**: View() []string (rows field/method collision
  renamed); textCol = authorW+8 so the cursor marker eats SLACK, not
  the author label; Tail windows by LINE with cursor-follow so `k`-nav
  never vanishes; human rows carry TabActive (matches the workspace
  convention); markdown rides an injected seam — kit must NOT import
  internal/preview.
- **Mouse**: MouseModeCellMotion set on the shell's tea.View; surface
  coords are body-local (row 1 of the screen = body row 0). Wheel
  synthesizes j/k ×3 through HandleKey with guards (finder, composer,
  terminal, rename/action, insert mode, forms, dialogs never see it).
- **chroma seam** (editor/syntax.go — the ONLY chroma import): kinds →
  theme tokens, never chroma hexes; whole-buffer lex cached on
  textbuf.Buffer.Seq (bumped in snapshotBefore/insertAt/deleteRange —
  NOT a version check against 0, the highlighter carries a done flag);
  cursor/visual lines render plain so rune inversion stays exact;
  unknown lexer/oversize = plain, never fake-colored.
- **floatPopup** covers to the right margin (no cell-splicing) and
  EXTENDS past short buffers (appending rows the panel pads) — the
  first version broke the LSP popup test by clipping the box to a
  2-row base.
- **internal/vt**: SGR passes through INLINE (colors are session
  content, not chrome); newline grows history LAZILY (a trailing \n
  leaves the cursor unmaterialized → Pending() → the "_" prompt
  marker lands on its own row); ESC+charset is 3 bytes (ESC ( B).
- **bus.Now** (injectable clock) — pins transcript stamps in goldens;
  wall-clock minute boundaries made this load-bearing.
- **statusFlash toasts**: 4s TTL from FIRST appearance; identical
  content keeps its stamp (dedupe); form-internal errors stay
  persistent (validation feedback, not a toast).
- **diffRows cache** keyed by layout+file-shape fingerprint + openID —
  the fingerprint alone let two same-shaped reviews alias stale row
  pointers. Benchmarked: warm 4.4µs vs cold 27.5µs (20 files × 40
  lines).
- **theme lint** allows lipgloss.NewStyle + token refs; use
  TextStyle/InfoText/AccentDimText/AccentText/Accent2Bold/Keycap/
  AddWash/DelWash. lipgloss.Style is NOT comparable — flag a wash with
  a bool or compare Kind, never `style == lipgloss.Style{}`.
- Verify FAIL on TestCLIRuntimeEndToEnd = the known CLI-stub flake —
  rerun isolated.

## Gotchas carried (still load-bearing)

1. go-git Push needs a REGISTERED remote; fixtures use bare local origins.
2. Test fakes must fully implement seams; event pumps must NOT re-arm.
3. Read form fields BEFORE closeForm(); waitReply before
   crew.Handle — mirror assertions come from the reply message, not a
   provider call log (the Mock's call log is gone with the engine).
4. bus.History(ch,0) excludes threaded rows.
5. requestTurn must call crew.Handle SYNCHRONOUSLY.
6. Policy rules are ROOT-RELATIVE (ADR-0010); glamor renders H2 `## `.
7. macOS /var→/private/var EvalSymlinks.
8. g-chords are editor-owned; WorkspaceEdit bottom-up; LSP flows
   guard on client.
9. Seatbelt deny-default profiles need the system allows (/System,
   /usr/lib, dyld caches, mach-lookup) or wrapped processes die
   cryptically; network stays policy-engine territory. M8 adds
   per-CLI StateRoots to the rw set — same system-allow list applies.
10. sandbox.go's Sandbox interface (Name/Wrap) is load-bearing —
    never redesign it casually; adapters implement it as-is.
11. runtime guards deny-all by policy default: Guard.Exec tests need
    an explicit exec allow in policy_json.
12. fuzzy.Match and Index.Rank share matchRunes so scores can't drift.
13. Gates that start work from a keypress MUST queue through TakeCmd.
14. `go run` of internal packages from /tmp fails ("use of internal
    package not allowed"); drive via a transient file INSIDE the
    repo, delete after. `go build ./cmd/dhi` drops a `dhi` binary in
    cwd — remember to delete it.
15. Settings layer semantics: zero values in a fileLayer mean UNSET
    (bools needing explicit-false use *bool); strict load rejects
    unknown keys per-layer before merge.
16. doctor must stay runnable on broken installs: use
    settings.LoadBestEffort anywhere diagnostics read configs.
17. runtime.New REQUIRES Config.Sandbox; test harnesses inject
    sandbox.Noop{} explicitly.
18. lipgloss multi-line Render re-pads lines to the longest — style
    multi-line content line-by-line or goldens break (theme.Faint).
19. `theme.Motion` is a variable, not a function (F-012).
20. Manifest Parse takes id from the filename stem; strict decode
    rejects undecoded keys — new manifest keys must land in the
    file struct + validation + round-trip test together.
21. sandbox.Sandbox.Wrap's contract is BINARY-FIRST: argv[0] is the
    wrapped program and the return value is the COMPLETE command
    line (`[wrapper…, --, argv…]`). Never prepend the CLI path after
    wrapping — the session-20 agent bug was exactly that (wrapper
    bits became claude's prompt args → "malformed final line").
    recordingSandbox.lastArg asserts the invariant.
22. runtime.extendSandboxForRoster merges each rostered CLI's
    StateRoots + binary tree (dir, resolved-symlink dir + parent) via
    sandbox.RootExtender BEFORE guards are built, in New and live
    Reload — claude lives under ~/.local and writes ~/.claude, the
    workspace jail alone denies its exec (exit 71, silent).
23. Seatbelt reads are broad by design (bun/node CLIs touch fonts/
    tz/certs; per-CLI enumeration aborts cryptically, SIGABRT no
    stderr): `(allow file-read*)` + explicit credential denies
    (~/.ssh, ~/.gnupg, ~/.aws, ~/.kube, gcloud, azure, .netrc — deny
    wins in SBPL). Writes + exec stay deny-default jailed. Tightening
    = deliberate future ADR, never casual.
24. Live agent verification: `DHI_SMOKE_CLAUDE=1 go test
    ./internal/agentkit/runtime/ -run TestLiveClaudeSeatbeltSmoke`
    (real claude 2.1.177 through the real seatbelt; expect a PONG
    result event). Gate-style like DHI_SMOKE_GIT/DHI_SMOKE_NET.

## Just finished (session 25 — planning re-base + M14 P4)

- Full design session (grilling) locked the "virtual workspace" north
  star and produced: ADR-0019–0022, F-030–F-035, ROADMAP "North star" +
  M15–M20, product.md vision/principles update.
- M14 P4 shipped (F-029): `internal/gitcore/identity.go`,
  `Manager.GitIdentityEnv`, `tasks.Store.SetIdentity`, editor
  `WithIdentity`, review PR-body/bullet changes, doctor `identity` row,
  and tests across gitcore/tasks/doctor/review/editor/reviewer.
- M15 P0 (engine inversion) landed the same session: manifest schema 3
  + engine seam, settings `engine` default, runtime resolution, doctor.
- `make verify` green (the CLI-stub e2e flaked once under load, passed
  isolated and on rerun — known).
- Next: M15 P1 (the IDE tool catalog).

## Just finished (session 24 — M14 P3 complete)

- Commits: M14 P0 58ab4b1, P1 fc75c76, P2 2ea8efc (all pushed to
  local main, ahead of origin). P3 (this session) is in the working
  tree, uncommitted: `internal/mcp/server.go`, `internal/agentkit/
  dhitools/`, runtime serving + doctor row + ADR-0018 + F-028/ROADMAP/
  STATE updates.
- `make verify` green (yes — the race suite runs).
- Next: M14 P4 (F-029 user identity), then the deferred backlog below.

## Open questions for user

- Resolved 2026-09-26: work with the CLIs detected on the machine
  (claude/codex/opencode); cursor/copilot/gemini can be installed during
  implementation. An adapter is selectable as an engine only when it is
  both installed and MCP-verified; until then manifests naming it refuse
  by name (no fixture-first shipping).

## Next up

1. **Optional follow-ons**: none — all landed.
2. **Engine kinds**: `cli:<name>` only (ADR-0024 dropped the `api:`
   path). Reinstating a direct-API engine would be a fresh ADR + spec.
3. **(Deferred, F-026/F-017/F-027/F-020/M11)** — the older, unspecified
   backlog; pick and spec one when wanted.

## Session 19 gotchas (M12)

- **HintBar clips, never wraps**: the keymap segment goes through
  ansi.Clip against the width left by the status segment; compose the
  row from self-contained segments (each with its own chrome bg) or
  the flash's SGR reset kills the bg mid-row.
- **kit.Rail foot sits at the LAST row** (h-1) — at h-2 it overwrote
  the last nav row when content exactly filled Height.
- **Column count strings**: Columns.View returns header+Height rows;
  board lanesH budgets must subtract 1 for the header or the detail
  tail clips under the HintBar.
- **Settings pane geometry**: panel inner = h-2 rows; content pads to
  h-3 + HintBar = h-2 exactly (the old code under-filled by 2 rows).
- **kit.List.Inset renders padding INSIDE the style** (RailDim rows:
  bg+fg one render) — padTo outside the render leaves raw spaces on
  the default background; badge segments carry their own bg style.
- **Color SGR assertions don't work in tests** — lipgloss's test color
  profile may degrade to Ascii; assert geometry + stripped content,
  never escape sequences (faint `\x1b[2m` is the exception).
- **Statusline is per-frame** (`buildStatus()` in compose); the old
  per-switch `a.status` reset is gone. Surfaces opt in via
  StatusContext/StatusHints assertions — nil-safe degradation.
- **Workspace section budgets**: mainPane gives sections h-3 (panel
  inner h-2 minus the HintBar) and the full inner width (w-4, was
  w-6) — activeSectionFor no longer subtracts its own margins.
- **Replay/modal geometry**: activeSectionFor checks `m.replay` FIRST
  (board/channels cases would paint over the modal).

## Session 18 gotchas (M11)

- **kit.Form.Cur() is exported for tests** — the field cursor is
  unexported; typeDialog-style helpers tab until `Cur() == i`.
- **CloseDialog before building flash strings**: `dtarget` is zeroed
  by closeDialog, so capture `target := m.dtarget` first (the team
  delete flash was "team  deleted" until fixed).
- **Board cursors are per-lane** (`boardActive` + `boardCur[4]`);
  flat task indexes are wrong. `s` re-finds the card after SetStatus
  so focus follows it into the new lane; inbox run-jumps select the
  card inside its lane.
- **The board renders lanes even without a task store** (rows all
  empty + the unavailable note) — the early-return version broke the
  "board reads as a board" assertion.
- **activeSectionFor must check `m.replay != nil` first** — the
  explicit secBoard case bypassed the modal replay and the board
  painted over it.
- **Empty lanes get NO placeholder row appended by the board** —
  kit.Columns renders EmptyRow itself; appending one inflates the
  lane header counts.
- **Slack floor geometry**: transW = width − railW − ctxW − 2 (the
  two separator columns); rail/context cells are veil-padded via
  padToANSI (measures ansi.Strip); transcript rows padToANSI'd.
- **Thread-pane wrap width must budget the author prefix** (`width-12`)
  — the prefix is prepended after wrapWords, so wrap-then-prefix
  overflowed and ansi.Clip corrupted a style sequence mid-escape
  (clipPlain cut raw runes; use ansi.Clip everywhere).
- **chatPane keys are width-aware via `lastWidth`** (set per render);
  tests must render once before pressing tab (rail focus is
  width-gated at slackCtxMin).
- **Rail rows render as ONE styled string** — concatenating two
  rendered segments under an outer bg loses the row bg after the
  first segment's SGR reset.
- **Settings autoNext/autoResult use the injectable `m.now`** — the
  autopilots golden is wall-clock dependent otherwise.
- **Confirm dialogs route enter → submitConfirmDialog** — forgetting
  that branch silently closed the dialog without acting.
- **Workspace Init semantics preserved exactly** (goroutine pumps,
  pane resubscribe, armAutopilots; NO direct catchUpAutopilots call —
  launch catch-up rides the due-now tick; adding one double-runs).
- **kit.Modal carries title+lines only for workspace forms** —
  modalLines already render busy/error rows; setting box.Busy/Error
  duplicates them.

## Session 17 gotchas (F-017 / M9)

- **`unread.File` is `.dhi/unread.json`** (full relative path, the
  pack-provenance precedent). An early version used bare `unread.json`
  and wrote `<root>/unread.json`; the doctor suite caught it only
  because its fixture wrote `.dhi/unread.json` directly — keep the
  store and doctor on the ONE constant.
- **Doctor's unread row must stay read-only**: `unread.Open` SEEDS
  (writes) on a missing file. Doctor reads raw bytes + `unread.Decode`;
  the store's Open is the only writer. A read-only diagnostic that
  creates state would mask "fresh install" from "corrupted".
- **`signal()` must hold the mutex** while iterating `subs` — MarkRead
  unlocks BEFORE signaling, so an unlocked iteration raced with
  Subscribe's cancel delete (-race caught it in TestSubscribeFiresOnWrite).
- **Scan's thread rule**: a top-level message whose thread scope
  (`<channel>#<rootID>`) carries a watermark counts as read (max of the
  two limits). Without this, the inbox mention jump (which marks the
  THREAD scope) could never clear a top-level mention row.
- **Snooze keys are (channel, messageID)** — agent messages only.
  Approval/run_failed/in_review items refuse `z` with a named hint
  (snoozing them would need a different keying; noted as a scope cut).
- **`gotoInbox` in tests loops `for m.sec != secInbox`** — a fixed `]`
  count breaks when the test is ALREADY at inbox (form submit leaves
  you there; 8 more `]` lands on autopilots).
- **Snoozed rows stay dim even under the cursor** (parked ≠ urgent);
  the word-wrapped "snoozed until HH:MM" suffix can split across lines —
  assert "snoozed until" and the time separately.
- Editor chat shares the store via `editor.WithUnread` in main; the
  workspace Deps `Unread` field takes precedence, falling back to
  opening its own (tests) — one read state, two surfaces.

P4 (F-016 inbox) landed: `internal/inbox` is a pure aggregation
(`Build(apprs, bus, tasks)`, no state) that lists approvals, unreplied
@you mentions, failed runs on open tasks, and in-review tasks in
severity-then-age order. The INBOX 9th pane is a launcher: `enter`/`o`
jumps to the owning surface (editor chat approvals, CHANNELS thread,
run-replay, Reviewer) via narrow injected seams that degrade to a named
hint when absent. The `!N` statusline segment is recomputed per frame.

## Session 16 gotchas (F-016 / P4)

- **Inbox is pure + per-frame**: `m.inboxItems()` recomputes
  `inbox.Build` on every call (view + `AttentionCount`), so a row
  disappears the frame its home surface resolves it — no read-marks, no
  cache, nothing to invalidate. The `!N` statusline reads the same
  `AttentionCount()` via an interface assertion in `App.compose()`, so it
  clears in lockstep. Keep `Build` side-effect free (it is the
  table-tested contract).
- **Mention thread rule**: "replied" means a later `Author == bus.Human`
  message in the SAME thread — `repliedByYou(thread, i)` scans
  `thread[i+1:]`. A top-level DM message IS its own thread
  (`bus.ThreadOf` = own id), so a plain new top-level message does NOT
  close it; only a threaded reply (`Thread: dm.ID`) does. Tests assert
  both the close-by-threaded-reply and the not-closed-by-top-level cases.
- **Jump seams are injected closures**, not surface refs: workspace
  `Deps` carries `OpenChat func() bool` + `OpenReview func(id) bool`
  (main wires them to `app.FocusEditorChat()` / `app.SelectReviewer(id)`
  which assert `FocusChat()` / `SelectReview(string) bool` on the editor /
  reviewer surfaces). A nil/false seam degrades to a named `m.inboxHint`
  rendered via `theme.WarningText()` — visible, never silent. Mention +
  run jumps are internal (chatpane.openAt / openReplay), no seam needed.
- **INBOX is the 9th pane (secInbox)**; `TestSectionCyclingWraps` was
  updated so `[` from the first wraps to inbox and 8 `]` reach it.
  Inbox rows word-wrap at the pane width (`wordWrap` in view.go: break at
  spaces, hard-break overlong tokens); the kind glyph (◆ approve / ✗
  run_failed / ✓ in_review / @ mention) + cursor are prefixed per-line.
- `chatPane` must be built from the bus BEFORE the mention jump test
  asserts `openAt` — `newChatPane` starts with an empty channel rail;
  `refreshPaneRail()` populates it. Without that, `openAt` returns false.
- App `compose()` builds a LOCAL copy of the statusline (`status :=
  *a.status`, copies `Left`) before prepending the `!N` segment — never
  mutate `a.status.Left` in place or it re-prepends every frame.

## Session 15 gotchas (F-015 / P3)

- Autopilot **execution is a bus post**, not a direct agent call: run
  now / catch-up / ticks all post `bus.Message{Channel:"dm:<agent>",
  Author: bus.Human, Text:"[autopilot <slug>] <prompt>"}` then
  `rt.Handle(ctx,msg)` in a goroutine. Success = `MarkRan(slug, now)`
  ONLY after the turn returns, so a crash mid-turn never marks ran and
  catch-up re-fires exactly once (persist-before-visibility).
  `bus.Post` is synchronous and stamps `m.At = time.Now()` itself — tests
  observe the recorded posts (no goroutine sleeps).
- **Dangling agent** (not in Store.ids): run_autopilot_now refuses with
  `"agent <x> not on roster (recheck autopilot card or roster)"` and is
  NOT marked ran — the named fix is the ADR-0011 no-guess rule on the
  UI surface too. Catch-up skips (never refuses) dangling cards.
- `NextArm` is the ONLY arming input: interval → `now + every` (never
  ran → now), daily/weekly → next weekday instant since last mark ran,
  paused card → never arms. Ticks re-arm via `autopilotTickMsg`
  (armSeq guard = one chain). Paused/absent store → `nil` cmd = no
  chain. In-session ticks run ONLY while Workspace is the active surface
  (App routes async msgs to Active); catch-up covers boot.
- Launch catch-up = Workspace `Init()`, not "after gate release" — the
  gate may not exist at all (gate only on block/offer/bootstrap), and
  App.Init runs every surface's Init at boot. Catch-up runs the due set
  in slug order, exactly once (posts are synchronous, so slug-order is
  testable without sleeps).
- AUTOPILOTS is the 8th pane (secAutopilots, after secInspect); `[`
  from the first section wraps to it. Columns: name/agent/schedule/
  next-due/last-result; next-due shows "due" when due-last-checked,
  else `Next` formatted (interval "in 10m", daily "today 09:00",
  weekly "Fri 17:00"), last-result = newest agent run status else
  "ran HH:MM" (LastRun) else "-". Widths: 20/12/28/16 — keep content
  under them or pads collapse (golden `workspace_autopilots`).

## Session 14 gotchas (F-014 / P2)

- `tasks.Run.Runtime` is now stored prefixed (`cli:claude`); run decode
  is strict — a hand-edited card with a bad `[[run]]` status refuses the
  whole card (F-011 strict-data spirit) AND gets a line-precise warning
  from doctor `runs/store` via `tasks.CheckRuns` (which re-parses raw
  lines — validateRun messages map to the offending field's line).
- Rollup cost math: `HasCost` is the declared `cost:false` marker; older
  cards written before the field decode as cost-less → sums are marked
  "partial" rather than pretending to know the cost (ADR-0011: never
  guess). `-1` token runs never leak zeroes: `TokPartial` flags them.
- Replay pane is modal on `workspace.sectionKey` AFTER `[`/`]` handling
  so section switching still works; j/k/g/G scroll (G = bottom), esc
  closes; all other keys are swallowed so card state can't mutate
  behind the pane. Replay re-renders/wraps on geometry change
  (`width/height` cache on `runReplay`).
- `flashErr` messages are only visible while a modal is open (form.kind
  fNone = invisible) — test the `m.form.err` value, not the rendered
  output, for refused-key assertions.
- Golden gotcha: the missing-transcript golden embeds an absolute
  `t.TempDir()` path; it's stable because Go seeds temp dir names per
  test identity — regenerate goldens rather than hand-editing.

## Session 13 gotchas (wave 3)

1. All three wave-3 contracts are DOCUMENTED, not live-verified —
   cursor-agent's stream-json (system/assistant/tool_call/result),
   copilot's session JSONL envelope (data.content/toolRequests,
   tool.execution_end.result), gemini's stream-json timeline
   (init/message/tool_use/tool_result/error/result). Each adapter
   carries its live-verify checklist; the terminal event always gets
   `last_message` stitched on because none of the three put the final
   summary text on their terminal result event.
2. copilot's JSONL nests name/result under `data` (tool.execution_end
   has `data:{name,result}`), not top-level — reading top-level fields
   silently matched nothing.
3. Doctor + goldens needed no changes with 6 adapters: doctor iterates
   the registry generically and the golden surfaces never render the
   full runtime set.

## Session 12 gotchas (wave 2)

1. The `CLI` adapter is a struct of function fields (claude.go), not an
   interface — new adapters like codex.go must follow the struct shape
   exactly (`ParseStream func(io.Reader) <-chan StreamEvent`, `Finalize
   func(string) (string, Usage, error)`), and register in `allAdapters()`
   (declared once, in claude.go).
2. codex never puts the final-text and usage on one line:
   `turn.completed` has usage but no text, the last assistant reply is a
   separate `agent_message`. Adapters must stitch the summary onto the
   terminal event themselves (a normalized payload with `last_message`)
   so `Finalize` gets both (same for opencode's stop `step_finish`).
3. codex `item.started` AND `item.completed` both carry the same
   `command_execution` — emitting both would double the transcript
   rows, so the codex parser shows commands on `started` and only
   surfaces failures on `completed`.
4. opencode emits `tool_use` (not `tool`): `part.type == "tool"`,
   fields `part.tool`, `part.state.metadata.exit`. exit != 0 or
   status error ⇒ EventError.
5. codex reports no cost (HasCost=false); opencode reports cost only
   when the provider emits it (0 when unconfigured) → HasCost = cost > 0.
6. Retry state lives in a package var `cliRetryBackoff` (default 30s);
   runtime tests set it to 1ms + restore. The retry notice posted to the
   thread is prefix-matched in tests, not equality-matched (it embeds
   the backoff duration).
7. `rt.Handle` dispatches async goroutines; `rt.Turn` is the synchronous
   error-returning path — retry tests assert transcript files/history
   after `Turn` returns, not via Handle.
8. The "unknown runtime" manifest-test fixture previously used
   `runtime = "codex"` as its invalid value — every adapter addition
   turns a former negative fixture valid. Grep for the CLINames when
   adding adapters.

## Session 11 gotchas (engine removal)

1. `/bin/sh` echo is xpg_echo on macOS: `echo "a\nb"` emits a literal
   `\n`. Fixture stubs must use `printf '%s\n'` (and for any JSON line
   built from the prompt, build it in Go or awk-escape — never `$2`
   raw, or embedded newlines in the prompt produce invalid JSONL).
2. BSD sed (macOS) does not accept `:a;N;$!ba`. Use awk for
   newline-joining (or keep stub JSON single-line via Go-side
   escaping in `stubcli.FixedReply`).
3. A fixture `claude` needs system dirs in PATH (`/usr/bin:/bin`) for
   its own helpers (base64/awk) to resolve — a PATH-only CLIEnv makes
   the stub hang.
4. `Approvals.remove()` had a genuine missing-unlock bug (lock never
   released) revealed when the rewritten turn tests exercised cancel;
   fixed with `defer`. `wait` is now exported as `Ask`.
5. Roster/org/pack/workspace-surface fixtures all construct manifests
   in Go — every one needs `Runtime: "claude"` (or `runtime =`
   "claude" in TOML) or strict-parse/marshal self-check fails.
6. Editor/reviewer e2e tests were rebuilt from `provider.Mock` to the
   real runtime + `stubcli.FixedReply` stub — F-005 acceptance flow
   survives unchanged on a fake PATH (the reviewer/chat seams didn't
   move).

## Session 10 gotchas (P0 research)

1. Headless contracts VERIFIED on this machine (2026-09-08):
   claude 2.1.177 (`-p --output-format stream-json --verbose`,
   terminal `result` carries `total_cost_usd`+`usage`), codex-cli
   0.147.0 (`exec --json -C <dir> --sandbox danger-full-access
   --dangerously-bypass-approvals-and-sandbox` — that flag is
   documented as "for environments that are externally sandboxed",
   i.e. made for our seatbelt wrap), opencode 1.18.25 (`run
   --format json --dir <dir> --title <slug>`).
2. gemini/cursor-agent/copilot are NOT installed here — wave 3
   adapters are fixture-first; their live-verify checklists (version,
   flags, stream shape, cost location) must be filled in the adapter
   file before the doctor row may report OK.
3. DHI has no LICENSE file (all-rights-reserved) — flagged to the
   user in the plan discussion, no decision recorded yet; do not
   publish DHI code snippets as reusable upstream prior art until
   that's settled.
4. Workspace view panes: secMembers secOrg secPacks secStandards
   secChannels secTasks secInspect (view.go sectionID) — F-015 adds
   secAutopilots (8th), F-016 adds secInbox (9th); rail counts +
   sectionSwitcher tests iterate 0..secCount, so every insertion
   shifts nothing (append at the end) but label()/count cases grow.
5. Task card = internal/tasks (Task struct, ChangeSet,
   RecordChangeSet, AttachFn/DetachFn, Subscribe) — `[[run]]`
   records extend the card TOML there; the runs dir is
   `.dhi/agents/<id>/runs/` next to memory journals.
6. (void, ADR-0013) runtime.Config had a Providers map + Turn branch —
   the provider layer is deleted; Turn is now a thin CLI dispatcher.
7. (void, ADR-0013) provider.Event had no usage field — there is no
   provider.Event anymore; claude's terminal `result` carries
   `total_cost_usd` + `usage`, and the `[[run]]` schema is
   `cli:<name>` only (F-014).

## Gotchas carried (still load-bearing)

1. go-git Push needs a REGISTERED remote; fixtures use bare local origins.
2. Test fakes must fully implement seams; event pumps must NOT re-arm.
3. Read form fields BEFORE closeForm(); waitReply before
   crew.Handle — mirror assertions come from the reply message, not a
   provider call log (the Mock's call log is gone with the engine).
4. bus.History(ch,0) excludes threaded rows.
5. requestTurn must call crew.Handle SYNCHRONOUSLY.
6. Policy rules are ROOT-RELATIVE (ADR-0010); glamor renders H2 `## `.
7. macOS /var→/private/var EvalSymlinks.
8. g-chords are editor-owned; WorkspaceEdit bottom-up; LSP flows
   guard on client.
9. Seatbelt deny-default profiles need the system allows (/System,
   /usr/lib, dyld caches, mach-lookup) or wrapped processes die
   cryptically; network stays policy-engine territory. M8 adds
   per-CLI StateRoots to the rw set — same system-allow list applies.
10. sandbox.go's Sandbox interface (Name/Wrap) is load-bearing —
    never redesign it casually; adapters implement it as-is.
11. runtime guards deny-all by policy default: Guard.Exec tests need
    an explicit exec allow in policy_json.
12. fuzzy.Match and Index.Rank share matchRunes so scores can't drift.
13. Gates that start work from a keypress MUST queue through TakeCmd.
14. `go run` of internal packages from /tmp fails ("use of internal
    package not allowed"); drive via a transient file INSIDE the
    repo, delete after. `go build ./cmd/dhi` drops a `dhi` binary in
    cwd — remember to delete it.
15. Settings layer semantics: zero values in a fileLayer mean UNSET
    (bools needing explicit-false use *bool); strict load rejects
    unknown keys per-layer before merge.
16. doctor must stay runnable on broken installs: use
    settings.LoadBestEffort anywhere diagnostics read configs.
17. runtime.New REQUIRES Config.Sandbox; test harnesses inject
    sandbox.Noop{} explicitly.
18. lipgloss multi-line Render re-pads lines to the longest — style
    multi-line content line-by-line or goldens break (theme.Faint).
19. `theme.Motion` is a variable, not a function (F-012).
20. Manifest Parse takes id from the filename stem; strict decode
    rejects undecoded keys — new manifest keys must land in the
    file struct + validation + round-trip test together.
21. sandbox.Sandbox.Wrap's contract is BINARY-FIRST: argv[0] is the
    wrapped program and the return value is the COMPLETE command
    line (`[wrapper…, --, argv…]`). Never prepend the CLI path after
    wrapping — the session-20 agent bug was exactly that (wrapper
    bits became claude's prompt args → "malformed final line").
    recordingSandbox.lastArg asserts the invariant.
22. runtime.extendSandboxForRoster merges each rostered CLI's
    StateRoots + binary tree (dir, resolved-symlink dir + parent) via
    sandbox.RootExtender BEFORE guards are built, in New and live
    Reload — claude lives under ~/.local and writes ~/.claude, the
    workspace jail alone denies its exec (exit 71, silent).
23. Seatbelt reads are broad by design (bun/node CLIs touch fonts/
    tz/certs; per-CLI enumeration aborts cryptically, SIGABRT no
    stderr): `(allow file-read*)` + explicit credential denies
    (~/.ssh, ~/.gnupg, ~/.aws, ~/.kube, gcloud, azure, .netrc — deny
    wins in SBPL). Writes + exec stay deny-default jailed. Tightening
    = deliberate future ADR, never casual.
24. Live agent verification: `DHI_SMOKE_CLAUDE=1 go test
    ./internal/agentkit/runtime/ -run TestLiveClaudeSeatbeltSmoke`
    (real claude 2.1.177 through the real seatbelt; expect a PONG
    result event). Gate-style like DHI_SMOKE_GIT/DHI_SMOKE_NET.

## Just finished (session 21 — agent connection fix + hint dedup)

- **Agent "unable to connect" FIXED** (commits a2216c8, 10971b2):
  cliSpawnOnce now wraps binary-first argv (bug 1), and
  runtime.extendSandboxForRoots admits rostered CLI state+binary roots
  via sandbox.RootExtender before guards build (bug 2 — seatbelt had
  never actually engaged for agent spawns; tests masked it via Noop).
  Live-verified: real claude through the real seatbelt returns PONG +
  usage. Hint-dedup from M12 feedback landed: StatusHints removed
  everywhere, channels hints render once via chatPane.hints().
- Working tree clean; M11+M12+this fix all committed. Next actions in
  "Next up" below.

## Just finished (M12 — coherent UX, complete)

- kit: HintBar (chrome bottom row: status + clipped keymap, exact
  width), Rail (shared nav sidebar: inset rows, counts/badges,
  cursor, foot), breakpoints WCompact/WDock/WWide, Column.Accent,
  List.Inset.
- theme: BgChrome (dark #262F3E / light #E6E0D0) + ChromeBar/
  ChromeStatus/RailDim/RailMuted.
- Settings: left-rail IA; workspace/ideator/reviewer rails on the
  shared primitive; all top-of-body keymap rows deleted; chrome
  HintBar pins status+keys at every pane foot.
- Statusline contextual: kit.ModeChip + StatusContext/StatusHints
  interface assertions on all five surfaces; rebuilt per frame in
  app.buildStatus.
- Responsive: full-width stacks 60–83 cols; board detail side-by-side
  at >=120; sections use the full panel inner width.
- Goldens regenerated deliberately across kit/app/settings/workspace/
  ideator/reviewer/editor.

## Session 17 gotchas (M10)

- **`runtime.Reload` had zero callers** — crew changes never went live.
  The fix is ONE injected seam (`reloadRoster` closure in main →
  `LoadRoster` → `Reload`), wired into Settings CRUD, the ORG pane
  crew ops, and the import flow. Runtime tests prove the contract
  (manifest on disk → @mention routes without restart).
- **Settings row math**: the panel inner height is h−2; strip(1) +
  blank(1) + content + blank(1) + foot(1) → pad content to h−6 or the
  foot clips. `SetContent` REPLACES content — one call only.
- **Panel width clips long lines** (no wrap) — assert errors on the
  model state + a short visible marker, never the full message.
- **`stubcli.FixedReply` must single-quote the JSON** — double-quoted
  sh mangles backticks (command substitution) in any reply carrying
  markdown fences, and backslash-escapes pass through literally in
  single quotes, so no escaping beyond the `'\''` dance.
- **Toolbridge design**: the neutral `StreamEvent` has no structured
  tool args and run-to-completion CLIs can't receive mid-turn results —
  so actions parse from the FINAL message (` ```dhi-action ` blocks,
  the suggestion-block pattern) and results post to the thread for the
  next turn. One code path for all six adapters.
- **Every bridge action goes through `Approvals.Ask`** — tests MUST
  resolve (poll `List()` → `Resolve(id, true)`) or the turn goroutine
  blocks forever and the 5s waits flake. Full-suite CLI-stub runs can
  still flake TestDetect/TestDM under load — rerun in isolation.

## Open questions for user

- LICENSE: decided 2026-09-09 — **MIT** (LICENSE file + README; the
  go.mod `license` directive is dropped — this toolchain rejects it).
  No longer blocking upstream sharing.
- Wave-3 CLIs (cursor-agent, copilot, gemini) aren't installed on the
  dev machine — do you have accounts/installs for live verification,
  or should wave 3 stay fixture-only until you install them?

## Next up

1. **Wave-3 live verify** (needs installs): once cursor-agent/copilot/
   gemini are on the machine, run a real task per adapter, fill each
   adapter's live-verify checklist + set `Tested`, then doctor reports
   OK. Until then the adapters stay fixture-first and doctor marks a
   detected version untested (FAIL), never a guess.
2. **(Deferred, F-017)** snooze-expiry push notifications, per-message
   read granularity, multi-human read states, bulk "mark all read";
   inbox items from autopilot completions / doctor regressions.
3. **(Deferred, F-020)** MCP-style third-party tool registries;
   per-agent env/secrets editing in Settings; a curated remote agent
   registry for F-019 discovery.
4. **(Deferred, M11)** per-agent env/secrets editing rides the AGENTS
   profile; board drag-free reordering (move-card between lanes beyond
   `s` cycling); doctor rows for the new settings sections; the F-024
   `clip()` note — all truncation now goes through ansi.Clip.
