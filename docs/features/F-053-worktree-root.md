# F-053 — Configurable worktree location

Status: done (M26)

## Behaviour
- `[worktrees] root = "..."` in user or workspace `config.toml` (personal, never in the tracked
  conventions) moves linked git worktrees out of `.dhi/`. `""` keeps `.dhi/tasks/<slug>/<member>`
  and `.dhi/reviews/<id>/<member>` exactly as before.
- `~` expands against home; a relative path is taken from the workspace root. The base is
  namespaced `<root>/<workspace-name>-<hash of its path>/{tasks,reviews}/...` so workspaces
  sharing a root never collide.
- Boot (`internal/boot`) resolves and creates the base, adds it to the OS sandbox's writable
  roots, and blocks with the fix if it cannot (ADR-0011). The agent runtime adds it to the path
  jail (`runtime.Config.ExtraRoots`).
- Cards keep storing worktree paths relative to the workspace root (e.g. `../../fast/wt/...`),
  so every existing consumer works unchanged. Changing the setting does not move existing
  worktrees; they stay where their cards say.
- Settings: row `worktrees.root` cycles inside `.dhi/` / `../.dhi-worktrees` / `~/dhi-worktrees`
  (other values are hand-edited); takes effect after a restart.

## Acceptance
- [x] default layout identical to the old one; external layout stored relative and joins back
- [x] home / relative / messy paths; shared roots never collide; stable across runs
- [x] boot creates the base, allows it in the sandbox profile, blocks an unusable root with a fix
- [x] Settings row persists to config and says restart + existing worktrees stay
