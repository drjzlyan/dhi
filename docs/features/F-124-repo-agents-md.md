# F-124 — Carry the AGENTS.md of repos the CLI does not load

Status: done · gap G-A2

DHI runs a CLI with its cwd at the workspace root, or at the first changeset's worktree on a task
turn. Each CLI loads instruction files from its cwd only. A workspace with several member repos
therefore left every other member's AGENTS.md unread. DHI now puts those files in the system prompt.

## What the canary showed (decides the design)
A canary word in `AGENTS.md`, and separately in `CLAUDE.md`, of an empty git dir; the prompt asked
for the word with tools off. Plain headless runs, no permission flags.

| CLI | version | AGENTS.md | CLAUDE.md |
|---|---|---|---|
| claude | 2.1.285 | read | read |
| codex | 0.162.1 | read | not read |
| opencode | 1.19.0 | read | read |
| copilot | 1.0.88 | read | read |
| cursor | 2026.10.12 | read | read |
| antigravity (`agy`) | 1.4.12 | read | not read |

So the CLI already sees AGENTS.md in its cwd. The gap that remains is every other member.

## Behaviour
- `standards.RepoInstructions(members, roots, cwd, cliReads)` renders each member's `AGENTS.md`
  as `<member>/AGENTS.md:` blocks, sorted by member. Missing, empty and non-regular files are
  skipped. Symlinks are followed (AGENTS.md → CLAUDE.md is common).
- `clirun.CLI.ReadsAgentsMD` marks a canary-verified adapter. For those, the member whose
  directory is the cwd is left out so no file shows twice. Adapters without the flag
  get every member's file.
- On a task-bound turn a member's directory is its worktree (`memberRoots`), so the branch's copy
  is the one carried.
- Section `repo-instructions`, tag `<repo_instructions>`, priority 2, floor 600, after standards.
  Caps: 6000 runes per file, 16000 in all; a cut file names its path.
- Not fenced as untrusted data (ADR-0046): AGENTS.md is instructions by design. Whether to trust an
  unknown repo's instructions at all is G-S1 (restricted mode).
- Settings → standards preview shows the same block (chat turn at the root, CLI reads its own).
- `dhi doctor` row `runtime/<cli>/agents-md` says which route applies.

## Decision: no suppression
The CLI's own copy is not switched off. It is the same file, so there is nothing to dedupe against
beyond the cwd rule above, and suppressing would also silence the user's own global instruction
files (ADR-0044: the user's system is read-only). Reopen only with a reason and a canary-verified flag.

## Acceptance
- A member's AGENTS.md reaches the system block when the CLI's cwd is elsewhere. ✔ `TestPromptCarriesMemberAgentsMD`
- The cwd member is skipped for a verified CLI and carried for an unverified one. ✔ `standards` tests
- The worktree copy wins on a task turn. ✔
- Caps and truncation. ✔
- Live `make eval` (F-099): not run. Existing fixtures are single-repo and the new section is empty
  there, so their prompts are unchanged.
