# F-089 — Agents ask for secrets by name

Status: done (M32 P4) · ADR-0039

## Behaviour
- **`secret_request {name, reason}`** is a harness tool. The name must be
  an env-var name.
  - A stored secret the agent was already granted is reported available
    straight away.
  - Otherwise a card goes on the approvals queue (`AskSecret`). The card
    says whether the value still has to be typed (`Approval.Missing`).
  - The tool result only ever says `$NAME is set for you in DHI's run
    tool`. It never contains the value.
- **Typing the value.** Answering the card from the workspace chat pane or
  the inbox opens a masked form (`fSecret`): the value plus "save for: this
  workspace / every workspace" (`g` preselects every workspace). The value
  goes straight to the credential store, then the card is granted.
  - The editor sidebar can deny a missing secret but not fill it in; it
    has no masked field.
  - A focused `kit` secret field is now masked too: `CursorValue` used to
    show the characters.
- **Store** (`credstore`): global secrets stay in `[credentials]`.
  Workspace secrets go in `[workspaces."<abs root>".credentials]` in the
  same 0600 user file, never under `.dhi/`. `LookupIn` checks the
  workspace first, then global. Also new: `SetIn`, `DeleteIn` and
  `Secrets` (names and scopes only). `Values` includes workspace secrets,
  so they are redacted too.
- **Injection** (`run` and `skill_run` only). `Deps.SecretEnv` passes
  `NAME=value` for each secret granted to the agent, through
  `EnvRunner.RunEnv`. A runner without `RunEnv` refuses rather than running
  without them. The values are never added to the agent CLI's environment,
  where its own shell could print them into its session.
  - Output that echoes a value is redacted (F-088).
  - `run` is network-denied unless the agent has the network scope, which
    the tool text says to request.
- **Prompt:** grounding tells every agent to call `secret_request` and
  never ask for a credential in chat. It names the secrets already granted
  (`$NAME`), never their values.
- **Settings → SECRETS:** names, scope and which employees may use each.
  `a` adds one (masked), `e` replaces the value, `x` deletes. Revoking an
  agent's use is AGENTS → `r`.
- **Doctor** `secrets/host-env` warns, by name only, about exported
  variables that look like credentials. Every agent inherits those (your
  choice).

## Acceptance
- [x] missing secret → Missing card; value stored + yes → result names $NAME, never the value
- [x] run env carries granted secrets for that agent only; prompt names them
- [x] a yes without a value fails by name
- [x] run passes secrets through RunEnv; a plain runner refuses
- [x] chat/inbox: yes opens the masked form; the value never renders; saved for the workspace; granted
- [x] SECRETS: add masked, list names only
- [x] doctor names token-like env vars without values
- [x] live: an agent asks for a secret, `run` uses it, transcript/bus/trace show ‹secret:NAME› (M32 P7; run in F-092 with STRIPE_TEST_KEY. The `@yf` label suffix it showed is gone since F-104)
