# ADR-0024: There is no direct-API engine kind

Date: 2026-09-30 · Status: accepted · Supersedes: ADR-0019 §5 (the
"direct-API engine kind is designed but not built" clause) only — every
other ADR-0019 decision stands. Companion to: ADR-0019 (DHI owns the
loop, the CLI is the engine), ADR-0011 (no silent fallbacks), ADR-0005
(hermetic toolchain). Serves: F-030.

## Context

ADR-0019 §5 recorded `engine = "api:<provider>"` as a designed-but-not-
built escape hatch: an engine where DHI talks to a provider's HTTP API
directly, owns the whole tool loop, and therefore offers a *hard* tool
guarantee (the only tools that exist are DHI's) and offline inference.
The manifest seam reserved the `api:` prefix and refused it by name at
parse (`internal/agentkit/manifest.ParseEngine`), and F-030 left a
deferred item tracking it.

The kind was never built, and on review it is not wanted. The ownership
guarantee it promised is already delivered a different way: ADR-0019's
commodity model serves only DHI's declared tools over the MCP loopback
endpoint, and `dhi-action` (a second, agent-emitted delivery path) was
removed at parity, so a host CLI's route to DHI's surface is the single,
audited one. The residual is a host CLI's *own native* tools, which are
documented best-effort containment — not a gap a second engine kind
closes cheaply. Building `api:` would mean per-vendor HTTP clients and
streaming parsers (stdlib-only under ADR-0005), a tool-call turn loop,
provider key handling, network policy, and per-provider cost accounting —
a milestone paid for a guarantee the product does not need.

## Decision

1. **Drop the direct-API engine kind.** There is exactly one engine
   kind: `engine = "cli:<name>"`. A manifest naming any other kind
   (`api:…` included) refuses by name as an unknown kind — the prefix is
   no longer reserved and carries no "not built yet" special case.
2. **Containment remains best-effort, and is stated as such.** Doctor,
   product docs, and the system block continue to describe host-CLI
   native tools as best-effort containment (ADR-0019 §5, first half).
   No surface claims a hard guarantee.
3. **No code, schema, or roadmap artifact reserves the path.** The
   `case "api"` branch and its "not built yet" message are removed; the
   ROADMAP deferred bullet and the F-030 deferred item are removed.
4. **Reinstating it requires a new ADR.** If absolute scope or offline
   inference is ever required, it is a fresh decision with its own
   feature spec — not a resurrection of this clause. This ADR supersedes
   only ADR-0019 §5's forwarding reference; ADR-0019 is otherwise
   untouched (append-only history).

## Consequences

- `ParseEngine` simplifies to `cli` + unknown-kind; the manifest test
  asserts the plain unknown-kind refusal.
- F-030's deferred "direct-API engine kind" item and the ROADMAP's
  `api:<provider>` line are deleted; M15 is complete with no asterisk.
- Doctor's "containment caveat" wording stays (it is a truth claim, not
  a pointer to future work).
- The tool-surface guarantee is described purely as what ADR-0019 +
  ADR-0018 already make auditable: one declared tool surface, one
  approvals ledger, no second delivery path.
