# ADR-0035: DHI's control files are read-only to wrapped children

Date: 2026-10-15 · Status: accepted · Companion to: ADR-0006 (path jail
and OS sandbox), ADR-0012 §3 (the OS sandbox is the boundary), ADR-0032
(autonomy envelope), ADR-0034 (forged tools). Found in the M29 review.

## Context

The agent CLI's sandbox write roots included the whole `.dhi/` tree,
because task and review worktrees and ideation artifacts live there. So
an agent could natively rewrite DHI's control files, and M29 made those
files consequential:
- its own manifest (tools, policy);
- an approved forged tool or its script (ADR-0034's "nothing runs
  unapproved");
- `grants.json` (granting itself a scope);
- `.dhi/config.toml` (raising its own budget or autonomy, re-read every
  turn);
- the spend ledger, the task cards (`tests_pass`), proposals and memory.

## Decision

1. Every child DHI wraps runs under a guarded adapter: agent CLIs, the
   `run`/verify runner and forged scripts. `.dhi/` is write-denied, and
   only the trees a child legitimately writes are reopened:
   - each task worktree (`.dhi/tasks/<slug>/…`),
   - each review worktree (`.dhi/reviews/<id>/…`),
   - `.dhi/sessions`.

   The card and review files *beside* the worktrees stay read-only.
2. DHI's own process keeps writing `.dhi/` as before. That covers the
   served MCP tools, which run in DHI rather than in the child, so
   memory, KB, tasks and proposals still change through the audited tool
   surface.
3. The mechanics:
   - **Seatbelt:** `deny file-write` on the guard, then
     `allow file-write` on the reopened subtrees. In SBPL the last
     matching rule wins, which was verified live and is pinned by a live
     test.
   - **Bubblewrap:** a read-only bind of the guard, then rw binds of each
     existing reopened directory, expanded at wrap time.
   - `"dir/*"` means "each subdirectory, not the files".
   - The human's terminal is not wrapped and is unaffected.

## Consequences

- An agent's only route to DHI state is the served tool surface with its
  scopes, approvals and proposals. "Approved means approved" now holds
  against native writes.
- An external `worktrees.root` is a separate rw root and is unaffected.
- An adapter without guard support (Noop, i.e. `security.sandbox = off`)
  runs unguarded. That mode already opts out of OS isolation by name.
