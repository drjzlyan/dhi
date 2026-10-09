# F-064 — UI cohesion: background integrity, a livelier theme, richer components

Status: in progress (M28)

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
  borders stay, and a one-cell margin beside it (widened to whole words) is cleared
  of text so nothing butts against the edge. There is no dim veil and no
  shadow row, and the box is one solid elevated block, edges included.
  The dialog is one row shorter than before.
- **Guard.** `golden.BgHoles` / `AssertBgIntegrity` parse SGR per cell. Every
  box drawn with corner glyphs must have every interior cell painted and
  its right edge in place. Every raw-ANSI `golden.Snapshot` runs the check,
  and each surface has a sweep test over its sections and dialogs.
- **Theme and components.** Phase 2 adds shade and accent tokens, and
  Phase 3 upgrades the tree, terminal, board, channels, reviewer, settings,
  ideator and chrome. Both are listed in the acceptance section.

## Acceptance
- [x] integrity check passes for every surface/state swept (workspace
      sections + new-task dialog + narrow; editor tree/buffer/drawer/wide;
      reviewer screen/side-by-side/narrow/files/reviews; ideator sections +
      dialog; settings sections; help overlay; bootgate; kit)
- [x] a dialog changes no backdrop cell's background, and backdrop
      characters outside the cleared margin stay unchanged (kit test)
- [x] the reviewer diff fills the panel's inner width and never crosses
      the edge
- [ ] theme gains header/zebra/cursor-line/rule shades and two accents in
      all three themes, covered by the contrast tests
- [ ] component upgrades: tree, terminal drawer, editor tabs/mode, board
      cards, channels, reviewer files, settings groups, ideator cards, chrome
- [ ] README screenshots cover every surface
