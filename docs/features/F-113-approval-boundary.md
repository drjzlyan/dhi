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
- Without bwrap the adapter is built anyway and looks bwrap up at each spawn: a spawn names the
  install (`:install bwrap` in the editor raises the card), and once installed agents run without a
  restart. Boot shows a warning. There is no separate "paused" queue: the turn fails visibly.
- Tool homes: ~/.cargo and ~/.gradle are writable (cargo locks files in CARGO_HOME, Gradle keeps
  its daemon there) while credential files stay read- and write-denied; ~/.rustup and Apple's
  developer dirs (xcode-select's, Command Line Tools) are read+exec. Live on macOS: sandboxed
  `go build`, `cargo test --offline` and `gradle --offline build` pass.
- `:install <tool>` raises the install card for any host tool DHI knows.
- `cmd/dhi/writes_audit_test.go` lists every file that writes, with counts; a new write site fails
  until it is reviewed. The npm writability probe uses access(2) instead of a test file.
- Not done: write/exec grants reaching `run` (they reach the agent CLI's own sandbox); Linux ENOENT
  on unbound paths is not recognised (too ambiguous).
