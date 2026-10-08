# F-045: Starter teams + personas

Status: complete (M25 P4) · Builds on F-027 (behaviour library),
F-043 (wizard), F-042 (conventions).

## Problem

A new workspace has no employees. Creating a useful team means hand-writing
several manifests, picking roles/skills/tools, and wiring a team and lead.
And "who" an employee is — how they talk, how much they say — is only
free-text in `system`.

## Design

### Personas (a library kind next to roles and skills)
A **persona** is a communication style, independent of the job:

```toml
# .dhi/personas/<slug>.toml  (local shadows built-in, like roles/skills)
schema = 1
description = "Patient, explains the why"
tone = "patient and encouraging"
verbosity = "thorough"            # terse | balanced | thorough
traits = ["explains the reasoning behind a change", "offers one clear next step"]
guidance = """optional free text appended verbatim"""
```
Built-ins: `pragmatic` · `mentor` · `meticulous` · `friendly` · `terse`.
`library.Store` gains `Persona(slug)` / `Personas()`; `.dhi/personas/` is
reserved by `workspace.Create`.

Agent manifests gain **schema 6** `persona = "<slug>"` (optional; a dangling
slug loads and doctor warns by name, like role/skills). `behavior.Compose`
renders it as a **Voice** block after the role template and before skills, so
the order is: manifest system → role → voice → skills → (runtime) grounding,
memory, standards, conventions, workflow. Role says *what the job is*, persona
says *how they speak*; they never overwrite each other.

`model = "default"` is a sentinel meaning "the CLI's own default model"
(adapters previously forwarded whatever string was stored, so a starter team
could not name a model that exists for every CLI).

### Starter teams (`internal/agentkit/starter`)
Embedded templates; each is a team plus its employees:

| Template | Team | Employees |
|---|---|---|
| `solo` | `crew` | a fixer + a reviewer (you lead) |
| `squad` | `squad` | planner (lead), fixer, reviewer, scribe |
| `studio` | `studio` | planner (lead), two fixers, reviewer, scout, scribe |

Each employee: `id`, `name`, `role`, `persona`, `skills`. Tools and sandbox
policy come from the role (never from the template), so a template can't grant
more than its roles allow. `starter.Apply` writes manifests through the
existing `org.CreateAgent` (validated round-trip), creates the team with its
lead and members, skips ids that already exist (reported, never overwritten),
and is all-or-nothing on validation: every manifest is validated before the
first file is written.

Engine: employees inherit the workspace default engine (no `engine` key in the
manifest) so a teammate on a different CLI isn't forced onto yours; the wizard
sets the default engine in the user's workspace config from the detected CLIs.

### Wizard step "team"
Shown only when the workspace has no agents. A `kit.Form` with `team`
[solo|squad|studio] and `engine` [detected CLIs… | decide later], over a live
preview (name · role · persona, who leads). `enter` applies, `esc` skips.
Applying marks the run as changed → relaunch (ADR-0026).

## Acceptance criteria
- [x] persona cards parse strictly, local shadows built-in, bad cards become named warnings
- [x] manifest schema 6 `persona`; schema <6 + persona refuses; round-trips
- [x] Compose output includes the Voice block in the documented order; empty persona adds nothing
- [x] `model = "default"` sends no `--model`
- [x] every template validates: roles/personas/skills resolve in the built-in library, ids are slugs, lead ∈ members
- [x] Apply is atomic on validation failure, skips existing ids, creates team+lead, idempotent re-run
- [x] wizard step applies only to agent-less workspaces; relaunch flagged; engine written when chosen
- [x] default engine follows a preference order (claude, codex, opencode, cursor-agent, copilot, antigravity) and keeps a configured engine
- [x] Settings LIBRARY lists personas (read-only: they are plain files); agent form has a `persona` field; `dhi doctor` has an `agents/library` check
- [x] walked end to end in tmux (fresh HOME + project): four employees, squad team led by Atlas, doctor green

## Latent bugs found and fixed on the way
- **Policy presets were invalid.** `library.PolicyPresetJSON` emitted `list`/`search`
  ops that `sandbox.ParsePolicy` rejects, so any role-based agent would have
  failed to load the first time a preset was applied. Nothing applied presets
  until now. Fixed, with a test that parses every preset.
- **Editing an agent in Settings dropped everything the form does not show**
  (sandbox policy, scopes, workflow, engine, timeout, retries). The edit now
  starts from the stored manifest and changes only the form fields.
- `doctor` promised to name dangling role/skill references but never did; the
  new `agents/library` check does (and covers personas and malformed cards).
- A data race in the reviewer test double (`fakeCrew.handled`) made `make verify` flaky.

## Deferred
Persona authoring in the TUI (cards are plain files under `.dhi/personas/`);
per-user verbosity override; persona cards in packs; avatars; a `model` picker
(starter employees use the CLI's own default model).
