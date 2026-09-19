# ADR-0016: Agent spec v2 — roles and skills as first-class library entries

Date: 2026-09-19 · Status: accepted · Serves: F-027.
Bounds: ADR-0011 (strict data), ADR-0012 (host CLIs), ADR-0005
(hermetic, no system dependencies).

## Context

An agent's behaviour is one flat `system` string. The product goal is
an office of employees the CEO defines entirely in the IDE: a
predefined format for people, reusable across the company, shareable
through packs (F-019), and previewable exactly as delivered. Surveys
found no role/skill/persona concept anywhere; teams only select
standards layers; memory and KB exist but are inspection-only.

## Decision

1. **Spec v2.** `agent.toml` gains `role` (one slug) and `skills`
   (list). `schema = 2` marks new files; schema-1 loads unchanged with
   empty role/skills — forward- and back-compatible without migration
   tooling. Strict decode still refuses unknown keys.
2. **Roles are job descriptions.** `.dhi/roles/<slug>.toml`:
   description, default tools allowlist, default policy preset, system
   template with `{{agent}}`/`{{workspace}}` substitution. A role does
   not silently widen tools: its allowlist is the *default at create
   time*, and the manifest's own list stays the runtime truth (the
   form prefills from the role; edits are explicit).
3. **Skills are teachable procedures.** `.dhi/skills/<slug>.md` with
   tiny frontmatter (name, description); the body is instruction
   markdown. No executable content.
4. **One composer.** `internal/agentkit/behavior.Compose(manifest,
   library, standards, grounding)` is the single source of the
   effective system block — deterministic, pure, table-tested — used
   by turns, the Settings preview, and doctor. Skills extend the
   role, the role extends the manifest, standards remain the final
   layer. Attachment order is manifest order; duplicates are removed
   with the first occurrence winning.
5. **The library is embedded + local + packed.** Built-in roles/skills
   ship in the binary (registry pattern like the toolchain manifest);
   user files in `.dhi/` shadow built-ins by slug (marked); pack.toml
   may ship both (all-before-write install, provenance per F-008).
6. **Dangling references are warnings, not errors.** A manifest naming
   a missing role/skill loads fine and turns fine (the block renders
   without it) while doctor warns by name — consistent with the
   standards-layer refusal philosophy: *malformed* content refuses a
   turn; *absent optional* content degrades visibly.

## Consequences

- `internal/agentkit/behavior` is new and pure; runtime gains one
  injected library lookup seam (no service import).
- Settings gains a LIBRARY section and the AGENTS form gains role/
  skills fields (P2/P5).
- Doctor gains library suites (malformed cards line-precise, dangling
  refs warned).
- Pack format v2 is additive; v1 packs install unchanged.
- The effective-prompt preview must be byte-exact with turns — the
  same function guarantees it; a divergence is a bug, not a drift.
