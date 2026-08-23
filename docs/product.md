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

## Personas

| Persona | Needs |
|---|---|
| **The nvim veteran** | modal editing that feels native; zero mouse dependency; fast TUI; no IDE lock-in |
| **The polyrepo maintainer** | 3–15 services in different folders that ship together; unified nav/search/branches |
| **The crew lead** | assign tasks to named agents; see progress; review their output with real diff tooling |
| **The reviewer** | worktree-isolated review of any PR or feature branch; route questions to an agent mid-review |

## Core journeys (drive milestone scope)

1. **Pair program** — open editor + chat pane; agent sees current buffer/diff;
   suggestions apply directly into the buffer. *(M3)*
2. **Delegate** — post a task to `#team-backend`; planner agent splits it;
   workers claim subtasks in isolated worktrees; lead watches threads. *(M4)*
3. **Review** — request review of PR-42 or current feature worktree; DHI spins
   up a review worktree; hunk-level comments; "ask agent" on any thread. *(M5)*
4. **Ideate** — canvas pane with markdown preview; diagram-skilled and
   planning-skilled agents contribute artifacts; export to issues later. *(M5)*
5. **Install expertise** — marketplace pack = skills + MCP servers + knowledge;
   installed agents appear in roster and teams. *(M6)*

## Product principles

- **Worktree-first:** features, tasks, and reviews are isolated by default;
  merging back is an explicit act.
- **Human-in-the-loop always:** agents propose; humans approve destructive or
  shared-boundary actions (knowledge contributions default to review mode).
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
