# F-040: Editor essentials — format, outline, git gutter, tests

Status: complete (M23 part C; split panes, regex project replace and
terminal alt-screen deferred) · Companion to: F-002, F-009, F-026.

## Behaviour

- **Format on save + `:fmt`:** Go buffers with a live language server are
  formatted through `textDocument/formatting` before `:w`/`:wq` writes,
  as one undo group. A failing/timed-out server never blocks the save: the
  message says `written — saved unformatted (<reason>)`. `:set nofmt` /
  `:set fmt` toggle it. Server calls are bounded (3 s). `textbuf.Editor`
  gained a `SetBeforeSave` hook.
- **Outline / go to symbol (`:sym`):** `textDocument/documentSymbol`
  (hierarchical and flat shapes), a filterable picker, enter jumps.
- **Git gutter:** lines added/modified/deleted-below relative to HEAD are
  marked in the column between the line number and the text
  (`gitcore.Repo.HeadContent`, `internal/linediff` LCS with a size guard,
  recomputed only when the buffer version changes, HEAD snapshot dropped
  on commit). Untracked files and non-repos show no markers.
- **Tests (`:test`, `:test all`, `:test <pattern>`):** `internal/testrun`
  runs `go test -json` with the hermetic PATH (host PATH never consulted),
  parses pass/fail/skip and failure locations (including build errors);
  a green run is one status line, a red one opens a failures list where
  `enter` jumps to the failing line and `r` reruns. One run at a time,
  5-minute cap.

## Acceptance

- [x] LSP: formatting edits + null, documentSymbol both shapes
- [x] format-on-save applies edits (undo restores), failure still saves
      and says so, `:set nofmt`, `:fmt`, no-server messages, timeout
- [x] symbol picker filter/jump/esc/refusal
- [x] `HeadContent` (tracked, worktree ignored, untracked, no commits)
- [x] `linediff` marks table + size guard; gutter renders/caches/invalidates
- [x] testrun parser (counts, locations, build errors), `LookPath` env-only,
      stub-`go` runs; editor red/green/refusal/rerun flows

## Deferred

- Vertical split panes; regex/case/word project find-replace; editor
  settings & keybinding overrides; multi-cursor, folding, snippets, undo
  tree; command palette (M24).
- Terminal alt-screen/private modes in `internal/vt` (needed to run vim or
  a TUI debugger inside the drawer) — a cursor-addressed screen model is a
  rewrite of the line-based emulator, not an increment.
- Test debugging and `-race`/`-run` presets; coverage display.
