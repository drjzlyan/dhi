# F-091 — Employees suggest skills; you install them, verified, here or everywhere

> **Amended (session 61, ADR-0043):** suggestions name a marketplace plugin or a GitHub/git URL instead of a registry entry; DHI pins the commit and shows every skill and script in full (F-105).

Status: done (M32 P6) · ADR-0039, ADR-0022

## Behaviour
- **`skill_search {query}`** lists entries in the DHI registry. That is the
  only source skills are installed from, per your decision.
- **`skill_suggest {entry, reason}`**:
  1. DHI fetches the entry (`registry.Fetch`) and refuses by name unless
     its content digest matches the index. The index must verify against a
     pinned publisher key when one is pinned.
  2. It reads every skill in the pack with `pack.ReadSkills`: the doc
     parses, is at most 64 KiB, and any `script:` exists beside it (64 KiB
     cap).
  3. It records a review in `.dhi/proposals/skills.json` (`skillprop`):
     source, version, sha256, how it was verified, each skill's full text,
     each script in full, and what the pack ships besides skills (not
     installed).
  4. It posts `@you … Review it in Settings → PROPOSALS`.
  - A pack without skills is refused; install it from MARKETPLACE.
- **PROPOSALS → SKILLS:**
  - The row shows signed or unsigned, the skill count and "ships a
    script".
  - `v` shows the whole review.
  - `y` asks "install for: this workspace / every workspace" and "add to
    <agent>: yes / no". The install re-fetches and requires the reviewed
    digest, so changed content is refused. It writes docs and scripts to
    `.dhi/skills` or your `<config>/dhi/skills`, attaches if chosen, and
    resolves the record (decision, scope, time).
  - `n` declines.
  - A global install never attaches by itself: attached skills ride in
    every prompt.
- **Your skills** (`library.UserSkillsDir`): loaded in every workspace
  after built-ins, labelled `yours`. A workspace's own card of the same
  slug wins. Their scripts run via `skill_run`, and the dir is a
  read-only, exec-allowed sandbox root (`boot.Input.ReadOnly`). Deleting
  one in LIBRARY removes it everywhere.
- **Your registry** (`registry.UserDir`):
  - Keys pinned there are trusted (and enforced) in every workspace.
  - Its cached index serves a workspace that never refreshed its own.
  - MARKETPLACE `r` (refresh) and `t` (pin key) ask "for: this workspace /
    every workspace".
- **Pack fixes:** pack installs now copy skill scripts (executable) and
  record them, so uninstall removes them.

## Not changed
- Installing a whole pack from a path or URL in PACKS stays as it was. You
  start that install yourself and see the source. Agents can only
  suggest registry entries, which are always digest-checked.

## Acceptance
- [x] registry: yours serves a workspace without an index; your pinned key is enforced in every workspace; tampered content refused at fetch
- [x] pack: scripts installed beside skills and removed on uninstall; a missing script refuses
- [x] review records scripts in full; install here and for every workspace; another workspace sees `yours`
- [x] install refuses content that differs from what was reviewed; a skill-less pack is refused
- [x] PROPOSALS: row, full review, y → choose scope → installed and resolved
- [ ] live: suggest from a local signed test registry, approve for every workspace, use it in a second workspace (M32 P7)
