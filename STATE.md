# STATE — current position

Updated: 2026-08-24 (session: M4 P0 — hermetic git spike)

## Where we are

**M4 PHASE 0 COMPLETE (`make verify` green; not yet committed).**
Hermetic git decision landed as ADR-0009: DHI-built transport-free git
(NO_CURL/EXPAT/GETTEXT/PERL/TCLTK) from pinned upstream source, published
by our release workflow, pinned in the registry like rg/uv/node. go-git
keeps network + object ops; the CLI owns the worktree lifecycle. Spike
validated live on darwin/arm64: real git 2.55.0 built via
scripts/build-hermetic-git.sh (4.2 MB, OS libs only) passed init→commit→
worktree add/list/remove/prune→local clone round-trip.
Next: P1 member management.

## Just finished

- `docs/adr/0009-hermetic-git-binary.md` (supersedes ADR-0008 exclusion).
- `internal/toolchain/gitenv.go`: `GitEnv` (shim-first PATH +
  GIT_CONFIG_NOSYSTEM=1, GIT_TERMINAL_PROMPT=0, GIT_CONFIG_GLOBAL under
  prefix), `EnsureGitConfig` (defaultBranch=main, empty hooksPath dir),
  `GitBin`. User terminals untouched — hardening is GitEnv-only.
- `internal/gitcore/exec.go`: Runner seam (Run/Version/WorktreeAdd/
  WorktreeList-porcelain/WorktreeRemove/Prune), ResolveRunner refuses
  missing shim, 2-min per-invocation timeout, stderr in errors.
- Doctor `Git()` suite: silent while registry has no pin; else
  lockfile/shim/version agreement (warn stale lock, fail mismatch).
- `.github/workflows/release-git.yml` + build script emitting ready-
  to-paste manifest snippets per platform.
- ROADMAP M4 restructured into P0–P5 phases (P0 checked off).

## Gotchas learned

1. macOS /var → /private/var: git reports resolved paths from
   `worktree list --porcelain`; compare with filepath.EvalSymlinks.
2. `git commit -am` never stages NEW files — smoke flows need explicit
   `add -A`; quiet mode can also swallow stderr on exit 1.
3. cmd.Dir must exist for exec even when a stub ignores it — fake-git
   tests need real temp dirs as the "repo".
4. nil cmd.Env inherits the parent environment; only explicit slices
   isolate. Runner always sets env explicitly in production paths.
5. Carried: shim recursion guard; nil Events chan wiring; bounded
   shutdown; duplicate JSON tags → RawMessage decode; net.Pipe needs
   read pumps; deny-all policies without policy_json.

## Next up (P1 member management)

1. `internal/workspace`: Save (atomic tmp+rename), AddMember/
   RemoveMember/RenameMember with existing alias rules + dup guards;
   removal requires explicit confirm, never deletes trees silently.
2. gitcore local clone helper (hermetic git or go-git — either seam)
   behind the same API used by packs later.
3. Live re-resolution: guarded mutation of shared *Workspace +
   notification channel; editor tree/search/terminal tabs rebuild.
4. Workspace view members pane UI (a/r/d keys, form modals, goldens).

## Release checklist blocking the registry flip

Dispatch release-git workflow (or tag hermetic-git-v2.55.0), publish
artifacts as GitHub release, cross-check source digest
457fdb04dc8728e007d4688695e6912e6f680727920f2a40bf11eacc17505357
(git-2.55.0.tar.xz @ kernel.org) against kernel.org's published value,
then paste script-emitted platform blocks into registry/manifest.json.
Until flipped: doctor shows nothing (no pin = feature absent), smoke
test runs via DHI_SMOKE_GIT_BIN override.

## Open questions for user

- None blocking. (Commit of P0 pending user request.)
