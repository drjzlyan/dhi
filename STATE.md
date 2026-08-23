# STATE — current position

Updated: 2026-08-23 (session: M2 — git view over go-git)

## Where we are

Markdown preview committed (`9b8fdd2`). **This session (uncommitted —
commit next): git view.** `make verify` green; 18 packages under
`-race` (go-git v5 added per ADR-0008).

## Just finished

- `internal/gitcore`: go-git service seam — Open/IsRepo, Status (index+
  worktree codes, Staged flag), Stage (. and per-path with delete
  fallback), Unstage (mixed reset w/ Files, keeps worktree), Commit
  (explicit author required; refuses empty msg / nothing staged),
  Log(n) subjects, CurrentBranch (unborn handled).
- Editor ctrl+j panel: open→focused→blurred→closed cycle; repo picked
  from active buffer's member else first member that IsRepo; tabs
  status/log on `tab`; j/k cursor; s stage · S stage-all · u unstage ·
  c commit → inline message input (esc cancels); errors + commit hash
  feedback in-panel; esc blurs leaving sessions alive.
- Goldens: status + log views. Buffer command line now renders vpath
  instead of absolute path (`:w` message was leaking tempdirs into
  goldens).

## Gotchas learned (do not re-learn these)

1. Panel input modes must intercept keys BEFORE the panel's global
   switch — top-level esc case ate the commit-input cancel.
2. perl slurp edits can prepend garbage at position 0 when the pattern
   partially matches quoted chars; prefer edit tool for tricky blocks.
3. Status-line messages containing paths must be vpath-substituted for
   portable goldens (second occurrence of this class of bug).
4. go-git API notes: wt.AddWithOptions(All)/AddGlob; Log returns an
   interface (no pointer type); unborn HEAD → zero hash sentinel.

## Next up (rest of M2)

1. Commit this session (user approves diff first).
2. LSP foundation (installable servers via toolchain; diagnostics +
   completion wiring) and settings skeleton — the last two M2 items.
   Worktree create/switch/remove deferred out of the git MVP line into
   M5-adjacent polish (Reviewer needs them first).

## Open questions for user

- None blocking.
