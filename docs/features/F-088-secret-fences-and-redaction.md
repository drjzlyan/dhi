# F-088 — Secret fences and redaction

Status: done (M32 P3) · ADR-0039

This comes before the secrets feature (F-089) on purpose: a secret DHI
injects by name is only kept out of the agent's view if the agent can't
read the store and never gets the value back.

## Behaviour
- **The sandboxes fence DHI's own secrets.** `sandbox.CredentialPaths` now
  also lists `<config>/dhi/credentials.toml` and `<config>/dhi/grants.json`.
  - macOS: the Seatbelt profile denies reading them, as it does `~/.ssh`.
  - Linux: bubblewrap already shows only bound roots. When a bound root
    covers a credential path (a CLI state root or a grant over
    `~/.config`), the path is masked: a directory becomes an empty tmpfs, a
    file `/dev/null`.
  - The same list is never grantable (F-087).
- **`internal/redact`.** A `Redactor` replaces every stored credential
  value (6 characters or longer) with `‹secret:NAME›`, longest value first.
  It re-reads the store when the file's size or mtime changes, so a secret
  added mid-session is covered.
  - It runs on every bus post, where it is persisted and delivered
    redacted. This covers agent replies, stream lines, notices and a
    secret you paste into chat.
  - It runs on every served tool result, DHI's and bridged MCP servers'
    (`redactHandler`), so the agent never reads the value back.
  - It runs on run summaries and errors (stderr tail included) and on
    every transcript record, including the full prompt text when
    `agents.trace_prompts` is on. Trace exports are built from these.

## Not done, on purpose
- **No keychain fence.** Claude Code on macOS reads its own login from the
  Keychain through `/usr/bin/security`. Denying that binary would break
  the engine. Secrets DHI stores live in the fenced file, never the
  keychain. A value someone put in the `dhi-mcp` keychain service (the
  ADR-0028 lookup) stays readable from inside the sandbox, and doctor
  should say so (P7).
- **Literal values only.** A transformed value (base64, reversed, split)
  is not caught. ADR-0039 records this.
- **Host env.** Agents still inherit your exported variables, per your
  decision.

## Acceptance
- [x] Seatbelt profile denies reading the credentials and grants files
- [x] bubblewrap masks the secrets file inside a bound root, and nothing outside one
- [x] bus: delivered and persisted text redacted
- [x] served tool results redacted
- [x] redactor: longest value first; picks up a new secret when the store changes; nil is a no-op
