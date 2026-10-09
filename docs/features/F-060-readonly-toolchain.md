# F-060 — The toolchain is read-only to agents (ro-root differentiation)

Status: done (M27 P7; the F-010 "ro-root policy differentiation" deferral)

## Problem
The OS sandbox adapters (seatbelt, bubblewrap) supported read-only roots
from F-010, but boot never passed any. The whole toolchain prefix was a
writable root, so any sandboxed agent process could replace DHI's pinned
`go`, `node`, `git` or `gh`. The next build or review would then run the
replacement.

## Behaviour
- Boot passes the toolchain prefix as a **read-only** root: agents can read
  and execute the pinned tools but never modify them.
- Only the prefix's runtime caches stay writable
  (`toolchain.WritableSubdirs`):
  - `npm-cache`, which npx-run MCP servers use;
  - `mcp-home`, the MCP servers' private HOME.
  Boot creates them so the binds exist.
- Bubblewrap now binds read-only roots before writable ones, so a writable
  child inside a read-only root stays writable (a later bind shadows an
  earlier one). Seatbelt rules are additive and needed no change.
- DHI itself installs and updates the toolchain outside the sandbox, so
  installs, `:lsp install` and gopls/dlv builds are unaffected.
- A coding CLI that DHI installed under the prefix can no longer
  self-update in place. DHI's version policy (ADR-0027) owns those
  upgrades.

## Acceptance
- [x] boot: the prefix is out of the write rule, its caches are in, and the
      prefix stays readable/executable
- [x] live under the real sandbox (`sandbox-exec` on macOS; `bwrap` on
      Linux CI where user namespaces allow it): a write into the read-only
      root is refused, the cache inside it accepts writes, reads work
- [x] bubblewrap bind order: ro first, rw on top
