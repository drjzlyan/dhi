# ADR-0011: No silent fallbacks — strict boot, explicit opt-out, gated install

Date: 2026-09-02 · Status: accepted · Supersedes: the "degrade visibly"
clause of ADR-0005 (hermetic toolchain) and ADR-0006's Noop-default
reading; F-011 is the implementation.

## Context

ADR-0005 said host tools are "never used silently (explicit opt-in
fallback flag only)" and missing pieces "degrade visibly". In practice
"visibly" meant doctor warnings: the OS sandbox silently fell back to
Noop, terminals silently inherited the host PATH when the toolchain
prefix was unresolvable, broken configs silently substituted defaults,
and capabilities (rg/LSP/gh/git-shim) were silently inert. Each of
these is a quiet downgrade of exactly the guarantees DHI exists to
make (hermeticity, confinement, reproducibility).

## Decision

Every missing dependency resolves to exactly one of:

1. **Hard requirement** — boot refuses with the reason and the fix.
   Protection-critical seams (OS sandbox, lockfile integrity, config
   validity) are hard requirements.
2. **Explicit opt-out** — a setting the user deliberately chose (e.g.
   `security.sandbox = "off"`). Honored and labeled; never a default
   the program picks.
3. **Confirmation-gated install** — boot offers to install missing
   hermetic pieces through the toolchain. Declining leaves the
   capability refusing at use time with a named fix and a failing
   doctor row. There is no host-tool path.

`dhi doctor` remains runnable while boot is blocked (it is the
diagnostic tool for blocked boots) and reports Fail for every refused
seam.

## Consequences

- Availability drops on machines that cannot meet requirements (Linux
  without bwrap, broken configs). That is the point: DHI refuses to
  pretend.
- New code must not add fallback branches; the boot-audit package is
  the single place resolution policy lives, and it is table-tested.
- gh moves from host CLI to registry-pinned shim (pipeline per the
  git precedent); until dispatched, PR flows refuse visibly.
- ADR-0008/0009's "no silent fallback to host tools" language is
  unchanged and now enforced structurally.
