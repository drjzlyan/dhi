# STATE — current position

Updated: 2026-08-24 (session: M3 closed)

## Where we are

**M3 COMPLETE AND VERIFIED (`make verify` green; not yet committed).**
Agent runtime end-to-end: roster manifests → Anthropic/Mock providers →
sandboxed VPath tools + MCP bridge → message bus with JSONL replay →
memory/KB → editor chat sidebar (ctrl+a). Doctor grew an agents suite.
Next milestone: M4 Workspace full.

## Just finished

- `internal/agentkit/{manifest,provider,tools,bus,runtime,memory,knowledge}`
  + `internal/mcp` + `internal/jsonl`; all under `-race`.
- Provider conformance suite runs Mock AND Anthropic-through-httptest
  through identical scenarios (text ordering, tool round trip, cancel).
- Sidebar: tri-state toggle, [/] channel switch, ^f apply-suggestion,
  y/n approvals wired to sandbox Ask decisions.
- cmd/dhi wires the runtime only when `.dhi/agents/` has a roster;
  missing API keys warn in doctor, fail at first turn.
- F-007 flipped done; ROADMAP M3 checked off.

## Gotchas learned

1. encoding/json drops struct fields sharing a duplicate tag — the SSE
   "delta" object differs per event type; decode via json.RawMessage
   and re-unmarshal per type.
2. net.Pipe is synchronous: any test-side pipe end needs its own read
   pump or writers block forever (MCP pipeClient).
3. Manifests without policy_json are deny-all by design — harnesses
   must ship explicit policies or every tool call errors.
4. Sandbox ops are read/write/exec/net only; the list tool maps to
   OpRead. Policies referencing "list" fail validation.
5. Carried: shim recursion guard (resolve symlinks before re-exec);
   nil Events chan wiring; bounded shutdown; fake servers echo URIs +
   null-reply unknown requests.

## Next up (M4 per ROADMAP)

1. Spec F-008-workspace-management? No — F-003 exists; write
   implementation plan against it before code (add/remove/rename
   members from UI, atomic workspace.toml rewrite, live re-resolution).
2. Org UI: create/edit/archive agents, teams; marketplace packs
   (path/git) install into .dhi/agents + MCP config.
3. Channels UI on the Workspace view (#general, teams, DMs) reusing
   agentkit/bus; threads + task cards (.dhi/tasks schema first).
4. Task↔ChangeSet binding (per-repo worktrees via gitcore); kanban.
5. Inspection dashboards: current work, activity, memory, KB.
6. Attach-points: invite agents into Ideator/Reviewer sessions.

## Open questions for user

- None blocking.
