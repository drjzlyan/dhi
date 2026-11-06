# F-123 — Switch off the agent CLIs' own web tools

Status: done for claude, codex, opencode, copilot, antigravity · cursor cannot be restricted (reason below) · gap G-C1 · ADR-0046

Every adapter runs its CLI with its own permission checks off (the OS sandbox is the boundary,
ADR-0012). That leaves the CLI's built-in web fetch and search tools available, and a web page is
the easiest place for an injected instruction to come from (F-122) and for data to leave through.
DHI now switches those tools off by default where the CLI has a way to, and says so where it does not.

## Behaviour
- Setting `agents.native_web`: `deny` (default) or `allow`. It is read each turn (`liveTuning`), so
  a change in Settings applies to the next spawn. An agent cannot propose it (security posture,
  not in `settings.proposable`). Settings shows it as "CLI web tools" and names the CLIs it
  cannot reach.
- `clirun.CLI.WebDenyOK` declares a live-verified mechanism; `RunInput.DenyWeb` and
  `CLI.DenyWebEnv` carry it. `Registry.CannotDenyWeb()` is the single source for the list that
  Settings and doctor print, and a test ties `WebDenyOK` to a real argv/env change.
- The zero `Tuning` denies, so a missing tuning never widens the surface.

| CLI | Mechanism | Verified |
|---|---|---|
| claude | `--disallowedTools WebFetch,WebSearch`, in the argv, stdin and resume branches | 2.1.285, `system/init` tool list under `bypassPermissions`: both present without the flag, both gone with it |
| codex | `-c web_search=disabled`, fresh and `exec resume`; prompt stays the last arg | 0.162.1: asked "do you have a web search tool", `live` → YES, `disabled` → NO; bad values rejected by name |
| opencode | env `OPENCODE_PERMISSION={"webfetch":"deny","websearch":"deny"}`, set after the pass-through | 1.19.0: resolved into `debug config`; `webfetch` left the tool list |
| copilot | `--excluded-tools=web_fetch,fetch_copilot_cli_documentation` (removes the tools from the model's view, so `--allow-all-tools` cannot restore them). It has no web search tool | 1.0.88: both listed without the flag, both absent with it. Not run together with `--allow-all-tools` (that run was blocked); the CLI's help states allow flags do not expose excluded tools |
| antigravity | a `PreToolUse` hook, matcher `search_web\|read_url_content\|open_browser_url\|browser_.*`, in `config/hooks.json` of the per-turn `--gemini_dir` mirror. It is merged into the user's own `hooks.json`, which is never edited; a hooks file that is not a JSON object stops the turn by name. The denial needs the mirror: a spawn with no IDE-tools mirror (reflection, no served tools) gets a deny-only one that keeps the user's `mcp_config.json`; a spawn is refused by name only when that mirror cannot be built, or its mirror was built before `native_web` became deny | agy 1.3.4, in a throwaway mirror: the hook ran for `search_web` and the step failed with "tool call denied by pre-tool hook". Not run under `--dangerously-skip-permissions` (that run was blocked); the hook contract documents `deny` as a hard block |
| cursor | **none possible with `--force`.** Read from the shipped 2026.10.12 source: `requestWebFetchApproval` returns approved when `skipApproval`, `forceApprove` or run-everything is set, and `requestWebSearchApproval` when `forceApprove`, run-everything or `autoAcceptWebSearch` is set. Neither consults a deny rule, so a `permissions.deny` entry in `cli-config.json` or a project `.cursor/cli.json` would not be read. Headless runs need `--force` for shell and writes, so dropping it is not an option. | static source reading, not a live run (a `--force` run is blocked here). `WebDenyWhy` carries the reason; doctor prints it; a test requires one for every adapter that cannot deny |

- `dhi doctor`: `runtime/<cli>/web` is OK ("switched off unless agents.native_web = allow") or a
  warning ("no verified way to switch off …").

## What this does not do
- It does not stop a shell command (`curl`) run by the agent; the sandbox's network rules and
  scopes govern that. Phrase it as "native web tools off", not "no network".
- It does not stop what a CLI's own MCP servers or plugins fetch.
- Claude's `WebSearch` runs on the vendor's side, so no local sandbox could block it; the flag is
  the only control (taken from the flag's behaviour; not separately tested).

## Acceptance
- [x] Claude: flag present in the argv, stdin and resume branches; absent without `DenyWeb`.
- [x] Codex: setting present fresh and resumed; prompt still last.
- [x] Opencode env set after the pass-through, only when denying.
- [x] `WebDenyOK` equals "argv or env actually changes" for every adapter.
- [x] Runtime: default denies; `AllowNativeWeb` drops it (argv and env).
- [x] Settings validation and default; doctor rows for a supported and an unsupported CLI.
- [x] Live checks for claude, codex and opencode (table above).
- [x] Copilot: `--excluded-tools` present only when denying; antigravity: hook merged not edited, valid deny reply from the inline command, broken user file refused, mirror carries the hook by default, a spawn with no tools mirror gets a deny-only one, and a mirror that cannot be built (or lacks the hook) is refused by name.
- [x] Cursor: investigated; no mechanism exists under `--force`, so it is listed as unable to restrict with the reason. Re-check on each cursor version (the adapter comment says so). Open decision: whether `deny` should refuse to run a CLI that cannot comply, instead of warning.
- [x] `make verify`.
