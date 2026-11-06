# F-139 — OSC 9 setting

Status: done · gap G-E8

M37 (F-117) emits an OSC 9 desktop notification for a toast while the terminal is blurred.
This adds a Settings toggle to turn that off (default: on, as shipped).

## Behaviour
- Top-level `desktop_notifications` (bool, default true), in `config.toml` and the Settings
  CONFIG section directly under `reduced_motion` (label "desktop notifications").
- Live switch follows the `theme.Motion` precedent: `settings.Config.Apply()` calls
  `theme.SetNotify`; `internal/tui/app/toast.go` reads `theme.Notify` at emit time. Settings
  never imports the app; no package-level state lives in `app`.
- Takes effect immediately; persisted by the normal Settings save.

## Acceptance
- Default on, a layer turns it off, `Apply` drives `theme.Notify` (`TestDesktopNotificationsDefaultAndApply`).
- Settings row toggles live and persists (`TestDesktopNotificationsToggle`).
- OSC 9 only when blurred and on (`TestOSC9RespectsSetting`).
- Row-count-dependent Settings tests updated (`TestTabWidthCyclesAndPersists`, `TestLineNumbersToggle`).
