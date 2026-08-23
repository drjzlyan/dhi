# STATE — current position

Updated: 2026-08-23 (session: M2 — ripgrep fan-out search)

## Where we are

Editor nav tree + fuzzy find committed (`52f8ea9`). **This session
(uncommitted — commit next): cross-repo ripgrep content search** wired
end-to-end through the hermetic toolchain. `make verify` green; 14
packages pass under `-race`; live-artifact smoke passes with
DHI_SMOKE_NET=1.

## Just finished

- `internal/search` service: `Searcher` iface + `Ripgrep` backend.
  Runs DHI's own rg from the shim path with --json -F -S, 1MB file cap,
  2000-hit cap; streams hits over a channel; context-cancel stops rg.
  NDJSON parser unit-tested offline (match/begin/end/summary/garbage).
- Editor integration:
  - `editor.New(version, ws, opts...)` + `WithSearcher`; `s` opens the
    query overlay; enter runs fan-out across all member roots.
  - New modeResults view: hit list (`vpath:line  text`), live count +
    searching indicator, errors surfaced in-pane (e.g. rg missing).
  - enter on a hit jumps (reveal in tree + open vpath); esc cancels the
    running search and returns to nav. Streaming via hitMsg/searchDoneMsg
    through Surface.Update.
  - No searcher → `s` inert (ADR-0005: no silent host-rg fallback).
- cmd/dhi wires `search.Ripgrep{Bin: <prefix>/bin/rg}` only when the
  shim exists.
- Live gated test `TestLiveRipgrepThroughToolchainShim`: pinned install →
  shim → spawn → parse, all green.

## Gotchas learned (do not re-learn these)

1. Test fixtures must share ONE workspace instance per test — each
   setupWorkspace() call makes a different tempdir; paths built from a
   second instance never match the model's ws (hit labels fell back to
   basenames and jumps silently no-op'd).
2. startSearch switches to modeResults BEFORE calling the searcher so
   startup errors render inside the results pane.
3. exec.CommandContext variadic gotcha: build args then append roots —
   don't mix literals and spread in one call.
4. Carried: sorted member order at Load; ANSI-strip before measuring;
   Panel fields not chainable; dirs-first tree ordering in key sequences.

## Next up (rest of M2)

1. Commit this session (user approves diff first).
2. Modal buffer MVP: normal/insert modes, motions, d/c/y, :w :q :wq :e —
   the biggest remaining M2 chunk; design buffer model first
   (gap buffer vs rope vs lines slice) behind an editor core package so
   tests drive it without the TUI.
3. Then terminal drawer (PTY), markdown preview, git view (go-git),
   LSP foundation, settings skeleton.

## Open questions for user

- None blocking.
