# F-155 — Local history: snapshot per save, compare, restore

Status: done · gap G-R3

Overwriting an edit with `:w` or autosave left no way back short of git, and
only for committed work.

## Decision
Snapshots are plain files under `.dhi/history/<hash of path>/<unixnano>.snap`
(per user, gitignored). The write runs on its own goroutine with the text
already in hand, so a save never waits on it. Limits are named constants in
`internal/localhistory`: 50 snapshots per file, 64 MiB for the whole store
(oldest across files go first, checked every 20 snapshots), and files over
4 MiB are not snapshotted. A save identical to the newest snapshot adds
nothing. Counts and the compare view run snapshot to buffer, as in VS Code.

## Behaviour
- Every save through the editor's after-save hook snapshots the saved text:
  `:w`, `:w <new name>`, autosave (F-142), the result of format-on-save, and
  agent edits saved through that path. DHI's own files under `.dhi` are skipped.
- `:localhistory` (also in the command palette) lists the open file's
  snapshots newest first: time, size, `+a -r` lines versus the buffer.
- `enter` opens a read-only unified comparison (snapshot to buffer, three
  lines of context); `r`, in the list or the comparison, puts the snapshot
  in the buffer as one undo step. Nothing is written until `:w`.
- Snapshot failures are silent: history never fails a save.

## Acceptance
- Save, identical save, compare, restore, then one undo returns the prior
  text and disk is untouched (`TestLocalHistorySaveCompareRestore`).
- Autosave snapshots (`TestLocalHistoryAutosaveSnapshots`); an empty list
  writes nothing (`TestLocalHistoryEmptyWritesNothing`).
- Store: ordering, dedupe, same-instant saves, per-file and total caps,
  oversize skip, id traversal (`internal/localhistory` tests).
- `Buffer.SetText` is one undo step (`TestSetTextIsOneUndoStep`).
- New write site recorded in `cmd/dhi/testdata/write_sites.txt`.
