# STATE — current position

Updated: 2026-08-23 (session: M2 closed)

## Where we are

**M2 COMPLETE AND COMMITTED (`5ea7b75`).** All five views live; F-002
flipped to done with per-component annotations. `make verify` green;
21 packages under `-race`. Next milestone: M3 agent runtime.

## Just finished

- go1.27.0 pinned in registry (digests from go.dev JSON + independent
  hash check); BuildInstall source-build pipeline; gopls v0.23.0 built
  hermetically through the pinned toolchain (live smoke passing).
- F-002 flipped done: nav tree/fuzzy/rg search ✓ buffers/tabs ✓
  terminal drawer ✓ git view ✓ preview ✓ LSP foundation ✓; chat
  sidebar → M3, worktrees → M5, polish → M7.

## Gotchas learned

1. Shim recursion: never exec a shim by PATH when Env() prepends its
   own dir — resolve symlinks to real binaries first.
2. defer f(g(x)) evaluates g at registration — wrap closures for
   teardown-time work.
3. Go module cache: files 0444, dirs 0555 — chmod both before RemoveAll.
4. Carried: nil Events chan wiring; readLoop before handshake; bounded
   shutdown; fake servers echo URIs + null-reply unknown requests.

## Next up (M3 agent runtime per ROADMAP)

1. Feature spec F-003-adjacent: write docs/features/F-0xx-agent-runtime
   spec (manifest format, Provider iface, tools, message bus) BEFORE code.
2. agentkit manifest + validation; Provider iface w/ Anthropic + Mock.
3. Namespaced VPath tools behind sandbox Guard policies.
4. Message bus (DMs/channels/threads, mention turns, JSONL persistence).
5. Editor chat sidebar consuming the runtime.
6. Optional UX: bootstrap surface offers "build gopls" step post-install.

## Open questions for user

- None blocking.
