# F-061 — Full-screen programs in the terminal

Status: done (M27 P7; the M23 "terminal alt-screen" deferral). Decision: ADR-0030.

## Behaviour
- The terminal drawer (`ctrl+t` in the editor) runs a real terminal
  emulator, so vim, htop, less, git's pager and fzf work inside DHI.
- When a program switches to the alternate screen, the terminal takes the
  whole body ("terminal · full screen"). The PTY is resized to match, and
  focus moves to the terminal. When the program exits, the drawer comes
  back at its normal size.
- The cursor is drawn where the program put it, while the terminal has
  focus.
- Terminal queries (cursor position, device attributes) are answered.
- Keys use the xterm encoding: every `ctrl+<letter>`, `alt+<key>`, arrows
  with modifiers, home/end/page keys, delete/insert, F1–F12.
  `ctrl+t`, `alt+1..9` and `alt+n` stay the drawer's own keys.
- A focused terminal owns `ctrl+c` (it interrupts the program); `ctrl+q`
  quits DHI, and the statusline says so.

## Acceptance
- [x] `internal/vt`: output + colours, alt screen enter/leave, rows fit their
      box (no raw tabs), cursor-position query answered
- [x] drawer: full-screen takeover resizes the emulator to the body; every row
      fits; leaving returns to the drawer; key encoding table
- [x] shell: ctrl+c goes to a focused terminal, ctrl+q always quits
- [x] live in tmux: vim opened a file in the drawer and showed its status line;
      `:q` returned to the drawer; `ctrl+c` interrupted `sleep` and DHI kept
      running
