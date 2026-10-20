# F-100 — README recordings of real flows

Status: planned (M34 P8) · ADR-0041

## Why
The user wants recordings of real flows (onboarding a workspace, every view, updating settings) in
the README, in place of static screenshots.

## Behaviour
- VHS tapes in `scripts/tapes/` (requires `brew install vhs`). `scripts/record.sh [flow]` builds
  dhi, seeds a dummy project into a temp dir, sandboxes HOME/XDG, and runs the tapes. `make record`
  runs every flow.
- `scripts/demo -fresh` seeds an "acme" storefront (`api/` Go, `web/` TS) with no `.dhi/`, so
  onboarding starts from zero. The existing seeded mode is used for the view tours. Recordings spend
  no tokens.
- Flows: onboarding, editor, views, settings, and optionally agent-task.
- Output goes to `docs/assets/recordings/`, as GIF (inline in the README, about 6 MB budget per
  flow) and MP4. The old PNGs are removed; `screenshots.sh` stays as a verification tool.

## Acceptance
- [ ] Every flow records from a clean checkout with `make record`.
- [ ] Frames checked: no unexpected gate, no tofu glyphs, size budget held.
- [ ] README shows the GIFs with captions and alt text.
