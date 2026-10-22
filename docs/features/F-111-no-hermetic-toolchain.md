# F-111 — No DHI toolchain

Status: planned (M36 P3) · ADR-0044 · supersedes F-060, the toolchain parts of F-011/F-044/F-046/F-050

## Behaviour
- No downloads, shims, lockfile, bootstrap screen or relaunch-on-tools. Boot blocks only on a broken
  workspace or settings file.
- A named repair offers to delete an old `~/.local/share/dhi/toolchain` (size shown).

## Acceptance
- [ ] Launch with no toolchain folder opens the editor directly.
- [ ] The repair removes the folder only on yes.
