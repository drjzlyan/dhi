# F-033: Ideation round-table & canvas

Status: planned (M18) · Companion to: ADR-0019 (DHI owns the loop),
F-004 (ideator), F-027 (roles/skills). Product rule: ideation is a
session you are *in*, not a transcript you read afterwards.

## Summary

Today the Ideator is one shared channel per session with a flat artifact
list: a single conversation, no turn-taking, no diagrams. F-033 makes it
a round-table — a moderator, invited participants, 1:1 and group and
breakout modes — over a shared canvas of markdown and mermaid artifacts
that the user and agents edit live.

## Part A — session & round-table

- A session = `{moderator, participants[], mode (1:1|group|breakout),
  topic, artifacts[], parent}`. The **user** opens sessions and
  breakouts; an agent may *propose* one (a visible request), never open
  it.
- The moderator grants the floor; a speaker holds it for one turn. When
  addressed, any participant may respond; agents may address each other
  directly (not only `@mention`). The session channel is the live
  transcript.
- Turn order and floor state are recorded, so a session replays.

## Part B — canvas

- Artifacts are markdown or **mermaid** documents under
  `.dhi/sessions/<slug>/`, editable by the user and by agents through
  `artifact_*` tools (ADR-0019), rendered live in preview.
- Artifact status stays draft → reviewed → approved/rejected, content-
  hash keyed (a fresh revision flips back to draft). A structured
  node/edge diagram editor is the follow-on.

## Part C — surface

- IDEATOR: SESSIONS · PARTICIPANTS · CANVAS · TRANSCRIPT, with a floor
  indicator and a moderator control. Breakouts appear nested under their
  parent session.

## Acceptance criteria

- [ ] A group session with a moderator runs a multi-agent exchange where
      agents address each other; the transcript is ordered and replays.
- [ ] Agents can create/edit a mermaid artifact; preview renders it live;
      status resets to draft on content change.
- [ ] An agent cannot open a session or breakout — only propose one.
- [ ] A 1:1 mode and a group mode both work; a breakout nests under its
      parent.
- [ ] `make verify` green per phase.

## Deferred

- Structured diagram editor (nodes/edges) beyond mermaid.
- Voice/video (out of scope for a terminal-native product).
- Non-ideator (repo-mutating) artifacts.
