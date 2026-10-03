# F-026: UI beauty, usefulness & interaction (M13)

Status: implemented (M13, 2026-09-19 — P0–P8)
Companion to: F-025 (coherent UX — its deferred items: scrollbars, mouse),
ADR-0014 (workspace floor), ADR-0015 (UI dependency promotions).
Scope decisions (2026-09-18, user): syntax highlighting via chroma = IN;
terminal ANSI scrollback = IN; mouse support = IN (wheel + click, no drag).

## Summary

M12 made the chrome coherent; M13 makes the content beautiful, useful,
and interactive. All five surfaces + shell are in scope, phased so every
phase lands on green `make verify`. Theme owns all styling, kit owns all
primitives, surfaces consume them — no exceptions.

## P0 — Specs & decisions (this file)

- F-026 as the acceptance-criteria record; per-phase checkboxes.
- ADR-0015: promote `alecthomas/chroma/v2` + `mattn/go-runewidth` to
  direct deps (both already in the module graph via glamour — zero new
  code in the graph).

## P1 — Theme & kit foundation (consistency core)

- Theme: `Info` semantic token, `AccentDim`, keycap style, elevation
  tokens, motion token set; breakpoints move kit → theme (kit
  re-exports); `BorderFocused` distinct from `Accent`; adopt-or-delete
  dead tokens (PanelBorder dead branch, AppFrame, RadiusPad, unused
  glyphs).
- `internal/ansi`: display-cell width math (go-runewidth) replacing
  rune-count helpers in ansi + kit.
- kit: `Scroller`/`Scrollbar` (offset + position indicator); `List`
  renders `Desc`, wraps titles, fixes badge overflow, gains headers;
  `Rail` glyph slot + scroll + counts; `Modal` shadow + scroll for tall
  bodies + ellipsis clip; `Form` canonical (in-value cursor, shift+tab,
  paste) absorbing workspace/settings duplicates; `HintBar` ellipsis
  char; `StatusLine` Center + overflow truncation; `Panel` footer slot.
- `Transcript` primitive: one renderer for editor chat / workspace
  CHANNELS / ideator CHAT / reviewer threads — timestamps, day
  dividers, wrap, author styles, unread markers, glamour fences.

## P2 — Interaction & mouse foundation

- App enables mouse; nil-safe `Wheel`/`Click` seams (degrade like
  StatusContext).
- Wheel rides the P1 Scroller everywhere a cursor window exists.
- Click routes on shared primitives: rail rows, lists, tabs, lanes,
  diff lines. No drag.

## P3 — Workspace (INBOX · BOARD · CHANNELS · REPOS)

- Scrollers + indicators on all four sections; lane scroll offsets.
- Board: width-proportional card grid, badges, `S` backward status +
  `m` move-to-lane, detail pane word-wrap + overflow affordance.
- Channels: composer always visible (blurred affordance), bus.Post
  failure flashes (F-011), timestamps + new-messages separator, useful
  empty-state context column.
- Inbox: relative timestamps, distinct snooze affordance, tinted
  attention count on rail.
- Repos: branch/last-activity info, `e` open-in-editor, width-aware
  paths, InsetBg zone.
- Replay: word wrap, advertised close key, `[`/`]` close-then-switch.
- Forms on kit.Form; flash announcements deduped.

## P4 — Editor

- Syntax highlighting via chroma behind a textbuf renderer seam,
  theme-mapped token colors.
- Buffer: stable gutter + optional relative numbers; styled status row.
- LSP popups (comp/actions/hover) as floating bordered panels anchored
  at cursor (ElevatedBg + DialogEdge).
- Tab strip polish (kind glyphs, close affordance); git panel styled
  indicator + inline diff preview.
- Chat sidebar on P1 Transcript (fences, timestamps, unread parity).
- Find/replace surface (`:s`, `:%s` + interactive panel).

## P5 — Terminal (ANSI)

- Parse-and-keep common SGR (fg/bg/attrs) + cursor sequences in
  scrollback; horizontal scroll + scrollbar. Isolated phase.

## P6 — Ideator · Reviewer · Settings (consistency sweep)

- Rail counts/badges everywhere; ideator artifacts grouped + sorted,
  CHAT unread/timestamps via Transcript.
- Reviewer: per-kind diff background wash, de-dimmed context, styled
  hunk headers, colored review states, wrapped thread rows with anchor
  context, cached diffRows, spinner on busy.
- Settings: agent forms → kit.Form, profile/last-run modals scroll,
  standards `v` opens the selected row, schedule presets, import
  progress affordance, CONFIG grouping + help lines.

## P7 — Shell coherence

- Help overlay: contextual per-surface key sections + globals,
  scrollable, dynamic view count (1–9), wording parity.
- Statusline: keycap-styled global hints, dynamic count, overflow
  strategy; flash → toast semantics (auto-expiry, deduped).

## P8 — Closeout

- Deliberate golden regeneration (reviewed like code); perf benches
  (diff cache, render churn); STATE/ROADMAP closeout; `make verify`.

## Acceptance criteria

- [x] P1: ansi/kit measure display cells; theme lint green; no
  rune-count width math left in kit.
- [x] P2: wheel scrolls every pane with a cursor window (guarded key
  synthesis through the existing nav paths); cursor never walks into
  clipped rows invisibly (Scroller follow + Transcript Tail + Columns
  lane windows); tab-bar click selects surfaces; body-local Click seam
  routed.
- [x] P3: all four workspace sections take real pane width; board grid
  adapts with lane scroll + `S`/`m`; composer visible when blurred;
  failed post flashes; replay wraps words and closes on `[`/`]`.
- [x] P4: buffers syntax-colored (chroma, theme-mapped, seq-cached);
  popups bordered/floating at the cursor; chat renders fences +
  timestamps; `:s`/`:%s` work.
- [x] P5: terminal scrollback keeps color + cursor moves (internal/vt).
- [x] P6: rails carry counts; diff has kind washes + styled hunk
  headers + cached flatten; settings forms on kit.Form; display
  dialogs scroll; standards `v` previews the selected row.
- [x] P7: help overlay contextual + dynamic count + keycaps; statusline
  keycap hints; toasts expire (4s) with dedupe.
- [x] Every phase: unit tests + deliberately reviewed goldens +
  `make verify` green. Perf: diffRows cache benchmarked 6× faster warm
  (4.4µs vs 27.5µs at 20 files × 40 lines, 50× fewer bytes).

## Deferred (stays out)

- ~~M7 LSP nav (definition/references)~~ — **landed** (F-009/F-030
  LSP verbs); ~~ideator diagram preview~~ — **landed** (M18 mermaid).
  Open: mouse drag; per-surface identity accent ramps (one ramp keeps
  consistency).
