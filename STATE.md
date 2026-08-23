# STATE — current position

Updated: 2026-08-23 (session: M0 foundation build)

## Where we are

M0 **complete** (code + tests + goldens + docs). Not yet committed to git —
repo initialized but first commit pending user review.

## Just finished

- Scaffolded module `github.com/drjzlyan/dhi` on Go 1.26 with
  `charm.land/bubbletea/v2 v2.0.9` + `charm.land/lipgloss/v2 v2.0.6`
  (**v2 module paths are charm.land, NOT github.com/charmbracelet**).
- Theme tokens + lint test enforcing no raw colors outside theme pkg.
- Kit: Panel (cell-rendered, own border painter), Tabs, StatusLine, List.
- App shell: registry router, keys 1-9/tab/shift+tab/?/ctrl+c, help overlay.
- Surfaces: home (gradient logo) + 8 milestone placeholders.
- Golden harness ANSI-stripped under `testdata/goldens/`, env
  `DHI_UPDATE_GOLDENS=1`.

## Gotchas learned (do not re-learn these)

1. Bubble Tea v2: `Model.View() tea.View` (use `tea.NewView(s)`; set
   `.AltScreen = true` on the View struct — there is NO WithAltScreen option).
2. Keys are `tea.KeyPressMsg`; match via `.String()` → "tab", "shift+tab",
   "ctrl+c", "esc". Construct in tests:
   `tea.KeyPressMsg{Code: tea.KeyTab}`, `{Code:'c',Mod:tea.ModCtrl}`,
   runes need `{Text:"j", Code:'j'}`.
3. Never style single border chars with a style that has `.Border(...)` set —
   it draws a box around each glyph (this broke Panel once already).
4. Badge/alignment math must count the *rendered* width (`[badge]` incl.
   brackets), not the raw value.
5. `t.Fatalf` uses Goexit — cannot be recovered; golden harness exposes
   pure `Compare()` for negative-path tests.

## Next up

1. `git add -A && git commit` initial M0 (user asked to review first).
2. Start M1 per ROADMAP: toolchain.Manager skeleton + registry manifest +
   doctor check suite; then Bootstrap surface animations.

## Open questions for user

- None blocking. (Marketplace hosting model deferred to M6 spec.)
