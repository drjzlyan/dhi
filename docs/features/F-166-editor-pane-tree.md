# F-166 — More than two editor splits

Status: done · gap G-E17

F-057 allowed one split: the focused tab beside one other. The buffer area is now a small tree
of panes, side by side and stacked, nested, up to `maxPanes` (4).

## Behaviour
- In normal mode, after `ctrl+w`:
  - `v` splits the focused pane side by side, `s` stacks it. Focus stays on the pane you split;
    the new pane shows the previous tab that no pane shows yet (else the same buffer).
  - `h j k l` (or arrows) move focus to the nearest pane in that direction; `w` / `W` cycle
    in reading order.
  - `c` / `q` close the focused pane (its tab stays open); `o` keeps only the focused pane.
  - `=` equalizes. Shares are always equal (no resize keys), so it changes nothing.
- Ex: `:vs`, `:sp`, `:only`, `:close`. A fifth pane is refused with a note.
- A split inside a node of the same direction adds a sibling, so three side-by-side panes get a
  third each; a different direction nests.
- Minimum pane: 36 columns by 8 rows. A split that would break it is refused with a note; a
  layout that no longer fits after a resize folds to one pane and returns when the window grows.
- Each pane draws its own tab through the normal buffer view, so wrap, block visual, blame and
  conflict markers apply per pane, at the pane's width. Popups belong to the focused pane.
- A click focuses the pane under it and places the cursor.
- Closing a buffer removes its panes only; the others stay.
- Session restore saves `layout` (a tree of `dir`, `kids`, leaf `path`, `focus`) in
  `.dhi/tmp/session.json`. A file with only the old `split` field still restores as two panes;
  a layout over the cap, or with fewer than two surviving panes, is ignored. Two-pane sessions
  also write `split` so older builds read them.
- Limit: one editor per tab. The same buffer in two panes shares its cursor and scroll.
- Behaviour changes from F-057: `ctrl+w s` is now a stacked split (was side by side); `h` / `l`
  move by direction instead of swapping panes; panes keep their places when focus moves.

## Acceptance
- 3- and 4-pane layouts render and tile the area (`TestSplitTreeThreeAndFourPanes`,
  goldens `editor_split3_160x40`, `editor_split4_160x40`); the cap is enforced.
- Direction focus and cycling (`TestSplitFocusByDirectionAndCycle`); close / only / `=` / ex
  forms (`TestSplitClosePaneOnlyEqualize`).
- Refusal when too small (`TestSplitRefusedWhenTooSmall`); closing a buffer
  (`TestSplitCloseBufferKeepsOtherPanes`); one buffer in three panes (`TestSplitSameBufferTwice`).
- Click focus (`TestSplitClickFocusesPane`); wrap and block visual in panes
  (`TestSplitPanesRenderPerBuffer`).
- Session round trip, old field, and cap (`TestSessionRestoresPaneTree`).
- The two-pane view is unchanged from F-057 (`TestSplitPanes`, byte-identical render).
