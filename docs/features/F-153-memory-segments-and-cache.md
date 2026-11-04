# F-153 — Channel segments, KB index cache, spend-ledger rotation

Status: done · gap G-B6

ADR-0037 left three open: channel logs grew in one file (stream lines make them the fastest
growers), every `kb_search` re-read and re-indexed every entry, and `spend.jsonl` grew forever.
ADR-0037 set no parameters; these are named constants.

## Decision
- Rotate by size, reading transparently across the pieces. One mechanism in `internal/jsonl`
  serves channels and the ledger: the active file is renamed `<name>.seg0001`, `.seg0002`, ...
  (oldest first) under the append lock, and `Append` reopens if a rotation swapped the file
  between its open and its lock. Segments do not end in `.jsonl`, so channel listing ignores them.
- `bus.ChannelSegmentBytes` = 8 MiB; `budget.LedgerSegmentBytes` = 1 MiB.
- The ledger rotates by size, not month: task totals are all-time, so every segment is read anyway.
- KB cache is in memory only (no new on-disk format).

## Behaviour
- `bus.Post` rotates a channel file at the threshold. History, All, ReplyCount and the parse
  cache span all segments (cache key = file count, total size, newest mtime). `Recent` reaches into
  older segments when the active file holds fewer than the window. `Open` seeds the ID counter and
  repairs corrupt lines in every segment.
- `budget.Ledger.Append` rotates; `Entries` reads segments then the active file, so `Tally` day and
  task totals are identical across a boundary.
- `knowledge.Store.Search` keeps each entry body keyed by file size and mtime, and the TF-IDF
  index keyed by every entry's id, title, tags, size and mtime. An unchanged KB costs one stat per
  entry; an edit re-reads only that file and rebuilds the index.
- Files from older versions (single file, no segments; `index.json` + entries) read unchanged.

## Acceptance
- Rotation, ordering, `Recent` across segments, reopen ID counter, no segment leaks into
  `Channels` (`TestChannelRotatesAndReadsAcrossSegments`); old channel file reads and then rotates
  (`TestLegacyChannelFileReads`).
- Ledger totals across the boundary (`TestLedgerRotationKeepsTotals`); old ledger reads
  (`TestLegacyLedgerReads`).
- Primitives (`TestSegmentsRotateAndTail`, `TestSegmentsAbsentAndNoOp`).
- Warm searches do no reads or index builds; edit and new entry invalidate
  (`TestSearchCachesAndInvalidates`); old KB opens and searches (`TestLegacyKBFixtureSearches`).

## Open
- The chat UI still loads a channel's whole history (all segments) into memory; paging it is a
  separate change. Doctor's large-log check does not yet sum segments.
