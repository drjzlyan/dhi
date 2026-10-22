# F-110 — Host tools and package-manager installs

Status: planned (M36 P2) · ADR-0044

## Behaviour
- `hosttools` knows the tools DHI uses (git, rg, gh, go, node, uv, language servers, dlv, coding
  CLIs), how to detect them, and how each package manager installs them.
- A missing tool raises a card: the exact command, `y` runs it in a terminal tab, `c` copies it.
  After it exits DHI re-reads the environment and the capability retries.
- No package manager, or a vendor-script tool: the command and a doc link only.
- Settings → SETUP lists tools, versions and Install buttons.

## Acceptance
- [ ] Recipes per manager (brew, apt, dnf, pacman, zypper; npm/go/uv/rustup).
- [ ] Each consumer (LSP, search, tests, debugger, CLIs, gh) finds host tools and offers installs.
