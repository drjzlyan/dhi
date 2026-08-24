# F-007: Agent runtime — agentkit, tools, message bus, chat sidebar

Status: done (M3, 2026-08-24) · Marketplace packs/org UI track M4 ·
embedding-backed KB retrieval deferred (ADR-0007 seam)
Decisions: ADR-0003 (Anthropic-first + Mock), ADR-0005 (hermetic), ADR-0006 (path-jail-first), ADR-0007 (file-based memory + KB)

## Summary

The runtime that turns agents from copy in a spec into teammates: rostered
agent manifests under `.dhi/agents/`, one narrow streaming `Provider`
interface (scripted Mock for tests, Anthropic Messages API for real use),
namespaced VPath tools executed strictly behind `sandbox.Guard` policies,
an MCP client bridging external servers into the same guarded registry, a
message bus with channels/DMs/threads persisted as JSONL, per-agent private
memory plus a shared knowledge base (ADR-0007), and an Editor chat sidebar
that consumes all of it.

## Design decisions (binding)

1. **Provider is hand-rolled HTTP + SSE** against the Anthropic Messages
   API. No SDK dependency. Streaming-first (`Stream(ctx, req) <-chan
   StreamEvent`). The adapter is verified offline against an `httptest`
   fixture server; CI never needs keys or network.
2. **MCP client ships in M3**, both stdio and streamable-http transports.
   Remote tools are bridged into the same tool registry and pass through
   the same `sandbox.Guard` policy checks as native tools. No bypass path.
3. **Chat sidebar is a right-side panel** toggled with `ctrl+a`, following
   the drawer/git-panel focused→blurred→closed tri-state pattern.
4. **Human-in-the-loop:** sandbox `Ask` decisions and KB contributions
   under `review` policy land in pending queues surfaced in the sidebar;
   nothing executes or publishes without explicit approval.
5. **All agent filesystem/exec access goes through VPath + Guard.** Tools
   address files only as `<member>/<rel-path>`; absolute paths from the
   model are rejected before they reach the jail.

## Components

1. **Manifest + roster** (`internal/agentkit/manifest`) — `.dhi/agents/<id>.toml`:
   id/name/model/system prompt/tool allowlist/embedded sandbox policy
   JSON/provider env-var name. Strict validation (unknown keys, bad ids,
   missing policies); roster loader returns deterministic order.
2. **Provider** (`internal/agentkit/provider`) — `Provider` interface +
   scripted `MockProvider` (deterministic conversations for every test) +
   Anthropic adapter (SSE streaming, tool-use blocks, key read once from
   declared env var, never logged or persisted).
3. **Tools** (`internal/agentkit/tools`) — `Tool` iface; built-ins:
   `read` / `write` / `list` / `search` over `<member>:<path>` vpaths,
   each call checked via `sandbox.Guard.Check`; exec via `Guard.Exec` with
   `toolchain.Manager.Env()` PATH. Pending-approval queue collects `Ask`.
4. **MCP client** (`internal/mcp`) — initialize handshake, capability
   negotiation, stdio + streamable-http transports; server tools exposed
   as namespaced tools (`mcp__<server>__<tool>`) behind Guard policies.
5. **Message bus** (`internal/agentkit/bus`) — channels (`#name`), DMs,
   threads; `@agent` mention triggers a turn: system prompt + memory slice
   + KB hits + tool round-trips → streamed reply. JSONL append/replay
   persistence under `.dhi/channels/`. One goroutine per active turn;
   events fan out to UI subscribers via channels.
6. **Memory + KnowledgeStore** (`internal/agentkit/memory`,
   `internal/agentkit/knowledge`) — private journal.jsonl + notes.md per
   agent; shared KB markdown entries + `knowledge/index.json` provenance;
   retrieval = injected `search.Searcher` (managed rg) + recency/importance
   scoring; contribution policy `auto|review` per workspace.
7. **Chat sidebar** (`internal/tui/surfaces/editor/chat.go`) — roster
   picker (switch mid-session), streaming transcript render, mention input,
   apply-suggestion→active-buffer action, approval prompts for pending
   Ask/review items.

## Non-goals (this milestone)

- Marketplace packs, org/team management (M4). Worktree-scoped tasks (M4).
- Embedding-based retrieval (ADR-0007 leaves seam open).
- Multi-provider adapters beyond Anthropic/Mock (ADR-0003).

## Acceptance criteria

- Agent manifests: valid roster loads deterministically; tampered manifests
  (bad id, unknown keys, missing policy, undeclared env var) fail with
  actionable errors. Table tests.
- Providers: Mock and Anthropic pass identical conformance tests (stream
  ordering, tool-call blocks, abort mid-stream). Anthropic adapter tested
  against httptest fixture incl. SSE chunk boundaries, error events, 401.
- Tools: write outside jail → Deny, never touches disk (tamper test);
  `Ask` rule → queued, not executed until approved; approved write lands
  byte-exact. Exec runs through shim PATH; recursion guard respected.
- MCP: fake stdio + http servers (echo URIs, null-reply unknown methods)
  negotiate, list tools, execute; unknown method → clean error; server
  death mid-call surfaces as error event, no goroutine leak (race tests).
- Bus: mention triggers exactly one turn; reply streams to subscribers;
  kill + reopen replays full transcript from JSONL byte-identically.
- Memory/KB: journal appends survive restarts; KB retrieval returns scored
  hits from fixture corpus via managed rg; `review` policy parks
  contribution in pending queue instead of writing index.
- Sidebar: goldens for closed/open/focused states; apply-suggestion edits
  active buffer; approvals reachable entirely by keyboard.

## Verification

`make verify` green (fmt+vet+build+race). New packages carry unit tests;
visual changes regenerate goldens deliberately via `DHI_UPDATE_GOLDENS=1`.
