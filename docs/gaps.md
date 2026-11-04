# Gap tracker — DHI vs Claude Code, opencode, VS Code

This is the single source of truth for the gaps found in the session-64 comparison. Work them one
at a time. ROADMAP M38 points here, and STATE.md records which gap is in progress.

Sources: DHI's own docs and code audits; Claude Code and opencode docs (read 2026-11-03);
VS Code docs (mostly from memory, version not verified). Gaps marked **verified** were checked in
the code. The rest come from the audit agents' reports and are re-checked when the spec is written.

## How to work a gap

1. Set the row to `in progress` and note it in STATE.md.
2. Re-check the claim in the code. If it is wrong or stale, mark the row `dropped` with the reason.
3. Write `docs/features/F-###-slug.md` with behaviour and acceptance criteria (AGENTS.md rule 2).
   Take the next free F number then. If the gap needs a decision, write the ADR first (marked **ADR**).
4. Implement with tests. Visual changes regenerate goldens deliberately. Prompt changes run
   `make eval` live (`DHI_EVAL=1`), per F-099.
5. `make verify`. Flip the spec to done, set the row to `done` with the spec id, tick the box.

Status values: `todo`, `in progress`, `done`, `dropped`, `blocked (ADR)`.
Constraints every spec must respect: ADR-0004 minimal CLI, ADR-0011 no silent fallbacks,
ADR-0019/0024 CLI engine only, ADR-0043 standard formats, ADR-0044 user's system read-only,
no daemon.

## Phase 1 — Trust and prompt safety (do first)

- [x] **G-A1** `done` (F-122, ADR-0046) — Fence untrusted content in prompts (**verified** no content-trust code; ADR-0022 covers install trust only).
      Wrap `kb_search`, `channel_read`, task comments, journals/notes/digests and bridged MCP results in an
      `<untrusted_data source=…>` fence plus a standing operating rule. Add injection eval fixtures.
      Files: `internal/agentkit/dhitools`, `mcpbridge`, `runtime/clirun.go`. Absorbs **G-B5**, the web half of **G-C9**.
- [x] **G-C1** `done` for claude/codex/opencode/copilot/antigravity (F-123); cursor cannot be restricted (its `--force` skips deny rules); decided: it runs with the doctor warning, `deny` does not refuse it. Native-tool containment (**verified**: `claude.go:45,53` `bypassPermissions`, `codex.go:79`, `antigravity.go:66`; no deny flags).
      Per CLI, verify and pass tool restrictions where one exists (e.g. claude `--disallowedTools`); where none exists,
      `dhi doctor` and Settings say so. Absorbs the native web fetch/search half of **G-C9**.
- [x] **G-A2** `done` (F-124) — Carry the AGENTS.md of member repos the CLI does not load from its cwd. Canary runs showed claude, codex,
      opencode, copilot and cursor already read AGENTS.md in their cwd, so the CLI's copy is kept (no suppression) and only the
      other members' files are added. Shown in the Settings preview and doctor.
- [x] **G-A3** `done` (F-125) — Golden snapshots of the assembled prompt (`cliPromptAt`, which calls `behavior.Compose`). One golden for fresh and one for compacted; every engine must match, and one that differs gets its own golden.
- [x] **G-A4** `done` (F-126) — Over-size prompt delivery (above 96 KiB the system block moves into stdin): record it in the run
      record and doctor, or trim earlier.

## Phase 2 — Context and memory

- [x] **G-B1** `done` (F-127) — Per-chat context and cost meter (ROADMAP M37 S2). Data is in `Episode.LastContext` and `Usage`.
- [x] **G-B2** `done` (F-135) — Calibrate the 4-runes-per-token budget from CLI usage events; optional model summary via
      `agents.reflect_model`; user `:compact`.
- [x] **G-B4** `done` (F-134) — Fork a conversation (episode) by key. Also "new conversation" in a DM (ROADMAP M37 S2).
- [x] **G-B3** `done` (F-145) — Agent-facing symbols tool backed by LSP (repo-map-lite). Embeddings stay out of scope.
- [x] **G-B7** `done` (F-152) — Path-scoped rules (`paths:` frontmatter on standards/skills). Low priority.
- [x] **G-B6** `done` (F-153) — Open ADR-0037 items: channel file segmentation, KB index cache, spend-ledger rotation.
- [x] **G-B8** `done` (F-130) — Per-chat model picker (ROADMAP M37 S2).

## Phase 3 — Agent loop

- [x] **G-C2** `done` (F-144) — Gated plan mode (ROADMAP M37 S3): read-only turn that publishes `plan_update`, human approval card,
      then the write turn. Absorbs **G-A5** (per-mode prompts: plan, review, ask).
- [x] **G-C4** `done` (F-131) — Repeated-tool-call detection from the stream; in-flight spend check that stops a run at the cap.
- [ ] **G-C7** Wider evals: injection, lessons, orchestration, forge; multi-run live scoring.
- [ ] **G-C11** Re-verify resume for opencode, cursor, copilot.
- [x] **G-C8** `done` (F-164) — Optional OpenTelemetry export. Low priority.
- [x] **G-LSP-live** `done` (F-163) — Run LSP `gd` and rename live with gopls installed (open ROADMAP M37 item).

## Phase 4 — Editor, already on the roadmap (S2)

- [x] **G-E1** `done` (F-128) — LSP signature help
- [x] **G-E2** `done` (F-132) — LSP `didSave` and `didClose`
- [x] **G-E3** `done` (F-133) — `f`/`t`/`%` motions
- [x] **G-E4** `done` (F-136) — Word wrap
- [x] **G-E5** `done` (F-137) — Regex in buffer `:s`
- [x] **G-E6** `done` (F-138) — Large-file size warning
- [x] **G-E7** `done` (F-147) — Git gutter past 2000 lines
- [x] **G-E8** `done` (F-139) — OSC 9 setting

## Phase 5 — Editor depth (S3 and new)

- [ ] **G-E9** Folding
- [x] **G-E10** `done` (F-150) — Comment toggle
- [x] **G-E11** `done` (F-154) — Workspace symbols
- [x] **G-E12** `done` (F-158) — LSP code actions and quick fix
- [ ] **G-E13** Multi-cursor
- [x] **G-E14** `done` (F-159) — Block visual (`ctrl+v`)
- [ ] **G-E15** Breadcrumbs and sticky scroll
- [ ] **G-E16** Inlay hints
- [ ] **G-E17** More than 2 splits
- [ ] **G-E18** Snippets
- [ ] **G-E19** Selection expand and shrink
- [ ] **G-E20** Search results buffer ("search editor")
- [x] **G-E21** `done` (F-160) — EditorConfig
- [ ] **G-E22** LSP semantic tokens, call hierarchy, peek popup

## Phase 6 — Reliability, git, terminal, tasks

- [x] **G-R1** `done` (F-142) — Autosave
- [x] **G-R2** `done` (F-161) — Hot exit with swap recovery
- [x] **G-R3** `done` (F-155) — Local history (snapshot per save, restore, compare)
- [x] **G-R4** `done` (F-149) — Session restore of tabs and splits
- [x] **G-T1** `done` (F-146) — Task runner reading existing `package.json`, `Makefile`, `justfile` targets, with problem matchers
      (no new file format, ADR-0043)
- [x] **G-G1** `done` (F-156) — Merge-conflict tools (ROADMAP M37 S3)
- [x] **G-G2** `done` (F-151) — Line-level staging (ROADMAP M37 S3)
- [x] **G-G3** `done` (F-143) — Inline blame on every line (ROADMAP M37 S3)
- [x] **G-G4** `done` (F-129) — Stash
- [x] **G-G5** `done` (F-140) — File history and timeline
- [ ] **G-G6** Interactive rebase through a terminal tab
- [x] **G-G7** `done` (F-162) — Branch graph and cherry-pick
- [x] **G-M1** `done` (F-141) — Split terminals
- [x] **G-M2** `done` (F-157) — Terminal shell integration (command detection, jump between commands, send selection)
- [x] **G-S1** `done` (F-148) — Restricted mode for unknown repos (before an LSP or formatter runs)

## Phase 7 — Needs a decision first (**ADR**) or research

- [ ] **G-C3** `blocked (ADR)` — proposed ADR-0047, awaiting decision. Lifecycle hooks, reading the vendor hooks format from installed plugins. No DHI-only format (ADR-0043). **ADR**
- [ ] **G-C5** `blocked (ADR)` — proposed ADR-0048, awaiting decision. Engine failover as an explicit user-set per-agent `fallback_engine`; per-task model routing. **ADR** (ADR-0011)
- [ ] **G-C6** `blocked (ADR)` — proposed ADR-0049, awaiting decision. Headless/CI mode (e.g. `dhi run <task>`) and a GitHub Action. **ADR** (ADR-0004)
- [ ] **G-C10** `blocked (ADR)` — proposed ADR-0050, awaiting decision. ACP client engine (research first)
- [ ] **G-X1** (no ADR needed; approach: per-language runner table, pytest+debugpy first) DAP and test runner beyond Go (ROADMAP M37 S3)
- [ ] **G-X2** `blocked (ADR)` — proposed ADR-0051, awaiting decision. AI ghost text and next-edit prediction (ROADMAP M37 S3)
- [ ] **G-X3** (no ADR needed; images only where the CLI accepts them, export via redact+fence, `user-invocable` skills as slash commands) Chat extras: image attachments, session export, user-invocable skills as slash commands

## Not pursued

Webviews, pixel minimap, colour pickers, rich notebook output, voice, Live Share, GPU rendering, a
marketplace UI (ADR-0043), a resident daemon or client/server split, cloud routines, the Claude Code
`auto` classifier mode, a DHI-only extension API.

## Where DHI is already ahead (don't copy)

Multi-agent workspace (channels, board, inbox), worktree-first tasks, the Review surface, the OS sandbox
with approval cards, lessons (the Brain), resumable episodes, `:undo-turn`, Ideator, spend ledger with
budgets, a Go DAP debugger, LSP for six languages.

## Done log

(Add one line per finished gap: `G-id — F-### — date`.)

G-A1 — F-122 — 2026-11-03 (live injection eval moved to G-C7)
G-C1 — F-123 — 2026-11-03 (five of six CLIs; cursor cannot be restricted, reason in F-123)
G-A3 — F-125 — 2026-11-03 (memory/KB/lessons sections not in the fixture)
G-A4 — F-126 — 2026-11-03 (recorded and shown, not trimmed)
G-B1 — F-127 — 2026-11-04 (DM header; limit is session_tokens when a CLI session is kept)
G-E1 — F-128 — 2026-11-04 (fake LSP only; single-line call detection)
G-G4 — F-129 — 2026-11-04 (tracked changes only, no -u)
G-B8 — F-130 — 2026-11-04 (/model in a DM; free-text name, no catalogue; reflection keeps the agent's model)
G-E2 — F-132 — 2026-11-04 (fake LSP only; rename in the tree does not send didClose)
G-C4 — F-131 — 2026-11-04 (loop: claude/codex/opencode only; in-flight USD only for opencode, tokens elsewhere)
G-E3 — F-133 — 2026-11-04 (Haiku draft; coordinator fixed d% d; d,)
G-B4 — F-134 — 2026-11-04 (/fork drops the CLI session, rebuilds from the summary; /new)
G-G5 — F-140 — 2026-11-04 (:history and h in the git panel; read-only diff)
G-E5 — F-137 — 2026-11-04 (vim magic → RE2; no c flag; '<,'> not prefilled on :)
G-E4 — F-136 — 2026-11-04 (editor.wrap + :set wrap; ctrl-d/u and wheel stay logical)
G-B2 — F-135 — 2026-11-04 (calibrated ratio (claude reports per-call context); /compact; reflect_model reused; prompt change — live eval pending)
G-M1 — F-141 — 2026-11-04 (alt+s split, alt+o focus)
G-R1 — F-142 — 2026-11-04 (off/delay/focus; focus mode ignores surface switches)
G-G3 — F-143 — 2026-11-04 (cursor-line blame; :e! does not refetch)
G-B3 — F-145 — 2026-11-04 (code_symbols tool; only servers already started by the human (on-demand start is a product decision))
G-E7 — F-147 — 2026-11-04 (Myers diff; Hunks still quadratic)
G-E6 — F-138 — 2026-11-04 (5 MiB prompt; reads 8 KiB to sniff binary)
G-E8 — F-139 — 2026-11-04 (desktop_notifications; theme.Notify)
G-T1 — F-146 — 2026-11-04 (npm/pnpm/yarn, make, just; two problem regexes)
G-C2 — F-144 — 2026-11-04 (/plan /review /ask; read-only for claude+codex, others refused; live eval + flag check pending; approved plan reposted as a human message)
G-G2 — F-151 — 2026-11-04 (V range + s/u; contiguous ranges; git apply --cached)
G-E10 — F-150 — 2026-11-04 (gcc/gc{motion}/ctrl+/; also fixed count-before-operator)
G-R4 — F-149 — 2026-11-04 (tabs+split on quit to .dhi/tmp/session.json; large files skipped)
G-S1 — F-148 — 2026-11-04 (trust in ~/.config/dhi/trust.json; editor start points only, agent tools not gated (open))
G-B7 — F-152 — 2026-11-04 (.claude/rules/**/*.md per member with paths:; skills paths:; files from the trigger text only)
G-E11 — F-154 — 2026-11-04 (:wsym picker; no key binding)
G-B6 — F-153 — 2026-11-04 (channel segments 8 MiB, ledger 1 MiB, in-memory KB cache)
G-R3 — F-155 — 2026-11-04 (.dhi/history; :localhistory compare/restore; project replace not snapshotted)
G-G1 — F-156 — 2026-11-04 (]x/[x, :conflict, panel UU marks, stage offer)
G-M2 — F-157 — 2026-11-04 (OSC 133/7; alt+k/j prompts; alt+enter or :send)
G-E21 — F-160 — 2026-11-04 (editorconfig > detected > settings; bare indent_size means spaces (deviation, documented))
G-R2 — F-161 — 2026-11-04 (swap 2s debounce in .dhi/tmp/swap; recover card; quit guard k = keep for next time)
G-E14 — F-159 — 2026-11-04 (ctrl+v; block ops/register; suggestion/agent-edit guarded)
G-LSP-live — F-163 — 2026-11-04 (TestLiveGopls (DHI_LIVE_LSP=1), gopls 0.24: all features pass)
G-E12 — F-158 — 2026-11-04 (quickfix marker, ranked ga, resolve, command after edit, :organize; live gopls code_actions passes)
G-G7 — F-162 — 2026-11-04 (:graph / g in panel; cherry-pick with confirm; branch at commit)
G-C8 — F-164 — 2026-11-04 (OTLP/HTTP JSON from standard OTEL_* env; metadata only; doctor row)
