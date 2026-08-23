# ADR-0005: Hermetic, always-DHI-managed toolchain

Date: 2026-08-23 · Status: accepted

## Context

DHI needs git, ripgrep, and Node/uv (for MCP servers). Depending on host
installs makes behavior unpredictable; using system installers violates
isolation.

## Decision

DHI manages **all** its dependencies inside an XDG prefix
(`~/.local/share/dhi/toolchain/`), driven by a versioned registry manifest
(pinned URLs + sha256 per platform: git, ripgrep, node, uv). Bootstrap:
download → verify → extract → atomically activate → lockfile. Shims are
prepended to PATH **only for DHI's child processes**. No sudo, no system
package managers; uninstall = delete the folder. Host tools are never used
silently (explicit opt-in fallback flag only). `dhi doctor` audits integrity;
missing pieces degrade visibly. Animated first-run bootstrap is event-driven
and fully testable headlessly against fixture servers.

## Consequences

- Reproducible environments; clean machines get one ~200MB download.
- Registry manifest becomes security-sensitive supply-chain surface → pin,
  review, checksum everything.
