# F-120 — Agent context, inline edit, "fix with agent", undo a turn

Status: done (M37 P5) · amends F-007, F-038, F-117

These items come from the session-63 audit's S2 agent-loop list: context control, inline edit,
fix-from-here and checkpoints.

## Behaviour
- **Context mentions in the editor chat**
  - `@file` puts in the open file's path.
  - `@selection` attaches the visual selection, or the cursor line, as a fenced block with its
    line numbers.
  - `@problems` attaches the file's diagnostics.
  - `@path/to/file` gives that file's path. A workspace path, a path relative to the current
    file, and an absolute path all work.
  - Agent names stay mentions.
  - While you type `@…`, the line under the composer suggests agents, the context words, and
    open and recent files; tab completes the first one. Once the message has mentions, the
    same line lists what it will carry (`+ app.go · selection (3 lines) · 1 problem`).
- **Inline edit (ctrl+k)**
  - In a buffer (normal or visual mode), ctrl+k asks for an instruction and sends the
    selection, or the cursor line, to the helper agent. The helper is your pair partner if you
    are pairing, else the default agent (F-117).
  - The agent is asked to answer with `editor_propose_edit`, so the result arrives as a
    suggestion you review hunk by hunk (F-117). The keyboard stays in the buffer.
  - `:change <instruction>` does the same.
- **Fix with agent**
  - `:fix`, or the palette "Fix this problem with an agent", sends the cursor line's problem
    with the surrounding lines.
  - In the test failures list, `f` sends the failing test, its location and its last 60 lines
    of output.
  - The palette's "Ask an agent about the terminal output" sends the active terminal's screen.
- **Undo an agent's chat turn**
  - A chat turn that is not on a card edits the repos in place. DHI snapshots every member
    before and after such a turn, into `.dhi/checkpoints/<member>.git` used with
    `--work-tree=<member>`.
  - The member's own `.git` is never written: no refs, stash or index changes. Tracked and
    untracked files are captured; anything the member's `.gitignore`, its `info/exclude`
    (copied in) or your global excludes ignore is not.
  - Only turns that changed files are kept, the last 30 of them. Older snapshots are pruned
    and garbage-collected.
  - `:undo-turn`, or the palette "Undo the last agent turn…", lists the files the turn
    added, changed or deleted, and restores them on `y`. Open buffers follow.
  - A file you edited after the turn is named and kept; `f` restores it too.
  - If a snapshot fails, the thread says "undo is not available for this turn", and the turn
    goes on.
- `.dhi/checkpoints/` is added to the runtime gitignore entries, so the `gitignore` repair adds
  it to existing workspaces. The README's "What DHI changes on your machine" describes it.

## Acceptance
- [x] Mentions expand, with chips and suggestions (`mentions_test.go`).
- [x] ctrl+k on a `V` selection DMs the helper with the code and the propose-edit instruction,
      and the keyboard stays in the buffer.
- [x] Checkpoint store with real git: edits, additions and deletions are undone; a later edit
      is reported as a conflict; old turns are pruned; the member's `.git` gains nothing
      (`checkpoint_test.go`).
- [x] Undo from the editor restores the files, and the open buffer follows
      (`undoturn_test.go`).
- [x] `make verify` passes. The new write sites are reviewed: `.dhi/checkpoints`, and restores
      inside member repos on your yes.

## Notes
- The snapshot runs at the start of the turn (`git add -A` with a separate index). On a large
  repo the first one takes a few seconds; later ones only rehash changed files.
- A live agent turn followed by `:undo-turn` needs a signed-in CLI and has not been run live
  yet. The runtime hook is in `cliTurn` (`checkpointTurn`).
