# STATE — current position

Updated: 2026-08-23 (session: M1 foundation packages)

## Where we are

M0 committed. **M1 core packages implemented and verified** (uncommitted —
commit next): `internal/toolchain`, `internal/sandbox`, `internal/workspace`,
`internal/doctor`, `surfaces/bootstrap` component, and `dhi doctor [--json]`
wired into `cmd/dhi`. `make verify` green; all new suites pass under `-race`.
IA alignment commit from earlier today: `bb2c238`.

## Just finished

- `internal/toolchain`: versioned manifest schema (https-only pins, loopback
  http allowed for fixtures), sha256 verify-before-extract, tar.gz/zip
  extractor with strip + zip-slip/absolute-symlink rejection + size caps,
  Manager pipeline (resolve→download→verify→extract→activate→lockfile),
  atomic lockfile, shim symlink dir (`<root>/bin`), stage-level events.
  httptest fixture server with tamper cases; re-install is a no-op.
- `internal/sandbox`: path-jail with symlink-canonicalized roots,
  deny-by-default policy engine (read/write/exec/net → allow/deny/ask,
  first-match-wins, glob patterns incl. `dir/**`), `Sandbox` seam +
  Noop adapter, `Guard` coupling jail+policy+sandbox.
- `internal/workspace`: `.dhi/workspace.toml` (BurntSushi/toml added as the
  only new dep), member validation (name regex, existing dirs, dup paths),
  VPath resolver `<member>/<rel>` + reverse mapping, `.dhi/` reserved dirs.
- `internal/doctor`: ok/warn/fail check suite over toolchain + workspace;
  JSON report; exit codes 0/1/2 in CLI.
- `cmd/dhi`: `dhi doctor [--json]` subcommand (ADR-0004 exception).
- `surfaces/bootstrap`: event-driven first-run installer view reusing the
  brand hero; spinner/stage rows fully message-driven (deterministic).

## Gotchas learned (do not re-learn these)

1. macOS `/var/folders` is itself a symlink: sandbox jail roots MUST be
   `EvalSymlinks`-canonicalized at registration or every Contains() fails.
2. Go cannot assign through map values (`m[k].Field = x` is a compile
   error) — build/mutate locals then store back (hit it in manifest tests).
3. Archive strip semantics: dir entry `"name/"` with strip=1 must be
   skipped silently; files consumed entirely by strip are an error.
4. Assertions on styled TUI output must run on `ansi.Strip`ped text —
   glyphs and labels are separated by escape sequences in raw strings.
5. `httptest.Server.URL` is loopback http, which manifest validation
   deliberately allows — keep that exemption narrow (hostname check).

## Next up (finish M1)

1. Commit M1 packages (user approves diff first).
2. Production registry manifest content: pinned URLs+sha256 for git,
   ripgrep, node, uv per supported platform (security-sensitive — review
   carefully); wire real download end-to-end once.
3. Child-process PATH injection via `Manager.ShimDir()` (env seam for
   terminals/LSP/git spawned by DHI only).
4. Bootstrap gating: run bootstrap surface when doctor reports prefix
   missing; then transition into Workspace.
5. Then M2 Editor core (see ROADMAP).

## Open questions for user

- Registry hosting: where should the production manifest live (GitHub
  raw? dhi.dev)? Blocks item 2 above.
