# F-052 — Persona authoring and live reload

Status: done (M26)

## Why
Personas were "plain files" you edited by hand, and a conventions or library edit only took
effect after restarting DHI: the runtime opened the behaviour library once and captured the
conventions value at launch.

## Behaviour
- **Personas in Settings → LIBRARY.** `p` authors a persona (slug, description, tone,
  verbosity, traits separated by `;`, free guidance). `e` edits one; on a builtin it saves a
  *local copy with the same slug*, which shadows the builtin, and deleting that copy (`x`)
  restores it. Builtins are never modified. Errors name the field and keep the form open.
- **Live conventions.** `conventions.Source` replaces the launch-time value. Consumers read it
  at the moment they need a rule: the `git_commit` checks, the copyright header on a new
  file, the PR title and body, the task and review branch names, and the agents' system
  prompt. `settings.LiveConventions` re-reads when any of the four layer files changes (a
  Settings save, or a hand edit of the tracked `.dhi/conventions.toml`). A broken edit keeps
  the previous rules in force and is reported through `Live.Err()`.
- **Live library.** `library.Fingerprint` summarises the role/skill/persona cards; the runtime
  re-opens the library when it changes, so an edited persona, role or skill governs the
  crew's next turn. A turn already running keeps the snapshot it started with.

## Acceptance
- [x] persona create / edit / customize-builtin / delete round trip; bad input keeps the form open
- [x] unchanged files are not re-read; an edit is picked up; a broken edit keeps the old rules
- [x] commit rules, PR body, branch names, copyright and prompt follow the live source
- [x] the runtime picks up a new, edited and removed persona without a restart
- [x] the Settings flash no longer claims "applies on restart"
