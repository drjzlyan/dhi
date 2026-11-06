# F-143 — Inline blame on the cursor line

Status: done · gap G-G3

`:blame` (F-121) answers for one line on request. GitLens-style blame shows it as you move.

## Behaviour
- `editor.blame` (Settings row "inline blame", default off) and the session override
  `:set blame` / `:set noblame` (beats the setting, like `:set wrap`).
- On: after the end of the cursor line, dimmed, `author, 2 days ago · subject` of the line's
  last commit; a line not in any commit reads `not committed yet`.
- `gitcore.Blame` runs the user's `git blame --porcelain` once per file on a goroutine and
  posts the result as a message; the cache lives on the tab. A save, a disk reload and a
  commit drop it (a result from before the drop is discarded). Rendering never waits for git.
- Between saves, edits are mapped by comparing the buffer with the blamed text: the
  unchanged head and tail keep their blame; an edited or inserted line reads
  `not committed yet`; a line that cannot be placed shows nothing.
- Shown only if it fits: truncated with an ellipsis down to 10 cells, else omitted. With wrap
  on it rides the last visual row of the line. Untracked files show nothing.

## Acceptance
- Annotation follows the cursor, one row only (`TestBlameShowsOnCursorLineOnly`).
- Off by default; `:set blame|noblame` and override precedence (`TestBlameOffByDefaultAndSetCommands`).
- Uncommitted and shifted lines (`TestBlameUncommittedAndEdits`, `TestBlameForMapping`).
- Wrap: last row, omitted when full (`TestBlameWrapLastRowOrOmitted`); fit (`TestBlameSuffixFit`).
- Real git, async, invalidated on save (`TestBlameWithRealGit`); stale result dropped
  (`TestBlameStaleResultDropped`).
- Parser (`TestParseBlame`); settings (`TestEditorBlameLayer`, `TestBlameToggle`).
- Goldens `editor_blame_100x30`, `editor_blame_wrap_100x30`; existing goldens unchanged.
