# ADR-0047: Lifecycle hooks are read from installed plugins, in the vendor format

Date: 2026-11-04 · Status: proposed · Companion to: ADR-0043 (standard formats), ADR-0022 (install
trust), ADR-0044 (approval boundary), ADR-0018 (loopback tool surface)
Serves: gap G-C3.

## Context
ADR-0043 §3 reads plugins in the vendor format but "lists and does not run commands, agents or
hooks". Today `internal/agentkit/install/plugin.go` keeps `hooks` as a raw JSON blob and reports it
in `Unused`. Vendor plugins that ship hooks (`hooks/hooks.json` or the manifest's `hooks`:
`{"hooks":{"PreToolUse":[{"matcher":"Bash","hooks":[{"type":"command","command":"…"}]}]}}`; events
such as PreToolUse, PostToolUse, UserPromptSubmit, Stop, SessionStart; stdin is a JSON event, exit
code 2 blocks) get no behavior from them in DHI.
DHI owns the tool surface (ADR-0019, ADR-0018), so it sees every tool call it serves, but not the
host CLI's own native tools.

## Options
1. **Keep listing only.** No work, no new attack surface. Users cannot run the policy hooks their
   plugins ship; the "Unused" row stays a permanent gap.
2. **Run a vendor-format subset at DHI's tool boundary.** Parse the same JSON; fire `command` hooks
   on the events DHI can see (PreToolUse/PostToolUse on DHI-served tools, UserPromptSubmit, Stop,
   SessionStart). No DHI-only format, no new file. Native CLI tool calls are not hooked; doctor says so.
3. **Delegate to the CLI's own hook system** by writing hook config into each CLI's home. Writes
   outside DHI's folders (ADR-0044 §3), covers one vendor, and bypasses DHI's approvals.

## Recommendation (the user decides): option 2
- Only `type: "command"`; other types are listed as unsupported, not run.
- A hook is a shell command, the most dangerous plugin part. It runs under the OS sandbox with the
  calling agent's scopes, never wider. The install card shows every hook command verbatim (ADR-0022:
  pinned commit, scripts shown before they can run); a plugin's hooks are off until the user enables
  them per plugin.
- Matchers map by tool name; DHI documents a table from vendor names to DHI tools (`Bash` to `run`,
  `Edit`/`Write` to the edit tools).
- A blocking exit code is a deny, recorded in the run transcript like any approval decision.
- Timeouts are mandatory; a failed or timed-out hook is a named error in the transcript, never a
  silent skip (ADR-0011).

## Consequences
- Plugin hooks work in DHI where events overlap with the vendor's, with a stated coverage gap.
- A new path executes plugin-provided commands; mitigated by sandbox, card and per-plugin opt-in.
- Hook output that reaches a prompt is fenced as untrusted data (ADR-0046).

## If accepted
1. `internal/agentkit/install`: parse hooks into typed events, drop `hooks` from `Unused`, put the
   commands into the install card data.
2. `internal/agentkit/hooks` (new): event model, matcher, sandboxed exec with timeout, stdin/exit
   code contract; table-tested.
3. `internal/agentkit/dhitools` and `internal/agentkit/runtime`: call Pre/Post hooks around served
   tool calls; Stop and SessionStart at run boundaries.
4. `internal/agentkit/scopes`: hooks run with the agent's scopes; exec scope required to enable.
5. `internal/agentkit/untrusted` (fence output), `internal/doctor` (coverage row), Settings toggle.
6. Spec F-TBD (next free number); ADR-0043 §3 amended by reference.
