# F-111 — No DHI toolchain

Status: done (M36 P3) · ADR-0044 · supersedes F-060, the toolchain parts of F-011/F-044/F-046/F-050

## Behaviour
- No downloads, shims, lockfile, bootstrap screen or relaunch-on-tools. Boot blocks only on a broken
  workspace or settings file.
- A named repair offers to delete an old `~/.local/share/dhi/toolchain` (size shown).

## Acceptance
- [x] Launch with no toolchain folder opens the editor directly (live, tmux).
- [x] The repair removes the folder only on yes (shown live with an 817 MB folder; not applied).

## Notes
- The boot gate is the blocked screen only; `internal/toolchain`, the bootstrap surface and the
  relaunch-on-tools gate are deleted, with the pin scripts and the pin/release-git workflows.
- The git runner is the user's git with their environment (`gitcore.ResolveRunner(look, env)`, plus
  `GIT_TERMINAL_PROMPT=0`), done here because the toolchain runner went away; the rest of F-112
  (CLI network operations, identity choice) stays in P4.
- Verify's optional toolchains (Rust, JDK, Maven, Gradle, .NET) are no longer downloaded: a missing
  one leaves the work unverified, named, with the install command.
- Bridged MCP servers keep a private HOME, now `.dhi/mcp-home/` (gitignored).
- Clipboard and browser seams moved to `internal/hosttools/os.go`.
