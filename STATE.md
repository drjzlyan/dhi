# STATE — current position

Updated: 2026-08-25 (session: M4 CLOSED — P5 shipped)

## Where we are

**M4 COMPLETE (all six phases, a647b0e → c6aa2c8; `make verify` green,
not yet committed as a closure tag).** The Workspace view is the full
seven-section operations floor: MEMBERS · ORG · PACKS · STANDARDS ·
CHANNELS · TASKS · INSPECT. F-003 acceptance criteria are met in code;
the one external dependency is the git registry flip (release checklist
below) which activates task worktrees. Next milestone: **M5 Reviewer
full (F-005)** — its inputs consume the Roster/turnHandler seams that
now exist.

## Just finished (P3)

- `chatpane.go`: CHANNELS section — rail (#general seeded, `#<team>`
  channels from org registry, sorted DMs per rostered agent; selection
  survives rebuilds), transcript w/ cursor + wrapWords, thread
  drill-down (`t` root+replies / `c` back), composer (`i` focus).
- Posting persists via bus as "you", then routes through the new
  narrow `turnHandler` interface (*runtime.Runtime satisfies it) so
  @mentions trigger turns; crew-less workspaces can still chat.
- cmd/dhi now opens the bus for EVERY workspace (`openBus`) and shares
  it between wsview and the runtime.
- Gotchas: bus.History(ch,0) excludes threaded rows by design — thread
  views must stitch root+replies themselves; toggle fields must gate
  ←/→ or they eat spaces; typeInto-style helpers must REPLACE prefills.

## Release checklist blocking the git registry flip

Dispatch `.github/workflows/release-git.yml`, publish artifacts as GH
release `hermetic-git-v2.55.0`, cross-check source digest
457fdb04dc8728e007d4688695e6912e6f680727920f2a40bf11eacc17505357
(git-2.55.0.tar.xz @ kernel.org), paste script-emitted platform blocks
into `internal/toolchain/registry/manifest.json`. Until flipped:
doctor stays silent about git; smoke runs via DHI_SMOKE_GIT_BIN.

## Gotchas learned (carried)

1. Rename-modal targets captured at open time, never derived from live
   buffers; buffer identity also lives in openVPath/openPath.
2. waitReply before asserting provider.Calls(); async UI ops resolve
   through event chans pumped by listen cmds.
3. Replace-mode standards keep ONLY built-ins + override entries.
4. macOS /var→/private/var vs git-reported paths (EvalSymlinks);
   quiet git swallows stderr on exit 1; `commit -am` skips new files.
5. os.CreateTemp needs its dir pre-created; sed bulk renames need
   re-grep; fixture member dirs must match registered member paths.

## Gotchas added this session

- bus.History(ch,0) excludes threaded rows — thread views stitch
  root+History(ch,id) themselves (chatpane.visibleHistory).
- New confirm-modal kinds must be added to formKey's confirm branch or
  enter routes to submitForm instead of submitConfirm.
- Guard modal-opening keys behind their service being present (tasks n/a/w/t).

## Just finished (P5)

- `internal/agentkit/profile`: Build() aggregates roster manifest, org
  teams, tasks (open/done split), bus activity (per-channel scan,
  newest-first, capped), memory journal+notes, KB contributions (new
  Store.ContributionsBy), standards block; independent degradation.
- INSPECT pane: list + expandable profile with activity/memory/KB/
  standards sections; per-workspace store caches.
- Seams: profile.Roster (AgentIDs+Manifest) on Runtime; turnHandler
  already narrow. M5/M6 can be built against interfaces only.

## Gotchas added

- knowledge.ContributionsBy reads the in-memory index first (Store.mu)
  then falls back to disk — inspection UIs may run long after Open.

## Next up (M5 Reviewer per F-005)

1. Spec check F-005 → implementation plan before code (inputs:
   PR via gh | local worktree/branch → auto review worktree).
2. Diff engine over gitcore (hermetic git diff --no-index or go-git
   patches), GitHub-style file/hunk model shared with future side-by-side.
3. Review surface skeleton replacing placeholder; line/hunk comments
   persisted under .dhi/reviews/; pending-review batching + viewed marks.
4. Agent invites per-thread + complete-agent-review mode via existing seams.
5. Completion flow: gh post | dispatch fixing agent (task card!) |
   open-in-editor handoff.

## Open questions for user

- None blocking. M4 closure commit/tag pending user request.
