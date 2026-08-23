# STATE — current position

Updated: 2026-08-23 (session: M1 closed — production registry pins + ADR-0008)

## Where we are

M1 finish-out committed (`1c19da1`). **This session (uncommitted — commit
next): production registry pins landed; M1 is COMPLETE.** Embedded
manifest now carries ripgrep 15.2.0, uv 0.12.5, node 24.19.0 LTS for
darwin/arm64 + linux/amd64. Live-artifact end-to-end install verified.
Next session starts M2 Editor core.

## Just finished

- Git sourcing decision: **go-git, no managed git binary** (ADR-0008).
  Registry manifest deliberately excludes git; DHI's git view/agent ops
  go through a `gitcore` service seam over pure Go (M2).
- Pins computed by downloading each artifact and hashing with shasum;
  ripgrep digests cross-checked against upstream `.sha256` sidecars
  (exact match). Node pinned to Active LTS v24 'Krypton' line.
- Archive layouts verified before writing Strip/BinDir: rg+uv strip=1
  binaries at root; node strip=1 BinDir=bin (npm/npx/corepack are
  symlinks — extractor recreates them).
- `InstallEmbedded(ctx, names)` now takes a tool filter (used by smoke).
- `registry_smoke_test.go`: full pipeline vs live artifacts, gated behind
  `DHI_SMOKE_NET=1` (hermetic by default). Passed in 0.66s.
- `TestEmbeddedRegistryValid` now refuses an empty registry at release
  time (guards against shipping the seed).

## Gotchas learned (do not re-learn these)

1. ripgrep publishes official per-asset `.sha256` sidecars — always
   cross-check computed pins against them where available.
2. node linux x64 ships both .tar.xz and .tar.gz; we pin the .tar.gz
   (extractor supports gzip/zip only — adding xz means a new dep, avoid).
3. uv tarballs have NO version in filename (`uv-aarch64-apple-darwin.tar.gz`);
   version lives only in the release tag — pins must record it explicitly.
4. Carried: gate keys intercept before handleGlobal; jail roots need
   EvalSymlinks on macOS; map-value field assignment is a compile error;
   TUI assertions run on ANSI-stripped output.

## Next up (M2 Editor core — see ROADMAP)

1. Commit this session (user approves diff first).
2. Feature spec F-002 is written; start with nav tree grouped by member
   repo + fuzzy find (`internal/tui/surfaces/editor`), then modal buffer
   MVP. `gitcore` service seam over go-git lands with the Git view item.
3. Platform coverage follow-up (non-blocking): darwin/amd64 +
   linux/arm64 pins when needed.

## Open questions for user

- None blocking.
