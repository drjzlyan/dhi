# F-034: Pack registry & MCP install

Status: done (M19) · Companion to: ADR-0022 (third-party content
trust), ADR-0019 (DHI owns the loop), F-018/F-019 (agents, packs).
Product rule: installing expertise is one visible, reversible, offline-
capable act inside the IDE.

## Delivery notes (M19)

- Pack schema 2 ships `roles`, `skills`, `standards`, `workflows` and
  `mcp_servers`; every kind validates before the first write, conflicts
  refuse by name, provenance records all kinds, and uninstall removes
  exactly what it wrote (standards are workspace-layer lines the pack
  added, removed by value on uninstall).
- `internal/agentkit/mcpserver` is the `.dhi/mcp/<slug>.toml` card store
  (stdio/http, declared env NAMES only, declared origins).
- `internal/agentkit/registry` is the signed index: `index.toml` pins a
  SHA-256 content digest per pack; refresh clones through the hermetic
  git path and caches under `.dhi/registry` (offline browse); install
  recomputes the canonical digest and refuses by name on mismatch.
  Digest pinning is the baseline trust anchor; **Ed25519 index
  signatures** are now enforced: a publisher ships a detached
  `index.toml.sig`, the user pins the publisher key in
  `.dhi/registry/trusted_keys` (MARKETPLACE `t` pin / `T` clear), and
  every refresh + cached read verifies the signature. With a key pinned
  an unsigned/tampered index refuses by name; with no key the registry
  stays in its documented digest-only mode (`sign.go`).
- Settings MARKETPLACE browses/searches/inspects/installs; `doctor`
  reports `registry`, `packs/provenance` and `mcp/servers` posture.
- `internal/agentkit/mcpbridge` serves installed servers to an agent
  under the trust model (per-agent allowlist, sandboxed stdio spawn,
  declared-origin network, keychain/env credentials by name, network
  scope + approvals on every call), composed into the per-turn loopback
  served tool set.

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
  the index entry verifies.- Settings MARKETPLACE: browse, search, inspect contents, install, and
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

- [x] A pack installs roles, skills, standards, workflows, and an MCP
      config with one provenance record; uninstall removes exactly those.
- [x] A registry entry whose digest fails verification refuses by name
      and installs nothing.
- [x] An agent with the MCP server in scope can call it under the
      sandbox; an agent without it is refused; credentials never touch
      `.dhi/` (cards store env NAMES only).
- [x] Browsing works offline from cache.
- [x] `make verify` green per phase.

## Deferred

- ~~Executable skill scripts~~ — **landed** (F-027).
- Multiple/community registries and dependency resolution.
- ~~Cryptographic signature verification of the index~~ — **landed**
  (Ed25519 index signatures; `t`/`T` in MARKETPLACE).
