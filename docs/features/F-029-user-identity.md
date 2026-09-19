# F-029: Strict user identity — the office is private, the work is the user's

Status: planned (M14 P0 accepted 2026-09-19)
Companion to: F-005 (reviewer ownership-split, R4), ADR-0012 (host
agent CLIs), ADR-0011 (no silent fallbacks).
Product rule: inside the IDE agents collaborate; everything that
reaches the outside world is the user.

## Summary

The user's accounts are the only identity that crosses to GitHub and
git remotes. F-029 closes the last leaks: synthetic commit identities
(`you <you@dhi>` on task cards, `dhi <dhi@local>` in the editor
panel) become the user's real git identity, agent handles stop
appearing in external-PR comment bullets, and the PR-body DHI footer
is dropped. Agent review text already posts under the user's gh
account; that stays, with no attribution ever added.

## Part A — git identity

- One identity resolver (gitcore): user.name/user.email from the
  user's git config; a named refusal when unset (doctor names the
  fix; commit paths refuse with the same message).
- `tasks.Store.Commit` (task-card commit) and the editor git panel
  use the resolved identity. No synthetic identities remain in the
  repo.
- Agent-CLI commits already inherit the user's git config (host env
  pass-through) — unchanged, now documented as the contract.

## Part B — GitHub posts

- External-PR consolidated comments: bullets lose agent handles —
  the comment body reads as the user's own summary (content from
  agent threads, voice of the user).
- PR body on CreatePRForBranch: the DHI footer is dropped entirely.
- Own-PR threaded comments and issue comments continue verbatim under
  the user's gh account with no agent attribution added or stripped
  (R4's attribution-stripping keeps legacy compatibility).
- Pending review batch (`s`) remains the user's explicit gate.

## Part C — doctor

- New `identity` row: warns when git user.name/email is unset (naming
  the fix: `git config --global …`), and fails when a commit path
  would refuse (drift between doctor and reality is the bug).

## Acceptance criteria

- [ ] A task-card commit and an editor commit on a machine with git
      identity set produce commits authored by that identity (table-
      tested against a fixture config).
- [ ] With no git identity, commit paths refuse naming the fix, and
      doctor reports the same.
- [ ] An external-PR consolidated comment contains no agent id; the
      PR body contains no DHI footer.
- [ ] No attribution is added to any outbound GitHub text.
- [ ] `make verify` green per phase.

## Deferred

- Per-agent git identities or commit trailers (Co-Authored-By) — the
  product rule is the opposite: one outward identity.
