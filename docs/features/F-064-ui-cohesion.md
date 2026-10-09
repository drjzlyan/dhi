# F-064 — UI cohesion: background integrity, a livelier theme, richer components

Status: done (M28)

## Problem
Backgrounds were inconsistent across the app. A dialog left dark bands
beside and below its box, wiped the pane borders on its rows, and showed
a patchy interior. The same glitch appeared in tree rows, code rows, the
terminal, chat rows and hint bars. The reviewer's diff wash ran past its
panel edge. One root cause: rows built from styled fragments were then
wrapped in a background style, and every inner reset dropped that
background for the rest of the row.

Beyond the glitches, the screens felt flat. Board cards were bare
one-liners, the file tree and terminal drawer looked basic, settings was a
key/value dump, and there were few shade steps or accent colors to give
structure.

## Behaviour
- **Background integrity.** `ansi.Fill` re-opens a row's background after
  every escape that clears it. `kit.PaintRow` (clip, pad, fill) paints
  panel, dialog, help and inset rows, so no styled fragment can punch a
  hole or paint past its width.
- **Dialogs touch only their box.** `kit.Overlay` splices the box into the
  backdrop rows. Cells beside it keep their characters and colors, pane
  borders stay, and a one-cell margin beside it (widened to whole phrases) is cleared
  of text so nothing butts against the edge. There is no dim veil and no
  shadow row, and the box is one solid elevated block, edges included.
  The dialog is one row shorter than before.
- **Guard.** `golden.BgHoles` / `AssertBgIntegrity` parse SGR per cell. Every
  box drawn with corner glyphs must have every interior cell painted and
  its right edge in place. Every raw-ANSI `golden.Snapshot` runs the check,
  and each surface has a sweep test over its sections and dialogs.
- **Theme.** New tokens in all three themes: a header shade, zebra and
  cursor-line shades, a quiet rule color, and teal and rose accents. New
  helpers cover tinted pills, section headers, rules, stable per-author
  colors and file-kind glyphs (plain Unicode, no Nerd Font).
- **Components.**
  - The file tree has guide rails, chevrons and colored kind glyphs, with
    git letters and branch pills from a cached go-git status.
  - The terminal drawer and git panel span the full width, show tabs as
    pills, light their edge when focused, and carry their keys in a hint
    footer.
  - The editor shows tabs and the mode as pills and colors the current
    line number.
  - Board cards take two lines, separated by rules, under header strips
    with counts.
  - Channels give each agent its own color, use rule dividers and show
    the composer as an input bar.
  - Ideator sessions render as two-line cards with mode pills.
  - The reviewer adds kind glyphs, a viewed meter and header strips, with
    the review state as a pill.
  - Settings groups CONFIG with dotted leaders and colored booleans.
  - The active tab, section strip and mode chip are pills in the view's
    accent.

## Acceptance
- [x] integrity check passes for every surface/state swept (workspace
      sections + new-task dialog + narrow; editor tree/buffer/drawer/wide;
      reviewer screen/side-by-side/narrow/files/reviews; ideator sections +
      dialog; settings sections; help overlay; bootgate; kit)
- [x] a dialog changes no backdrop cell's background, and backdrop
      characters outside the cleared margin stay unchanged (kit test)
- [x] the reviewer diff fills the panel's inner width and never crosses
      the edge
- [x] theme gains header/zebra/cursor-line/rule shades and two accents in
      all three themes, covered by the contrast tests
- [x] component upgrades: tree, terminal drawer, editor tabs/mode, board
      cards, channels, reviewer files, settings groups, ideator cards, chrome
- [x] README screenshots (12, all regenerated) cover every surface
