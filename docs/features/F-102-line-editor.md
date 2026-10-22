# F-102 — One line editor for every text field

Status: planned (session 61, M35 P2)

## Why
About twenty inputs were append-only: no cursor, backspace removed only the last byte, and three of
them sliced bytes so one backspace after `é` left broken UTF-8. Insert mode had no ctrl+w/ctrl+u,
home/end or word motion, and in-buffer search was a stub.

## Behaviour
- `kit.Input` is the one editor for single- and multi-line fields: rune-safe cursor, word motion
  (`alt`/`ctrl` + arrows), home/end and `ctrl+a/e`, `ctrl+w` and `alt+backspace` delete a word,
  `ctrl+u` / `ctrl+k` kill to start/end, undo/redo, and paste.
- Composers, filters, the palette query, find/search/rename/replace, commit messages, and ideator
  and reviewer forms all use it.
- In the editor chat composer, `[`/`]` and the approval letters act only while it is empty.
- Buffer insert mode: home/end, ctrl+w, ctrl+u, alt+backspace, word motion.
- Buffer normal mode: `/ ? n N *` search, `gg`, `ctrl+d/u`, pgup/pgdown.

## Acceptance
- [ ] Table tests for `kit.Input` editing, including é, emoji and ¡.
- [ ] Each migrated field moves its cursor and deletes words.
- [ ] Buffer search finds forward/back and wraps.
