# F-069 — Tool forge: agent-built tools, approved by the human

Status: done (M29 P5) · ADR-0034

## Problem
Agents repeated the same command sequences run after run, paying for
them every time. They had no way to turn that into a reusable tool, and
there was nowhere for a human to vet one.

## Behaviour
- **`internal/agentkit/forge`** defines tools.
  - A tool is either a **recipe** (≤ 8 allowlisted argv steps with typed
    `{{input}}` templates, no shell) or a **script** (`#!`, exec scope,
    sandboxed runner).
  - Scope is read, write, exec or git; push, network and admin are
    refused.
  - Inputs are string, integer or boolean, validated strictly per call.
  - The store holds pending, active and archived tools, versions, and
    approval provenance, plus usage counts.
- **`tool_propose`** is a harness tool. It validates, parks the tool as
  the next version, and `@you`-mentions the human.
- **`custom__<name>`** is how every approved tool is served to every
  agent, at its declared scope.
  - Recipe steps are re-checked against the allowlist on every call.
  - Inside a task worktree, the envelope covers the tool like `run`.
- **Repetition hint.** The detector reads the agent's last 20
  transcripts. A sequence recurring in 3 or more runs, and not covered by
  an active or pending tool, earns a `tool_propose` nudge in the prompt.
- **Settings → PROPOSALS.** Pending items are listed first; `v` shows the
  full definition and why, `y` approves, `n` rejects. Forged tools follow
  with their scope, version and call count; `x` archives.

## Acceptance
- [x] Validation refuses: bad name, no description, a dangerous scope,
      both bodies or neither, undeclared or templated programs,
      non-allowlisted steps, bad input types, and non-exec scripts.
- [x] Arguments are typed. Templating never splits or shell-evaluates an
      argument, and an omitted optional input drops its argument.
- [x] Store lifecycle: propose (v1), approve, edit → v2 pending with v1
      still active, reject, archive, script made executable, usage
      counted.
- [x] Served path: pending is not served; approved is served without an
      allowlist entry, asks at its scope, runs the expanded argv in
      Workdir without network, and refuses bad arguments before running.
      Unsafe proposals are refused.
- [x] Detector: repeated triples and pairs are found with normalised
      commands, ignoring trivial ones. The prompt hint is silenced by a
      covering tool.
- [x] PROPOSALS: golden + integrity sweep, view, approve, reject and
      archive.
- [x] `make verify` and lint are green.
