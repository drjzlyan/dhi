# F-138 — Large-file size warning

Status: done · gap G-E6

Opening a file above a size threshold (5 MiB) shows an inline confirm dialog before loading it,
with the file size displayed. Binary files keep their existing handling and do not show the warning.
The user can proceed (y) or cancel (n) without opening a tab.

## Behaviour
- Files ≥ 5 MiB show a confirmation dialog inline in the editor, styled like tree-operation
  dialogs (F-119).
- The dialog displays the file size in human-readable form (KiB, MiB, GiB).
- Pressing y or enter opens the file; pressing n or esc cancels without opening.
- Binary files skip the prompt and keep their existing error handling (ErrBinary).
- All open paths funnel through `Model.open()` -> `openNode(n, confirmed)`; confirming re-enters
  that same path with `confirmed=true`, so there is no second copy of the open/save hooks.
- Only the first 8 KiB is read to tell text from binary (never the whole file). A file already
  open as a tab just switches to it, with no prompt.

## Acceptance
- A 5 MiB+ text file shows a confirm prompt with size on opening (`TestLargeFileWarning`).
- Pressing y or enter opens the file and opens a tab (`TestLargeFileConfirm`).
- Pressing n or esc cancels and leaves no tab open (`TestLargeFileCancel`).
- A file under 5 MiB opens without prompting (`TestSmallFileNoWarning`).
- Re-opening an already-open large file does not prompt (`TestLargeFileReopenSkipsPrompt`).
- A binary file above 5 MiB keeps its existing error behaviour (`TestLargeBinaryNoWarning`).
