# F-161 — Hot exit with swap recovery

Status: done · gap G-R2

A crash or kill lost every unsaved edit, and quitting with edits meant saving
or dropping them one decision at a time.

## Decision
Swaps are plain files in `.dhi/tmp/swap/<hash of path>.swp` (per user,
gitignored): one JSON header line (path, hash and mtime of the disk text the
buffer started from, time, DHI pid, `hot`) then the buffer text. They are
written on a goroutine `swapDelay` (2 s) after the last change, from the
existing 500 ms editor tick. Buffers over 4 MiB, untitled buffers and DHI's
own files (`.dhi`) get no swap. A live owner only produces a warning.

Ownership is an flock, not a pid (a pid is reused after a reboot): each DHI holds an exclusive
lock on `instance-<nonce>.lock` in the swap folder and records the nonce and host in every swap
header. Live means the lock is held; swaps from another host are never live; only swaps without a
nonce (older ones) fall back to the pid check. Quitting, and "keep for next time", release the
lock; stale lock files are swept on the next start.

## Behaviour
- Removed on save, on close, when the buffer is clean again, and by "quit
  without saving".
- Opening a file with a swap from a dead pid shows a card: `r` recover (swap
  text into the buffer as one undo step from the disk text), `c` compare
  (read-only unified diff file to swap, same renderer as F-155), `d`
  discard, `esc` decide later (swap kept, no new swap for that tab). The
  card says when the file changed on disk since the swap began. A swap equal
  to the file is deleted silently.
- A swap owned by a live pid: message "open in another DHI"; this tab writes
  no swap so the other DHI's is not overwritten.
- Quit guard (F-115) gains `k` keep for next time: every dirty buffer gets a
  swap with `hot: true`, then DHI quits without asking per buffer. With
  session restore (F-149) on, those tabs reopen with the recovered text and
  no card, unless the file changed on disk meanwhile (then the card). With it
  off, the card appears when the file is opened. If a buffer cannot be kept
  (untitled, too large, swap write failed) the guard stays open and says so.

## Acceptance
- Debounce, header, drop on save/close (`TestSwapWrittenDebouncedAndDroppedOnSave`,
  `TestSwapDroppedOnClose`); store round trip (`internal/swapfile` tests).
- Recover is one undo step; compare is read-only; discard; disk-changed note;
  esc; live pid; identical swap (`TestSwapRecoverIsOneUndoStep`,
  `TestSwapCompareAndDiscard`, `TestSwapCardSaysDiskChanged`,
  `TestSwapEscKeepsSwapAndHoldsNewOnes`, `TestSwapLivePIDWarnsWithoutCard`,
  `TestSwapIdenticalToDiskIsDropped`).
- Hot exit (`TestHotExitRestoresWithoutCard`, `TestHotExitWithSessionOffShowsCard`,
  `TestDiscardSwapsOnQuitWithoutSaving`; app: `TestQuitKeepForNextTime`,
  `TestQuitKeepFailureStays`, `TestQuitWithoutSavingDiscardsSwaps`,
  `TestQuitKeepNotOfferedWithoutHotExit`).
- Write sites recorded in `cmd/dhi/testdata/write_sites.txt`.
- Ownership by lock: `TestLiveByLockNotByPID`, `TestLiveOldSwapFallsBackToPID`, `TestOtherHostNeverLive`
  (swapfile); `TestSwapReusedPIDStillRecovers`, `TestSwapHeldLockWarns` (editor).
