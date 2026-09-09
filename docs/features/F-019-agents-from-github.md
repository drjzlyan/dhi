# F-019: Add agents from GitHub — one flow, manifest or pack

Status: planned (M10 P2) · Milestone: M10 · Depends on: F-018
(settings agents), F-008 (packs). Inspired by: the user goal — "they
can add agents from github".

## Summary

The service layer already installs agent packs from a git URL
(`pack.Installer` → hermetic `gitcore.Clone` → validate everything →
track provenance in `.dhi/marketplace.json`), but only packs, and only
from the ORG pane. F-019 adds a single "add from source" flow that
accepts a local path or a git URL and auto-detects what it points at:
a pack (`pack.toml`) installs as today; bare agent manifests import
into the roster. One input, one flow, in Settings (ORG PACKS keeps
pack-install too).

## Part A — the flow

`g` on the Settings AGENTS section (and the existing ORG packs form)
opens one source form: a local path or a git URL, optionally with a
`#sub/path` fragment naming a subdirectory to import from.

1. Resolve + clone/pull to a temp dir via `gitcore.Clone` (hermetic
   shim, ADR-0005). A missing shim refuses by name — never shells out
   to host git (F-010).
2. Detect: `pack.toml` present → pack install (existing validated
   flow, provenance tracked). Otherwise every `*.toml` under the
   target dir (recursively) is treated as a candidate agent manifest.
3. Validate ALL candidates first (manifest.Parse strict): any invalid
   manifest refuses the whole import with the file + reason named —
   validate-before-first-write, the pack rule.
4. Import each valid manifest via `org.CreateAgent`: a duplicate id
   skips with a named reason ("already exists", "is archived —
   restore instead"). Summary flash: `imported 2 from <source>;
   skipped 1 (scout: already exists)`.
5. The reload pump (F-018) makes imported agents live immediately.

## Part B — provenance and ownership

Imported single manifests are ordinary roster files: hand-editable,
CRUD-manageable in Settings, no marketplace tracking (unlike packs,
whose uninstall removes exactly what install wrote). This is stated
in the import summary so ownership is never a guess. Pack installs
keep their provenance flow unchanged.

## Acceptance criteria

- Fixture repos (local paths exercise the same clone/inspect code):
  a repo with `pack.toml` installs as a pack; a repo with bare
  manifests imports them; a repo with both prefers the pack; a
  `#sub/path` fragment scopes the import.
- Duplicate ids skip with named reasons; invalid manifests refuse the
  whole import naming file + reason; nothing half-writes.
- A missing git shim refuses by name (test with the refused seam).
- Imported agents are mention-addressable without restart (pump test).
- Settings + ORG tests; goldens for the source form and the summary
  flash; `make verify` green.

## Deferred

- A curated remote registry/index (discovery, stars, versions) — the
  flow is ready for one; nothing ships until there is a registry to
  trust.
- Auth-scoped private repos (ssh agent forwarding) beyond what the
  hermetic shim already supports.
