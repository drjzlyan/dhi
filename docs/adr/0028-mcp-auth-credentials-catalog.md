# ADR-0028: Bearer-token MCP servers, a user-level credential file, and a verified catalog

Date: 2026-10-08 · Status: accepted · Companion to: ADR-0022 (third-party
content trust), ADR-0017/0018 (served tools). Serves: F-047.

## Context

The MCP bridge (F-034) can dial stdio servers and plain HTTP servers, resolving
declared env *names* from the environment or the macOS keychain. Three gaps stop
it from reaching the tools companies actually use:

- HTTP servers could not send an `Authorization` header, so every hosted server
  that accepts a token (GitHub, Linear) was unreachable.
- Stdio servers were launched with the host `PATH`, not DHI's pinned `uvx`/`npx`.
- Credentials had a home only on macOS; on Linux the sole option was exporting
  variables by hand.
- There was no curated list: nothing said which server to run for Jira, or how
  to get its token.

## Decision

1. **HTTP cards may declare `auth_env`** — the *name* of a credential sent as
   `Authorization: Bearer <value>`. Card schema 2 (1 still loads). The value is
   resolved at dial time; a missing credential is a named refusal.
2. **Stdio servers launch with DHI's tool shims first on `PATH`** (so `uvx` and `npx`
   are the pinned hermetic ones) **and a private `HOME`/`TMPDIR` inside the sandbox's
   writable toolchain prefix.** Without the latter they cannot write their caches
   under the OS sandbox and fail; with it they also never touch the user's real home.
3. **Credentials resolve: environment → `credentials.toml` → macOS keychain.**
   `credentials.toml` lives in the user config dir (never `.dhi/`), directory
   `0700`, file `0600`, written atomically. It is plaintext, like `~/.aws/credentials`;
   the UI says so. Cards still hold names only.
4. **A catalog of verified servers** (`internal/agentkit/catalog`). An entry is only
   added when its command, package and environment names come from the vendor's or
   maintainer's documentation and its package version is pinned from the registry.
   Each entry carries a trust label (`vendor-hosted`, `vendor`, `community`), the
   origins it needs, and where the user creates the token. An integration we cannot
   reach honestly is listed as **unavailable with the reason** (Microsoft Teams:
   delegated Entra ID sign-in only; DHI has no OAuth client yet).
5. **`mcp__<server>__*`** is a valid tool reference meaning every tool of that
   server. Every call still needs approval (ADR-0022), so the wildcard widens
   discovery, not authority.
6. **Setup never runs without a yes** and shows where each secret will be stored.

## Consequences

- Jira, Confluence, GitHub, Linear, Notion and Slack become one-screen setups.
- Third-party code still runs with a user's token: the sandbox, declared
  origins, pinned versions and per-call approvals bound it, but the `community`
  label is an honest warning, not a guarantee.
- OAuth-only servers (Teams, Slack's hosted server, Notion's hosted server) stay
  out until an OAuth client exists.
