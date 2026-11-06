# F-156 — Merge-conflict tools

Status: done · gap G-G1

A file left mid-merge showed raw `<<<<<<<` markers with no help: nothing coloured the sides, no way to jump
between conflicts or take a side, and the git panel did not say which files were unmerged.

## Decision
- The parser is pure and lives in `textbuf` (`ParseConflicts`). A marker is exactly seven characters at
  column 0 followed by end of line or a space (a bare `=======` must end the line); other lengths are text.
  A marker out of order inside a block (a second `<<<<<<<`) is content; a block that closes before its
  separator, or never closes, is skipped. CRLF is tolerated.
- Unmerged files come from the user's git (`status --porcelain=v1 -z`), not go-git, whose status cannot
  tell a conflict from a change (ADR-0044). Staging a resolved file is `git add -- <path>`.
- No short accept keys: `co`/`ct`/`cb` collide with vim's `c` operator and `gx` is vim's. Accepting is the ex
  command; the keys are the vim-style `]x` / `[x` jumps. `d]x` is not a motion and drops the operator.
- No action-menu popup in this change (open question below).

## Behaviour
- Highlight: ours, diff3 base, theirs and the marker lines get their own wash (`theme.ConflictOurs/Base/Theirs/Marker`);
  conflict lines render without syntax colour so the wash is unbroken.
- `]x` / `[x` (count steps that many) move to a conflict's opening line and say "conflict N of M"; at the
  last one, "no more conflicts".
- `:conflict ours|theirs|both|base` replaces the conflict under the cursor (`both` = ours then theirs; `base`
  needs diff3 and says so otherwise). One undo step each; the cursor lands where the block began.
  `:conflict next|prev` jump; bare `:conflict` prints usage.
- Git panel: unmerged files lead the unstaged list with their code (`UU`, `AA`, `DU`...) in the danger colour and
  are never listed as staged; `s` on one runs `git add`.
- Once a buffer that held markers has none left and is saved (`:w`, `ctrl+s`) and git still lists the file as
  unmerged, the status line asks "stage <file>? (y/n)": `y` stages, any other key dismisses.

## Acceptance
- Parser: `TestParseConflicts` (plain, diff3, two, CRLF, bare, lengths 6/8, nested-looking, malformed,
  unterminated, indented), `TestConflictRegions`, `TestResolveChoices`.
- Keys: `TestJumpConflicts` (incl. counts, ends), `TestJumpWithOperatorPendingIsNoMotion`, `TestNoConflictsMessage`.
- Accept: `TestConflictExAccept`, `TestConflictBaseNeedsDiff3`, `TestConflictAcceptIsOneUndoStep`,
  `TestConflictLastResolvedSaysSo`, `TestConflictUsageAndNextPrev`.
- Git: `TestParseConflicts` / `TestConflictsAndStagePath` (gitcore, real merge).
- Surface: `TestConflictResolveSaveAndStage`, `TestConflictStageDeclined`, `TestGitPanelMarksConflicts`,
  `TestNoStageOfferWithoutConflict`.
