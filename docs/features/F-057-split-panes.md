# F-057 — Split panes

Status: done (M27 P4; the M23 "split panes" deferral)

## Behaviour
- In a buffer, in normal mode:
  - `ctrl+w v` splits the editor into two panes side by side: the focused
    buffer, plus the previous buffer (or the same buffer twice when only
    one is open, like vim's `:vsplit`).
  - `ctrl+w w` (or `h` / `l`) moves focus between panes.
  - `ctrl+w q` / `ctrl+w o` returns to one pane.
- The ex commands `:vs` and `:only` do the same. In insert mode, `ctrl+w`
  stays the buffer's own key.
- The focused pane keeps the identity-accent border and owns popups (hover,
  completion, code actions). The other pane is drawn with a quiet edge.
- Each pane needs at least 36 columns. Narrower windows fold back to one
  pane, and the split returns when the window widens.
- Closing the focused buffer ends the split on the other buffer. Search
  results and markdown preview take the whole area.

## Acceptance
- [x] split / switch / :only / close-ends-split (editor test)
- [x] narrow fold-back; the layout contract covers the split at every size
