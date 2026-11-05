# F-146 — Task runner over the project's own task files

Status: done · gap G-T1

Running a project's build, lint or dev script meant remembering its name and typing it in a
terminal, and compiler errors in the output were not clickable.

## Decision
Read what the project already declares; add no format (ADR-0043). Run it with the user's tool
(ADR-0044): the runner is resolved on the user's PATH and a missing one is named, never
installed or replaced.

## Behaviour
- `internal/taskrun` discovers tasks per workspace member:
  - `package.json` scripts, in file order; `pnpm` when `pnpm-lock.yaml` exists, `yarn` for
    `yarn.lock`, else `npm` (`<pm> run <name>`).
  - `Makefile`/`makefile`/`GNUmakefile` `target:` lines; `.PHONY` and other dot targets, pattern
    rules, `$(var)` targets and assignments are skipped; `## text` becomes the hint.
  - `justfile`: the output of `just --list --unsorted` when `just` is on the PATH (a failing
    `just` is reported, not hidden), else the recipe names parsed from the file.
- `:task` (also palette "Run a task…") opens a fuzzy picker (`kit.Palette`); the group is the
  member name when there are several. `:task <name>` runs the one task of that exact name.
- Enter opens a focused drawer tab in the member folder running `npm run x` / `make x` / `just x`.
- When the tab exits, its output is scanned for `file:line[:col]: message` (Go, gcc, eslint
  unix) and `file(line,col): message` (tsc) lines naming existing files. They replace the
  previous task problems and show in `:diag`, `]d`/`[d`, the gutter and the line message.

## Acceptance
- Scripts keep order and pick the manager by lockfile (`TestPackageScriptsKeepOrderAndPickManagerByLockfile`);
  bad JSON is reported (`TestBadPackageJSONIsReported`).
- Makefile and justfile parsing (`TestMakeTargetsSkipSpecialPatternAndAssignments`,
  `TestJustParsedWithoutJustAndFromList`).
- Output matching (`TestParseProblems`).
- Picker lists, filters and runs in the member folder
  (`TestTaskPickerListsFiltersAndRunsInTheMemberFolder`); by name and missing tool
  (`TestTaskByNameAndMissingTool`); no task files (`TestTaskWithNoTaskFilesSaysSo`);
  problems reach `:diag` (`TestTaskOutputProblemsFeedDiagAndJump`).
- In a folder that is not trusted (F-148) recipes come from parsing the justfile; `just --list` is
  not run (`TestRestrictedTaskListNeverRunsJust`).
