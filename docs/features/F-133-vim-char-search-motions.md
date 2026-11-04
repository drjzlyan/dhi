# F-133 — vim character-search motions f/F/t/T/;/,/% 

Status: done · gap G-E3

Find and navigate to characters on the current line with vim-style motions. f/F/t/T move to/until a target rune; ; and , repeat or reverse the last find; % jumps to a matching bracket. All work with operators (d, c, y) and extend visual selections.

## Behaviour

- `f{c}` moves the cursor to the next `c` on the current line (inclusive, forward).
- `F{c}` moves to the previous `c` (exclusive, backward).
- `t{c}` moves to the character just before the next `c` (inclusive, forward).
- `T{c}` moves to the character just after the previous `c` (exclusive, backward).
- `;` repeats the last find motion in the same direction.
- `,` repeats the last find motion in the opposite direction (f↔F, t↔T).
- `%` jumps to a matching bracket (()[]{}) on the current line. If the cursor is not on a bracket, searches forward for one. Searching for the match works across bracket nesting.
- All motions work with counts: `3fx` finds the 3rd occurrence of x.
- All motions work with operators: `dfx` deletes from the cursor to x (inclusive).
- All motions extend visual selections: `vfx` selects up to x; `v,` reverses the find direction within the selection.

## Acceptance

- `TestFindCharMotion`: f/F/t/T move cursor correctly with correct inclusivity.
- `TestDeleteWithFind`: operators (d) compose with find motions.
- `TestRepeatFind`: ; repeats the last find; , reverses the direction.
- `TestMatchBracket`: % jumps to matching brackets and forward-searches if not on one.
- Find motions work in visual mode; ; and , extend/shrink the selection.
- Editor surface passes `AwaitingChar()` gate before intercepting [ and ] for ]d/[d (F-116), so `f[`, `F]`, `t]`, etc. work.
