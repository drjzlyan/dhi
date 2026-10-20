# F-096 — Real UI components in kit

Status: planned (M34 P2) · ADR-0041

## Why
Controls were not drawn as controls: a form field was a dim label plus `[value]`, buttons were
keycaps followed by a label, checkboxes were hand-drawn glyphs, and status was plain colored text.

## Behaviour
- `kit.Button`: a filled pill with variants (primary, secondary, danger, ghost) and states (focused,
  disabled), with a key shown inside. `kit.Buttons` renders through it.
- `kit.Input`: an InputBg field with a focus edge, placeholder, cursor and optional help/error rows.
  Single-line, or multi-line where `ctrl+j` inserts a newline.
- `kit.Toggle`, `kit.Checkbox`, `kit.Radio`: a glyph plus a label, so state never depends on color
  alone.
- `kit.Badge`: a status pill with a glyph (ok, warn, error, info, neutral, busy).
- `kit.Card`: stripe, title, body, optional badge and buttons, and a selected state.
- `kit.Checklist`: rows with a state glyph, title, a one-line why, and an action.
- Form rows render through Input/Toggle at one row each, so dialog and Settings geometry is
  unchanged. Modal footers use Button. Toast, Tabs, StatusLine and Rail read the roles.

## Acceptance
- [ ] Each component: deterministic render, state variants, click spans, unit tests and a golden.
- [ ] Form heights unchanged (existing click tests pass without new offsets).
- [ ] Goldens regenerated and reviewed.
