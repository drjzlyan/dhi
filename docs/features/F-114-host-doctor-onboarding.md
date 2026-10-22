# F-114 — Doctor, onboarding and docs for host tools

Status: done (M36 P6) · ADR-0044

## Behaviour
- Doctor: shell environment (time, PATH entries), each tool with version or install command, the
  package manager, sandbox helper, git identity source, credential helper and ssh-agent (names only).
- Setup: a "tools" step replaces the download; identity follows F-112.
- README: no download; "What DHI changes on your machine".

## Acceptance
- [x] Doctor sections present and tested (live output reviewed on macOS).

## Notes
- Setup "tools" lists git, rg, gh (bwrap on Linux) as core plus language servers and dlv, with the
  path where found; applies (and is a catch-up step) only while a core tool is missing.
- Palette "Reload shell environment" re-reads the login shell (F-109).
- Linux doctor output not reviewed live (no Docker daemon in this session).
