# STATE — current position

Updated: 2026-08-26 (session 2: M5 CLOSED + M5.x PR round-trip shipped)

## Where we are

**M5 COMPLETE plus the M5.x PR round-trip addendum (e0f49ee → f6f2ff9;
`make verify` green).** On top of the full Reviewer floor you can now:
create a PR straight from a review/task worktree (`C` in Reviewer, `p`
on TASKS cards; dirty trees refused visibly; push rides go-git with
`gh auth token` BasicAuth), and pull GitHub review/issue comments into
review threads (reply-chain mapping, RemoteID-deduped, auto-import on
open + `R`). Next milestone: **M6 Ideator full (F-004)**.

## Gotchas added this session

1. go-git Push needs a REGISTERED remote (CreateRemote first in tests;
   member clones always have origin).
2. Test fakes must fully implement seams — an interface-satisfying stub
   returning zero-value PRMeta silently broke link-back assertions.
3. Surface fixtures now use real git repos w/ bare local origins so
   push flows run hermetically offline.
4. gh accepts full remote URLs as --repo scope (no OWNER/REPO parse).
5. When swapping m.svc in surface tests mid-flow, remember dependent
   state (diffFn/tokenFn) lives per-service instance.

## Just finished (P1–P3)

- `internal/gitdiff`: unified parser → FileDiff/Hunk/Line with rename/
  binary/mode handling; `Pair()` side-by-side alignment; golden fixture.
  Gotchas: hunk headers are BOUNDARIES — handle `@@` before hunk-body
  state or adjacent hunks collapse into one; expand tabs before width
  math or styled cells misalign; never crop() styled strings (ANSI bytes
  count as runes).
- `internal/review`: TOML cards `.dhi/reviews/<id>.toml` (threads w/
  bus_thread correlation, viewed marks, pending flags), WorktreeFn/
  DiscardFn seams, gh seam interface + exec impl (author JSON is
  {"login":…}), Service.Start/Patch/Diff/Discard/PostComment. gitcore
  gained Fetch (refspec→local ref hash), RemoteURL, Branches. PR flow =
  go-git fetch of refs/pull/N/head → detached worktree at sha.
- `surfaces/reviewer`: rail+pane dock like wsview; DIFF renders from a
  flat viewRow model (headers/lines/side-pairs) with wrap-aware visual
  heights and scroll clamping in render (not key handlers).

## Just finished (P4–P6)

- Comments: composer anchored via anchorAtCursor (new-side line preferred,
  old-side fallback w/ Side=old); thread drill-down flattens
  threads+comments for cursor nav; drafts editable/deletable, submitted
  immutable (store enforces via checkPending).
- Agents: @mention in composer → invite(threadID) posts root context msg
  once (BusThread persisted) then Handle dispatches; mirrorBus folds
  replies back (matched thread or new "(review)" file-level thread).
  Complete mode (`A`) posts ```diff prompt capped at 48k chars.
- Completion: `P` posts consolidated markdown (agent comments suffixed
  "_DHI agent @id_") via gh → MarkPosted; `F` creates task
  `fix-<reviewID>` bound to the SAME worktree via tasks.RecordChangeSet
  (new) + BindThread(review channel); `e` handoff through
  editor.OpenPaths (new exported buffer opener) + app.OpenInEditor
  (interface assertion + focus switch).

## Gotchas from the M5 build (carried)

1. Go closure aliasing: read form fields BEFORE closeForm() resets them
   (submitForm fAgentReview panic).
2. Test event pumps must NOT re-arm listeners (`go cmd()`) — a stray
   listener steals the next event and deadlocks the next pumpCmd.
3. workspace.Create needs member dirs pre-created (stat check).
4. go-git remote config: CreateRemote on PlainInit'd repo works offline;
   gh accepts full remote URLs as --repo scope.
5. kit.Panel overlay pattern: pass the REAL pane body to stackOver —
   overlaying onto a single-space string truncates to one line.
6. review.Service.CanDiff vs HasRunner: tests inject diffFn without a
   runner; gate UI loading on CanDiff.

## Carried gotchas (still load-bearing)

- bus.History(ch,0) excludes threaded rows; thread views stitch roots.
- New confirm-modal kinds must join formKey's confirm branch.
- Guard modal-opening keys behind service presence (tasks n/a/w/t;
  reviewer A/F/P/e).
- Rename-modal targets captured at open time; waitReply before
  provider.Calls(); macOS /var→/private/var EvalSymlinks.

## Next up (M6 Ideator per F-004)

1. Spec check F-004 → plan: sessions = invited agent set + thread +
   artifact folder under `.dhi/sessions/` (or reuse channels?).
2. Read-only artifact tree + preview (markdown first via glamour —
   preview pkg exists).
3. Approve/reject flow; rejection→revision loop back to authoring agent.
4. Export later via MCP (seam exists in tools registry).

## Open questions for user

- None blocking. M5 closure tag pending user request.
