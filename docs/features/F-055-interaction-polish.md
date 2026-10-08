# F-055 — Interaction polish: clicks, notices, identity, readable data

Status: done (M27 P3). Closes the F-041 deferrals (searchable help shipped in F-054).

## Behaviour
- **Clicks everywhere** (`kit.HitMap`):
  - Views record click zones while they render, so a click resolves against
    exactly what is on screen.
  - Board: a lane header focuses the lane; a card selects it; a second
    click opens it on its floor.
  - Inbox, reviews, review files and Ideator sessions: a click selects; a
    second click does what `enter` does.
  - Diff lines move the diff cursor.
  - The compact section strip switches sections.
  - Every hint in a pane's keymap row acts like its key
    (`kit.HintKeyAt`).
  - Editor file tree: select, then a second click opens the file or toggles
    the folder.
  - Settings CONFIG rows: select, then a second click changes the value.
  - Clicks are ignored while a dialog is open.
- **`kit.Notice`** (what / why / fix) replaces bare `(… unavailable)` lines.
  It covers sessions, reviews, channels, the library and the ideation bus.
  **`kit.Loading`** (spinner + skeleton rows) replaces `loading diff…` and
  `working…`.
- **Identity accents:** the active tab and focused panel edges use the
  view's colour. Workspace is cyan, Editor violet, Ideator amber, Reviewer
  green, Settings neutral. High contrast keeps one focus colour.
- **Initial theme follows the terminal:** when no config layer names a
  theme, the app asks the terminal for its background and picks dark or
  light. Choosing a theme in Settings records it. `Save` writes `theme`
  only when it was chosen. Settings shows `auto · <theme>` until then.
- **Wide inbox:** at a pane width of ≥120 (about a 150-column terminal) the
  selected item's preview docks beside the list and follows the cursor.
- **Help alias:** `ctrl+/` (and the `ctrl+_` many terminals send for it)
  opens help.
- **Readability fixes** found with real data (`scripts/demo`):
  - Board cards lead with the title. The assignee joins from 30 columns,
    the slug from 48, and "unassigned" is not printed.
  - The detail pane is a quarter of the width (32–48 columns).
  - `kit.Restyle` keeps a row's background and selection highlight through
    the resets of its inner segments.
  - Chat bodies are in the text colour; the author label carries who spoke
    (you: cyan, agents: violet).
  - The editor expands tabs to 4-column stops. Raw tabs pushed Go code past
    the panel edge; the layout contract now rejects any raw tab.
  - The review header no longer abbreviates branch names as if they were
    SHAs.
  - Activity reads as sentences ("set priority high").

## Acceptance
- [x] click tests: board cards (docked + compact), strip, dialog guard,
      hint-bar key, editor tree select/open; `HitMap`, `HintKeyAt`, `Restyle`
      unit tests
- [x] `Notice`/`Loading` render what/why/fix at exact width
- [x] accents distinct per view; high contrast unchanged
- [x] theme follows the background only without a chosen theme; `ThemeSet`
      from layers, never saved unchosen
- [x] wide inbox preview golden + cursor-follow
- [x] tab expansion + `visCol` unit tests; contract fails on a raw tab
