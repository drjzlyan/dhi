# F-147 — Git gutter on long files

Status: done · gap G-E7

`linediff.Diff` built an O(n·m) LCS table capped at 4M cells, so past roughly 2000 changed lines
(after prefix/suffix trim) the gutter silently showed nothing.

## Decision
Replace the core with Myers' O((n+m)·D) diff in linear space (middle snake), over interned line
ids. The prefix/suffix trim and the `Diff(older, newer) ([]Mark, bool)` contract and Mark
semantics are unchanged. `Hunks` (F-117 proposal review) keeps its own table and `maxCells`.

## Behaviour
- `ok` is false only when the edit distance (inserted plus deleted lines) exceeds
  `maxEditDistance` (5000), never because the file is long.
- A 50k-line file with a few edits diffs in well under 200ms.

## Acceptance
- Existing `TestDiffMarks` cases pass unchanged.
- Edit distance and kept-line alignment match the old LCS on random inputs
  (`TestMyersMatchesLCSOracle`, old implementation kept as the oracle).
- 50k lines, few edits: ok, under 200ms (`TestDiffLargeFileFewEdits`).
- Full rewrite past the cap reports ok=false; 1000 scattered edits still diff
  (`TestDiffGivesUpOnlyPastEditDistance`).
