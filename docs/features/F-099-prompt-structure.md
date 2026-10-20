# F-099 — Tagged prompt sections, operating rules, and agent visibility

Status: planned (M34 P6) · ADR-0041

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
- A status pill `● N working` (Secondary) appears next to `◆ N waiting`; clicking it opens the
  agent's thread. Approval cards state the consequence of each choice.

## Acceptance
- [ ] Tags balanced and ordered under a forced tight budget (test).
- [ ] Offline evals pass.
- [ ] Live `make eval` before/after recorded here (outcome, tokens, cache reads, cost, time).
- [ ] At 72 columns, overflow drops hints, then `working`, then `setup`; the mode block and
      `waiting` always stay.
