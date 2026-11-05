# F-148 — Restricted mode for folders you have not trusted

Status: done · gap G-S1

Opening a repo was enough for DHI to run what the repo steers: gopls runs `go list`, a formatter
runs on save, tasks, tests and the debugger run its build config. ADR-0044 says DHI asks before it
acts on the user's system; a freshly cloned repo got no question.

## Decision
Per member folder, one yes, kept in DHI's own state: `trust.json` beside `config.toml` in the user
config folder (`internal/trust`). Never inside the repo or `.dhi/`, so a repo cannot trust itself.
Trusting a folder covers the folders below it. No new config format: the file is DHI state, edited
only through `:trust` / `:untrust`.

## Behaviour
- Untrusted folders still edit, search and use git. Gated at the start point of each feature:
  language-server start (`lspOpenDoc`), format (`formatEditor`: on save and `:fmt`), `runTask`,
  `:test`, `:debug`.
- A gated feature says `restricted: trust this folder to <what> (:trust)` by name (ADR-0011) and
  opens the trust card once per folder per session (`y` trusts, `n` stays restricted).
- `:trust` / `:untrust` and a palette action ("Trust this folder…" / "Stop trusting this folder")
  flip it; trusting starts the language server that was held back.
- The setup wizard's workspace creation trusts the new workspace root.
- `dhi doctor` row `workspace/trust`: OK when every member is trusted, Warn naming the restricted
  members; Warn when the record is unreadable (everything stays restricted).
- An editor built without `WithTrust` (tests, embedders) trusts everything; `cmd` always wires it.

## Acceptance
- Store: untrusted by default, persists, covers children not siblings, untrust, corrupt record
  trusts nothing (`internal/trust` tests).
- Tasks, tests, debug and format refuse by name until trusted
  (`TestRestrictedBlocksTasksAndNamesTheWayOut`, `TestRestrictedBlocksTestsAndDebug`,
  `TestRestrictedBlocksFormat`).
- The server is held back, the card asks once, `y` records and starts it
  (`TestRestrictedHoldsTheLanguageServerAndAsksOnce`, `TestTrustCardYesRecordsAndStartsTheServer`).
- `:trust`, `:untrust`, palette (`TestTrustUntrustCommands`, `TestTrustPaletteActionFlips`).
- Doctor row (`TestTrustRow`).
- `:untrust` stops the running language servers (`TestUntrustStopsRunningLanguageServers`).
- Listing tasks (F-146) in an untrusted folder reads the justfile and never runs `just`, which
  evaluates the file (`TestRestrictedTaskListNeverRunsJust`).

## Not covered
Agent tools (shell, MCP) are governed by the sandbox and approval cards (ADR-0006, ADR-0044), not by
folder trust.
