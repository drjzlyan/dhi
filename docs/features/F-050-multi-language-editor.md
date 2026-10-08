# F-050 — Multi-language editor (language servers + formatters)

Status: done (M26)

## Why
The editor's LSP was Go-only (five `.go` suffix gates, `"go","gopls"` hard-wired) and, as
found while building this, **never started in the shipped app** (`editor.WithLSP` was only
used by tests). Both are fixed here.

## Behaviour
- One table (`internal/langserver`) maps file extension → language id, server binary, args,
  how DHI provisions it, and indentation. Built-ins: Go, TypeScript/JavaScript, Python, Bash,
  YAML, JSON. The LSP `languageId` sent in `didOpen` comes from the table (`.tsx` is
  `typescriptreact`, `.sh` is `shellscript`).
- Defaults work with no configuration. Everything is overridable in settings:

  ```toml
  [editor.languages.python]
  enabled = true               # false switches the language's server off
  command = "/opt/homebrew/bin/basedpyright-langserver"   # absolute path or a toolchain binary name
  args = ["--stdio"]
  formatter = ["ruff", "format", "-"]   # stdin → stdout; empty = the language server's formatting

  [editor.languages.rust]      # a language DHI does not know: needs exts + command
  exts = [".rs"]
  command = "/Users/me/.cargo/bin/rust-analyzer"
  ```
  An absolute `command` is the user's explicit choice, never a PATH fallback (ADR-0005/0011).
- **Provisioning is confirm-gated and never happens on open.** Opening a file whose server is
  missing says so once per language and names `:lsp install <lang>`. That command prints the
  exact pinned packages and destination; `:lsp install <lang> yes` installs them with DHI's own
  npm (`toolchain.NPMInstall`) into `<toolchain>/lsp/<lang>`. Go's gopls ships with the toolchain.
  Verified pins (installed and handshaken with the hermetic npm, 2026-10-08):
  `typescript-language-server@6.0.1` + `typescript@5.9.3` (TypeScript 7 has no `tsserver`, the
  server refuses to start), `pyright@1.1.414` (`pyright-langserver --stdio`),
  `bash-language-server@5.8.1` (`start`), `yaml-language-server@1.24.0`,
  `vscode-langservers-extracted@4.10.0` (`vscode-json-language-server`).
  Not provisioned by DHI (no verified hashes): rust-analyzer, clangd — set `command` yourself.
- **Formatting:** an external `formatter` if configured; else the server's formatting **only if
  it advertises `documentFormattingProvider`** (pyright does not and returns "Unhandled
  method"); otherwise a named "no formatter for X" — never a host-tool fallback. Indentation
  follows the language (Go tabs; others spaces).
- Debugging and the test runner stay Go-only (delve / `go test`).

## Acceptance
- [x] extension → language table test; user overrides, disable, and custom languages
- [x] unknown/invalid override keys are reported, not silently dropped
- [x] `languageId` per extension reaches `didOpen`
- [x] a `.ts` file reaches its own client; `.go` still reaches gopls
- [x] missing server says the fix once per language; nothing installs on open
- [x] `:lsp install` shows the plan, installs only on `yes`
- [x] formatting gated by the server's advertised capability; external formatter works
- [x] LSP manager started in the app and shut down on every `runTUI` return
- [x] real handshake with the pinned servers (gated live test, `DHI_SMOKE_LSP`)
