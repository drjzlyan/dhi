# STATE — current position

Updated: 2026-08-23 (session: M2 closed — go pin + hermetic gopls builds)

## Where we are

LSP foundation committed (`4184180`). **This session (uncommitted —
commit next): registry pins the Go toolchain; gopls builds from source
through it.** `make verify` green; 21 packages under `-race`. **M2 IS
COMPLETE.** Next milestone: M3 agent runtime.

## Just finished

- Research outcome: gopls ships NO official binaries (golang/go#79066,
  Go team declining). Decision: pin Go itself, build gopls hermetically.
- Registry manifest gains `go` tool: go1.27.0 darwin/arm64 + linux/amd64
  pinned with digests pulled from go.dev/dl JSON (authoritative) AND
  cross-checked by hashing our own download (exact match).
- `toolchain.BuildInstall(spec)`: source-build pipeline using the pinned
  go shim — GOTOOLCHAIN=local, GOPATH/GOMODCACHE/GOBIN under staging
  (removed after), CGO off. Activates like a normal tool: tools/<n>/<v>/
  bin/, shims linked, lockfile updated with built-binary digest.
  LocalDir variant for fixture modules (unit tests stay offline/fast).
- Live smoke (DHI_SMOKE_BUILD=1): pinned Go download → gopls v0.23.0
  built → shim → `gopls version` runs. PASS in ~33s warm.
- LSP Manager resolution unchanged (`<root>/bin/gopls`) — works as soon
  as gopls is installed/built.

## Gotchas learned (do not re-learn these)

1. **Shim recursion**: BuildInstall exec'd `<shimdir>/go` while Env()
   puts shimdir FIRST on PATH — the shim's `exec go` found ITSELF and
   spun at 100% CPU forever. Always resolve symlinks (EvalSymlinks) to
   the real binary for self-invoking tools.
2. **defer os.RemoveAll(writable(dir))** evaluates writable() at DEFER
   REGISTRATION time (empty dir). Must wrap: `defer func(){...}()`.
3. Go module cache marks files 0444 AND dirs 0555 — RemoveAll needs both
   chmodded (walk dirs→0700, files→0600).
4. gopls CLI is subcommand-style (`gopls version`), not flags (-version).
5. Carried: nil Events chan; readLoop before handshake; bounded shutdown;
   fake servers echo URIs + reply null to unknown requests.

## Next up (M3 agent runtime)

1. Commit this session (user approves diff first).
2. Flip F-002 status to done in docs/features/F-002-editor.md (all four
   acceptance criteria now demonstrable).
3. M3 per ROADMAP: agentkit manifest + validation; Provider iface with
   Anthropic + scripted Mock; namespaced VPath tools behind sandbox
   Guard; message bus w/ JSONL persistence; Editor chat sidebar.
4. Bootstrap surface could offer optional "build gopls" step after main
   install (minutes-long) — UX decision when M3 lands.

## Open questions for user

- None blocking.
