# STATE — current position

Updated: 2026-09-01 (session 4: M6 Ideator full shipped)

## Where we are

**M6 COMPLETE (F-004, ADR-0010; `make verify` green).** The Ideator
replaced the placeholder (view 3): sessions with invited agents + implicit
bus channels + artifact folders, read-only artifact tree with
draft→reviewed→approved/rejected statuses, glamour markdown preview,
session CHAT with @mention dispatch, and the reject→revision loop back
to the authoring agent. Agents produce artifacts via the new reserved
`.dhi` vpath pseudo-member (jailed, policy-gated). Next milestone: **M7
Hardening & polish** (rich LSP, perf, animation) — or close M5 by
dispatching the pending `release-git` (see ROADMAP P0 "On you").

## Gotchas added this session

1. Policy rules are ROOT-RELATIVE: a manifest rule `sessions/**`
   also matches `sessions/` dirs inside member repos — accepted (both
   stay in the jail/user repos; documented in ADR-0010).
2. glamor dark style renders H2 with a literal `## ` prefix (H1 has
   none) — preview goldens must expect it; both editor and ideator share
   `internal/preview`, so behavior is consistent.
3. `bus.History(ch,0)` top-level-only semantics made dedicated
   per-session channels the right call (not threads inside #general) —
   agent context windows see the whole ideation conversation.
4. Ideator `open()` doesn't set the section; key handlers that need
   ARTIFACTS must set `m.sec` explicitly (tests hit this twice).
5. requestTurn must call crew.Handle SYNCHRONOUSLY (runtime fans out
   its own goroutines) — a goroutine wrapper races fake-crew assertions.
6. Artifact sort is lexicographic ("option-btree.md" < "option-log.md")
   — fixture assertions must match sort order, not intent.

## Just finished (M6)

- `internal/workspace`: reserved `.dhi` vpath alias (ParseVPath/Resolve/
  VPathFor; member `.dhi` can't collide — names start [a-z0-9]) +
  `DirSessions` reservation. `internal/agentkit/runtime`: `.dhi` added as
  a jail root in New/Reload; grounding line tells agents the dotdir
  convention. Tests: alias round-trip, traversal rejection, policy
  default-deny + sessions/** allow via write tool.
- `internal/ideation` (new): session TOML cards `.dhi/sessions/<slug>.toml`
  (SchemaVersion 1, strict decode, malformed→warnings, mutate/
  writeCard/commit/Subscribe — full tasks/review blueprint). Session =
  name/topic/agents/channel + `[[artifact]]` records (path, author,
  status, notes, hash). `Scan()` merges the filesystem: new file→draft,
  hash change→draft (keeps author+notes), vanished→dropped. Transitions:
  MarkReviewed/Approve (approved is terminal)/Reject(notes required).
  Slugify keeps [a-z0-9._-].
- `surfaces/ideator` (new): rail+pane dock; SESSIONS (n modal: name/
  topic/agents CSV · enter open+scan · x remove confirm), ARTIFACTS
  (enter preview · v reviewed · a approve · r reject-notes modal ·
  s rescan), PREVIEW (memoized glamour md / raw, j/k scroll), CHAT
  (i composer, posts to session channel + crew dispatch, j/k scroll).
  agent.go: dispatchRevision (posts "@author please revise `<vpath>`:
  notes" → requestTurn), mirrorBus claims artifact authorship from
  agent chatter referencing `.dhi/sessions/<id>/...` vpaths.
- cmd/dhi: ideation store wired into ideator Deps (placeholder removed).
  doctor: `sessions/store` suite (malformed cards, dangling invites) +
  `.dhi/sessions` in the reserved-dir checks.

## Gotchas from M5 (carried, still load-bearing)

1. go-git Push needs a REGISTERED remote (member clones always have
   origin); test fixtures use bare local origins for hermetic push flows.
2. Test fakes must fully implement seams — zero-value stubs silently
   break assertions.
3. Go closure aliasing: read form fields BEFORE closeForm() resets them.
4. Test event pumps must NOT re-arm listeners (`go cmd()` steals events).
5. New confirm-modal kinds must join formKey's confirm branch.
6. Guard modal-opening keys behind service presence.
7. bus.History(ch,0) excludes threaded rows; thread views stitch roots.
8. macOS /var→/private/var EvalSymlinks; rename-modal targets captured
   at open time; waitReply before provider.Calls().

## Next up (M7 per ROADMAP)

1. Rich LSP features (hover, rename, refactor, code actions) — build on
   `internal/lsp` stdio client + gopls hermetic build.
2. Performance passes (large repos, many buffers); OS-sandbox adapters
   on by default (sandbox seam exists since M1).
3. Animation polish (bootstrap/transitions) + reduced-motion everywhere.
4. M5 addendum leftovers: dispatch `release-git` for v2.55.0 (user step)
   — doctor degrades visibly until merged.
5. Post-M6 backlog (F-004): artifact export via MCP/repo paths; diagram
   (SVG/mermaid) preview; `.dhi`-per-root policy scoping refinement.

## Open questions for user

- None blocking. M5 closure tag still pending user request; same for M6.
