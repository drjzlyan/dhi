# F-008: Marketplace packs

Status: in progress · Milestone: M4 (P2) · Parent: [F-003](F-003-workspace.md)

## Summary

Installing new agents = copying a **pack**: a versioned directory of
agent manifests (+ future extensions) fetched from a local path or a git
URL. Packs keep org onboarding to one command instead of hand-writing
TOML per agent, while staying plain files a user can audit.

## Pack layout (v1)

```
<pack-root>/
  pack.toml          # required, strict-decoded
  agents/
    <id>.toml        # one manifest per agent, stem = identity
```

```toml
schema = 1
name = "acme-crew"        # slug; the install key
version = "0.1.0"
description = "Acme's review crew"
agents = ["agents/alice.toml", "agents/bob.toml"]
```

Reserved-for-later keys are absent by design until they ship:
`mcp` (server config), `knowledge` (KB seed docs). Unknown keys are
errors so forward-compatible formats bump `schema`.

## Semantics

- **Sources:** absolute/relative local directory, or a git URL — cloned
  through `gitcore.Clone` into a temp dir (network stays go-git,
  ADR-0008/0009). Half-clones are cleaned up; clone failures never touch
  the roster.
- **Validate-all-then-install:** every listed manifest parses before the
  first file is written. A pack that fails validation changes nothing.
- **Conflict rule:** an agent id already on disk installs only if it was
  installed by *the same pack name* (idempotent reinstall/update).
  Foreign or hand-written ids abort with the conflict list.
- **Provenance:** `.dhi/marketplace.json` records `{pack → source,
  installed_at, agent_ids}` atomically after success. Uninstall removes
  exactly the recorded files.
- **Reload:** the caller (UI/runtime) reloads rosters after install;
  the pack layer only touches disk.

## Acceptance criteria

- Install from a local dir registers its agents; doctor reports healthy
  provenance.
- Reinstalling the same pack updates its agents in place; installing a
  different pack claiming an existing manual agent fails without side
  effects.
- Uninstall removes exactly the pack's agents and its provenance entry.
