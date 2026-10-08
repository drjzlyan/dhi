# F-041: UX polish — palette, typing safety, presence, accessibility

Status: complete (M24) · Companion to: F-012 (motion), F-025 (chrome),
F-026 (UI beauty). Origin: the session-48 audit of the shell against the
brief ("extremely beautiful", "top UX principles").

## A real bug fixed first: the shell stole typed characters

The shell handled `1`–`9`, `?`, `tab` and `shift+tab` before any surface
saw a key. Typing `2+2?` in a chat message, `fix?` in an editor comment,
or a digit in a form field switched views or opened help; vim counts
(`3dd`) and tab-indent were unreachable in the real app (tests drove the
surfaces directly and never saw it).

- `surfaces.InputCapturer` (`CapturesInput() bool`): a surface collecting
  free text — focused buffer, terminal, chat composer, git panel, any
  form/dialog, the board filter, message search, reaction picker, review
  composer, finder/symbol prompts — owns plain keys. Only ctrl-chords stay
  global (`ctrl+c`/`ctrl+q`, `ctrl+p`).
- The statusline hints follow the state (`1-N views`, `tab`, `? help`
  vanish while typing; `^p palette` always shows; the view count is the
  registry size, not a constant).
- To leave a focused editor buffer: `esc`, or the palette from anywhere.

## Command palette (`ctrl+p`)

`kit.Palette` (fuzzy via `internal/fuzzy`) over a context-aware list:
"Go to <surface>", the active surface's `surfaces.CommandProvider`
commands, help, theme (Dark/Light/High contrast), quit. Workspace,
reviewer, ideator, settings contribute section jumps (and board
new/filter); the editor contributes find, search, terminal/git/chat
toggles, save, format, symbols, tests, breakpoints, debug start/stop/step,
pair/unpair per rostered agent and "review suggestions" — each shown only
when it applies. It works while a surface is typing and dismisses on click.

## Presence

A tab-bar chip — `⠹ 2 agents working` (static `◐` under reduced motion;
dropped, never clipped, on narrow bars) — driven by
`Runtime.ActiveCount()` (in-flight turns). One tick chain at 120 ms runs
only while agents work and motion is on.

## Welcome, empty states, clicks

- First run in a workspace shows a dismissible welcome card (keys, palette,
  sections, @mentions); the seen-marker is `.dhi/welcome.seen`
  (gitignored). It waits for any boot gate and owns the keyboard until
  dismissed.
- `kit.EmptyState` (glyph, title, why, action) replaces bare "(none)" in the
  inbox, reviews and sessions. The duplicated ideator keymap line is gone
  (bottom bar only, per F-025).
- Rail clicks: `kit.Rail.RowAt` + `Click` on workspace, ideator, reviewer
  and settings jump to the clicked section (not through dialogs/forms).
- Inbox stamps right-align on the row's first line instead of wrapping onto
  their own line at narrow widths.

## Accessibility

- Every text token now meets WCAG AA (4.5:1) on every surface it sits on —
  the hint bar and selected rows included (`TextMuted` was 2.3:1 on the dark
  hint bar, 2.3–2.8:1 in the light theme); `AccentDim` ≥ 3:1. Enforced by
  `theme/contrast_test.go`, with the Text > Dim > Muted order preserved.
- New `high-contrast` theme (true black, white text, 6:1+ text, visible
  borders), selectable in Settings and the palette.
- `NO_COLOR` / 16-colour terminals: already handled by Bubble Tea v2's
  renderer (`colorprofile`); no extra code needed (the audit's claim that
  it was missing was wrong).

## Acceptance

- [x] shell keeps digits/`?`/tab for a typing surface; `ctrl+c` still quits;
      per-surface `CapturesInput` (editor, workspace channels/forms/filter)
- [x] palette: rank/pick/close/clamp/view; shell open/run/go-to/esc/click;
      works mid-typing; editor commands reflect state
- [x] activity chip: shows/hides, animates, stops, static under reduced
      motion, dropped when narrow, exact bar width; `ActiveCount`
- [x] welcome: once, owns keys, quits, click-dismiss, waits for the gate
- [x] EmptyState content/centering/bounds; goldens reviewed
- [x] rail clicks (kit + four surfaces); inbox stamp alignment
- [x] contrast + hierarchy + high-contrast border tests

## Deferred (judged lower value than the above)

- Searchable help overlay; a `ctrl+/` help alias (terminals do not deliver
  it reliably); `kit.Notice` (what/why/retry) and a loading skeleton — no
  call site needed them yet, so no unused component was added.
- Click handling inside lists, lanes, diff lines and the hint bar keys.
- Per-surface identity accents; a wide-layout (≥160) secondary detail pane.
- Terminal-background auto-detection of the initial theme.
