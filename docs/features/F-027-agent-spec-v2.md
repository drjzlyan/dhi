# F-027: Agent spec v2 — roles, skills, behaviour library

Status: implemented (M14, 2026-09-26)
Companion to: ADR-0016 (spec v2 + library format), F-018 (agents in
Settings), F-019 (agents from GitHub), F-023 (settings management).
Product rule: the IDE is the product — an agent is defined entirely in
the IDE, from a predefined format, against a shareable library.

## Summary

Today an agent's behaviour is one flat `system` string edited in a
six-field form. An "employee" needs a defined role, teachable skills,
and a company library of both. F-027 introduces agent spec v2: `role`
and `skills` join the manifest, roles and skills live in first-class
on-disk stores, a built-in library ships embedded in the binary, and
Settings gains a LIBRARY section where the user browses, authors, and
attaches them. Effective behaviour is one deterministic composer the
user can preview exactly as the agent will receive it.

## Part A — spec v2

- `agent.toml` `schema = 2`: adds `role` (one slug, optional) and
  `skills` (list of slugs, optional). Schema-1 files load unchanged
  (empty role/skills); unknown keys still refuse (strict decode).
- `role`/`skills` reference library slugs; a dangling reference is a
  named doctor warning, never a silent empty block.
- The Settings AGENTS form carries the new fields (P2/P5).

## Part B — roles and skills stores

- **Role** `.dhi/roles/<slug>.toml`: description, default tools
  allowlist, default policy preset, system template (supports
  `{{agent}}` and `{{workspace}}` substitution). A role is the job
  description: what tools the position holds and how it talks.
- **Skill** `.dhi/skills/<slug>.md`: markdown instruction document
  with a tiny frontmatter (name, description). A skill is a teachable
  procedure, not a persona.
- Both stores: strict decode, malformed cards → named doctor
  warnings, change subscriptions for live reload.

## Part C — effective behaviour composer

- Effective system block = manifest system + role template + attached
  skill bodies + standards (existing layered resolve) + grounding —
  one deterministic composer (`internal/agentkit/behavior`), pure and
  table-tested; the same function serves turns, profile preview, and
  doctor.
- Precedence: skills extend the role, role extends the manifest,
  standards stay the last layer (unchanged F-025-era contract).

## Part D — the library

- Built-in library embedded in the binary (registry pattern, like the
  toolchain manifest): roles reviewer / fixer / planner / scribe /
  scout; skills code-review / test-writing / docs / incident-triage.
  Built-ins are versioned with the app and render with a builtin
  marker.
- User-authored entries in `.dhi/` shadow built-ins by slug.
- Settings LIBRARY section: browse (source column: builtin/local, plus
  `pack:<name>` when a card came from an installed marketplace pack —
  read from `pack.Installer.Records()` provenance),
  create/edit via strict forms, attach/detach on agents.
- Packs may ship roles/skills: `pack.toml` gains `roles`/`skills`
  lists; install validates all-before-write; provenance records them.

## Acceptance criteria

- [ ] A schema-2 manifest with a role + two skills renders the full
      composed system block on every runtime (P1 dependency).
- [ ] Settings LIBRARY creates/edits a role and a skill through strict
      round-trip forms; the agent form picks them.
- [ ] Built-in roles/skills are visible without any user file; a
      user-authored shadow overrides the builtin (marked).
- [ ] Dangling role/skill refs warn in doctor by name; malformed
      library cards warn with line precision.
- [ ] Packs install roles/skills with provenance; cross-pack conflict
      refusal follows the agent precedent.
- [ ] The effective-prompt preview (`v` on an agent) is byte-exact
      with what the turn assembles.
- [ ] `make verify` green per phase.

## Deferred

- ~~Skill scripts/tools (a skill that carries executable steps)~~ —
  **landed:** a local skill may declare `script: <rel-path>` in its
  frontmatter (relative to `.dhi/skills`, no `..`); the file must be
  executable and is run directly (shebang, no shell) under the OS
  sandbox with network denied. The human runs it from LIBRARY (`r`);
  agents request it via the served `skill_run` tool (exec scope +
  approvals). `library.Store.Script` refuses builtin/absent/
  non-executable/escaping paths by name.
- Remote-only library browsing — the library is embedded + local +
  packs (F-019 flow), not a marketplace.
