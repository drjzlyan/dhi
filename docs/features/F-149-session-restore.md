# F-149 — Reopen the last session's tabs and split

Status: done · gap G-R4

Quitting DHI dropped every open tab and the split; the next launch showed the start page.

## Decision
- Large files (F-138) are skipped on launch, not deferred: a startup never shows the
  confirmation prompt and never loads a 5 MiB buffer unasked. They are named in the note.
- Scroll is not stored. The buffer view derives it from the cursor, so the restored cursor
  restores the view. Unsaved text is not stored (hot exit is G-R2).
- The recent-files list is already persisted (`.dhi/tmp/recent-files`, F-119); restoring does
  not reorder it.

## Behaviour
- On quit (every quit path) the editor writes `.dhi/tmp/session.json` (per user, gitignored):
  open file tabs with cursor line/col, the active tab, and the other pane's tab if split.
  DHI's own buffers (the commit message) are not saved.
- On launch in the same workspace the tabs reopen, the saved tab is focused and the split
  returns when both buffers did. Files that are gone or over 5 MiB are skipped with one toast
  listing them; an unreadable session file is ignored with a toast.
- Settings `editor.restore_session` (default on) controls both saving and restoring.

## Acceptance
- Round trip of tabs, cursors, active tab and split (`TestSessionRestoreRoundTrip`).
- Setting off neither saves nor restores (`TestSessionRestoreOff`,
  `TestRestoreSessionToggle`, `TestEditorRestoreSessionLayer`).
- Gone and large files are skipped, no prompt, one toast (`TestSessionRestoreSkipsGoneAndLarge`).
- Recent list untouched; corrupt file reported (`TestSessionRestoreKeepsRecentAndIgnoresCorrupt`).
- Quit writes the session (`TestQuitSavesSession`).
