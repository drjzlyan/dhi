# F-115 — Data safety, crash fixes and background delivery

Status: done (M37 P0) · ADR-0045 · amends F-002 (editor core), F-061, F-012

From the session-63 audit (plan `~/.claude/plans/i-want-dhi-to-dazzling-sky.md`). The problems
below were seen in a live tmux run or confirmed in the code.

## Behaviour
- **Saving is byte-exact.** If you save a file without changing it, the bytes on disk stay the same.
  Before this fix, each save added a blank line at the end of the file.
  - The file's line endings are kept: CRLF stays CRLF, and so does a missing final newline.
  - The write is atomic: a temp file is written, then renamed over the old one. The file's mode is
    kept, and a symlink is followed, so the link itself survives.
  - A new (scratch) file ends with a newline the first time it is saved.
- **Binary files are refused.** A file with a NUL byte in its first 8 KiB does not open: "binary
  file — DHI edits text files only".
- **Changes from outside DHI are never overwritten silently.**
  - Each buffer remembers the size, modification time and SHA-256 of the file as it last read
    or wrote it.
  - `:w` refuses if the file changed since then, even when the size is the same: "file changed
    on disk… :e! reloads it, :w! overwrites it". `:w!` overwrites anyway.
  - Every 2 s the editor checks open files. A buffer with no edits reloads and keeps its cursor
    ("reloaded — the file changed on disk"). A buffer with edits is warned once per change.
  - `:e` refuses to throw away your edits; `:e!` does.
  - `Buffer.NoteDisk` records DHI's own writes to a file outside the buffer, so they are not
    reported as outside changes.
- **Quitting asks before losing work.**
  - ctrl+c or ctrl+q, from any path (the palette, help, a gate), checks every surface that
    implements `surfaces.Unsaved`. If anything is unsaved, a "Quit DHI?" dialog lists it:
    - `s`: save all and quit. If a save fails, the dialog shows the error and DHI stays open.
    - `d`: quit without saving.
    - `esc`: go back to work.
  - In a buffer, ctrl+c in insert, visual or command mode acts like esc (as in vim). It never
    quits.
- **No crash in a workspace without agents.**
  - The views receive the runtime as an interface that is really nil when there are no agents.
    Before, they got a nil pointer wrapped in an interface, which passes a nil check and then
    crashes.
  - `Runtime.Handle`, `AgentIDs`, `Manifest` and `isAgent` are safe to call on a nil runtime.
- **Clicking a hint never crashes.** `kit.ClickKey` returns `/` for a `/ find` hint. A range
  such as `1-5 views` is not clickable.
- **Background messages reach every view** (ADR-0045). Before, a view stopped getting live
  updates once you left it: Workspace refresh, autopilot ticks, Settings installs, the editor's
  terminal pump, LSP diagnostics and the disk poll.
- **Views never stay dimmed.** Every way of switching views starts the fade's frame tick: a
  click, an agent's request, or a jump from another view.

## Acceptance
- [x] Open, then `:w` round-trips `""`, `x`, `x\n`, `x\n\n`, `x\r\n`, `a\r\nb` and UTF-8 byte
      for byte (`textbuf/disk_test.go`). Live: a CRLF file keeps its endings, and `main.go`
      gains no newline.
- [x] An outside rewrite of the same size is caught. `:w` refuses, `:w!` writes, `:e!` reloads
      and keeps the cursor. Live: the warning, the refusal, and a clean buffer reloading.
- [x] Quit dialog: esc cancels, `d` quits, `s` saves and quits, and a failed save keeps DHI open
      (`app/quit_test.go`). Live: ctrl+c leaves insert mode, and a second ctrl+c opens the dialog.
- [x] A nil runtime is harmless (`runtime/nil_test.go`). `ClickKey` covers `/`, ranges and
      `space`.
- [x] A background message reaches the hidden views, and a view switch made without a key ends
      its fade.

## Notes
- The save-time hash check is the real guard. The poll is a convenience, and it relies on
  background delivery to reach the editor while another view is on screen.
- A hard-linked file loses its link on save, because rename replaces the path. This is rare,
  and vim's default has the same trade-off.
- The new write sites in `textbuf/disk.go` write only the file the user saved. They are recorded
  in `cmd/dhi/testdata/write_sites.txt`.
