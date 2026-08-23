# STATE — current position

Updated: 2026-08-23 (session: M2 — PTY terminal drawer)

## Where we are

Multi-buffer tabs committed (`b2a61ed`). **This session (uncommitted —
commit next): PTY terminal drawer.** `make verify` green; 16 packages
under `-race` (creack/pty added as the only new dep — pure Go syscalls).

## Just finished

- `internal/term`: PTY session service — Start(dir,label,env,argv) with
  user-shell default ($SHELL → bash → sh), streamed output channel,
  Write/Resize(SIGWINCH)/Close(idempotent), ctx-cancel kills.
- Editor drawer: ctrl+t cycle = closed→open+focus→blurred(kept
  alive)→closed. One lazily-created cwd-pinned tab per member repo on
  first open; alt+1..9 switches; alt+n spawns an extra tab in the same
  dir. While focused every key goes to the pty via termKeyBytes
  (enter=\r, backspace=0x7f, arrows=CSI, ctrl+c/d/l/u).
- Scrollback MVP: ANSI-stripped tail (1000-line cap), partial-line
  buffer, [process exited] marker. Full VT emulation deferred to M7.
- cmd/dhi passes Manager.Env(nil) via WithTermEnv so drawer children get
  DHI's shim PATH only.
- Live test drives a real shell through the drawer: typed echo reaches
  pty, output lands in scrollback ("dhi-live-42").

## Gotchas learned (do not re-learn these)

1. Channels consumed by listen cmds must be created in New(), NOT Init()
   — unit tests never run Init, and pump goroutines silently block on a
   nil channel forever (output vanished; tests hung on poll timeouts).
2. Headless tests need m.drainTerm() inside poll loops: listenTerm cmds
   are only scheduled by a real Bubble Tea program loop.
3. pty echo means the terminal shows its own input — no local echo in
   the drawer; don't double-render keystrokes.
4. Carried: multi-start fuzzy scoring; undo groups; sorted member order.

## Next up (rest of M2)

1. Commit this session (user approves diff first).
2. Markdown preview (GitHub-style rendering; goldmark candidate dep),
   then git view over go-git (status/stage/commit/log/worktrees), LSP
   foundation, settings skeleton.

## Open questions for user

- None blocking.
