# F-034: Pack registry & MCP install

Status: planned (M19) · Companion to: ADR-0022 (third-party content
trust), ADR-0019 (DHI owns the loop), F-018/F-019 (agents, packs).
Product rule: installing expertise is one visible, reversible, offline-
capable act inside the IDE.

## Summary

Users should install agents, roles, skills, standards, workflows, and
MCP servers from inside the IDE. Today packs ship agents only, from a
path or git URL, with no catalog, and external MCP is dead code. F-034
extends the pack to every installable kind, adds a signed git registry,
and makes third-party MCP servers reachable under the sandbox/scope
model.

## Part A — the pack, extended

- `pack.toml` gains `roles`, `skills`, `standards`, `workflows`, and
  `mcp_servers` lists. Install validates **all** before any write;
  cross-pack conflicts refuse by name; provenance records every kind;
  uninstall removes exactly what was recorded.

## Part B — the signed git registry

- A curated index (a git repo of pack manifests + digests) is fetched
  through the hermetic git path, digested/signature-checked, and cached.
  Browsing/search works offline from cache; install upgrades only when
  the index entry verifies.
- Settings MARKETPLACE: browse, search, inspect contents, install, and
  see provenance.

## Part C — MCP servers under trust

- An installed MCP server is reachable only by agents whose scope allows
  it (per-agent allowlist). It is spawned under the OS sandbox, reaches
  only declared network origins (otherwise denied), takes credentials
  from the OS keychain, and every tool call crosses approvals. A server
  that cannot be sandboxed is not offered.
- `doctor` reports registry freshness, pack provenance, and per-server
  sandbox/network posture.

## Acceptance criteria

- [ ] A pack installs roles, skills, standards, workflows, and an MCP
      config with one provenance record; uninstall removes exactly those.
- [ ] A registry entry whose digest fails verification refuses by name
      and installs nothing.
- [ ] An agent with the MCP server in scope can call it under the
      sandbox; an agent without it is refused; credentials never touch
      `.dhi/`.
- [ ] Browsing works offline from cache.
- [ ] `make verify` green per phase.

## Deferred

- Executable skill scripts (same trust machinery as MCP).
- Multiple/community registries and dependency resolution.
