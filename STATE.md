# STATE — current position

Updated: 2026-08-23 (session: M2 — modal buffer MVP)

## Where we are

Ripgrep search committed (`1ae5ece`). **This session (uncommitted —
commit next): modal editing core + surface integration.** `make verify`
green; 15 packages under `-race`.

## Just finished

- `internal/textbuf` (TUI-free editing core, ~full unit coverage):
  - Buffer: lines storage, rune-offset columns, sticky wantCol,
    operation-level undo/redo with **undo groups** (one insert session =
    one `u`), dirty tracking, Open/Save (trailing-newline convention).
  - Motions: h j k l 0 $ w b e gg G (+line wrap on w like vim), counts.
  - Modal Editor: normal/insert/visual/command; operators d/c/y over
    motions AND doubled forms (dd/cc/yy ×count); cw/cb ≡ ce special case;
    x X D C J o O i a A I v p P u ctrl+r; unnamed register linewise-aware;
    commands :w :q :q! :wq :x :e with dirty-refusal messages.
  - EOF-sentinel ranges (z.Line == LineCount()) handled in yank/delete.
- Editor surface: opening a file loads a buffer, main pane renders a
  scrolled viewport with line-number gutter, inverted cursor block,
  visual-selection highlight, mode chip + dirty dot in the panel title,
  and a command/message line. Focus model: open→buffer focus; esc in
  NORMAL → tree focus; `/`+`s` tree-only. :w/:wq write through to disk;
  :q refuses when dirty.

## Gotchas learned (do not re-learn these)

1. withCursor must slice r[col+1:] for the tail — r[col:] duplicated the
   cursor rune into the render (goldens caught it).
2. deleteRange: clamp cursor only AFTER restoring the empty-lines guard —
   clamping against an empty slice panics [-1].
3. Doubled operators need max(pendingCnt, count) — the second keystroke's
   count() reads cleared digits and silently degrades 2dd → dd.
4. vim semantics honored deliberately: yw includes trailing whitespace;
   w wraps across lines; cw keeps trailing ws (≡ce); insert sessions are
   one undo unit.
5. feed()/typeKeys() helpers exist per test package — don't reference
   textbuf's from surfaces/editor tests.

## Next up (rest of M2)

1. Commit this session (user approves diff first).
2. Multi-buffer tabs + :b-style switching; then PTY terminal drawer
   (per-member tabs), markdown preview, git view via go-git, LSP
   foundation, settings skeleton.

## Open questions for user

- None blocking.
