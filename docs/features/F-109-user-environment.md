# F-109 — The user's environment, read once

Status: done (M36 P1) · ADR-0044

## Behaviour
- At startup `hostenv` runs the login shell (`$SHELL -ilc`, marker-delimited `env -0`, 3 s timeout)
  and caches the environment; the palette's "Reload shell environment" reads it again.
- Every child — terminal pane, language servers, tests, debugger, formatters, agents, `run`, MCP
  servers — starts from it. `hostenv.LookPath` resolves tools on its PATH.
- Fallback: DHI's own environment, with the reason in doctor.

## Acceptance
- [x] Parser: marker, NUL-separated pairs, noisy rc output before the marker, timeout fallback.
- [x] A tool only on the login PATH (e.g. under nvm) is found.

## Notes
- The probe runs in a new session (no controlling terminal) and kills its process group on timeout,
  with a short WaitDelay: a first version hung on a real zsh whose rc files left a background
  process holding the output pipe. Live on the user's zsh: 109 ms, Homebrew and ~/.local/bin found.
- `hostenv.Apply()` makes the resolved environment DHI's own at process start, so existing
  `os.Environ`/`exec.LookPath` callers (and doctor) see the user's PATH.
- Agents no longer get redirected build caches (BuildEnv/ToolEnv): they use the user's.
