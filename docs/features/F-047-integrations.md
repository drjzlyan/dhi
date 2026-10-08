# F-047: Integrations catalog + guided setup

Status: complete (M25 P6) · ADR-0028 · Builds on F-034 (MCP bridge), F-043 (wizard), F-045 (employees).

## Problem
Employees can only help with Jira, Confluence, Slack and friends if an MCP server
is wired up with credentials. Today that means hand-writing a card, knowing the
right server and its env names, and exporting secrets yourself (Linux has no store).

## Design
- **Credential store** `internal/credstore`: `credentials.toml` in the user config
  dir (0700/0600, atomic). `mcpbridge.DefaultLookup`: env → file → keychain.
- **Card schema 2**: HTTP `auth_env` → Bearer. `mcp.BearerClient`.
- **Bridge**: tool shims first on a stdio server's `PATH`; `mcp__<server>__*`.
- **Catalog** (all facts read from vendor/maintainer docs on 2026-10-08; versions
  pinned from the npm / PyPI registries):

  | Entry | Server | Trust | Credentials |
  |---|---|---|---|
  | `atlassian` (Jira + Confluence) | `uvx mcp-atlassian@0.23.1` | community | site URL, email, API token |
  | `github` | `https://api.githubcopilot.com/mcp/` (Bearer PAT) | vendor-hosted | PAT |
  | `linear` | `https://mcp.linear.app/mcp` (Bearer API key) | vendor-hosted | API key |
  | `notion` | `npx -y @notionhq/notion-mcp-server@2.5.2` | vendor | `NOTION_TOKEN` |
  | `slack` | `npx -y @zencoderai/slack-mcp-server@0.0.1` | community (fork of an archived reference server) | `SLACK_BOT_TOKEN`, `SLACK_TEAM_ID` |
  | `teams` | — | unavailable | Microsoft's server needs delegated Entra sign-in (OAuth) |

- **`catalog.Install`** writes the credentials (store) and the card (`.dhi/mcp/<slug>.toml`,
  which holds no secret and can be shared with the team); **`catalog.Enable`** adds
  `mcp__<slug>__*` to chosen employees' tools through the validated manifest round trip.
- **Wizard step "integrations"** (after "team"): list with status → pick one → trust
  label, what it can do, where to create the token → masked fields → who gets it
  (everyone / the lead / nobody yet) → confirm showing where secrets are stored.

## Acceptance criteria
- [x] credstore: 0600/0700, atomic, env wins over file, delete, corrupt file errors by name and is never overwritten
- [x] card schema 2 `auth_env` (https only, name validated, not on stdio), schema 1 cards still load; bearer header sent on every request and the caller's request is not mutated; missing credential refused by name
- [x] stdio PATH starts with the tool shims; `mcp__<server>__*` is a valid ref and honoured by the bridge allowlist (and only for that server)
- [x] every catalog entry is a valid card; stdio servers pinned to an exact version and run through `npx`/`uvx`; inputs cover exactly the declared credentials; Teams is unavailable with a reason
- [x] Install is all-or-nothing and never writes a secret into the card; Enable preserves every other manifest field, is idempotent and validates before writing
- [x] wizard step: list, trust label, steps + token URL, masked fields, validation before any write, who-gets-it, confirmation naming the storage, esc steps back without writing, failure reported and not marked changed
- [x] masked input never renders the secret (kit test + the step's screens + a real terminal)
- [x] doctor reports unresolved credentials by name only
- [x] all three stdio catalog servers started through the bridge's own dialer **under the real macOS seatbelt sandbox** with dummy credentials and listed their tools: `mcp-atlassian@0.23.1` 98 (README: 98), `@notionhq/notion-mcp-server@2.5.2` 24, `@zencoderai/slack-mcp-server@0.0.1` 8 (README: 8)
- [x] a server that dies on start fails the dial in about a second with its own stderr (it used to be discarded and the dial waited out its whole timeout)
- [x] walked end to end in tmux: connect Linear → `0700` dir, `0600` credentials file, secret-free card, `mcp__linear__*` on every employee

## Found by running it under the sandbox
The first probe ran without the sandbox and passed. Under the real seatbelt profile
**every stdio entry failed**: `uv` and `npm` write caches/logs/temp under `~`, which the
sandbox forbids, and the failure surfaced only as a 3-minute hang (stderr was discarded
and a dead child was never noticed). Fixed twice over: stdio servers now get a private
`HOME` and `TMPDIR` inside the writable toolchain prefix (`<prefix>/mcp-home`,
`Deps.HomeDir`, which also keeps third-party code out of the user's real home), and
`mcp.Stdio` keeps the last 2 KB of stderr and fails every pending call the moment the
child exits. Only the seatbelt profile was exercised (macOS); the Linux bubblewrap
profile has not been run.

## Not verified here
- No call was made to a vendor service with a real credential (no accounts here); the
  Linear and GitHub bearer path is covered by a test server, Atlassian by a live start
  with dummy credentials (it lists tools without authenticating).
- Credentials are stored in plain text (documented, owner-only); keychain writes are deferred.

## Deferred
OAuth (Teams, hosted Slack/Notion); keychain writes; credential rotation UI; a signed remote catalog.
