# ADR-0009: Hermetic minimal git binary for worktree lifecycle

Date: 2026-08-24 · Status: accepted
Supersedes: the "no managed git binary" exclusion in
[ADR-0008](0008-go-git-no-managed-git.md) (that ADR explicitly reserved
this escape hatch: "a new pin + ADR, reviewed like supply-chain code").
go-git itself remains the transport and object-layer workhorse.

## Context

M4 task cards bind every assignment to a **ChangeSet**: per-repo linked
worktrees under `.dhi/tasks/<id>/` so agents work in isolation without
touching the user's checkout. go-git cannot create, list, or remove
linked worktrees — its v6 line ships an experimental `x/plumbing/worktree`
package only as a pre-release (`v6.0.0-alpha.5`), with an API that may
change without notice. Building a milestone feature on that is risk we
declined.

The realistic alternatives:

1. **go-git v6 alpha** — pure Go, but alpha-tagged major version plus
   experimental `x/` surface; migration churn across the editor's git
   view for an API that may still move.
2. **conda-forge prebuilt git** — real binaries exist for darwin/arm64
   and linux/amd64, but they drag a dependency closure (libcurl,
   openssl, pcre2, gettext, libiconv) into every pin, and conda binaries
   carry placeholder prefixes that must be byte-relocated at install
   time; macOS dyld resolves absolute install names, so extracted
   bundles are fragile outside their designed prefix.
3. **libgit2/git2go** — CGo breaks the plain-Go build requirement
   (AGENTS.md; ADR-0005).
4. **DHI-built minimal git from upstream source** — upstream publishes
   no binaries (same situation as gopls, which DHI already builds
   hermetically from source through its pinned Go toolchain), but *our*
   release CI can build once per pin and publish checksummed artifacts
   that the registry pins like any rg/uv/node artifact.

## Decision

Add a `git` tool to the registry manifest whose artifacts are produced
by **DHI's own release workflow** from the upstream kernel.org source
tarball at a pinned version, built **transport-free**:
`NO_CURL NO_EXPAT NO_GETTEXT NO_PERL NO_TCLTK`. One ~5 MB `bin/git`
per platform (darwin/arm64, linux/amd64), sha256-pinned like every other
registry artifact; the trust anchor chain is upstream source URL +
our CI provenance + the digest in the embedded manifest.

Division of labor behind the existing `internal/gitcore` seam:

- **Hermetic git CLI (exec):** worktree lifecycle (`worktree add/list/
  remove/prune`) now; further local plumbing later if parity demands.
- **go-git (in-process, unchanged):** clone/fetch/push, status/stage/
  commit/log — everything the M2 git view already ships. Because the
  build is transport-free, hermetic git never races go-git on network
  features.

Execution hygiene (ADR-0005, extended):

- The CLI resolves through the toolchain shim dir only — never host
  git, no silent fallback.
- Child processes get `GIT_CONFIG_NOSYSTEM=1`, `GIT_TERMINAL_PROMPT=0`,
  and `GIT_CONFIG_GLOBAL=<prefix>/git/config` (managed file: deterministic
  `init.defaultBranch`, empty `core.hooksPath`), so neither system nor
  user git config, aliases, or hooks leak into agent operations. User
  terminals inside DHI keep their own environment — hardening applies
  only to DHI-driven git children via `Manager.GitEnv`.
- Commits made by the CLI pass explicit `-c user.name/-c user.email`
  (mirroring `gitcore.CommitOptions`, which already requires authors).

## Consequences

- Worktree operations behave byte-for-byte like reference git; zero
  library churn; go-git stays pinned at its stable v5 line.
- Registry gains one more small artifact (~5 MB/platform); bootstrap
  downloads grow accordingly.
- Hermetic git cannot clone over the network by construction. If SSH/https
  clone parity ever needs to move off go-git (e.g. credential-helper
  auth), that means shipping `libexec/git-core` helpers + curl in the
  artifact — a deliberate follow-up pin, not a silent addition.
- Until the first artifacts are published (workflow
  `release-git.yml`, tag `hermetic-git-v2.55.0`), no `git` entry exists
  in `registry/manifest.json`; doctor reports it as "not installed yet"
  and all machinery degrades visibly (ADR-0005). Flipping the pin —
  URLs + digests from the published release — is a supply-chain review,
  same as any manifest change.
