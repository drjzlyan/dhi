# STATE — current position

Updated: 2026-08-25 (session: M4 P3 — channels floor shipped)

## Where we are

**M4 P0–P3 COMPLETE and committed (a647b0e → 7987f70; `make verify`
green).** The Workspace view is a five-section operations floor
(`[`/`]` switcher): MEMBERS · ORG · PACKS · STANDARDS · CHANNELS.
Hermetic git spike validated live (registry flip still pending first
artifact release — see release checklist below). Next: **P4 tasks ↔
ChangeSets + kanban**, then P5 inspection/attach-points.

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

## Next up (P4 tasks ↔ ChangeSets + kanban)

1. `internal/tasks`: per-task TOML under reserved `.dhi/tasks/<slug>.toml`
   (title/status backlog|active|in-review|done/assignee/team/thread
   binding/[[changesets]]); atomic writes; Subscribe pings; malformed
   files surface as store warnings for doctor.
2. Worktree binding behind an injectable AttachFn seam (gitcore.Runner
   in production once the registry pin flips; hermetic fake in tests):
   attach creates `.dhi/tasks/<slug>/<member>` linked worktree on branch
   `<branch-prefix>/<slug>`, records changeset; detach removes metadata,
   tree deletion only with explicit confirm.
3. TASKS section UI on the workspace view: grouped list, n new task,
   enter detail, s cycle status, a assign, w attach/detach, x remove.
4. Doctor tasks suite: parse warnings, dangling assignee/team refs.
5. Then P5 inspection dashboards + Roster/invite seam for M5/M6.

## Open questions for user

- None blocking.
