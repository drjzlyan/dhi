# F-151 — Stage and unstage single lines

Status: done · gap G-G2

The hunk diff viewer (F-121) stages or unstages a whole hunk. When one hunk mixes changes that
belong in different commits, the only way to split it was to edit the file.

## Behaviour
- `V` in the diff viewer starts a line range at the first changed line; `j`/`k` extend it
  (`+`/`-` lines inside the range are marked); `V` or `esc` cancels (esc leaves the range, not the
  diff); `n`/`p` end it by moving hunks.
- `s` (work-tree side) or `u` (staged side) acts on the marked lines only. The work tree is never
  touched.
- `gitcore.PartialPatch` builds the patch. Staging: an unmarked `-` becomes context, an unmarked
  `+` is dropped. Unstaging (applied reversed): an unmarked `+` becomes context, an unmarked `-`
  is dropped. The `@@` counts are recounted; a `\ No newline at end of file` marker follows the
  line above it. `Runner.ApplyPartial` feeds it to `git apply --cached [--reverse] -` on stdin.
- Refused: a past commit's diff (F-140, "a past commit is read-only"); an untracked file (stages
  whole with `s`); a range with no changed line.
- Known git limit (same as `git add -p`): when a block of `-` lines is followed by `+` lines, a
  marked `+` lands after the unmarked `-` lines that became context.

## Acceptance
- Table cases: mixed selection both directions, hunk at file start, whole-file delete, trimmed
  blank context, no-newline markers, refusals (`TestPartialPatch`, `TestPartialPatchRefuses`).
- Real git in a temp repo stages, stages a no-newline last line, and unstages single lines
  (`TestApplyPartialRealGit`).
- Editor: `TestStageAndUnstageLines`, `TestLineModeGuards` (esc, past-commit refusal).
