# STATE — current position

Updated: 2026-08-23 (session: M1 finish-out — embedded registry, PATH seam, shell gate)

## Where we are

M1 foundation committed (`83cfa87`). **This session (uncommitted — commit
next):** embedded-in-binary registry distribution implemented, child-process
PATH seam shipped, bootstrap gate wired into the app shell. `make verify`
green; everything passes under `-race`. Only remaining M1 item is
production registry pins.

## Just finished

- Decision recorded: production registry manifest is **embedded in every
  DHI binary** (`internal/toolchain/registry/manifest.json`, go:embed).
  Seed manifest has zero tools → bootstrap resolves to zero actions and
  degrades visibly per ADR-0005.
- `toolchain.Manager.InstallEmbedded(ctx)`; URL-based `Install` retained
  for fixtures + `DHI_REGISTRY` override env var (loopback http allowed).
- Manifest validation now permits empty tools map (seed state); embedded
  registry guarded by test.
- `Manager.Env(base)` — shim dir prepended to PATH for DHI's child
  processes only. nil base inherits environ; explicitly empty base stays
  host-free (shim-only PATH); idempotent.
- Shell gate: `app.Gate` iface + `SetGate`; while active it owns body and
  all keys except ctrl+c/ctrl+q; releases permanently once `Finished()`.
  `bootstrap.Model.Finished()` = done OR failed (never traps the user).
- `cmd/dhi` installs the gate on first run when `<prefix>/lock.json` is
  absent.

## Gotchas learned (do not re-learn these)

1. Gate key routing must intercept BEFORE `handleGlobal` — number keys
   ("2") are global bindings and would otherwise switch surfaces during
   first-run bootstrap.
2. macOS `/var/folders` is a symlink: jail roots need EvalSymlinks at
   registration (carried from last session).
3. Go map-value field assignment is a compile error; mutate locals then
   store back.
4. TUI assertions run on `ansi.Strip`ped output — glyphs/labels are split
   by escape codes in raw strings.

## Next up (close M1, then M2)

1. Commit this session's work (user approves diff first).
2. Production pinning workflow: choose artifact sources/versions for git,
   ripgrep, node, uv; fetch each per platform, compute sha256, fill
   `registry/manifest.json` (security-sensitive — review pins like code);
   real end-to-end bootstrap behind `DHI_REGISTRY` until then if needed.
3. Start M2 Editor core per ROADMAP (nav tree + fuzzy find first).

## Open questions for user

- None blocking (manifest hosting resolved: embedded-in-binary).
