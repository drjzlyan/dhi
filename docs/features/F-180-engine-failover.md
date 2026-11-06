# F-180 — Engine failover to a visible, editable fallback

Status: done · gap G-C5 · ADR-0048 (accepted, amended)

When an agent's CLI is signed out, rate-limited, missing or down, the run failed with a named reason
and the user reassigned the agent by hand; unattended runs (autopilot, goal loops) stalled. ADR-0011
forbids a program-chosen downgrade, so the alternative is an agent setting that is shown and editable.

## Decision (ADR-0048, amended)
`fallback_engine` and `fallback_model` are set by default, not left empty. When an agent's engine is
chosen (new agent, wizard team, or on load for an agent with none recorded) DHI picks the first
installed CLI that is not the agent's own, in `clirun.FallbackOrder` (claude, codex, opencode,
cursor-agent, copilot, antigravity; others after, alphabetically), and writes it into the manifest.
Settings → AGENTS shows and edits it, including `none`. Per-step workflow model routing is out of scope.

## Behaviour
- Manifest schema 7: `fallback_engine` = `"cli:<name>"` | `"none"` | unset (never chosen) and
  `fallback_model` (the fallback CLI's own model; empty = its default). Parsed strictly like `engine`:
  an unknown CLI, the agent's own engine, or a model without an engine refuses by name. Older
  manifests load with none recorded.
- No other CLI installed: nothing is written (a later start tries again) and doctor names why.
  `none` is a choice and is never replaced.
- Failover happens only for `clirun.Failure.Failover()`, a closed list: `auth` (signed out),
  `rate_limit`, `not_installed`, `unavailable` (503/502). A crash, stall, overflow or timeout never
  fails over.
- Only before any tool ran this turn (no `command` event on any attempt). After a tool call the
  episode stays resumable and the failure is reported as before. One move per turn; it does not spend
  the retry budget. The primary's session and model override are dropped (another vendor).
- A primary whose binary is missing at start builds the agent on its fallback when that resolves
  (otherwise the roster refuses as before) and the first turn announces it.
- If the fallback differs from the primary in native-web denial or MCP wiring (F-123), the first
  use asks through an approval card (`failover:<from>><to>` scope, remembered like any grant); a
  denial or a missing approvals queue leaves the original failure, named in the thread.
- Every failover: thread notice ("switching engine: cli:a → cli:b (why)"), toast (`OnFailover`),
  an `engine_failover` transcript record, `failover_from`/`failover_why` on the run record (its
  `runtime` is the engine that ran), and the trace run header.
- The spend ledger entry carries `engine`, the CLI that actually ran.
- `dhi doctor` row `agents/fallback` lists each agent's fallback and whether it resolves (warns when
  a chosen fallback is not installed).

## Acceptance
- Classifier and closed list: `TestClassify`, `TestFailureFailoverClosedList` (clirun).
- Default picker: `TestPickFallback` (order, excludes the primary, none installed gives a named note).
- Manifest: `TestFallbackFields` (strict parse, round trip, schema back-compat).
- Defaults written: `TestCreateAgentRecordsDefaultFallback`, `TestEnsureFallbacksFillsOnlyUnsetAgents`,
  `TestEnsureFallbacksNoOtherCLIChangesNothing` (org).
- Runtime (stub CLIs): `TestFailoverSignedOutRunsOnFallback` (first signed out, fallback runs, model
  and ledger engine), `TestFailoverNotAfterAToolRan`, `TestFailoverNeedsAFallbackAndAClosedClass`,
  `TestFailoverFallbackNotInstalled`, `TestFailoverPrimaryNotInstalled`,
  `TestFailoverWiringDifferenceAsksFirst`.
- Settings: `TestNewAgentFormShowsAndSavesDefaultFallback`,
  `TestAgentFallbackCanBeSetToNoneAndKeepsItsModel`, `TestAgentFallbackSameAsEngineRefused`,
  `TestAgentsListShowsFallback`; golden `settings_agents`.
- Doctor: `TestFallbacksRow`.
