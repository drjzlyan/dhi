# F-054 — Layout contract: every screen fits every window

Status: done (M27 P2)

## Problem
Found walking the TUI in tmux at 60x20 through 200x50:
- The body was never fitted to the terminal, so empty views left the
  statusline mid-screen with blank rows under it.
- Between 60 and 84 columns the railed views (Workspace, Ideator, Reviewer)
  fell back to a stripped stack, without their panel and hint bar.
- The help overlay overflowed narrow terminals and mangled `[ ]` hints into
  two rows.
- The tab bar clipped to `5…`.
- The first frame (before any resize) and tiny panes crashed the Workspace
  view, and a missing message bus crashed CHANNELS at narrow width.

## Behaviour
- **Shell:** `App.compose` fits the gate or surface body to exactly the rows
  between the tab bar and the statusline (`kit.Fit`). It clips styled lines
  with an SGR reset, so a cut never bleeds colour. The statusline is always
  the last row.
- **`kit.Center`** fills its area: it pads the bottom and clips the width.
  An unsized (0) area returns the block as is.
- **Railed views:** below the dock width (84) the rail folds into a one-line
  section strip, and the same panel (body + hint bar) takes the full width.
  `mainPane` clamps to a minimum size, so 0x0 and tiny panes render.
- **Editor:**
  - The file tree scales with the window: 34 columns at ≥120, 28 at ≥84,
    24 at ≥60. Below 60 a single pane shows: the open buffer, or the tree
    when nothing is open.
  - The crew chat narrows the buffer instead of overflowing.
  - A single-repo workspace opens with its tree expanded.
- **Tab bar:** shows full labels when they fit. Otherwise the active tab keeps
  its label and the rest show numbers, then numbers only. `Hit` uses the
  same layout.
- **Help overlay** (`?`):
  - Modal, at most 78 columns and never wider than the terminal.
  - Descriptions wrap under their key.
  - `j/k/pgup/pgdn/g/G` scroll; `/` filters (the M24 "searchable help"
    deferral); `esc` clears the filter, then closes.
  - Rows come from `kit.HintRows` (keeps `[ ]` together) and
    `kit.DedupeHelpRows`.
- **Landing outside a workspace:** `branding.NoWorkspace` shows one message
  and one action (`ctrl+p → Run setup wizard`) on every view. It drops the
  logo when the window is too small for it.
- **Fixes along the way:**
  - The welcome card is clamped to the terminal width.
  - Ideator/Reviewer sections no longer subtract the panel padding twice.
  - Empty states are vertically centred and full-width.
  - The `✦` glyph (missing from common terminal fonts) is now `◇`.
  - The stray rail footer is gone.
  - Settings keys get a column wide enough for the longest key.
  - The board reserves its detail pane only while a card is selected, and
    shows a real empty state.
  - The first-run consent and boot-block screens are a centred card at most
    80 columns wide, sized to their content.

## Acceptance
- [x] `internal/tui/layoutcontract`: every surface, every rail section, with
      and without a workspace, at 40x10 … 200x48 renders exactly H rows of at
      most W cells
- [x] same for editor states (buffer open, git panel) and for the composed
      shell (welcome, each view, help, filtered help, palette) at 40x12 … 200x50
- [x] no panic before the first resize or at 1x1 / 10x3 / 20x5
- [x] `kit.Fit`, `kit.Center`, `kit.HintRows`, `kit.DedupeHelpRows` and
      tab-bar density are unit-tested; goldens regenerated and reviewed
