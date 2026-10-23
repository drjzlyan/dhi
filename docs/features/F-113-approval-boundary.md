# F-113 — The approval boundary for agents

Status: done (M36 P5) · ADR-0044 · amends F-086, F-087

## Behaviour
- Writable by default: workspace members and worktrees, `.dhi/tmp/<agent>`, the CLI's state dir,
  standard package caches. Executable: system dirs and every dir on the user's PATH.
- Any other write raises a card; file-level grants for files in $HOME; exec denials ask for exec;
  Linux denials are recognised. network/exec grants take effect.
- `run` and verify installs are always sandboxed.
- `git_push`: a push card; on yes DHI pushes with the user's git. Agents never read credentials.
- A missing sandbox helper pauses agents behind an install card; the IDE works.

## Acceptance
- [x] Cards for $HOME files, exec and Linux (EROFS) denials; network grant takes effect.
- [x] `run` with network is wrapped (verify's dependency installs use the same runner).
- [x] git_push card ("push task/x to origin (url)") → push to a local bare remote.
- [x] Boot without bwrap is not blocked.

## Notes
- Exec roots are the PATH folders widened to their prefix when named `bin` (Homebrew's
  /opt/homebrew, nvm's versions/node/vX), never home or /. More credential files are fenced:
  ~/.config/gh, ~/.git-credentials, ~/.docker/config.json, ~/.cargo/credentials.toml,
  ~/.local/share/keyrings.
- Without bwrap the adapter is built anyway and looks bwrap up at each spawn. A turn that needs it
  waits: the thread says why, the editor's install card opens (a toast points to it, nothing is
  interrupted), and the turn continues once bwrap is installed — no restart. Boot shows a warning.
- A standard cache that does not exist yet (Linux ~/.cache, ~/go, ~/.npm) is pointed into
  `.dhi/cache` for agents and `run` (XDG_CACHE_HOME, GOMODCACHE, npm_config_cache), so a sandboxed
  build works without DHI creating folders in your home.
- Tool homes: ~/.cargo and ~/.gradle are writable (cargo locks files in CARGO_HOME, Gradle keeps
  its daemon there) while credential files stay read- and write-denied; ~/.rustup and Apple's
  developer dirs (xcode-select's, Command Line Tools) are read+exec. Live on macOS: sandboxed
  `go build`, `cargo test --offline` and `gradle --offline build` pass.
- `:install <tool>` raises the install card for any host tool DHI knows.
- `cmd/dhi/writes_audit_test.go` lists every file that writes, with counts; a new write site fails
  until it is reviewed. The npm writability probe uses access(2) instead of a test file.
- Write/exec grants reach `run` and `skill_run` too (`dhitools.GrantRunner`, a per-agent runner).
- Agent spawns and `run` read the user's environment per call, with the current PATH's folders as
  exec roots, so "Reload shell environment" reaches them without a restart.
- bubblewrap's own root is remounted read-only after the binds: before, a write to ~/.bashrc
  "succeeded" into a throwaway tmpfs file (live, Debian). Now it fails with EROFS and raises the
  file card; a granted file stays writable (checked with bwrap directly).
- Live on Linux (Debian bookworm arm64, Docker, root): without bwrap boot is not blocked, doctor and
  each spawn name `apt-get install -y bubblewrap` (no sudo as root); `:install bwrap` showed the card
  and ran apt in a terminal tab (exit 0, "bwrap is installed"); then workspace and ~/.npm writes
  worked, ~/.bashrc gave the card, ~/.ssh was not visible, git ran.
- Not recognised: Linux ENOENT on unbound paths (too ambiguous).
