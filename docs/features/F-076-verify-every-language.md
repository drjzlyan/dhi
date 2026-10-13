# F-076 — Verify every top language

Status: done (M30 P5)

## Problem
DHI checked an agent's work automatically only in Go modules (`go test ./...`).
Every other project came back "unverified", the agents' `run` tool knew only
`go` and `rg`, and the environment-failure check knew only Go's messages.

## Behaviour (P5a)
- **Detection** (`internal/agentkit/verifyplan`), per task worktree, from its files:

  | Ecosystem | Detected by | Prepare (network, once per lockfile state) | Test (offline) |
  |---|---|---|---|
  | Go | `go.mod` | — (uses the module cache) | `go test ./...` |
  | JS/TS | `package.json` with a real `test` script | `npm ci` / `corepack pnpm install --frozen-lockfile` / `corepack yarn install` (by lockfile); skipped with no dependencies | `npm test` / `corepack pnpm test` / `corepack yarn test` |
  | Python | `pyproject.toml`, `requirements*.txt`, `setup.py/cfg`, `pytest.ini`, `tox.ini` | `uv sync`, or `uv venv` + `uv pip install -r …` | `uv run --offline --no-sync pytest -q` when pytest is declared, else `python -m unittest discover` |
  | Rust | `Cargo.toml` | `cargo fetch` | `cargo test --offline` |
  | Java/Kotlin (Maven) | `pom.xml` | `mvn dependency:go-offline` | `mvn -o test` |
  | Java/Kotlin (Gradle) | `build.gradle(.kts)`, `settings.gradle(.kts)` | `gradle dependencies` | `gradle --offline test` |
  | .NET | a `.sln`, or one `.csproj`/`.fsproj`/`.vbproj` | `dotnet restore` | `dotnet test --no-restore` |

  A polyglot worktree gets one check per ecosystem.
- **Override.** `verify = ["make", "check"]` on a member in
  `.dhi/workspace.toml` replaces detection; `verify = []` turns it off.
- **Prepare is ask-gated.** It needs network, so it follows the agent's
  network scope: auto runs, deny reports "unverified (network is denied)",
  ask crosses the approvals inbox (and "allow always" persists as a grant).
  It reruns only when a lockfile changes.
- **Environment failures** (offline registry, missing toolchain, sandbox
  denial) are recognised per ecosystem and reported as unverified, never
  sent back to the agent as a bug.
- **Caches.** Agents' CLIs and DHI's verify build with npm, corepack, uv
  (and its Python installs), cargo, Gradle, Maven, NuGet and dotnet caches
  redirected into the toolchain's writable folders, because a sandboxed
  build may not write `$HOME`. The user's own terminals keep their caches.
- **Agents' `run` tool** allows each ecosystem's build and test commands
  (npm, corepack pnpm/yarn, uv, cargo, mvn, gradle, dotnet).

## Acceptance (P5a)
- [x] Detection table per ecosystem and lockfile; non-projects skipped;
      polyglot; override; fingerprint.
- [x] Prepare once per lockfile, network only for prepare, scope deny/ask.
- [x] Env-failure classification table.
- [x] Live: Node (`node --test`) and Python (`uv`) commands pass with the
      managed toolchain.

## Behaviour (P5b — ADR-0036)
- `rust`, `jdk` (Temurin 21 LTS), `maven`, `gradle` and `dotnet` (SDK
  LTS) are **optional** managed toolchains, pinned for every release
  platform. The first-run install skips them.
- When a verify needs one that is missing, DHI asks in the approvals
  inbox ("install the managed rust toolchain (N MB download)"); on
  approval it installs and continues. `dhi doctor` names the optional
  tools not yet installed.
- sha512 pins for vendors that publish only sha512; Rust's components
  are merged into one sysroot; `JAVA_HOME`/`DOTNET_ROOT` point at stable
  `current` links.

## Acceptance (P5b)
- [x] Optional tools excluded from first-run install and size total;
      sha512 verify and mismatch refusal; Rust component merge; current
      link (fixture-server tests).
- [x] Lazy install after approval, no re-ask once installed, denial named.
- [x] Live on darwin/arm64 (`DHI_SMOKE_OPTIONAL=1`): `cargo test`,
      `javac`/`java`, `mvn -v` on the managed JDK, `gradle --version`,
      `dotnet --version`.
