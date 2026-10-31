# F-121 — Git and terminal depth

Status: done (M37 P6) · amends F-008 (git panel), F-061 (terminal)

These are the git and terminal gaps from the session-63 audit's S2 list. Everything runs the
user's own git (ADR-0044).

## Behaviour
- **Git panel (ctrl+j)**
  - enter (or `d`) on a file opens its diff one hunk at a time; `j`/`k` move between hunks.
  - `s` stages the hunk on screen and `u` unstages a staged one. The raw hunk goes to
    `git apply --cached [--recount] [-R] -`, so "no newline at end of file" markers survive. A
    new (untracked) file stages whole.
  - `b` lists branches, most recent first. Typing filters the list; enter switches. A name that
    matches no branch creates it (`git switch -c`). Open buffers and the tree follow the switch.
  - `p` runs `git pull --ff-only` and `P` runs `git push` in a terminal tab you watch, so ssh
    passphrases and credential prompts are yours to answer. A first push sets the upstream
    (`-u origin <branch>`).
  - `C` opens a commit message buffer (`.dhi/tmp/COMMIT_EDITMSG`) for a subject, a blank line
    and a body. `:w` commits as your git identity and closes the buffer; `:q!` cancels. `c` is
    still the one-line commit.
  - The status list scrolls with the cursor. Before, rows past the panel's height could not be
    seen.
- **Blame:** `:blame` names who last changed the cursor line, the commit, its date and its
  subject.
- **Terminal**
  - shift+pgup and shift+pgdown (shift+↑/↓ for one line) scroll back through output that left
    the screen, using the emulator's scrollback (`vt.Screen.History`). Any other key returns to
    the live screen.
  - alt+w closes the tab and stops its program. alt+r restarts an exited tab in the same
    folder.
  - Output is routed to tabs by a stable id, not a position. Before, closing a tab, or a member
    leaving the workspace, could send one tab's output to another.
- `gitcore.Runner.RunInput` gives a git call stdin (for the patch).

## Acceptance
- [x] Two hunks; `s` stages only the first (`git diff --cached` shows it alone)
      (`gitdepth_test.go`).
- [x] `b` then `feat-x` creates and switches.
- [x] `C` plus a subject and body, then `:w`, makes a commit with that exact message and closes
      the buffer; blame names the author.
- [x] The git panel keeps the cursor row visible with 15 changes.
- [x] Terminal scrollback: back to `line-0`, then to the live screen; alt+w closes a tab
      (`drawer_test.go`).
- [x] Live in tmux: the diff view of a new file in a real repo.
- [x] `make verify` passes; the write site (COMMIT_EDITMSG in `.dhi/tmp`) is reviewed.

## Not in this phase
Inline blame for every line (gitlens style), staging single lines, merge conflict tools.
