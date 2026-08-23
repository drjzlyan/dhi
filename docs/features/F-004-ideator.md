# F-004: Ideator surface — think with the crew, don't touch the code

Status: planned · Milestone: M6

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
