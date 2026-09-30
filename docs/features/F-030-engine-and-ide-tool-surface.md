# F-030: Engine inversion & the IDE tool surface

Status: in progress (M15; P0 engine seam + P1 fs/git/ideation/run/
ask_human landed 2026-09-26; editor/LSP via ADR-0023 next) · Companion
to: ADR-0019 (DHI owns the loop, CLI
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
  ends). **Landed (2026-09-30):** all six adapters wired; the
  `toolbridge` parser and dispatch path are deleted, and `pr_open`
  became a served tool.

## Part B — the IDE tool catalog

- Filesystem: `read`, `write`, `patch`, `list`, `glob` — jailed by VPath
  and member roots (no escape, symlink-aware). **Landed:** all five.
  Read-only tools cap (1 MiB read, 200-match glob); `write` and `patch`
  are mutating and cross approvals; `patch` refuses absent/ambiguous
  matches unless `replace_all`.
- Search: `search` (ripgrep fan-out, read-only).
- Git: `git_status`, `git_diff`, `git_log`, `git_branch`, `git_commit`,
  `git_push` — write operations cross approvals and the active workflow.
  **Landed:** `git_status`/`git_log`/`git_branch`/`git_diff` (read-only,
  on the turn's workdir) and mutating `git_commit` (approval-gated,
  authored by the user's resolved identity); `git_push` remains.
- Editor/LSP: `editor_open`, `editor_reveal`, `editor_apply_edit`,
  `lsp_hover`, `lsp_definition`, `lsp_references`, `lsp_rename`,
  `lsp_code_action`. Edits apply through the same WorkspaceEdit path the
  human uses (bottom-up, one undo group). **Landed:** `editor_open`/`editor_reveal`/
  `editor_apply_edit` and `lsp_hover`/`lsp_definition`/`lsp_references`/
  `lsp_rename`/`lsp_code_action` via the ADR-0023 seam (runtime asks,
  app routes on the UI loop; VPaths resolved by the bridge; edits go
  through the live buffer when open, else the file; LSP runs off-loop
  and replies async; nil seam refuses by name).
- Product: tasks CRUD, `kb_search`/`kb_contribute`, `memory_*`,
  `channel_read`/`channel_post`, ideation `session_*`/`artifact_*`,
  board/PR/review reads. **Landed:** `ideation_list`/`ideation_read`
  (read-only; artifact write rides the file tools under
  `.dhi/sessions/`).
- Execution: `run` — **allowlisted** commands only (build/test/lint/git),
  OS-sandboxed, workflow-scoped, no free-form shell. **Landed:** a fixed
  safe set (`go build/test/vet/fmt`, `rg`), no shell, approval-gated;
  the per-agent/per-workflow command list arrives with M16.
- Meta: `ask_human` (an explicit question into the approvals surface).
  **Landed:** posts the question to the turn's thread (the human answers
  next turn; the approvals queue stays strictly for permission).

## Part C — capability scopes & approvals

- Scopes: `read · write · exec · network · git · push · admin`, each
  `auto | ask | deny`. Resolved manifest → team → workspace; conflict
  refuses by name. **Landed (core):** `internal/agentkit/scopes` (Scope/
  Effect/Set/ToolScope/Resolve), manifest schema 4 `[scopes]`, and the
  dhitools gate now denies/asks/allows by scope (replacing the `mutate`
  flag). Team layering landed (org `[teams.<slug>.scopes]`, preserved on
  update, resolved default→team→manifest); workspace scopes via settings `[scopes]`
  (resolved default→workspace→team→manifest); grant-memory landed (approvals `a` =
  allow-always per agent+scope; typed elsewhere in the composer). The
  Settings scopes editor landed (CONFIG rows cycle each scope
  auto→ask→deny, persisted live). P2 complete. Doctor gains an `authority` row listing
  each agent's non-default effects.
- Every mutating scope crosses the single approvals queue; answers
  `grant once` / `grant always` / `deny`; grant-memory is workspace-
  scoped and auditable. Every decision is recorded in the run.
- `doctor` reports an `agent-tools` row per agent: engine, MCP posture,
  scopes, and the containment caveat (native tools best-effort only).

## Part D — sandbox & containment

- Network deny-by-default for DHI-served children; declared origins
  per scope. **Landed:** the `run` tool is network-denied via the OS
  sandbox when the `network` scope is not `auto` (seatbelt `deny
  network*`, bwrap `--unshare-net`); host agent CLIs keep network
  (they must reach their vendors). A Noop sandbox cannot deny, so `run`
  refuses rather than leak. Seatbelt/bwrap
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
      the machine (claude/codex/opencode/antigravity today); cursor/
      copilot become selectable once installed and MCP-verified. The
      deprecated Gemini CLI was replaced by Antigravity (`agy`, live-
      verified 2026-09-26 on 1.2.11).
- [x] Every shipped adapter serves DHI tools over the loopback endpoint
      (MCP wiring verified); `dhi-action` is gone. Landed: claude
      (--mcp-config, file+argv), opencode (OPENCODE_CONFIG, env file),
      codex (`-c mcp_servers.dhi.url=`, inline argv), cursor-agent
      (worktree `.cursor/mcp.json` + git-exclude + `--approve-mcps`),
      copilot (worktree `.mcp.json` + git-exclude + `--allow-all-tools`
      `--disable-builtin-mcps`) — all live-verified end-to-end
      2026-09-26 (a real tool call through DHI's loopback server) —
      and antigravity (`agy`, live-verified 2026-09-30 on 1.2.11): a
      per-turn `--gemini_dir` mirror of `~/.gemini` (symlinked except
      `config/mcp_config.json`) injects the loopback endpoint without
      mutating the user's global config. `dhi-action` is removed at
      parity: `pr_open` is now a served tool (task_create/status/assign
      already were) and the `toolbridge` final-message parser is gone.
      The streamable-HTTP server was fixed in the same pass (GET SSE
      stream + SSE-framed POST replies + `2025-11-25` protocol +
      lowercase tool keys) — the legacy handler made opencode drop
      every served tool.
- [x] An agent can, in one turn: read/search/edit a file, run the
      allowlisted test command, commit, and post to its channel — each
      mutation surfaced as an approval (workflow-scoped commands land
      with M16).
- [ ] An operation outside scope is refused by name and recorded; a
      grant-always suppresses only that agent+scope.
- [ ] No raw shell exists; `run` refuses non-allowlisted commands.
- [ ] Network is denied unless declared; exec is sandbox-wrapped.
- [ ] `make verify` green per phase.

## Deferred

- The direct-API engine kind (`api:<provider>`) — the recorded path to a
  hard tool guarantee and offline inference.
- Multi-workspace (more than one workspace open per process).
