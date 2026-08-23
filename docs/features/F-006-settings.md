# F-006: Settings surface — everything configurable

Status: planned · Skeleton lands M2, full coverage tracks each feature milestone

## Summary

Every behavior across all five views is user-configurable through a Settings
view (and the same files are hand-editable). Precedence:
defaults < user config (`~/.config/dhi/config.toml`) < workspace config
(`<ws>/.dhi/config.toml`).

## Sections

| Section | Covers |
|---|---|
| Appearance | theme tokens selection, animations on/off, glyph set fallbacks |
| Keys | global keymap + per-surface keymaps (nvim-style overrides) |
| Editor | tab/space, wrap, scrolloff, buffer defaults, preview formats |
| Terminal | shell command, scrollback size, per-repo default env |
| Git | commit template, worktree naming scheme, lazygit-style keymap |
| Agents | default model/temperature per agent or team, tool allowlists, permission policies |
| Sandbox | path-jail scope extras, dangerous-op policy, OS-sandbox toggle (post-M1) |
| Knowledge | shared-KB contribution policy (`auto`/`review`), memory budgets |
| LSP | per-language server selection; install/manage via DHI toolchain |
| Toolchain | pinned versions view, network/fallback policy (ADR-0005) |

## Acceptance criteria

- Every setting above has a schema entry; unknown keys are reported by doctor.
- Changes apply live where safe (theme, keys) without restart.
- Settings UI is fully navigable by keyboard and covered by component tests;
  file round-trip (load→edit→save) unit-tested.
