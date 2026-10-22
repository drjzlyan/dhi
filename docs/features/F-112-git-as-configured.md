# F-112 — Git as the user has it

Status: planned (M36 P4) · ADR-0044 · amends F-083

## Behaviour
- DHI's git is the user's git with their environment: config, includeIf, hooks, ssh, credential
  helpers. Background operations set `GIT_TERMINAL_PROMPT=0` only.
- Clone, fetch and push use the git CLI.
- Identity: read from the user's config. Missing → a card: set it globally (default; the flash names
  ~/.gitconfig) or only in this workspace's repos (each member's `.git/config`, remembered for new
  members).
- No silent writes into repos: no `.git/info/exclude` appends; per-turn MCP files live in `.dhi/tmp`.

## Acceptance
- [ ] Runner env has no GIT_CONFIG_GLOBAL/NOSYSTEM.
- [ ] Push to an SSH-style and a file:// remote through the CLI.
- [ ] Identity global vs workspace-only writes.
