# F-051 — Tutorial steps that watch real actions

Status: done (M26)

## Why
Lesson steps could only wait for a view switch, the palette or the help overlay (F-048).
Everything else ("open a file", "save", "create a task") was read-and-press-continue, so a
lesson could not tell whether you had actually done the thing.

## Behaviour
- A step may declare `await = "do:<event>"`. Surfaces report the real action through the
  optional `surfaces.Emitter` seam (`SetEmitter(func(event string))`), which the shell wires
  to the coach in `app.New`. Events are emitted on the UI goroutine only, and only on
  success (a refused `:pair`, an empty comment or a task the store rejected report nothing).
- Event names live in the pure `internal/tutorial` package (`Events`). `ValidAwait` accepts
  only those, so a lesson cannot wait for an event that does not exist, and a test fails if an
  event is defined that no lesson waits for.
- Events: `editor.open|insert|save|format|pair|test|break|debug`, `task.created`,
  `review.opened|comment|submitted`.
- Steps that need something the user may not have (a crew for `:pair`, Go for `:test`) say
  so and can be skipped with `ctrl+]`.
- A new lesson, "Review a change like a pull request", teaches the F-049 flow.
- Editor, team and debug lessons now wait for the real action.

## Acceptance
- [x] `do:` grammar validated against the event table; unknown events rejected
- [x] a step advances on its event, ignores other events, and a repeat does not skip ahead
- [x] every event has an emitter test; refused or failed actions emit nothing
- [x] surfaces without an emitter, and actions with no lesson running, are harmless
- [x] walked in tmux: palette → editor lesson → open, insert, save advanced the strip
