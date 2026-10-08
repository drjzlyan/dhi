# F-056 — Project-wide replace (regex), previewed and confirmed

Status: done (M27 P4; the M23 "regex project replace" deferral)

## Behaviour
- **Search** (`s`): `ctrl+r` in the query box toggles **regex** mode. The
  box says which mode is on. Fixed-string stays the default, so typing
  `a.b` never surprises anyone.
- **Results:** `r` opens the replace prompt. The replacement may use
  `$1` / `${name}` from regex groups. The prompt previews every change
  (`file:line  old → new`, the changed text highlighted) and says what will
  be written:
  - open buffers are edited in place, one undo step each, and are not saved;
  - closed files are written to disk.
- `enter` applies; `esc` backs out without touching anything.
- **Matching mirrors the search:** rg's smart case (all-lowercase pattern
  = case-insensitive) and every non-overlapping match on a hit line.
  Fixed-string patterns are quoted, so the replacement is literal.
- **Safety:**
  - Only the lines the search returned are touched.
  - A line that changed since the search is skipped and reported as
    stale, never clobbered.
  - A pattern that matches nothing on a line leaves it alone.
  - An invalid regex is refused before the preview.
- **Result:** a flash reads `replaced N in M files` plus any stale or failed
  files. The results list re-runs, so what remains is visible.

## Design
`internal/projreplace` is pure: `Plan(hits, pattern, regex, repl)` gives
the change list and `Apply(changes, files)` writes it through a `Files`
seam. The editor implements that seam over open buffers
(`textbuf.Buffer.ReplaceLines`, one undo group) and the disk. The search
uses `search.RegexSearcher` when regex mode is on (`rg` without `-F`).

## Acceptance
- [x] plan: fixed vs regex, smart case, `$1` captures, several matches per line
- [x] apply: buffer vs disk, stale lines skipped and reported, unchanged
      lines untouched, one undo step per buffer
- [x] editor flow: ctrl+r toggles regex, r → preview → enter applies,
      esc aborts with nothing written
