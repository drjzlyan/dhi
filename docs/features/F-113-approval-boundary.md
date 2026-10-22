# F-113 — The approval boundary for agents

Status: planned (M36 P5) · ADR-0044 · amends F-086, F-087

## Behaviour
- Writable by default: workspace members and worktrees, `.dhi/tmp/<agent>`, the CLI's state dir,
  standard package caches. Executable: system dirs and every dir on the user's PATH.
- Any other write raises a card; file-level grants for files in $HOME; exec denials ask for exec;
  Linux denials are recognised. network/exec grants take effect.
- `run` and verify installs are always sandboxed.
- `git_push`: a push card; on yes DHI pushes with the user's git. Agents never read credentials.
- A missing sandbox helper pauses agents behind an install card; the IDE works.

## Acceptance
- [ ] Cards for $HOME files, exec and Linux denials; network grant takes effect.
- [ ] `run` with network is wrapped.
- [ ] git_push card → push to a local bare remote.
- [ ] Boot without bwrap is not blocked.
