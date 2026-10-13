# F-076 — Verify every top language

Status: in progress (M30 P5: P5a done — detection and verify; P5b — managed Rust/JDK/.NET toolchains)

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

## P5b (next)
Rust, a JDK, Maven, Gradle and .NET become optional managed toolchains,
installed when a project first needs one, after the human approves.
