# F-104 — Secret gap fixes

Status: planned (session 61, M35 P4) · builds on F-088, F-089

## Why
Secret requests already keep values out of the model's context, but: MCP servers looked secrets up
globally only, so a secret saved "for this workspace" never reached them; MCP env ignored per-agent
grants; plain settings such as `JIRA_URL` were redacted as secrets; the redaction label leaked the
workspace folder name; the editor sidebar could not fill a missing value.

## Behaviour
- MCP lookups use the workspace first, then global.
- A server's `${VAR}` / env / header values that are DHI secrets are passed only for agents granted
  them; a missing grant becomes a secret request card.
- Credentials have a kind: `secret` (redacted, needs a grant) or `setting` (plain). A named repair
  classifies existing entries.
- Redaction label: `‹secret:NAME›`.
- The editor sidebar's secret card opens the same masked form.
- Settings → SECRETS `t` lets a secret be exported into new terminal tabs.
- OAuth tokens are stored as secrets with an expiry.

## Acceptance
- [ ] Workspace-scoped secret reaches an MCP server; an ungranted agent does not get it.
- [ ] `JIRA_URL` values are not redacted; secrets are, as `‹secret:NAME›`.
- [ ] Repair classifies old entries; idempotent.
