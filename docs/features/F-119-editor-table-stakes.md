# F-119 — Editor table stakes

Status: done (M37 P4) · amends F-002, F-010, F-057

These are the editor gaps from the session-63 audit (tier S2) that every mainstream editor has.

## Behaviour
- **File operations in the tree** (all inside the workspace's repos):
  - `a` creates a file or folder in the folder under the cursor. A path such as `pkg/x.go`
    creates the folders it names; a trailing `/` makes a folder. A new file opens at once.
  - `r` renames or moves the item, to a path inside its repo. Open buffers follow the move.
    Moving a file that has unsaved edits is refused.
  - `d` asks first, then moves the item to `.dhi/tmp/trash/<time>/`. That folder is per-user
    and ignored by git; move the item back to restore it. Buffers showing it close. A dirty
    buffer is refused.
  - `:` with the tree focused runs editor commands that don't need a file (`:e path`, `:diag`,
    `:reopen`, `:lsp`, `:test all`).
- **The tree stays current.** The 2-second disk check also re-reads open folders, so files that
  an agent, git or the terminal creates or removes show up. Dot folders such as `.github` and
  `.vscode` are shown; `.git`, `.dhi` and `node_modules` are not.
- **Files**
  - `:e <path>` opens a path given as absolute, workspace-relative (`member/…`), or relative to
    the current file. A path that doesn't exist becomes a new file, inside a repo only.
  - `:w <path>` is "save as": it writes the buffer there and keeps editing the new file. An
    existing target needs `:w!`.
  - `:reopen` (or `:e #`, or the palette) reopens the last file you closed.
  - ctrl+s saves in any mode and keeps you in that mode.
  - Closing the last tab brings the start page back. It now shows a RECENT list of up to 5
    files (clickable, also in the palette); the list is stored in `.dhi/tmp`.
  - A file that was empty when opened ends with a newline on its first save.
- **Go to line:** `:42` and `42G`. `G` alone still goes to the end.
- **Smart insert**
  - Enter keeps the line's indent, and adds one level after `{`, `(`, `[` or `:`. Pressing
    Enter between a pair (`{|}`) opens an indented block.
  - Tab inserts the file's indent unit: the file's own style when it can be detected, else the
    language's (tabs for Go and Makefiles, 2 spaces for TypeScript, YAML and JSON, 4 for
    Python), else 4 spaces. Backspace removes a whole indent step of spaces.
  - Brackets and quotes pair, and typing the closer steps over it. A quote doesn't pair after a
    letter (so "don't" types normally) or before text. Backspace between an empty pair removes
    both.
- **Vim editing**
  - `V` selects whole lines (delete, yank and change are line-wise; paste puts lines below).
  - `>`/`<` in visual mode, and `>>`/`<<` (with a count) on a line, indent and outdent by one
    unit.
  - `.` repeats the last change: an operator, `x`, `p`, or a whole insert session such as
    `A!<esc>`.
- **Search marks**
  - After `/`, `?`, `*`, `n` or `N`, every match on screen is underlined. A line with a match is
    drawn in plain text so the marks stay exact.
  - In normal mode, the first `esc` (or `:noh`) clears the marks; the next `esc` leaves the
    buffer.
- **Long lines scroll sideways** so the cursor stays in view. Mouse clicks account for the
  scroll.
- The help overlay lists all of the above.

## Acceptance
- [x] Tree new, rename and delete; a refused path outside the workspace; outside changes and
      dot folders show (`treeops_test.go`).
- [x] `:w path`, `:w!`, `:e path`; recent files and `:reopen`.
- [x] `42G`, `:42`, `:99` (clamped) (`disk_test.go`).
- [x] Smart insert covers indent, block, colon, pairs, apostrophe, backspace and soft tab;
      `DetectIndent` (`smartinsert_test.go`).
- [x] `Vjd` plus `p`, `>>`, `.`, `V<`, `2>>`, and `.` after an insert or `dd`.
- [x] Search marks and esc; horizontal scroll.
- [x] Live in tmux: `a pkg/hello.go`, a block with a tab indent, a paired quote, and ctrl+s
      wrote the file.
- [x] `make verify` passes. The new write sites are reviewed: the tree and `:e` stay inside
      repos, and `:w <path>` writes where the user typed.

## Not in this phase (still listed as S3 in ROADMAP)
Block visual (ctrl+v), `f`/`t`/`%`, comment toggle, folding, regex in buffer `:s`, word wrap.
