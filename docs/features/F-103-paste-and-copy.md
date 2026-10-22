# F-103 — Paste and copy

Status: planned (session 61, M35 P3) · ADR-0042

## Why
Bubble Tea delivers cmd+v as a bracketed paste (`tea.PasteMsg`), and nothing handled it, so paste
did nothing anywhere, including the masked secret field. Nothing wrote the system clipboard, and
mouse capture blocks the terminal's own selection.

## Behaviour
- Paste goes to the focused thing: gate, palette, help filter, or the active surface
  (`surfaces.Paster`, falling back to the `paste:` key). CRLF becomes LF; over 1 MiB asks first.
  Editor pastes are one undo step; the terminal gets bracketed paste when the child asked for it.
- The masked secret field accepts paste and never shows it. No log, flash, toast, bus record or
  transcript records paste content.
- `internal/clipboard.Write` sends OSC 52 and also uses the OS clipboard tool through the
  `toolchain.Clipboard` seam (pbcopy; wl-copy/xclip when present).
- Copy: editor yank, `c`/`C` on chat and inbox messages, `c` on trace lines and toast details,
  palette "Copy file path" / "Copy relative path", OSC 52 from programs in the terminal drawer.
  Each copy shows "Copied".
- Mouse: click in a buffer moves the cursor, drag selects. Palette "Select text with the terminal"
  turns mouse capture off until esc, so native selection and cmd+c work anywhere.

## Acceptance
- [ ] PasteMsg reaches editor, composers, forms, secret field, terminal (tests).
- [ ] Sentinel pasted into the secret card appears in no file under the sandboxed config/data dirs.
- [ ] Yank emits SetClipboard and calls the clipboard seam.
- [ ] Live: tmux `paste-buffer -p` into each target; yank → pbpaste.
