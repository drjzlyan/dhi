# F-095 — One DHI brand identity with component roles

Status: in progress (session 60, M34 P1) · ADR-0041

## Why
The cyan/violet palette gave each view its own hue, and two views borrowed semantic colors for it
(the Ideator used Warning amber). The user asked for one new DHI identity that stays the same in
every view, where each kind of component has its own color.

## Behaviour
- Themes: `dhi-dusk` (dark, the default), `dhi-dawn` (light) and `high-contrast`. A missing `theme`
  setting follows the terminal background (dusk or dawn). The old names `dark-futuristic` and
  `light-paper` load as aliases of dusk and dawn.
- Brand: saffron primary, indigo secondary, lotus and teal tertiaries, steel neutral. Success
  (yellow-green), Warning (yellow) and Danger (red) are kept apart from the brand hues.
- Component roles in `theme.Tokens`: button (primary, secondary, ghost, danger), input (bg, edge,
  focus edge, placeholder), card (bg, edge, stripe), tab (active bg/fg), badge bg, focus, link.
- Every view renders the same focus color, tab pill and mode block. `SetSurface` only drives the
  fade-in.

## Acceptance
- [ ] Contrast: every text role ≥ 4.5:1 on the surfaces it sits on; button/tab foregrounds on their
      fills; input text and placeholder on InputBg (`contrast_test.go`).
- [ ] Distinctness: {Primary, Secondary, Success, Warning, Danger} pairwise ΔE above the test's
      threshold, in each theme.
- [ ] 256-color shade ladder still distinct.
- [ ] No raw `lipgloss.Color(` in kit, app, surfaces or branding.
- [ ] Old theme names load; the Settings row shows the new name.
- [ ] Every view's focus color is the same (replaces `TestSurfaceAccentsIdentifyViews`).
- [ ] Palette/component preview reviewed by the user (checkpoint).
