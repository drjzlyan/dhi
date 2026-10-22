# F-114 — Doctor, onboarding and docs for host tools

Status: planned (M36 P6) · ADR-0044

## Behaviour
- Doctor: shell environment (time, PATH entries), each tool with version or install command, the
  package manager, sandbox helper, git identity source, credential helper and ssh-agent (names only).
- Setup: a "tools" step replaces the download; identity follows F-112.
- README: no download; "What DHI changes on your machine".

## Acceptance
- [ ] Doctor sections present and tested.
