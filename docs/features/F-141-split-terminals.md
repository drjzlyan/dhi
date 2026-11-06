# F-141 — Split terminals in the drawer

Status: done · gap G-M1

The terminal drawer showed one tab at a time. Running a server beside a shell meant switching
tabs (F-121).

## Behaviour
- `alt+s` (focused drawer) opens a new shell in the active tab's folder and shows the two tabs
  side by side, the new one focused. The pair is two ordinary tabs; the strip lists both.
- `alt+o` moves focus between the two panes. Only the focused pane draws its cursor.
- `alt+w` closes the focused pane (its program is stopped); the other becomes the single pane.
  Closing either tab by any route ends the split.
- Each pane has its own PTY and emulator size: the body is split into two panes and a divider
  column; resizing the window or leaving the split re-sizes both. A full-screen program takes
  the whole body as before.
- Switching (`alt+1..9`) to a tab outside the pair shows that tab alone; switching back to a
  member shows the split again.
- Below 20 columns per pane the split is refused with a toast (and an existing split folds to
  the focused pane), like the buffer split (F-057).
- Keys avoid readline's meta keys (`alt+b/d/f`); they sit in the drawer hint row and help overlay.

## Acceptance
- Split opens beside, same folder, no third pane (`TestTermSplitOpensBesideInSameFolder`).
- Per-pane PTY/emulator sizes, resize, and restoring full width (`TestTermSplitPanesGetOwnSize`).
- Focus switching, both panes visible, close returns to one pane (`TestTermSplitFocusAndClose`).
- Narrow refusal (`TestTermSplitFoldsWhenNarrow`); hints and help (`TestTermSplitHelpAndHints`).
- Goldens `editor_drawer_split_100x30`, `editor_drawer_split_left_100x30` (`TestTermSplitGolden`).
