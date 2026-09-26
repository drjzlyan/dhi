# ADR-0022: Third-party content trust — signed git index, sandboxed MCP

Date: 2026-09-26 · Status: accepted · Companion to: ADR-0005 (hermetic
toolchain), ADR-0011 (no silent fallbacks), ADR-0019 (DHI owns the
loop), ADR-0006 (sandbox), F-018/F-019/F-020 (agents, packs, parity).
Serves: F-034.

## Context

Users must install agents, skills, roles, standards, workflows, and MCP
servers from inside the IDE. Today packs ship **agents only**, from a
local path or a git URL, with no catalog; the library is local-authoring
plus embedded builtins; the outbound MCP client is dead code. Adding
third-party executable content (MCP servers) to a hermetic, local-first
tool raises a trust question that must be answered by design, not by
hope.

## Decision

1. **One installable unit, one provenance record.** A pack may ship
   agents, roles, skills, standards, workflows, and **MCP server
   configs**; install validates all-before-any write, refuses cross-pack
   conflicts by name, and records provenance in `.dhi/marketplace.json`
   (the existing model, extended). Uninstall removes exactly what was
   recorded.
2. **The registry is a signed git index.** A curated index (a git
   repository of pack manifests + digests) is fetched through the
   hermetic git path and cached; entries are verified against pinned
   signatures/digests before install. This reuses DHI's pin-pipeline
   pattern and keeps ADR-0005's hermetic rule; there is no hosted
   service dependency. Browsing is offline-capable from the local cache.
3. **Third-party MCP servers are untrusted by default.** An installed
   server is reachable only by agents whose scope allows it (per-agent
   allowlist), is spawned under the OS sandbox, reaches only declared
   network origins with network otherwise denied, takes credentials from
   the OS keychain (never `.dhi/`), and has every tool call pass the
   approvals queue. A server that cannot be sandboxed is not offered.
4. **Nothing installs or executes silently.** Installation, first
   execution, and any scope expansion are visible, named acts in the
   approvals ledger and the doctor report.

## Consequences

- `internal/agentkit/pack` gains roles/skills/standards/workflows and
  MCP-config fields; `internal/agentkit/library` gains pack-sourced
  entries; the outbound `internal/mcp` client is revived and policy-
  gated (ADR-0019).
- The library/marketplace becomes a real catalog with search, while
  remaining functional offline from cache.
- `doctor` reports registry freshness, pack provenance, and per-server
  sandbox/network posture.
- Skills stay instruction documents for now; executable skill scripts
  remain deferred (they would need the same trust machinery as MCP).
