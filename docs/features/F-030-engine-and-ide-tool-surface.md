# F-030: Engine inversion & the IDE tool surface

Status: in progress (M15; P0 engine seam + P1 filesystem read tools
landed 2026-09-26) · Companion to: ADR-0019 (DHI owns the loop, CLI
is the engine), ADR-0011 (no silent fallbacks), ADR-0018 (loopback
serving), F-028 (the first tool surface). Product rule: agents work the
way the IDE works — through declared tools, under declared authority.

## Summary

ADR-0019 inverts ownership: the host CLI becomes the inference engine,
and DHI becomes the host that declares every tool, every authority, and
every record. F-030 is the foundation milestone that makes this real:
an `engine` seam with per-agent assignment and a workspace default; MCP
tool serving verified for **all six** adapters; the existing twelve
tools grown into the full IDE catalog; the flat slug allowlist replaced
by capability scopes with grant-memory approvals; and a tightened
sandbox (network deny-by-default, no free-form shell).

## Part A — the engine seam

- Manifest `schema = 3`: `engine = "cli:<name>"` (optional); workspace
  default engine in settings; per-agent override. Unknown engine names
  refuse (strict, naming the valid set). Schema-1/2 load with the
  default engine and **derived** scopes, never a silent grant.
- The six adapters become engine declarations; `Tested`/live-verify
  still gates doctor. Model selection is per-agent (`model` key).
- `dhi-action` final-message parsing is removed once every shipped
  adapter has verified MCP wiring (the `MCPOK=false` shipping state
  ends).

## Part B — the IDE tool catalog

- Filesystem: `read`, `write`, `patch`, `list`, `glob` — jailed by VPath
  and member roots (no escape, symlink-aware). **Landed:** all five.
  Read-only tools cap (1 MiB read, 200-match glob); `write` and `patch`
  are mutating and cross approvals; `patch` refuses absent/ambiguous
  matches unless `replace_all`.
- Search: `search` (ripgrep fan-out, read-only).
- Git: `git_status`, `git_diff`, `git_log`, `git_branch`, `git_commit`,
  `git_push` — write operations cross approvals and the active workflow.
- Editor/LSP: `editor_open`, `editor_reveal`, `editor_apply_edit`,
  `lsp_hover`, `lsp_definition`, `lsp_references`, `lsp_rename`,
  `lsp_code_action`. Edits apply through the same WorkspaceEdit path the
  human uses (bottom-up, one undo group).
- Product: tasks CRUD, `kb_search`/`kb_contribute`, `memory_*`,
  `channel_read`/`channel_post`, ideation `session_*`/`artifact_*`,
  board/PR/review reads.
- Execution: `run` — **allowlisted** commands only (build/test/lint/git),
  OS-sandboxed, workflow-scoped, no free-form shell.
- Meta: `ask_human` (an explicit question into the approvals surface).

## Part C — capability scopes & approvals

- Scopes: `read · write · exec · network · git · push · admin`, each
  `auto | ask | deny`. Resolved manifest → team → workspace; conflict
  refuses by name.
- Every mutating scope crosses the single approvals queue; answers
  `grant once` / `grant always` / `deny`; grant-memory is workspace-
  scoped and auditable. Every decision is recorded in the run.
- `doctor` reports an `agent-tools` row per agent: engine, MCP posture,
  scopes, and the containment caveat (native tools best-effort only).

## Part D — sandbox & containment

- Network deny-by-default; declared origins per scope/workflow. Exec and
  any external MCP server run under the OS sandbox. Seatbelt/bwrap
  profiles gain declared-network roots.
- Containment is documented as best-effort; the `api:` engine kind
  remains designed-not-built.

## Acceptance criteria

- [x] A schema-3 manifest selects an engine (workspace default +
      per-agent override); an unknown engine or `api:` kind refuses by
      name; schema-1/2 load with their engine derived from `runtime`
      (capability scopes land in P2).
      — landed: manifest schema 3 + `ParseEngine`/`EngineString`,
      settings `engine` default, `runtime.Config.DefaultEngine` +
      `engineName` resolution, doctor `agent-tools` resolves the
      inherited engine. Adapters remain selectable only when detected on
      the machine (claude/codex/opencode today); cursor/copilot/gemini
      become selectable once installed and MCP-verified.
- [ ] Every shipped adapter serves DHI tools over the loopback endpoint
      (MCP wiring verified); `dhi-action` is gone.
- [ ] An agent can, in one turn: read/search/edit a file, run the
      workflow's test command, commit, and post to its channel — each
      mutation surfaced as an approval.
- [ ] An operation outside scope is refused by name and recorded; a
      grant-always suppresses only that agent+scope.
- [ ] No raw shell exists; `run` refuses non-allowlisted commands.
- [ ] Network is denied unless declared; exec is sandbox-wrapped.
- [ ] `make verify` green per phase.

## Deferred

- The direct-API engine kind (`api:<provider>`) — the recorded path to a
  hard tool guarantee and offline inference.
- Multi-workspace (more than one workspace open per process).
