# STATE — current position

Updated: 2026-08-24 (session: M4 P2 — org/marketplace/standards services)

## Where we are

**M4 P0+P1+P2-services COMPLETE and committed (a647b0e, 6c2f3ca,
e1f6cc6, 8aa8e98; `make verify` green).** All of P2's domain work has
landed: org registry, crew CRUD with archive dir, Runtime.Reload with
sidebar refresh, marketplace packs per F-008, layered coding standards
injected into every agent turn. Remaining in P2 is UI only (org panel,
pack install flow, standards editors). After that: P3 channels UI.

## Just finished

- `internal/agentkit/org`: `.dhi/org.toml` (strict, atomic, Subscribe);
  crew.go CreateAgent/UpdateAgent/ArchiveAgent(→.archived/)/RestoreAgent
  over manifest.Marshal round-trip-checked writes.
- `internal/agentkit/pack`: F-008 install path|git (go-git clone to temp,
  half-clone cleanup), validate-all-then-install, same-pack update vs
  cross-pack conflict refusal, `.dhi/marketplace.json` provenance,
  Uninstall removes exactly recorded ids.
- `internal/agentkit/standards`: builtins→workspace→teams(sorted)→agent
  extend|replace; Resolve reads fresh per turn; Save validates slugs;
  Inspect for tooling. runtime.Config{Org,Standards} injects block after
  grounding; cmd/dhi loads org best-effort, Standards always on when a
  roster exists.
- Doctor `Standards()` suite: parse-failure warns (silent builtin
  fallback at runtime), dangling team/agent refs warned by name.
- Runtime.Reload(roster): buildEntry extracted from New; swap under mu;
  Changes() ping drives chatModel.refreshRoster (channel selection kept).

## Gotchas learned

1. Rename-modal targets must be captured at open time — deriving them
   from the live input buffer breaks as soon as the user types.
2. Buffer identity lives in openVPath/openPath too; clearing bufs alone
   leaves ghost tab titles.
3. Async turn tests must waitReply before asserting provider.Calls().
4. Replace-mode semantics tripped my own tests twice: replace keeps ONLY
   built-ins + override entries; write assertions accordingly.
5. os.CreateTemp needs its dir to pre-exist (standards Save mkdir).
6. Carried: macOS /var→/private/var vs git output paths; quiet git eats
   stderr on exit 1; sed bulk renames need re-grep; shim recursion
   guard; RawMessage decode for dup JSON tags; deny-all policies.

## Next up (P2c UI, then P3 channels)

1. Workspace view: org panel (create/edit teams+leads, archive/restore
   agents), pack install (a→URL/path modal) + uninstall listing,
   standards editor rows + effective-rules preview.
2. Settings surface: standards section sharing the same store seam.
3. Then P3 channels UI on the workspace floor (rail/transcript/thread/
   composer over bus; posting through Runtime.Handle).
4. Goldens regenerate deliberately for every visual chunk.

## Open questions for user

- None blocking.
