# STATE — current position

Updated: 2026-08-23 (session: M2 — multi-buffer tabs + :b commands)

## Where we are

Modal buffer MVP committed (`437d6a9`). **This session (uncommitted —
commit next): multi-buffer tabs** with ex-command switching. `make
verify` green; 15 packages under `-race`.

## Just finished

- textbuf.CommandDelegate seam: workspace-level ex commands handled by
  the surface via ExecEx(requester, cmd); unknown commands fall through
  to "not an editor command". Editor.SetMessage for delegate feedback.
- Surface tab model: `bufs []*bufTab{ed,vp,path}` + activeTab index.
  open() reuses an existing tab per path; :q closes the active tab and
  activates a neighbor (tree when none left).
- Commands: :bn/:bp cycle; :b <substring> matches vpath or abs path —
  unique match activates, ambiguous reports "more than one match".
- Tab strip above the buffer viewport: active tab bracketed+accent,
  others dim, dirty dot per tab.
- Fuzzy matcher fix: greedy first-match mis-scored "alpha/app.go" vs
  ".../helper.go" (tie at 64) — Match now scores greedy alignment from
  every start occurrence of the first rune; contiguous wins (94 vs 64).

## Gotchas learned (do not re-learn these)

1. Greedy subsequence scoring needs multi-start alignment; earliest
   occurrence locks onto wrong words and ties beat true contiguity.
2. Field/method name collisions (active int vs active()) cascade through
   perl renames — pick distinct names up front.
3. Blanket perl renames also rewrite func params (tabStrip active int);
   verify with go build before trusting.
4. Tree navigation sequences in tests must be derived from dirs-first
   ordering per directory level; one extra `down` silently targets the
   next repo header.

## Next up (rest of M2)

1. Commit this session (user approves diff first).
2. PTY terminal drawer (per-member tabs, Manager.Env PATH) — biggest
   remaining chunk; then markdown preview, git view (go-git), LSP
   foundation, settings skeleton.

## Open questions for user

- None blocking.
