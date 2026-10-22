# F-112 — Git as the user has it

Status: done (M36 P4) · ADR-0044 · amends F-083

## Behaviour
- DHI's git is the user's git with their environment: config, includeIf, hooks, ssh, credential
  helpers. Background operations set `GIT_TERMINAL_PROMPT=0` only.
- Clone, fetch and push use the git CLI.
- Identity: read from the user's config. Missing → a card: set it globally (default; the flash names
  ~/.gitconfig) or only in this workspace's repos (each member's `.git/config`, remembered for new
  members).
- Writes into repos stay inside the workspace (the boundary, ADR-0044) and are local-only: see Notes.

## Acceptance
- [x] Runner env has no GIT_CONFIG_GLOBAL/NOSYSTEM (only `GIT_TERMINAL_PROMPT=0` is added).
- [x] Clone, push, fetch and CloneAt through the CLI, honouring the user's config (an `insteadOf`
      rule in the test; go-git would ignore it).
- [x] Identity global vs workspace-only writes (wizard and Settings: enter / w).

## Notes
- git runs in its own session (no controlling terminal): ssh cannot draw a passphrase prompt over
  the TUI; a key not in the agent fails visibly (or uses SSH_ASKPASS).
- DHI no longer reads `gh auth token` for pushes; the user's credential helper or ssh key is used.
- Workspace-only identity is remembered in the workspace's setup state; a safe repair gives members
  added later the same identity.
- Deviation from the plan, for the user to decide: the loose copy of a packed default-branch ref is
  still written, but only under bubblewrap (which guards only existing files; seatbelt denies by
  path, so macOS writes nothing). The one cursor line in `.git/info/exclude` is still appended
  without a card. Both are local-only, never committed, inside the workspace's repos.
