# STATE — current position

Updated: 2026-08-24 (session: M4 P1 — member management)

## Where we are

**M4 PHASE 1 COMPLETE (`make verify` green; P0 committed a647b0e, P1
pending commit).** Workspace roster is now live-manageable end-to-end:
add (local dir or git URL→clone), rename, remove-with-confirm from the
Workspace view; `.dhi/workspace.toml` rewrites atomically; editor tree/
search/fuzzy/terminals/buffers reconcile without restart via workspace
change subscriptions. Next: P2 org + marketplace + coding standards.

## Just finished

- `internal/workspace`: RWMutex-guarded roster (`Members()` snapshots;
  field unexported — all readers migrated), atomic `Save` keeping
  relative paths under root, AddMember/RemoveMember/RenameMember
  persisting BEFORE memory commit, last-member invariant, `Subscribe`
  change events (Added/Removed/Renamed).
- Editor: roster watcher pump (`membersChangedMsg`), reloadMembers()
  rebuilds roots/rows/index, closes removed members' buffers+terms,
  resets openVPath identity.
- Workspace view rewrite: members pane + modal forms (add incl. async
  clone with half-clone cleanup, rename, remove confirm), dim roadmap
  rows for P2–P5; nil-ws hero preserved.
- `gitcore.Clone` (go-git, in-process); app-shell golden regenerated
  deliberately (hero + "not inside a DHI workspace").

## Gotchas learned

1. Rename-modal targets must be captured at open time — deriving them
   from the live input buffer breaks as soon as the user types.
2. Buffer identity lives in openVPath/openPath too; clearing bufs alone
   leaves ghost tab titles.
3. macOS /var→/private/var still applies to any path comparison against
   git output (carried).
4. sed bulk-renames of `.Members` → `.Members()`: always re-grep after;
   doctor slipped through the first pass (caught by vet).
5. Carried: shim recursion guard; nil Events chan wiring; bounded
   shutdown; RawMessage decode for duplicate JSON tags; net.Pipe read
   pumps; deny-all policies without policy_json; quiet git swallows
   stderr on exit 1.

## Next up (P2 org + marketplace + standards)

1. `internal/agentkit/org`: `.dhi/org.toml` sidecar (teams, leads,
   archived flags); strict decode; LoadDir-style API + tests.
2. Agent CRUD service: write `.dhi/agents/<id>.toml` through
   manifest.Parse validation; archive flag honored by loaders;
   `Runtime.Reload(roster)` under per-agent turnMu; chat sidebar
   roster refresh event.
3. Marketplace packs: pack.toml spec (agents [+mcp.json] [+kb seeds]);
   install path|git → validate all manifests → copy into .dhi/agents/;
   idempotent reinstall; uninstall. Doctor pack provenance checks.
4. Coding standards: `.dhi/standards.toml` layers (workspace/team/
   agent; extend|replace), pure resolver + table tests, injection at
   runtime.prompt() grounding point (Config seam from cmd/dhi),
   Settings section UI, doctor reference warnings.

## Open questions for user

- None blocking. (P1 commit pending this session's final step.)
