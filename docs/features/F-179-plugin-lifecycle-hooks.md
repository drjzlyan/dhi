# F-179 — Plugin lifecycle hooks in the vendor format

Status: done · gap G-C3 · ADR-0047 (accepted, with the boundary rule)

ADR-0043 §3 listed a plugin's `hooks` and ran nothing. Plugins that ship policy hooks
(`hooks/hooks.json` or the manifest's `hooks`) got no behaviour in DHI.

## Decision (ADR-0047)
Vendor JSON, no DHI-only format, no new file. Hooks fire at DHI's tool boundary and run edges, and on
a CLI's own tools only where the CLI takes hooks through per-run configuration DHI passes. DHI never
edits the user's persistent CLI config.

## Behaviour
- **Parse** (`install.ReadPlugin` -> `hooks.Parse`): events PreToolUse, PostToolUse,
  UserPromptSubmit, Stop, SessionStart; `type: "command"` only. Other types, other events, bad
  matchers and empty commands go to `Set.Unsupported` and show on the card as "not run". `hooks` is no
  longer in `Plugin.Unused`. Timeout is the vendor's seconds, default 30 s, capped at 120 s (clamp is a
  warning).
- **Install card** shows every hook command in full (wrapped to the dialog, never clipped) with its
  event, matcher and timeout. Installing copies the plugin folder to `.dhi/hooks/<plugin>` (or
  `<config>/dhi/hooks`) and records source, commit and digest in `installed.toml` under `[hooks]`.
  Hooks are **off**. Reinstalling resets to off.
- **Enable per plugin**: Settings, INTEGRATIONS, `h`. Enabling (and every run) checks the stored
  folder against the install digest; a changed copy is refused by name and never runs.
- **Matcher** is the vendor regex, anchored, over tool names. Vendor names map to DHI tools:
  Bash -> `run`; Write -> `write`; Edit -> `patch`, `editor_apply_edit`; MultiEdit -> `patch`;
  Read -> `read`; Glob -> `glob`, `list`; Grep -> `workspace_search`. DHI's own tool names and
  bridged `mcp__server__tool` names also match. `tool_name` on stdin is the vendor name; the DHI name
  rides in `dhi_tool_name`; `tool_input` gains `command` (run) and `file_path`.
- **Run**: `/bin/sh -c <command>` in the agent's workspace dir, JSON event on stdin, env gets
  `CLAUDE_PLUGIN_ROOT` (the stored folder) and the user's environment. It runs under the agent's OS
  sandbox with its scopes: no exec scope means no hooks (named record, once per turn); network only if
  the agent's network scope is auto; a sandbox that cannot deny the network refuses by name.
- **Exit codes**: 0 allow; 2 block (PreToolUse stops the call before the tool runs, UserPromptSubmit
  stops the turn, PostToolUse feeds the message back); anything else, a start failure or a timeout is a
  named `error` record. Failures do not block (vendor behaviour); they are visible, never silent.
- **Where DHI fires**: Pre/PostToolUse around every call on the served tool surface (DHI tools and
  bridged MCP servers; outside secret redaction so hooks never see a secret value); SessionStart,
  UserPromptSubmit and Stop at the CLI turn's edges. Stop is an observer: exit 2 is recorded, the turn
  is not restarted.
- **Prompt safety**: stdout of SessionStart/UserPromptSubmit hooks and block reasons returned to the
  agent are wrapped by `untrusted.Wrap` (ADR-0046) and can't close the fence.
- **Transcript**: each decision is a `hook` record in the attempt's saved transcript
  (`hook <plugin> [Event tool] allow|deny|error|skip: detail`); non-allow decisions and Stop are also
  posted in the thread.
- **Native tools (boundary rule)**: claude takes `--settings <file>`; DHI writes the enabled plugins'
  Pre/PostToolUse groups (vendor format = claude's) to a DHI temp file for that run and deletes it after.
  Nothing is written under `~/.claude`. DHI then skips, at its own boundary, any hook claude would also
  match on `mcp__dhi__<tool>`, so none fires twice. Boundary events stay with DHI so their output is
  fenced. codex, opencode, cursor-agent, copilot and antigravity have no verified per-run hook input
  (`CLI.HooksWhy`); `dhi doctor` has a `runtime/<cli>/hooks` row naming the gap.
- Write sites: `os.CreateTemp`/`Remove` of the per-run settings file in `runtime/hooks.go` (OS temp);
  the plugin copy in `install.go` (DHI hook folders). Recorded in `cmd/dhi/testdata/write_sites.txt`.

## Acceptance
- Vendor JSON parses to typed events, unsupported listed, timeouts always set
  (`TestParseVendorFormat`, `TestParseInnerMapAndErrors`, `TestPluginHooksAreParsedNotUnused`).
- Matcher table (`TestMatcherTable`, `TestNativeMatchIsTheCLIsOwn`); stdin shape (`TestInputJSONIsVendorShaped`).
- Block / allow / failure / timeout (`TestExitTwoBlocksAndIsRecorded`, `TestExitZeroAllowsAndNonMatchingNeverRuns`,
  `TestOtherFailuresAreNamedNotBlocking`, `TestTimeoutIsMandatoryAndNamed`).
- Scopes and sandbox (`TestExecScopeRequired`, `TestNetworkFollowsTheAgentsScope`,
  `TestSandboxExecRefusesWhatItCannotEnforce`, `TestSandboxExecRunsAndReportsExit`,
  `TestHooksNeedTheExecScope`, `TestHookSandboxRefusesWhenItCannotDenyNetwork`).
- Fencing (`TestPromptOutputIsFenced`); tool boundary (`TestWrapBlocksBeforeTheToolAndAnnotatesAfter`,
  `TestServedToolCallsAreHookedAtDHIsBoundary`); run edges and transcript
  (`TestBoundaryHooksFireFenceOutputAndLeaveTheTranscript`, `TestBlockedPromptNeverStartsTheCLI`).
- Off until enabled, digest check, remove (`TestInstallStoresHooksOffThenEnableRunsOnlyUnchanged`,
  `TestPickWithoutHooksStoresNone`); card and toggle (`TestInstallCardShowsHookCommandsAndHooksStartOff`).
- Per-run config for claude and its absence from the user's home
  (`TestClaudeGetsPluginToolHooksInAPerRunSettingsFile`, `TestClaudePerRunHooksSettings`,
  `TestNoEnabledHooksNoSettingsFile`, `TestNativeSettingsContents`); doctor
  (`TestEveryAdapterWithoutHooksArgsExplainsTheGap`, `TestHooksRowNamesTheCoverageGap`).

## Not verified live
Real `claude --settings` hook firing and exit-code-2 handling were not run (no real CLIs); the flag and
hook format follow Claude Code's documentation.
