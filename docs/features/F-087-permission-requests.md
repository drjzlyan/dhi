# F-087 — Agents ask for access; sandbox denials become cards

Status: done (M32 P2) · ADR-0039

## Behaviour
- **`permission_request {kind, target, reason}`** is a harness tool that
  every agent gets. The card it parks is the approval, so the tool itself
  needs no scope.
  - `write <abs folder>`: the path is normalized (`~` expanded, symlinks
    resolved, so `/tmp` becomes `/private/tmp`).
  - Refused by name: `/`, your home dir, the workspace `.dhi/`, DHI's user
    config dir (credentials and grants), and the credential trees the
    sandbox already hides (`sandbox.CredentialPaths`).
  - It waits for your answer. A grant sets the turn's relaunch: when the
    spawn ends, DHI posts `@agent continue: …` and the next spawn's sandbox
    includes the path (`spawnSandbox` → `WithExtraRoots`).
  - Asking again for a path inside a granted one returns at once.
  - `network` and `exec` ask for those capability scopes. The scope is
    checked on each call, so it applies straight away.
- **Denials become cards.** An EPERM tool error with a path
  (`clirun.DeniedPath`: claude tool results, codex command output) raises
  the same write-access card without the agent asking. There is one card
  per agent and path while it waits. A grant wakes the agent in the thread
  to retry. EACCES ("Permission denied") is ignored: no sandbox grant fixes
  a Unix permission problem.
- **Layers:** `tools.Layer` is once (in memory, this DHI session), this
  workspace (`.dhi/grants.json`) or everywhere (`<config>/dhi/grants.json`,
  loaded with `PersistUser`). Access grants are scopes of the form
  `write:<path>`, so the existing list and revoke work on them unchanged.
- **One meaning per key on every surface** (`Approvals.Decide`,
  `tools.DecideKeys`): `y` yes, `o` once, `a` always here, `g` everywhere,
  `n` no. A plain `y` on a sandbox-access card remembers it for the
  workspace, because a sandbox fixed at spawn can't be widened for a single
  call.
  - **Chat pane:** a pending ask from the agent shows above the status line
    in its DM (in a channel, any agent's ask shows), with the keys. They
    work while the composer is not focused.
  - **Inbox:** approval rows answer in place; enter still opens the editor
    chat.
  - **Editor chat sidebar:** the same keys.
- **Settings → AGENTS → ALWAYS ALLOWED** shows each grant's layer, with
  `write <path>` spelled out. `r` revokes the selected agent's grants.

## Deviations from the plan
- There is no separate GRANTS section; the existing ALWAYS ALLOWED list
  gained the layer column.
- There is no `read` kind. macOS reads are already open (except the
  credential trees, which are never grantable), and on Linux the only seam
  is a read-write root. That would grant more than was asked.
- DHI's own `run` tool uses one shared runner, so write grants reach the
  agent CLI's sandbox but not `run`.

## Acceptance
- [x] grant layers persist to their own files; once is memory-only; revoke rewrites the right file
- [x] guarded / broad targets refused before any card
- [x] permission_request → card → grant → relaunch; next spawn's sandbox has the root
- [x] a denial raises one card; a grant posts the continuation
- [x] DeniedPath: the yume-forge mkdir, touch/open/node errors; EACCES and "/" never offered
- [x] chat pane: the ask shows in its agent's DM with keys; y grants for the workspace; other DMs and the composer unaffected
- [ ] live: an agent blocked writing outside its roots gets a card, `y`, and finishes (M32 P7)
