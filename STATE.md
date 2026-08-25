# STATE — current position

Updated: 2026-08-25 (session: M4 P4 — tasks & ChangeSets shipped)

## Where we are

**M4 P0–P4 COMPLETE and committed (a647b0e onward; verify green).**
The Workspace view is a six-section operations floor: MEMBERS · ORG ·
PACKS · STANDARDS · CHANNELS · TASKS. Task cards persist as TOML,
bind ChangeSets through an injectable worktree seam (live once the git
registry pin flips), and carry thread bindings so progress lands in
the right conversation. Remaining in M4: P5 inspection dashboards +
Roster/invite seam for M5/M6.

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

## Next up (P5 inspection + attach-points)

1. Per-agent profile section: manifest inventory, current task/worktree
   state, activity timeline (bus history + memory journal), read-only
   notes, KB contributions, effective standards summary.
2. Narrow Roster/invite interface extracted from *runtime.Runtime use
   in editor chat + wsview turnHandler so M5 Reviewer/M6 Ideator plug
   in without churn.
3. Goldens deliberate; ROADMAP/STATE each session.

## Open questions for user

- None blocking.
