# DHI — Testing Strategy

Goal: **no manual QA**. The UI is a test target like any other layer.

## Pyramid

| Layer | Technique | Where |
|---|---|---|
| Unit | table-driven state tests (list nav, key routing, token completeness) | `*_test.go` beside code |
| Visual unit | golden snapshots of components (`Panel`, `List`, statusline) | `internal/tui/kit/testdata/goldens/` |
| Shell integration | scripted keys through `app.Update`, assert active surface / help toggle / quit | `internal/tui/app/app_test.go` |
| Shell composition | full-frame goldens at fixed 100×30 | `internal/tui/app/testdata/goldens/` |
| Service (later) | real `git` against temp repos; httptest fixture servers for toolchain downloads | M1+ |
| E2E (later) | teatest driving the whole program with scripted keystrokes, marker assertions | M2+ |

## Golden files

- ANSI-stripped for readable diffs — layout is asserted, colors evolve freely
  (targeted color assertions live in normal unit tests where they matter).
- Stored per-package: `<pkg>/testdata/goldens/<name>.golden`.
- **Regenerate deliberately:** `DHI_UPDATE_GOLDENS=1 go test ./...`
  then review the diff like code. CI never regenerates.
- Harness: `internal/testutil/golden` (`Snapshot`, `Contains`, pure `Compare`).

## Determinism rules

1. Fixed sizes everywhere (e.g. 100×30 shell frames).
2. No wall-clock reads inside render paths; animations advance on injected
   tick events (M1 bootstrap).
3. Theme pinned via `theme.SwapForTest(t, theme.Dark())` in visual tests.
4. Keys constructed explicitly:
   `{Code: tea.KeyTab}`, `{Text:"j", Code:'j'}` — see app_test.go.

## Invariants enforced by tests

- No raw `lipgloss.Color(` outside the theme package (`theme_test.go`).
- Statusline/list/panel rows pad to exact widths.
- Global keys always win over surface keys; unknown keys forward to surface.
- **Layout contract** (`internal/tui/layoutcontract`, F-054): every surface,
  every rail section, the editor's states (buffer, git panel, split) and the
  composed shell with each overlay render exactly the rows they were given
  and never a cell wider, from 40x10 to 200x50, with no raw tabs. They also
  survive the first frame before any resize and 1x1 panes.

## Commands

```sh
go test ./...                      # everything
DHI_UPDATE_GOLDENS=1 go test ./... # regenerate snapshots
make verify                        # fmt + vet + build + race tests
```

CI runs the non-regenerating set on every push/PR (see
`.github/workflows/ci.yml`).

## Screenshots

`scripts/screenshots.sh` regenerates `docs/assets/*.png` for the README. It
seeds a demo workspace (`go run ./scripts/demo <dir>`), drives the real TUI
in a private tmux server at fixed sizes, and renders the captures with
freeze. Needs tmux, python3 and an installed DHI toolchain.

