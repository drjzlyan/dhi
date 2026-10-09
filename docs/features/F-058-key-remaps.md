# F-058 — Key remaps

Status: done (M27 P7; the M1 "keybinding overrides" deferral)

## Behaviour
- `[keys]` in `config.toml` (user or workspace, merged key by key with the
  workspace winning) maps **the key you press** to **the key DHI acts on**:

  ```toml
  [keys]
  "ctrl+k" = "ctrl+p"   # palette on ctrl+k
  x = "n"               # x creates (wherever n does)
  m = "space"           # m marks a card
  ```

- **Translation:** the shell translates once, before any routing:
  - chords (`ctrl+…`, `alt+…`) always;
  - plain keys only while nothing is taking text, so a remapped `x` still
    types an x in a buffer, a form or the composer;
  - never inside a setup gate.
- **Display:** everything that shows a key goes through the reverse map —
  hint bars, the statusline (`^p` → `^k`), help rows and palette hints —
  so the screen always names the key to press. A click on a hint still
  acts as its logical key.
- **Validation:** both sides must be named and contain no spaces (write
  `space` for the space bar). `ctrl+c` always quits and cannot be
  remapped. Doctor no longer reports `keys.*` as unknown keys.
- Settings › CONFIG lists the active remaps read-only; you edit them in
  `config.toml`.

## Acceptance
- [x] kit: translate + display incl. caret form, alternatives, space, clearing
- [x] shell: translate when idle, not while typing, chords always
- [x] settings: per-key merge, validation, doctor accepts `keys.*`
