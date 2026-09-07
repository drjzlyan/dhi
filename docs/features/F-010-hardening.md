# F-010: OS-sandbox adapters on by default + performance passes

Status: done (M7, 2026-09-02) · Milestone: M7 · Closes: ADR-0006 seam

## Summary

Two hardening halves ship together:

1. **OS-sandbox adapters on by default.** ADR-0006 left a `Sandbox`
   interface with a Noop pass-through. This lands the real adapters —
   seatbelt on macOS (`sandbox-exec`), bubblewrap on Linux (`bwrap`) —
   selected automatically when the platform binary exists, injected into
   the agent runtime's `Guard`, and surfaced by doctor. A settings key
   (`security.sandbox = "auto" | "off"`, default auto) is the escape
   hatch.
2. **Performance passes (large repos, many buffers).** Benchmarks for the
   editor hot paths (fuzzy find, tree indexing, buffer rendering), plus
   targeted fixes the numbers justify — starting with the per-keystroke
   lowercase/rune allocations in fuzzy ranking over the 20k-path index.

## Part A — sandbox adapters (`internal/sandbox`)

- **seatbelt adapter** (`seatbelt.go`): wraps argv as
  `sandbox-exec -p <profile> -- argv…`. The SBPL profile is generated
  once at construction from the root lists and is **deny-by-default**
  with scoped allows:
  - `file-read*`/`file-write*` inside rw roots (workspace members +
    `.dhi` + toolchain prefix — the prefix is DHI-managed and caches
    write into it),
  - `file-read*` inside ro roots (reserved for future read-only
    scoping) plus the macOS system paths dyld/exec need (`/System`,
    `/usr/lib`, `/private/var/db/dyld`, `/dev`),
  - `process-exec*` inside the allowed trees + `/bin` `/usr/bin`
    `/sbin` `/usr/sbin`,
  - `network*` allowed — network permission stays governed by the
    policy engine at the tool layer (ADR-0006); the OS layer is
    defense-in-depth against path and process escape.
  Roots must be canonical (jail roots already are — `NewJail`
  EvalSymlinks them); profile text escapes quotes/backslashes.
- **bubblewrap adapter** (`bubblewrap.go`): `bwrap --unshare-all
  --share-net --die-with-parent --dev-bind /dev /dev --proc /proc
  --tmpfs /tmp` + `--ro-bind` for system dirs (`/usr` `/bin` `/sbin`
  `/lib` `/lib64` `/etc`) + `--bind` for each rw root + `--ro-bind` for
  each ro root, then the original argv.
- **selection** (`select.go`): `Detect(goos, lookPath)` returns
  `"seatbelt" | "bubblewrap" | "noop"` (darwin → sandbox-exec, linux →
  bwrap, anything else or lookup miss → noop). `Select(...)` builds the
  adapter from canonical roots; deterministic via injected GOOS +
  lookPath for tests.
- **injection:** `runtime.Config.Sandbox` (nil → Noop) — `buildEntry`
  assigns it to every agent's `Guard.Sandbox`. `Guard.Exec` is the
  single exec seam (F-007); the first exec-shaped tool therefore wraps
  without further changes. No production call site exists yet (agent
  git ops are in-process go-git; tools touch fs via os) — wiring MCP
  stdio spawns through the wrap lands with the first MCP consumer
  (deferred).
- **settings:** new `security` section, `sandbox = "auto"|"off"`;
  unknown values originally fell back to auto (superseded by ADR-0011/
  F-011: unknown values now refuse boot); `Known()` updated; off maps
  to Noop at wiring time.
- **doctor:** `sandbox/adapter` check — OK when the OS adapter is
  available (name in detail), Warn when degraded to path-jail-only
  (noop) so the gap is visible per ADR-0005. Supersedes under F-011:
  missing helper in auto mode is a Fail.

### Acceptance criteria (Part A)

- Wrap shapes: seatbelt argv is `[bin, "-p", profile, "--", orig…]`;
  bubblewrap argv ends with the original argv; every rw/ro root appears
  in the profile/bind set.
- Selection matrix over (goos × lookPath hit/miss) returns the expected
  adapter or Noop; `Detect` matches `Select` naming.
- Runtime test: a guard built with `Config.Sandbox` set delegates Wrap
  to it (test double observed), and `Guard.Exec` still refuses when
  policy denies.
- Settings round-trip: `security.sandbox = "off"` parses, sanitizes,
  saves; unknown values → auto; no unknown-key warnings.
- Doctor reports the adapter check; JSON output includes it.
- `make verify` green.

## Part B — performance passes

- **Benchmarks** (deterministic, no network): `fuzzy.Rank` over a 20k
  synthetic path index; `indexFiles` over a synthetic wide/deep tree;
  `bufferView` render for a large buffer (thousands of lines); textbuf
  insert/undo on a large buffer.
- **Fixes driven by the numbers**, starting with the known one:
  - fuzzy find lowercased + `[]rune`-converted every indexed path on
    every keystroke (2 allocations × 20k items × keystroke). The index
    now ships as a pre-lowered `fuzzy.Index` (lowercase + rune slice
    built once at index time); ranking consumes it
    (`Index.Rank`); the public `Match`/`Rank` API is unchanged and
    shares the same scoring core.
  - `greedyFrom` early-exits when the remaining text cannot hold the
    remaining pattern; hopeless start positions are never tried.
- **Many buffers:** the tab strip renders every open buffer every
  frame; when tabs exceed the available width the strip keeps the
  active tab anchored and elides the rest (`…+N` / `+N` markers that
  participate in the width budget) instead of overflowing the panel.
- Measured numbers (Apple M1 Pro, `go test -bench`):

| benchmark | before | after |
|---|---|---|
| `Rank` 20k paths, "main" | 8.05 ms · 3.63 MB · 20 017 allocs | 5.05 ms · 635 KB · **16 allocs** |
| `Rank` 20k paths, "edtr" | 8.82 ms · 3.62 MB · 20 017 allocs | 5.82 ms · 627 KB · **16 allocs** |
| `Rank` 20k paths, selective | 4.41 ms · 3.0 MB · 20 006 allocs | 1.37 ms · 1.8 KB · **5 allocs** |
| `bufferView` 5 000-line buffer | 55 µs · 7.9 KB · 228 allocs | unchanged (already windowed) |
| keystroke on 5 000-line buffer | 3.8 µs | unchanged |
| textbuf 20k lines, 50 inserts + undo | 590 µs | unchanged |

  The remaining rank time is the inherent O(paths × pattern) scan;
  per-keystroke allocations are gone.
- Document measured before/after numbers in this file.

### Acceptance criteria (Part B)

- Benchmarks exist for the four hot paths and run under `go test -bench`.
- Fuzzy ranking no longer allocates lowercase copies of the index per
  keystroke (20 017 → 16 allocs/op at 20k paths).
- Tab strip elides overflow (unit test: active anchored, markers
  present, width ≤ budget); existing goldens unchanged — the strip only
  differs when tabs overflow, which no golden exercises.
- `make verify` green.

## Deferred

- MCP stdio spawn wrapped through the sandbox (needs the first real
  MCP consumer in cmd/dhi wiring).
- Read-only ro-root policy differentiation (roots plumbed, unused).
- bubblewrap network namespace tightening (`--unshare-net`) once
  policy-driven net allowances can inform the profile.
