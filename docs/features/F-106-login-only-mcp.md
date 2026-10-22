# F-106 — Login-only MCP auth, like Claude Code

Status: planned (session 61, M35 P6) · ADR-0042

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
- [ ] httptest auth server: shipped client ID + fixed port, DCR, PKCE verify, refresh, 401 retry,
      token-request path, paste-back.
- [ ] Live: Atlassian and Slack login; an agent reads a Jira issue and searches Slack; Zoom by token.
