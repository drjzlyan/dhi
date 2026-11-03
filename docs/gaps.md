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

- [ ] **G-B1** Per-chat context and cost meter (ROADMAP M37 S2). Data is in `Episode.LastContext` and `Usage`.
- [ ] **G-B2** Calibrate the 4-runes-per-token budget from CLI usage events; optional model summary via
      `agents.reflect_model`; user `:compact`.
- [ ] **G-B4** Fork a conversation (episode) by key. Also "new conversation" in a DM (ROADMAP M37 S2).
- [ ] **G-B3** Agent-facing symbols tool backed by LSP (repo-map-lite). Embeddings stay out of scope.
- [ ] **G-B7** Path-scoped rules (`paths:` frontmatter on standards/skills). Low priority.
- [ ] **G-B6** Open ADR-0037 items: channel file segmentation, KB index cache, spend-ledger rotation.
- [ ] **G-B8** Per-chat model picker (ROADMAP M37 S2).

## Phase 3 — Agent loop

- [ ] **G-C2** Gated plan mode (ROADMAP M37 S3): read-only turn that publishes `plan_update`, human approval card,
      then the write turn. Absorbs **G-A5** (per-mode prompts: plan, review, ask).
- [ ] **G-C4** Repeated-tool-call detection from the stream; in-flight spend check that stops a run at the cap.
- [ ] **G-C7** Wider evals: injection, lessons, orchestration, forge; multi-run live scoring.
- [ ] **G-C11** Re-verify resume for opencode, cursor, copilot.
- [ ] **G-C8** Optional OpenTelemetry export. Low priority.
- [ ] **G-LSP-live** Run LSP `gd` and rename live with gopls installed (open ROADMAP M37 item).

## Phase 4 — Editor, already on the roadmap (S2)

- [ ] **G-E1** LSP signature help
- [ ] **G-E2** LSP `didSave` and `didClose`
- [ ] **G-E3** `f`/`t`/`%` motions
- [ ] **G-E4** Word wrap
- [ ] **G-E5** Regex in buffer `:s`
- [ ] **G-E6** Large-file size warning
- [ ] **G-E7** Git gutter past 2000 lines
- [ ] **G-E8** OSC 9 setting

## Phase 5 — Editor depth (S3 and new)

- [ ] **G-E9** Folding
- [ ] **G-E10** Comment toggle
- [ ] **G-E11** Workspace symbols
- [ ] **G-E12** LSP code actions and quick fix
- [ ] **G-E13** Multi-cursor
- [ ] **G-E14** Block visual (`ctrl+v`)
- [ ] **G-E15** Breadcrumbs and sticky scroll
- [ ] **G-E16** Inlay hints
- [ ] **G-E17** More than 2 splits
- [ ] **G-E18** Snippets
- [ ] **G-E19** Selection expand and shrink
- [ ] **G-E20** Search results buffer ("search editor")
- [ ] **G-E21** EditorConfig
- [ ] **G-E22** LSP semantic tokens, call hierarchy, peek popup

## Phase 6 — Reliability, git, terminal, tasks

- [ ] **G-R1** Autosave
- [ ] **G-R2** Hot exit with swap recovery
- [ ] **G-R3** Local history (snapshot per save, restore, compare)
- [ ] **G-R4** Session restore of tabs and splits
- [ ] **G-T1** Task runner reading existing `package.json`, `Makefile`, `justfile` targets, with problem matchers
      (no new file format, ADR-0043)
- [ ] **G-G1** Merge-conflict tools (ROADMAP M37 S3)
- [ ] **G-G2** Line-level staging (ROADMAP M37 S3)
- [ ] **G-G3** Inline blame on every line (ROADMAP M37 S3)
- [ ] **G-G4** Stash
- [ ] **G-G5** File history and timeline
- [ ] **G-G6** Interactive rebase through a terminal tab
- [ ] **G-G7** Branch graph and cherry-pick
- [ ] **G-M1** Split terminals
- [ ] **G-M2** Terminal shell integration (command detection, jump between commands, send selection)
- [ ] **G-S1** Restricted mode for unknown repos (before an LSP or formatter runs)

## Phase 7 — Needs a decision first (**ADR**) or research

- [ ] **G-C3** Lifecycle hooks, reading the vendor hooks format from installed plugins. No DHI-only format (ADR-0043). **ADR**
- [ ] **G-C5** Engine failover as an explicit user-set per-agent `fallback_engine`; per-task model routing. **ADR** (ADR-0011)
- [ ] **G-C6** Headless/CI mode (e.g. `dhi run <task>`) and a GitHub Action. **ADR** (ADR-0004)
- [ ] **G-C10** ACP client engine (research first)
- [ ] **G-X1** DAP and test runner beyond Go (ROADMAP M37 S3)
- [ ] **G-X2** AI ghost text and next-edit prediction (ROADMAP M37 S3)
- [ ] **G-X3** Chat extras: image attachments, session export, user-invocable skills as slash commands

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
