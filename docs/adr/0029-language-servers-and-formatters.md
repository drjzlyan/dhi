# ADR-0029: A language table, DHI-managed servers, and capability-gated formatting

Date: 2026-10-08 · Status: accepted · Companion to: ADR-0005 (hermetic),
ADR-0011 (no fallbacks), ADR-0027 (npm into a DHI-owned folder). Serves: F-050.

## Context

The editor spoke to one language server and hard-wired it (`"go"`, `"gopls"`,
five `.go` suffix checks). Worse, the manager was never constructed in the real
app, so no server ever ran outside tests. Other languages need a server chosen
by file type, a way to get it that respects the hermetic rules, and a formatter
story that does not paper over servers that cannot format.

## Decision

1. **One language table** (`internal/langserver`): extension → language id, LSP
   `languageId`, server binary and arguments, provisioning method, indentation.
   Built-ins: Go, TypeScript/JavaScript, Python, Bash, YAML, JSON. Users override
   or add languages under `[editor.languages.<id>]`; a bad override is reported
   by `dhi doctor` and ignored, never fatal.
2. **Servers are found without the host `PATH`**: an absolute `command` the user
   configured, then DHI's toolchain shim directory, then the language's managed
   install `<toolchain>/lsp/<id>`. rust-analyzer and clangd are not provisioned
   (no verified hashes); users point `command` at them.
3. **Provisioning is confirm-gated and never happens on open.** Opening a file
   whose server is missing says so once per language. `:lsp install <lang>` prints
   the pinned packages and destination; `... yes` installs them with DHI's own npm
   (`toolchain.NPMInstall`, ADR-0027). Pins are versions that were installed and
   handshaken by the gated live test (`DHI_SMOKE_LSP=1`). `typescript` is pinned to
   5.x because TypeScript 7 ships no `tsserver` and the server refuses to start.
4. **Formatting** uses, in order, the language's configured external `formatter`
   (stdin → stdout, run in the same hermetic environment as the terminal drawer,
   bare names resolved against that PATH only), else the server's formatting **only
   if it advertises `documentFormattingProvider`**. Otherwise the editor says there
   is no formatter. Pyright, for one, answers "Unhandled method" and is never asked.
5. The LSP manager is started with the app and shut down on **every** return from
   `runTUI`, because the setup relaunch loop re-enters it.

## Consequences

- Debugging and the test runner remain Go-only (delve, `go test`).
- A configured formatter or server `command` is the user's explicit choice, run
  with the hermetic environment; this is not a host-tool fallback.
- Server start is still synchronous on first open (as gopls was); a wedged server
  can stall that one open. Moving it off the UI thread is future work.
