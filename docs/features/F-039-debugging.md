# F-039: Debugging in the editor

Status: complete; verified against real delve v1.27.2 on macOS (see
"Real-delve verification") · Companion to:
F-038 (pairing), F-009 (LSP), ADR-0005 (hermetic toolchain).

## Summary

Set breakpoints, run the current Go package under a debugger, stop,
inspect and step — in the editor — and let an agent read the same state.

## Behaviour

- `internal/dap`: a Debug Adapter Protocol client (Content-Length framed
  JSON, request/response correlation, ordered events) and a `Session`
  that runs the launch handshake in the order adapters require
  (initialize → launch → `initialized` event → setBreakpoints →
  configurationDone → launch response), tracks stops (frames, locals of
  the top frame, a `StopSeq` counter), output (capped) and exit, and
  offers Continue/Next/StepIn/StepOut/Evaluate/SetBreakpoints/Stop.
  Connection loss ends the session with a visible error.
- `dap.StartDelve` resolves `dlv` from the DHI toolchain PATH (never the
  host PATH), runs `dlv dap --listen=127.0.0.1:0`, reads the announced
  address and connects. A missing adapter refuses by name.
- Editor commands: `:break` toggles a breakpoint at the cursor (also
  pushed to a live session; unbound lines are reported), `:debug` starts,
  `:cont :next :step :out` drive, `:eval <expr>` evaluates at the top
  frame, `:stop` ends. A stop jumps to the top frame's file/line, marks it
  `▶` in the gutter, and opens the debugger panel (stack, locals, output;
  `c n s o x`, `enter` go to frame, `esc` hide, `ctrl+d` reopen).
  Breakpoints show `●` in the gutter and beat a git change mark.
- Agents: served tool `debug_state` (Read scope) returns the stop reason,
  call stack (VPaths), locals and recent output.

## Acceptance

- [x] handshake order, breakpoints + launch args, stop capture, step,
      evaluate, continue-to-exit, stop/disconnect, connection loss,
      launch failure named, unverified breakpoints reported (`dap` tests
      against `testutil/dapfake`)
- [x] `dlv` launcher: connects to the announced address; missing adapter,
      silent adapter and refused connect each fail by name
- [x] editor flow end-to-end on the fake adapter (break → debug → stop →
      step → eval → continue → exit; `:stop` sends disconnect; refusals)
- [x] `debug_state` routes through `DebugAPI`, refuses without it

## Real-delve verification

Run on macOS (arm64) with delve v1.27.2 built from source by the pinned
go1.27.0 (`CGO_ENABLED=0`), after enabling macOS Developer mode:

- `dap.TestLiveDelve` (client) and `editor.TestLiveDebugThroughTheEditor`
  (editor commands) both PASS against the real adapter: spawn + announce +
  connect, initialize, launch with a build, breakpoint stop at the right
  file/line, locals (`n = 7`), `:eval n*3 = 21`, step to the next line,
  `▶` stop marker, continue to exit with code 0, and the program's own
  stdout captured (`result 49`).
- Gated like the other live smokes:
  `DHI_SMOKE_DAP=<dir with dlv> DHI_SMOKE_DAP_PROG=<dir with go.mod+main.go>
  go test ./internal/dap ./internal/tui/surfaces/editor -run Live -v`
  (`main.go` needs `r := n * n` on line 6). Without the variables they skip.
- **Bugs real delve found** (the scripted adapter could not have): a refused
  launch looked like a 3-minute hang (the handshake only waited for
  `initialized`); the adapter's reason lives in `body.error.format`, not
  `message`; and the debuggee's stdout never reached us until the launch
  asked for `outputMode: "remote"`.
- macOS prerequisite: a `go install`ed delve uses lldb's `debugserver`,
  which needs Developer mode / developer-tools authorization; before it was
  granted launch stalled and the smoke test skipped with that explanation.
  Linux has no such prerequisite.

## Deferred

- Hermetic `dlv` provisioning: not wired. As shipped, **`:debug` refuses
  by name until `dlv` is on the DHI toolchain PATH.** `toolchain.BuildSpec`
  can build it like `gopls` (v1.27.2 builds with `CGO_ENABLED=0`; add a
  `Delve()` spec and a confirm-first install in the boot gate).
- Watch expressions, conditional/log breakpoints, goroutine/thread list,
  attaching to a running process, test debugging (`mode = "test"`).
- Breakpoints do not follow line edits and are not persisted.
- Non-Go adapters (the client is adapter-agnostic; launch config is Go).
