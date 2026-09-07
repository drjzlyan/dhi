# F-011: No silent fallbacks — strict boot & explicit install gates

Status: done (M7, 2026-09-02) · Milestone: M7 · Supersedes: the
"degrade visibly" reading of [ADR-0005](../adr/0005-hermetic-toolchain.md)
· Decision: [ADR-0011](../adr/0011-no-silent-fallbacks.md)

## Summary

Every silent fallback in DHI is removed. Each missing dependency now
resolves to exactly one of three outcomes, never a quiet downgrade:

1. **Hard requirement** — boot refuses (named reason + named fix).
2. **Explicit opt-out** — the user set it (e.g. `security.sandbox =
   "off"`); honored, labeled, never implied.
3. **Confirmation-gated install** — boot offers to install the missing
   hermetic piece; declining boots the IDE but the capability refuses
   at use with the named fix, and doctor fails.

## Boot audit (`internal/boot`)

`boot.Audit(...)` runs before any surface renders and returns a
decision: proceed / offer-install (list) / block (reason). cmd/dhi
stays thin (ADR-0004): it renders the decision, the logic lives in the
package and is table-tested.

### Hard requirements (block boot)

- **OS sandbox unavailable** with `security.sandbox = auto`: missing
  helper binary (bwrap on Linux; sandbox-exec is always present on
  macOS) or profile failure. Fix named in the block screen. Explicit
  `off` boots with a visible "os sandbox disabled by config" label.
- **Malformed/invalid config** — settings or workspace.toml: parse
  errors, unknown keys, unknown theme, out-of-range values, broken
  schema. Refusal names file + key. `settings.sanitize` is deleted.
- **Broken workspace config** — `workspace.Load` gains `IsConfigError`:
  a workspace directory with a broken workspace.toml blocks; a plain
  directory (not a workspace) still boots into the empty-state editor.
- **Corrupt toolchain lockfile** — blocks with "re-run bootstrap".

### Confirmation-gated installs (decline ⇒ refuse-at-use)

Audit lists missing hermetic pieces: rg, uv, node, git shim (when the
registry entry exists), gh shim (once pinned), gopls (built from
source via `BuildInstall`). One confirmation lists everything;
approval runs targeted `Manager.InstallEmbedded` / `BuildInstall`
through the existing bootstrap surface. Declining or failing leaves:

- terminals refused to open (visible drawer line: hermetic PATH
  unavailable) — the host-env leak path (ADR-0005 violation) is dead;
- search/LSP/crew/PR flows refuse with named fixes instead of
  silently inert behavior;
- doctor reports each as Fail (not Warn).

### gh hermetic (removes the last host-tool dependency)

`review.GHCLI` runs the registry-pinned gh shim; the host `gh` lookup
path is removed. Pinning follows the git precedent: release pipeline +
auto-PR re-hash into `registry/manifest.json` (dispatch is the
irreducible trust step); until the entry exists, PR flows refuse with
"gh shim not installed — run bootstrap / dispatch pin pipeline" and
doctor fails the row.

## Acceptance criteria

- Boot-decision matrix table-tested (helper present/absent × mode
  auto/off × config states × lockfile states).
- `runtime.New` rejects a nil Sandbox; existing tests inject Noop
  explicitly (tests are allowed the explicit adapter; production is
  not allowed to omit one).
- No code path sets `cmd.Env = os.Environ()` for DHI child processes.
- Settings strictness: malformed/unknown/out-of-range → error naming
  file+key; round-trip tests updated; no silent defaults.
- workspace.Load: broken-config error distinguishable and surfaced;
  not-a-workspace unchanged.
- Standards: broken layers file refuses turns with the named path
  (built-in base layer remains; only the silent fallback dies).
- Task/session cards: malformed cards surface named errors; doctor
  fails those rows (surfaces stay usable for valid cards).
- gh: no host exec.LookPath in review; shim-first; refusal + doctor
  Fail until pinned.
- `make verify` green; new boot-block screens get deliberate goldens.

## Deferred

- Registry pin entries for gh (requires dispatching the pipeline —
  user trust step, same as release-git).
- Narrowing seatbelt/bwrap profiles per-op (roots plumbed, coarse for
  now).
