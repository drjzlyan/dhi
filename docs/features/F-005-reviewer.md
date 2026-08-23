# F-005: Reviewer surface — GitHub-grade review with agent crew

Status: planned · Milestone: M5

## Summary

Review any PR (rendered as its own worktree) or any local worktree/branch in
a GitHub-diff-style screen with inline line comments — and invite workspace
agents into the review, per line or in the reviewer chat.

## Components

1. **Inputs** — pick a PR (`gh`-backed) or a worktree/branch; DHI always
   creates a dedicated **review worktree** so reviewing never dirties your
   working copies.
2. **Diff UI** — file list + unified/side-by-side diffs; hunk navigation;
   syntax highlighting; expand-context; viewed/unviewed marks per file.
3. **Comments** — comment on any line or hunk; threads with resolve state;
   pending-review batching before submission.
4. **Agent participation** — @invite an agent on a specific line/thread ("why
   this locking approach?") or ask for a **complete agent review** of some/all
   files while you review the rest; agent comments appear as normal review
   threads authored by that agent.
5. **Completion actions**
   - PR: post approved comments to the PR (via `gh`), including agent-authored ones marked with attribution.
   - Worktree: dispatch a fixing agent on the review findings, or hand off to
     Editor ("open in editor") for the human to act.

## Acceptance criteria

- Open PR-42 → review worktree auto-created; diff navigable; comment on a
  line; invite agent on that thread; agent reply lands in-thread (MockProvider test).
- Complete review on a worktree → "dispatch fixer" spawns agent task bound to
  the same worktree; "open in editor" jumps to Editor with files loaded.
- All flows covered by scripted-key e2e tests against fixture repos.
