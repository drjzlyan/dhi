# F-102 — One line editor for every text field

Status: done (session 61, M35 P2)

## Why
About twenty inputs were append-only: no cursor, backspace removed only the last byte, and three of
them sliced bytes so one backspace after `é` left broken UTF-8. Insert mode had no ctrl+w/ctrl+u,
home/end or word motion, and in-buffer search was a stub.

## Behaviour
- `kit.Input` is the one editor for single- and multi-line fields: rune-safe cursor, word motion
  (`alt`/`ctrl` + arrows), home/end and `ctrl+a/e`, `ctrl+w` and `alt+backspace` delete a word,
  `ctrl+u` / `ctrl+k` kill to start/end, undo/redo, and paste.
- `kit.EditText` is the one editing core. `kit.Input` (composers, form inputs) and `kit.Form` text
  fields have a full cursor. Queries and short prompts whose cursor stays at the end (palette,
  help, symbols, rail, board, trace and brain filters, find, search, rename, replace, commit
  message, reviewer comments, ideator and reviewer forms) use `kit.EditQuery`/`EditRunes`: rune-safe
  backspace, ctrl+w, alt+backspace, ctrl+u and paste. ctrl+k stays "up" in pickers.
- In the editor chat composer, `[`/`]` and the approval letters act only while it is empty.
- Buffer insert mode: home/end, ctrl+w, ctrl+u, alt+backspace, word motion.
- Buffer normal mode: `/ ? n N *` search (smartcase, wraps with a note), `gg`, `ctrl+d/u`
  (half the visible rows), pgup/pgdown.

## Acceptance
- [x] Table tests for `kit.Input` editing, including é, emoji and ¡.
- [x] Each migrated field moves its cursor and deletes words.
- [x] Buffer search finds forward/back and wraps.
