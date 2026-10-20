# F-099 — Tagged prompt sections, operating rules, and agent visibility

Status: done (session 60, M34 P6) · ADR-0041

## Why
The headless prompt was a run of untagged paragraphs. The grounding rules were one run-on block
with no reasons. From the Editor (now the first screen), you could not see whether agents were
working.

## Behaviour
- System and prompt sections are wrapped in named tags (`<identity>`, `<operating_rules>`,
  `<request>`, …). The tags are added after the budget fit, so truncation never breaks them, and
  empty sections get no tag. Order stays stable → volatile, so the prompt cache prefix is kept.
- Grounding becomes a numbered operating-rules list, each rule with its reason.
- Finish contract: end each turn with what changed, how it was verified, and what is blocked or
  next. Kept only if the live evals don't regress.
- Agent activity already showed in the tab bar on every view; it is now a busy badge (Secondary).
  Approval previews state what once / always here / everywhere / no each do.

## Acceptance
- [x] Tags balanced and ordered under a forced tight budget (test).
- [x] Offline evals pass.
- [x] Live `make eval` before/after recorded here (outcome, tokens, cache reads, cost, time).
- [x] At 72 columns, overflow drops hints, then `working`, then `setup`; the mode block and
      `waiting` always stay.

## Live evals (session 60)
`make eval` with claude 2.1.285, 8 fixtures, one run per arm (an anecdote, not a claim). DHI arm:

| Prompt | Passed | Cost | Output tokens | Attempts |
|---|---|---|---|---|
| main (3a5e55b), untagged | 8/8 | $2.009 | 12,698 | 9 |
| tags + numbered rules (579e4ac) | 8/8 | $1.981 | 11,997 | 9 |
| + finish contract (cff82fc) | 8/8 | $2.017 | 12,089 | 9 |

No regression in outcome; cost within one run's noise of the baseline, so the finish contract was
merged. The fixtures score outcome, not the final reply, so the contract's intended effect (honest
"checked / unchecked" reports) is not measured by these numbers. The first tags run hit the CLI's
session limit (every run failed with "You've hit your session limit"); it was rerun after the reset.
