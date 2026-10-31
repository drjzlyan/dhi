# F-118 — First run and discoverability

Status: done (M37 P3) · amends F-097 (start page), F-041 (palette)

This covers the session-63 audit's items 18–20, which were seen in a live tmux run in a fresh
repo.

## Behaviour
- **Outside a workspace, every key either works or says why.**
  - `/`, `s`, enter, ctrl+a and n show "Create a workspace first — press c (about 10
    seconds)" on the start page. Before, `/` switched to a FIND mode that showed nothing and
    swallowed every key.
  - The status row offers only what works here: `c create a workspace` and `t tour`.
- **"Ask an agent" works before any agent exists.**
  - ctrl+a with no team opens the "Pick a team" step. DHI relaunches with the team, and a toast
    explains what is happening. Before, ctrl+a did nothing and said nothing.
  - The start page shows the "^a Ask an agent" button whenever there is a way to get an agent.
- **Palette**
  - "Find and open a file" can be found by searching either "open" or "find" (before, a search
    for "open" found nothing).
  - "Ask an agent (open chat)", or "(pick a team first)" when there is no team.
  - "New task".
  - The count reads "1 command", not "1 commands".

## Acceptance
- [x] With no workspace, `/` and `s` explain instead of entering a mode, and the hints are
      `c create a workspace` and `t tour` (`startpage_test.go`).
- [x] ctrl+a without a team runs the team step and shows a toast.
- [x] The start page golden shows the "Ask an agent" button.
- [x] `make verify` passes.
