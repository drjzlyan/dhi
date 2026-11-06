# F-140 — File history

Status: done · gap G-G5

Blame (`:blame`) names the last commit for one line; there was no way to see every commit that
touched a file, or what each changed, without leaving for a terminal.

## Behaviour
- `:history` (current buffer) or `h` in the git panel (the selected change, else the open buffer)
  opens a list from `git log --follow` through the user's git (`gitcore.FileLog`), newest first,
  at most 200 commits: short sha, relative date, author, subject. Renames are followed.
- `enter` opens that commit's patch for the file in the F-121 hunk viewer, read-only (`s`/`u`
  refuse); the header names the path as it was at that commit.
- `esc` leaves the diff for the list, `esc` again closes it. `j`/`k`/arrows move.
- A file with no commits, no open file, or no git says so in the message line (ADR-0011).

## Acceptance
- Commits list newest first across a rename; enter shows the old-name patch; staging refuses;
  esc steps back (`TestFileHistory`).
- `h` in the panel opens the selected file; untracked and no-file cases report
  (`TestFileHistoryFromThePanel`).
- Log parsing yields path and short sha (`TestFileLogParsesRenames`).
