# F-103 — Paste and copy

Status: done (session 61, M35 P3; buffer click-to-cursor deferred) · ADR-0042

## Why
Bubble Tea delivers cmd+v as a bracketed paste (`tea.PasteMsg`), and nothing handled it, so paste
did nothing anywhere, including the masked secret field. Nothing wrote the system clipboard, and
mouse capture blocks the terminal's own selection.

## Behaviour
- Paste goes to the focused thing: gate, palette, help filter, or the active surface
  (`surfaces.Paster`, falling back to the `paste:` key). CRLF becomes LF; over 1 MiB is refused with a toast (open it as a file).
  Editor pastes are one undo step; the terminal gets bracketed paste when the child asked for it.
- The masked secret field accepts paste and never shows it. No log, flash, toast, bus record or
  transcript records paste content.
- `internal/clipboard.Write` sends OSC 52 and also uses the OS clipboard tool through the
  `toolchain.Clipboard` seam (pbcopy; wl-copy/xclip when present).
- Copy: editor yank (`y`, `yy`, visual `y`), `Y` on a chat message, `Y` in a task trace (the
  visible events as Markdown), palette "Copy file path" / "Copy relative path", and OSC 52 from
  programs in the terminal drawer. Each copy shows "Copied" (or a warning naming the OS tool's
  error). Surfaces hand text over through `surfaces.Copier`.
- Mouse: palette "Select text with the mouse" turns mouse capture off (a SELECT mode block on the
  status row) until the next key, so the terminal's own drag-select and cmd+c work anywhere.
  Click-to-cursor inside a buffer is deferred: it needs the split-aware buffer geometry.

## Acceptance
- [x] PasteMsg reaches editor, composers, forms, secret field, terminal (tests).
- [x] Sentinel pasted into the secret card appears in no file under the sandboxed config/data dirs.
- [x] Yank emits SetClipboard and calls the clipboard seam.
- [ ] Live: tmux `paste-buffer -p` into each target; yank → pbpaste.
