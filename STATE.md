# STATE — current position

Updated: 2026-08-23 (session: M2 — settings skeleton)

## Where we are

Git view committed (`79183ae`). **This session (uncommitted — commit
next): settings skeleton.** `make verify` green; 20 packages under
`-race`. M2 remaining: LSP foundation only.

## Just finished

- theme.Light(): full daylight token set (light-paper).
- `internal/settings`: typed TOML schema (theme, editor.tab_width/
  line_numbers, terminal.scrollback); defaults<user<workspace precedence
  via pointer-diff layers (explicit false survives); sanitize clamps
  out-of-range + unknown theme names; UnknownKeys() for doctor; Save
  round-trip; Apply() swaps theme.Current live.
- Real Settings surface (view 5): j/k rows, ←/→/enter cycle values,
  auto-persist to nearest config path (workspace .dhi/config.toml when
  in a workspace, else user config), saved/session-only flash.
- cmd/dhi loads+applies config before first render; bootstrap gate
  refactored onto toolchainRoot() helper.
- doctor Config suite warns on unknown keys in .dhi/config.toml.

## Gotchas learned (do not re-learn these)

1. Bool config fields need pointer layers (*bool) to distinguish
   "explicitly false" from unset — plain bools make defaults sticky.
2. Settings persistence path = NEAREST scope that exists as a workspace;
   session-only flash when no path is available.
3. Carried: input-mode keys intercept before panel-global switch;
   vpath-substitute status messages for portable goldens; nil-channel
   pumps hang silently.

## Next up (close M2)

1. Commit this session (user approves diff first).
2. LSP foundation — the last M2 item. Design sketch:
   - `internal/lsp`: minimal JSON-RPC/LSP-over-stdio client
     (initialize, didOpen/didChange, publishDiagnostics receive,
     textDocument/completion, shutdown/exit) — no generic framework.
   - Server acquisition: resolve `<toolchain>/bin/<server>` shim;
     registry pins for gopls land with the next pinning run (same
     workflow as rg/uv/node); tests use a scripted fake server.
   - Surface: diagnostics count in buffer title/command line;
     completion on explicit key (ctrl+space) into a small popup list.
3. Then M3 agent runtime per ROADMAP.

## Open questions for user

- None blocking.
