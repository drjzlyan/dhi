# F-101 — Keys that work on every keyboard

Status: planned (session 61, M35 P1) · ADR-0042

## Why
macOS Terminal and iTerm2 send Option+1 as `¡`, not alt+1, unless "Option as Meta" is on, so view
switching typed characters into buffers. Option+arrow word motion did nothing. The editor took
readline keys (`ctrl+a/j/t/y/d`) from a focused terminal, F-058 remaps ran before the palette, two
`ctrl+p` bindings could never fire, and `ctrl+b` (wizard back) is tmux's prefix.

## Behaviour
- `kit.NormalizeKey` runs at the shell's single key chokepoint:
  - outside text entry, the US-layout Option characters `¡™£¢∞§¶•ªº` mean `alt+1..9,0`; inside
    text entry they stay text. The first unmapped Option character outside text shows a one-time
    toast ("turn on Option as Meta, or use ctrl+p → Go to").
  - `alt+b` / `alt+f` (what Terminal.app sends for Option+←/→) mean `alt+left` / `alt+right`.
- The view requests kitty alternate-key reporting where supported.
- A focused terminal keeps every control key; the editor's chords apply only when it is not focused.
- Remaps never apply inside the palette or help overlay.
- Symbols and the chat rail use `ctrl+k` for "up" (ctrl+p always opens the palette).
- The wizard goes back with `ctrl+b`, `alt+left`, or the clickable `‹ Back`.
- The terminal drawer sends xterm sequences for alt+arrows (`ESC[1;3D`).
- Hints show `⌥` on macOS. Help has a Keyboard section: Option as Meta per terminal, macOS Spaces
  taking ctrl+arrows, fn+arrow for home/end/pgup/pgdown.

## Acceptance
- [ ] Raw-byte decode table: `¡`, `ESC b`, `ESC[1;3D`, `ESC[1;5C`, kitty CSI-u, `ctrl+_`.
- [ ] `¡` on the start page selects view 1; in a focused buffer's insert mode it inserts `¡`.
- [ ] With the terminal focused, `ctrl+a` reaches the pty.
- [ ] A remap of `ctrl+k` does not change the palette's ctrl+k.
- [ ] `alt+left` goes back in the wizard.
