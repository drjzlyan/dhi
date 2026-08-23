# STATE — current position

Updated: 2026-08-23 (session: M2 closed — LSP foundation)

## Where we are

Settings skeleton committed (`2203974`). **This session (uncommitted —
commit next): LSP foundation — the LAST M2 roadmap item.** `make verify`
green; 21 packages under `-race`.

## Just finished

- `internal/lsp`: minimal JSON-RPC 2.0/LSP client over duplex io
  (Content-Length framing). initialize handshake, full-text
  didOpen/didChange, publishDiagnostics → Event stream,
  textDocument/completion (array + {items} shapes), polite Shutdown
  with 2s bounded wait. Server→client requests auto-answered null.
- Manager: per-language cached clients resolved from toolchain shims
  (`<root>/bin/gopls`); missing server = capability silently off
  (ADR-0005). Inject() seam for tests.
- Editor integration: .go buffers didOpen on open + full-text
  didChange after edits (content-diff dedup); diagnostics render as
  gutter-colored line numbers + ✗n/⚠n title chip; ctrl+space in insert
  mode opens completion popup (j/k/enter/esc) inserting accepted label.

## Gotchas learned (do not re-learn these)

1. **Client.Events was nil** — composite literal set `events` but never
   the exported alias; receives on a nil chan block forever while sync
   calls still work. Constructor assertion or vet-style check would
   catch this class.
2. readLoop must start BEFORE the initialize handshake — responses are
   routed by the reader; starting it after deadlocks every handshake.
3. Shutdown must use a bounded context: teardown hung because the fake
   server dropped unknown requests (call select waited on a ctx that
   never fired).
4. Fake LSP servers should reply null to ANY unknown request and echo
   received URIs back in diagnostics so tests stay path-agnostic.

## Next up

1. Commit this session (user approves diff first).
2. **M2 complete** after commit. Sweep F-002 acceptance criteria
   (cross-repo edit ✓, stage/commit without leaving editor ✓, md
   preview ✓, scripted-key coverage ✓) and flip F-002 status to done.
3. M3 agent runtime per ROADMAP: agentkit manifest + Provider iface
   (Anthropic + Mock), VPath tools behind sandbox policies, message bus.

## Open questions for user

- gopls registry pins: add to manifest during next pinning run (same
  workflow as rg/uv/node)? Blocks only real-server smoke; all tests use
  fake servers.