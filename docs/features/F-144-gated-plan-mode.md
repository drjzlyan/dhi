# F-144 — Gated plan mode and read-only modes

Status: done · gap G-C2 (absorbs G-A5)

A big change should be agreed before it is made. Until now a plan was only a request in the
prompt, and nothing stopped the agent from writing before the human had seen it.

## Decision
- The read-only turn is enforced by the CLI adapter, not by the prompt. An adapter declares
  `ReadOnlyOK` only where it has a mechanism: claude (`--disallowedTools` with Edit, Write,
  MultiEdit, NotebookEdit, Bash, merged with the web denial) and codex (`--sandbox read-only`, no
  bypass flag). Live-verified in F-170 (claude 2.1.285, codex 0.162.1). Every other adapter carries a `ReadOnlyWhy`
  and is refused by name (ADR-0011).
- DHI's own tools run at read scope only in these turns (`turnScopes`).
- A read-only turn never resumes a CLI session.

## Behaviour
- `/plan <request>` in a DM: the agent runs read-only with the plan-mode prompt, publishes the
  plan with `plan_update`, and its reply plus the numbered steps land in the thread.
- An approval card shows the plan (alt+y approve, alt+n reject, `/yes` `/no`; F-117). Any
  affirmative choice means "this plan, once"; a plan is never remembered.
- Approve: a human message "Plan approved. Carry it out…" with the steps is posted into the thread
  and the normal write turn runs with it in its prompt.
- Reject: nothing is written; the plan stays on the episode as a draft.
- A turn that never calls `plan_update` says so and shows no card.
- `/review <what>` and `/ask <question>` run the same read-only turn with their own mode prompt and
  post the reply; no card.
- `behavior.Compose` takes a `Mode` ("" = unchanged prompt, `plan`, `review`, `ask`).

## Acceptance
- Default prompt goldens unchanged; `prompt_mode_plan|review|ask` goldens (`TestPromptGoldens`).
- Argv: `TestReadOnlyOKMatchesAMechanism`, `TestClaudeReadOnlyDeniesWriteTools`,
  `TestCodexReadOnlyDropsTheBypass`.
- Runtime: `TestReadOnlyTurnAskDeniesWriteToolsAndReplies`, `TestReadOnlyTurnPlanWithoutPlanUpdateSaysSo`,
  `TestReadOnlyTurnRefusesACLIWithoutAReadOnlyMode`, `TestPlanApprovalRunsTheWriteTurnWithThePlan`,
  `TestPlanRejectionWritesNothingAndKeepsTheDraft`, `TestReadOnlyTurnScopesDenyEverythingButRead`.
- Card: `TestPlanApprovalIsNeverRemembered`. Commands: `TestParseModeCmd`,
  `TestPlanCommandStartsAReadOnlyTurn`, `TestModeCommandRefusals`.

## Not run
The mode prompts are a prompt change: a live eval is needed. (The claude and codex read-only flags
were live-verified in F-170, claude 2.1.285 and codex 0.162.1.)
