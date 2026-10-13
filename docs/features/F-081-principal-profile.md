# F-081 — You: role, writing style, reading style

Status: done (M31 P3)

## Problem
Employees deliver work in the user's name, but nothing told them so, or
how the user writes (commits, PR descriptions, comments) or likes to read
(replies in chat). Every employee answered the same fixed way.

## Behaviour
- **Profile.** `[profile]` in the user config (personal, follows you across
  workspaces): `role`; `writing.tone` (plain|formal|casual), `writing.length`
  (brief|normal|detailed), `writing.emoji`, `writing.language`;
  `reading.order` (summary-first|steps-first), `reading.format`
  (prose|bullets), `reading.detail` (normal|brief|detailed). Empty fields take
  defaults; other values are refused by name. `settings.SaveProfile` replaces
  only the `[profile]` table and refuses a result that would not load.
- **Voice.** `principal.Profile.Voice()` replaces the fixed teammate note
  (F-072) in every system prompt with one block: "You work for <git name>
  (<role>). What you deliver is their work: it ships under their git identity
  …", the chat rules (no tool logs, fenced code), how the user reads replies,
  and how to write in their name. The runtime reads it live through
  `Tuning.Principal`; the git name is resolved once, when it first succeeds.
- **Personas.** Employees composed after onboarding (F-079) take the persona
  that fits how you read: brief → terse, detailed → meticulous.
- **Wizard.** A `you` step after identity: eight fields on defaults (enter
  keeps them) and a live preview sentence of how replies will read.
- **Settings → CONFIG.** A YOU group at the top: every profile key cycles in
  place and saves at once.

## Acceptance
- [x] Profile layers, validates, saves without touching other keys
- [x] Voice: defaults and every choice; persona mapping
- [x] Prompt carries the principal's voice (default and set)
- [x] Wizard you step saves; starts from the saved profile; golden
- [x] Settings YOU rows cycle and persist
