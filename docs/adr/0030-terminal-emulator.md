# ADR-0030: A full VT emulator for the terminal drawer

Date: 2026-10-09 · Status: accepted · Supersedes the "not a full terminal"
scope of F-026 P5. Serves: F-061.

## Context

The drawer's screen (`internal/vt`) was a scrollback viewer. It passed
colours through and handled cursor moves, but ignored alternate screens,
scroll regions and private modes. vim, htop, less and git's pager could
not run in it, and terminal queries (cursor position reports) went
unanswered, so some programs hung waiting for them. Its tracking
(F-010/M23) deferred "terminal alt-screen". Writing a complete xterm
emulator in-house is a large, error-prone job.

## Decision

1. **Use `github.com/charmbracelet/x/vt`** (pure Go, MIT, from the same
   project as Bubble Tea and Lip Gloss, which DHI already depends on; it
   shares their `ultraviolet` cell model). `internal/vt` stays the seam:
   `Screen` wraps a `SafeEmulator`, so only that package imports it.
2. **The emulator and the PTY always share one size.** It is the drawer
   pane, or the whole body while a program holds the alternate screen
   (the drawer goes full screen and comes back when the program exits).
3. **Replies are wired back.** What the emulator answers to the
   program's queries is copied to the session's input.
4. **Keys** use the xterm encoding: every ctrl-letter, alt as an ESC
   prefix, navigation, delete/insert and F1–F12. A focused terminal
   owns `ctrl+c`; `ctrl+q` always quits DHI (`surfaces.CtrlCTaker`).

## Consequences

- The module has no tagged release; it is pinned to a pseudo-version, and
  `go.sum` records its hash. A Dependabot bump goes through the same CI,
  including `internal/vt`'s tests (alt screen, query replies, the box
  contract).
- Pure Go, no cgo, no system terminfo: ADR-0005 (hermetic) holds.
- Scrollback above the visible screen is kept by the emulator but not yet
  scrollable in the drawer (unchanged from before).
