# F-110 — Host tools and package-manager installs

Status: done (M36 P2; the Settings tools list moved to P6 with the setup "tools" step) · ADR-0044

## Behaviour
- `hosttools` knows the tools DHI uses (git, rg, gh, go, node, uv, language servers, dlv, coding
  CLIs), how to detect them, and how each package manager installs them.
- A missing tool raises a card: the exact command, `y` runs it in a terminal tab, `c` copies it.
  After it exits DHI re-reads the environment and the capability retries.
- No package manager, or a vendor-script tool: the command and a doc link only.
- Settings → SETUP lists tools, versions and Install buttons (P6, with the setup "tools" step).

## Acceptance
- [x] Recipes per manager (brew, apt, dnf, pacman, zypper; npm/go/uv/rustup).
- [x] Each consumer (LSP, search, tests, debugger, CLIs, gh) finds host tools and offers installs.

## Notes
- The editor's card runs the command in a drawer tab (`term.Options.Command`); the session now
  reaps its process and keeps the exit status, so the outcome is "installed", "failed (exit N)"
  or "finished but still not on your PATH". A miss re-reads the shell env off the UI loop.
- The setup wizard runs the command in the user's own terminal (`tea.ExecProcess`).
- `Find` also looks in `$GOBIN`/`$GOPATH/bin`, `~/.cargo/bin`, `~/.local/bin` (like VS Code's Go
  extension): `go install` succeeds without those being on PATH.
- npm is offered only when its global prefix is writable (a distro's root-owned `/usr` would fail).
- typescript stays on 5.x in the npm recipe: TypeScript 7 ships no tsserver.
- Linux live check: run `apt-get update` in a fresh Debian image first, or apt finds no packages.
