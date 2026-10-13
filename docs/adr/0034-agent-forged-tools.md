# ADR-0034: Agents forge tools; a human approves each version

Date: 2026-10-15 · Status: accepted · Companion to: ADR-0019 §4 (no
free-form shell; an allowlisted `run`), ADR-0022 (third-party content
trust), ADR-0032 (autonomy envelope). Serves: F-069 (M29 P5).

## Context

An agent that keeps running the same commands pays for them in tokens
every time, and so does every other agent. A reusable tool turns that
repetition into one call. But agent-written code that other agents then
run is a new trust category. It is neither DHI's own code nor a vetted
third-party pack (ADR-0022).

## Decision

1. **Two forms only.**
   - A **recipe** is up to 8 steps, each an argv on the `run` allowlist,
     with `{{input}}` templates. There is no shell. A template never
     splits an argument, and an argument that templates an omitted
     optional input is dropped.
   - A **script** has a `#!` line and runs through the same sandboxed
     runner as `skill_run`, with no network.
2. **Bounded scope.** An agent-made tool declares read, write, exec or
   git. A script is exec. Push, network and admin are refused at
   proposal. Calls are gated at the declared scope, and inside a task
   worktree the autonomy envelope covers them like `run`.
3. **Nothing runs unapproved.**
   - `tool_propose` validates the definition and parks it in
     `.dhi/tools/pending/`, then `@you`-mentions the human (it lands in
     the Inbox).
   - Settings → PROPOSALS shows the full definition before `y` approves
     it.
   - An edit is a new version that is approved again. The previous
     version stays active meanwhile.
   - `x` archives a tool.
   - Approval provenance (who and when) and usage counts are recorded.
4. **Approved tools are workspace tools.** They are served to every
   agent as `custom__<name>`, no allowlist entry needed. Recipe steps are
   re-checked against the allowlist at every call, so a later allowlist
   change also binds old tools.
5. **Repetition is detected locally.** A pure detector over each agent's
   last 20 run transcripts finds command sequences (1–3 commands) that
   recur in 3 or more runs. The prompt then nudges the agent to propose
   a tool. Sequences an active or pending tool already covers are not
   nudged.

## Consequences

- Recipes can only do what `run` can do, so the forge adds reuse, not
  power. Scripts add power and therefore always need exec scope and an
  approval.
- Tool files are plain TOML under `.dhi/tools/`, diffable and
  hand-editable. A hand edit still has to pass validation when loaded and
  approved.
