# F-104 — Secret gap fixes

Status: done (session 61, M35 P4) · builds on F-088, F-089

## Why
Secret requests already keep values out of the model's context, but: MCP servers looked secrets up
globally only, so a secret saved "for this workspace" never reached them; MCP env ignored per-agent
grants; plain settings such as `JIRA_URL` were redacted as secrets; the redaction label leaked the
workspace folder name; the editor sidebar could not fill a missing value.

## Behaviour
- MCP lookups use the workspace first, then global.
- A server's declared credentials go with the server: assigning `mcp__<server>__*` to an agent is
  the grant (P7's Assign step names them). Only names the server's card declares are looked up.
- The credentials file has a `[settings]` table for plain values (site URLs, a login name): they
  resolve like secrets but never reach the redactor. The safe repair `credential-kinds` moves the
  old catalog's JIRA_URL, CONFLUENCE_URL, JIRA_USERNAME, CONFLUENCE_USERNAME and SLACK_TEAM_ID.
- Redaction label: `‹secret:NAME›`. Two workspaces' values of one name are both redacted.
- The editor sidebar's secret card opens the same masked form.
- Exporting secrets into terminal tabs was dropped: the terminal is the user's own shell.
- OAuth tokens are stored as secrets with an expiry.

## Acceptance
- [x] A workspace-scoped secret reaches that workspace's MCP servers and no other's.
- [x] `JIRA_URL` values are not redacted; secrets are, as `‹secret:NAME›`.
- [x] Repair classifies old entries; idempotent.
- [x] The editor crew chat collects a missing secret masked (paste included).
