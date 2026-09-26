# DHI — Product Overview

## Vision

Software is increasingly built by *crews*: a human directing, and agents
researching, implementing, reviewing. Today those crews live in browser tabs
and ad-hoc CLIs, disconnected from the editor where the work actually happens.

DHI puts the whole crew inside one terminal-native IDE. The human keeps every
existing workflow (modal editing, git, terminals) and gains persistent,
structured teammates: agents with skills, memory, and shared knowledge who can
be chatted with, assigned tasks, asked for reviews, and watched as they work —
across one repo or twenty microservices at once.

DHI is the **host**; a detected host CLI is the **engine** (chosen globally or
per agent). Every agent works through DHI's declared IDE tools — the same
editor, git, board, channels, and ideation the human uses — under declared
capability scopes, feature workflows, and approvals. There is no free-form
shell and no unapproved mutation: the crew works *inside* the IDE, and every
step is visible and reversible. Because a host CLI keeps its own native tools,
this containment is best-effort; an in-house API engine is the recorded path to
a hard guarantee.

## Personas

| Persona | Needs |
|---|---|
| **The nvim veteran** | modal editing that feels native; zero mouse dependency; fast TUI; no IDE lock-in |
| **The polyrepo maintainer** | 3–15 services in different folders that ship together; unified nav/search/branches |
| **The crew lead** | assign tasks to named agents; see progress; review their output with real diff tooling |
| **The reviewer** | worktree-isolated review of any PR or feature branch; route questions to an agent mid-review |

## Interface — five views

```
1 Workspace (boot) › 2 Editor › 3 Ideator › 4 Reviewer › 5 Settings
```

| View | Role | Spec |
|---|---|---|
| **Workspace** | the company of agents: channels, org/teams, tasks, inspection, marketplace; boots first under the brand hero | F-003 |
| **Editor** | traditional IDE core: file nav, modal buffers, repo-tabbed terminal, git view, chat sidebar, preview, LSP | F-002 |
| **Ideator** | think-with-the-crew sessions; artifact nav + preview + approve; no editing | F-004 |
| **Reviewer** | GitHub-style PR/worktree review with line comments and agent participation | F-005 |
| **Settings** | everything configurable across all views | F-006 |

## Core journeys (drive milestone scope)

1. **Pair program** — open editor + chat pane; agent sees current buffer/diff;
   edits apply through the same undo-group path the human uses. *(M3, M20)*
2. **Delegate** — post a task to `#team-backend`; planner agent splits it;
   workers claim subtasks in isolated worktrees; lead watches live progress in
   the thread. *(M4, M20)*
3. **Work a feature** — a workflow creates the worktree, the agent implements
   test-first, tests must pass before a PR, review gates the merge. *(M16)*
4. **Review** — request review of PR-42 or current feature worktree; DHI spins
   up a review worktree; hunk-level comments; "ask agent" on any thread. *(M5)*
5. **Ideate** — a moderated round-table (1:1, group, breakout) over a shared
   canvas of markdown/mermaid artifacts the human and agents edit live. *(M18)*
6. **Cross projects** — one task changes several member repos; a declared
   dependency edge turns a change in A into a proposed change in B. *(M17)*
7. **Install expertise** — a pack ships agents, roles, skills, standards,
   workflows, and MCP servers from a signed registry; MCP servers run sandboxed
   under scope. *(M19)*

## Product principles

- **Tools over shell:** agents work through declared IDE tools, never a
  free-form shell; network is deny-by-default and every mutation is approved.
  Containment is best-effort while a host CLI is the engine — stated, not hidden.
- **Worktree-first:** features, tasks, and reviews are isolated by default;
  merging back is an explicit act.
- **Human-in-the-loop always:** agents propose; humans approve destructive or
  shared-boundary actions (knowledge contributions default to review mode).
- **Declared authority:** an agent's scopes (`read/write/exec/network/git/
  push/admin`) are configured, visible, and auditable; nothing is ambient.
- **How we work is configurable:** standards say what good code is; workflows
  say how a feature is done here — both shared, layered, and enforced at DHI's
  seams.
- **Terminal-native performance:** everything keyboard-driven; no hidden web views.
- **Deterministic where it counts:** reproducible toolchain, replayable test scenarios.

## Multi-repo workspace

A workspace = 1..N member repos registered in `.dhi/workspace.toml`
(relative paths, short keys). Files addressable as `<repo>:<path>`;
nav/search/terminals/agents span all members; cross-repo changes group into a
ChangeSet of per-repo worktrees. Opening a bare repo degrades to a singleton
workspace automatically.

## Non-goals

- Replacing neovim for pure text-editing power users who want it standalone.
- GUI/electron distribution.
- Being an LLM gateway for non-development use.
