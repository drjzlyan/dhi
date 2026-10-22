# F-109 — The user's environment, read once

Status: planned (M36 P1) · ADR-0044

## Behaviour
- At startup `hostenv` runs the login shell (`$SHELL -ilc`, marker-delimited `env -0`, 3 s timeout)
  and caches the environment; the palette's "Reload shell environment" reads it again.
- Every child — terminal pane, language servers, tests, debugger, formatters, agents, `run`, MCP
  servers — starts from it. `hostenv.LookPath` resolves tools on its PATH.
- Fallback: DHI's own environment, with the reason in doctor.

## Acceptance
- [ ] Parser: marker, NUL-separated pairs, noisy rc output before the marker, timeout fallback.
- [ ] A tool only on the login PATH (e.g. under nvm) is found.
