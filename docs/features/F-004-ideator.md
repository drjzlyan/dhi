# F-004: Ideator surface — think with the crew, don't touch the code

Status: done (M6, ADR-0010) · Milestone: M6

## Summary

Dedicated ideation sessions: invite an agent or a team, discuss a design /
architecture / documentation topic in chat, and let agents produce artifacts.
The Ideator has **no editing capability** — it navigates, previews, and
approves what was ideated.

## Components

1. **Sessions** — named ideation sessions; each binds an invited agent set
   (from the Workspace roster), a chat thread, and an artifact folder.
2. **Artifact navigation** — read-only file tree of what was ideated
   (markdown design docs, diagrams, plans, decision records).
3. **Preview** — GitHub-style rendered view for supported formats (markdown
   first; diagrams via agent-produced SVG/mermaid-rendered assets later).
4. **Approval flow** — each artifact is draft → reviewed → approved/rejected;
   approval is human-only; agents can be asked to revise rejected artifacts
   via chat.
5. **Export** (post-M6) — approved artifacts can land in a repo path or be
   exported to issue trackers via MCP (e.g. JIRA).

## Non-goals

- Buffer editing lives only in Editor; Ideator never mutates repo files.

## Acceptance criteria

- Start a session, invite two agents, request alternatives; both respond in
  chat and produce artifacts visible in the tree.
- Preview renders markdown; approve/reject states persist per artifact.
- Rejection routes back to chat as revision instructions to the authoring agent.

## Delivery notes (M6)

- Store: `internal/ideation` — TOML card per session at
  `.dhi/sessions/<slug>.toml` (tasks/review blueprint), artifact files at
  `.dhi/sessions/<slug>/`, bus channel `#ideation-<slug>`. Artifact
  statuses persist per file with a content hash; a rewritten file flips
  back to draft (revision loop detection).
- Agent production: reserved `.dhi` vpath pseudo-member (ADR-0010) lets
  agents `write` artifacts under the session folder through the normal
  path-jail + manifest policies.
- Surface: `surfaces/ideator` — SESSIONS · ARTIFACTS · PREVIEW · CHAT
  (`[`/`]`), read-only; glamour markdown preview (memoized, shared with
  the editor's `internal/preview`); session chat with @mention dispatch
  through the narrow `crew` seam (same pattern as the reviewer).
- Review loop: `r` records rejection notes and posts a revision request
  mentioning the artifact's claimed author on the session channel;
  authorship is claimed automatically when agent chatter references
  artifact vpaths. Approve is terminal and human-only.
- Doctor: `sessions/store` suite (malformed cards, dangling invites).
