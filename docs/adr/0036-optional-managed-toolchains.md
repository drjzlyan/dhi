# ADR-0036: Optional managed toolchains, installed on first use

Date: 2026-10-13 · Status: accepted · Extends: ADR-0005 (hermetic,
DHI-managed toolchain), ADR-0011 (no silent fallbacks). Companion to:
ADR-0032 (autonomy envelope), F-076.

## Context

DHI verified an agent's work only in Go modules. The user asked for the
top languages and chose managed toolchains over host tools. ADR-0005
forbids depending on host installs, so Rust, Java/Kotlin and .NET need
DHI-managed toolchains. They are large (a JDK ~200 MB, the .NET SDK
~220 MB, Rust ~300 MB) and most workspaces need none of them.

## Decision

- The registry manifest marks `rust`, `jdk`, `maven`, `gradle` and
  `dotnet` **optional**. The first-run install, its size total and
  `Resolve(nil)` cover required tools only.
- A verify that needs an optional tool asks the human through the
  **approvals inbox**, naming the tool and its download size; "allow
  always" grants the agent's `toolchain` scope. On approval DHI installs
  it with the same pipeline (download, verify, extract, atomic activate,
  lockfile). Denied or unavailable means the result is reported
  unverified, never faked. No new CLI subcommand (ADR-0004).
- **Pins** record the vendor's published checksum: sha256 where the
  vendor publishes one (Rust, Temurin, Gradle), or **sha512** where that
  is all it publishes (Apache Maven, .NET). The installer re-hashes every
  byte before extracting, so the pin script downloads nothing in full.
  A missing vendor checksum aborts pinning (`scripts/pin-optional.py`).
- **Rust** ships as per-component directories; DHI merges the components
  named in the archive's `components` file into one sysroot in Go (docs
  skipped) instead of running its `install.sh`.
- Each tool gets a stable `tools/<name>/current` link. Tool environment
  (`JAVA_HOME`, `DOTNET_ROOT`) points at it and is computed from the
  embedded manifest at launch, so a tool installed mid-session works
  without a restart. Package caches (`CARGO_HOME`, `GRADLE_USER_HOME`,
  Maven's local repository, `NUGET_PACKAGES`, `DOTNET_CLI_HOME`, npm, uv)
  are redirected into the toolchain's writable folders for agents and
  DHI's runner; the user's terminals keep their own.

## Consequences

- Polyglot workspaces get real verification; Go-only workspaces download
  nothing new.
- Five more pinned artifacts per platform to keep current (supply-chain
  surface, reviewed like code).
- A JVM tool shim (mvn, gradle) depends on the managed JDK: verify
  installs `jdk` with them.
