# ADR-0048: Engine failover is an explicit per-agent setting; models route per workflow step

Date: 2026-11-04 · Status: proposed · Companion to: ADR-0011 (no silent fallbacks), ADR-0019
(CLI engine), ADR-0031 (episodes, budgets), ADR-0016 (agent spec), ADR-0020 (workflows)
Serves: gap G-C5.

## Context
An agent names one engine (`cli:<name>`, `internal/agentkit/manifest`) and one model (`Agent.Model`,
`CLIModel()`; `"default"` means the CLI's own). When a CLI is missing, signed out or rate-limited,
the run fails with a named reason (`clirun/failure.go`, `denied.go`) and the user reassigns the agent
by hand. ADR-0011 forbids a program-chosen downgrade: an alternative must be a setting the user
deliberately chose, honored and labeled.
Per-task model choice exists only as the agent's single model; "cheap model for triage, strong one
for implementation" needs two agents.

## Options
1. **Do nothing.** Fully ADR-0011 compliant; unattended runs (autopilot, goal loops) stall on any
   outage or quota.
2. **Explicit `fallback_engine` per agent** (plus optional `fallback_model`). Used only for a closed
   list of failure classes (not installed, signed out, quota or rate limit, unavailable) and only if
   the first engine had no side effects yet (no tool call served). Every use is a transcript event
   and a toast; doctor lists configured fallbacks and whether they resolve.
3. **Automatic engine ranking** by availability or price. Smart, but it is the silent substitution
   ADR-0011 bans, and it changes which vendor sees the code.

## Recommendation (the user decides): option 2, plus model routing in workflows
- `fallback_engine` is unset by default and has no suggested value; setup may offer it as a named
  choice, never apply it. Validated strictly at parse like `engine` (unknown CLI = refusal).
- A fallback never crosses an approval boundary silently: if the second CLI differs in native-web
  or MCP-wiring status (F-123), first use shows a card.
- Failure after side effects does not fail over; the episode stays resumable (ADR-0031) and the user
  chooses.
- Model routing: a workflow step carries optional `model` and `engine` overrides, so routing is
  declared, not inferred. No heuristic router; the budget ledger already prices each run.

## Consequences
- Unattended runs survive an outage, only where the user opted in.
- Spend and data flow can move to another vendor; label, event and doctor row make that visible.
- More manifest fields (schema bump) and a failure classifier that must stay conservative.

## If accepted
1. `internal/agentkit/manifest`: `fallback_engine`, `fallback_model`, strict parse, schema bump.
2. `internal/agentkit/clirun`: expose a failure class (extend `failure.go`) as a closed enum.
3. `internal/agentkit/runtime`: one retry on the fallback when the class matches and no tool call was
   served; record an `engine_failover` event (`internal/agentkit/trace`).
4. `internal/agentkit/workflow`: per-step `model`/`engine` overrides, validated at load.
5. `internal/settings`, `internal/doctor`: Settings field and doctor row; table-driven tests.
6. Spec F-TBD (next free number).
