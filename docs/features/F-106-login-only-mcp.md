# F-106 — Login-only MCP auth, like Claude Code

Status: done (session 61, M35 P6); live sign-in with real accounts pending (needs the user's accounts) · ADR-0042

## Why
`internal/mcp` only sent a static bearer token. Official remote servers (Atlassian, Slack, Linear,
Notion) use OAuth. The user wants to add a server and just log in, with no OAuth apps.

## Behaviour
- On a 401 DHI discovers the server's auth (RFC 9728, RFC 8414), takes the client ID from the
  server config's `oauth.clientId` (Slack ships one in its plugin) or registers dynamically
  (Atlassian and most hosted servers), and runs PKCE in the browser with a loopback redirect
  (`oauth.callbackPort` when given). SSH sessions can paste the final redirect URL back.
- Tokens live in the secret store, refresh before expiry and on 401, and are revoked on disconnect.
- A server with neither (Zoom today) shows "needs a token"; its `${VAR}` headers become a masked
  secret request.
- Settings → INTEGRATIONS lists servers with origin, status and assigned bots: enter logs in,
  `d` disconnects, `a` assigns, `+` adds.

## Acceptance
- [x] httptest auth server: shipped client ID + fixed port, DCR, PKCE verify, refresh, 401 retry,
      token-request path, paste-back.
- [ ] Live: Atlassian and Slack login; an agent reads a Jira issue and searches Slack; Zoom by token.

## Notes (session 61)
- Discovery checked live (unauthenticated probe): Atlassian `mcp.atlassian.com/v2/mcp` → auth.atlassian.com
  with dynamic registration; Slack → slack.com OAuth with the plugin's client ID (no registration);
  Linear and Notion → their own endpoints with dynamic registration.
- Tokens are secrets in the credentials file (`MCP_OAUTH_<hash>`), so they are redacted like any secret.
- A run that meets a server without a sign-in starts it in the background (browser opens, toast, the
  address is copied) and the agent is told the server waits for a sign-in; the next run uses it.
  Installing a server from Settings or setup opens its sign-in right away.
- Settings → INTEGRATIONS: the sign-in column (signed in, sign in on first use, signing in…, needs
  VAR, ready), enter signs in, `p` pastes the address over SSH, `d` signs out (revokes).
- A server whose config carries its own Authorization header (Zoom's `${VAR}`) never uses OAuth; a
  missing variable names secret_request.
