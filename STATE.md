# STATE — current position

Updated: 2026-09-09 (session 11: M8 P1 wave 1 landed — ADR-0013,
in-house engine removed, claude runtime only)

## Where we are

**M8 P1 wave 1 is functionally complete: `make verify` green, no
net-new failures.** ADR-0013 shipped with F-013: `internal/agentkit/provider`
is deleted, the native tool registry machinery is gone, and the
runtime is a thin CLI dispatcher. `runtime` is now required + CLI-only
on every manifest (`""`/`"anthropic"` rejected, unknown → error naming
the set). All rostered agents run through registered host CLIs —
today only `claude` (2.1.177) is registered. Approvals queue/panel
retained (nothing enqueues yet; UI + `Approvals.Ask` are the seams for
the future CLI permission-prompt bridge). Doctor drops `agents/api_key`,
tracks `runtime/<cli>` + `runtime/<cli>/<env_key>` rows. Tests script
fixture CLI stubs on a temp PATH (`internal/testutil/stubcli`) — the
shape that replaced `provider.Mock`. `.dhi/agents/dev.toml` migrated
to `runtime = "claude"`. Next: M8 P1 waves 2–3 (codex, opencode;
cursor-agent, copilot, gemini), then M8 P2 (F-014 run observability).

## Session 11 gotchas (engine removal)

1. `/bin/sh` echo is xpg_echo on macOS: `echo "a\nb"` emits a literal
   `\n`. Fixture stubs must use `printf '%s\n'` (and for any JSON line
   built from the prompt, build it in Go or awk-escape — never `$2`
   raw, or embedded newlines in the prompt produce invalid JSONL).
2. BSD sed (macOS) does not accept `:a;N;$!ba`. Use awk for
   newline-joining (or keep stub JSON single-line via Go-side
   escaping in `stubcli.FixedReply`).
3. A fixture `claude` needs system dirs in PATH (`/usr/bin:/bin`) for
   its own helpers (base64/awk) to resolve — a PATH-only CLIEnv makes
   the stub hang.
4. `Approvals.remove()` had a genuine missing-unlock bug (lock never
   released) revealed when the rewritten turn tests exercised cancel;
   fixed with `defer`. `wait` is now exported as `Ask`.
5. Roster/org/pack/workspace-surface fixtures all construct manifests
   in Go — every one needs `Runtime: "claude"` (or `runtime =`
   "claude" in TOML) or strict-parse/marshal self-check fails.
6. Editor/reviewer e2e tests were rebuilt from `provider.Mock` to the
   real runtime + `stubcli.FixedReply` stub — F-005 acceptance flow
   survives unchanged on a fake PATH (the reviewer/chat seams didn't
   move).

## Session 10 gotchas (P0 research)

1. Headless contracts VERIFIED on this machine (2026-09-08):
   claude 2.1.177 (`-p --output-format stream-json --verbose`,
   terminal `result` carries `total_cost_usd`+`usage`), codex-cli
   0.147.0 (`exec --json -C <dir> --sandbox danger-full-access
   --dangerously-bypass-approvals-and-sandbox` — that flag is
   documented as "for environments that are externally sandboxed",
   i.e. made for our seatbelt wrap), opencode 1.18.25 (`run
   --format json --dir <dir> --title <slug>`).
2. gemini/cursor-agent/copilot are NOT installed here — wave 3
   adapters are fixture-first; their live-verify checklists (version,
   flags, stream shape, cost location) must be filled in the adapter
   file before the doctor row may report OK.
3. DHI has no LICENSE file (all-rights-reserved) — flagged to the
   user in the plan discussion, no decision recorded yet; do not
   publish DHI code snippets as reusable upstream prior art until
   that's settled.
4. Workspace view panes: secMembers secOrg secPacks secStandards
   secChannels secTasks secInspect (view.go sectionID) — F-015 adds
   secAutopilots (8th), F-016 adds secInbox (9th); rail counts +
   sectionSwitcher tests iterate 0..secCount, so every insertion
   shifts nothing (append at the end) but label()/count cases grow.
5. Task card = internal/tasks (Task struct, ChangeSet,
   RecordChangeSet, AttachFn/DetachFn, Subscribe) — `[[run]]`
   records extend the card TOML there; the runs dir is
   `.dhi/agents/<id>/runs/` next to memory journals.
6. (void, ADR-0013) runtime.Config had a Providers map + Turn branch —
   the provider layer is deleted; Turn is now a thin CLI dispatcher.
7. (void, ADR-0013) provider.Event had no usage field — there is no
   provider.Event anymore; claude's terminal `result` carries
   `total_cost_usd` + `usage`, and the `[[run]]` schema is
   `cli:<name>` only (F-014).

## Gotchas carried (still load-bearing)

1. go-git Push needs a REGISTERED remote; fixtures use bare local origins.
2. Test fakes must fully implement seams; event pumps must NOT re-arm.
3. Read form fields BEFORE closeForm(); waitReply before
   crew.Handle — mirror assertions come from the reply message, not a
   provider call log (the Mock's call log is gone with the engine).
4. bus.History(ch,0) excludes threaded rows.
5. requestTurn must call crew.Handle SYNCHRONOUSLY.
6. Policy rules are ROOT-RELATIVE (ADR-0010); glamor renders H2 `## `.
7. macOS /var→/private/var EvalSymlinks.
8. g-chords are editor-owned; WorkspaceEdit bottom-up; LSP flows
   guard on client.
9. Seatbelt deny-default profiles need the system allows (/System,
   /usr/lib, dyld caches, mach-lookup) or wrapped processes die
   cryptically; network stays policy-engine territory. M8 adds
   per-CLI StateRoots to the rw set — same system-allow list applies.
10. sandbox.go's Sandbox interface (Name/Wrap) is load-bearing —
    never redesign it casually; adapters implement it as-is.
11. runtime guards deny-all by policy default: Guard.Exec tests need
    an explicit exec allow in policy_json.
12. fuzzy.Match and Index.Rank share matchRunes so scores can't drift.
13. Gates that start work from a keypress MUST queue through TakeCmd.
14. `go run` of internal packages from /tmp fails ("use of internal
    package not allowed"); drive via a transient file INSIDE the
    repo, delete after. `go build ./cmd/dhi` drops a `dhi` binary in
    cwd — remember to delete it.
15. Settings layer semantics: zero values in a fileLayer mean UNSET
    (bools needing explicit-false use *bool); strict load rejects
    unknown keys per-layer before merge.
16. doctor must stay runnable on broken installs: use
    settings.LoadBestEffort anywhere diagnostics read configs.
17. runtime.New REQUIRES Config.Sandbox; test harnesses inject
    sandbox.Noop{} explicitly.
18. lipgloss multi-line Render re-pads lines to the longest — style
    multi-line content line-by-line or goldens break (theme.Faint).
19. `theme.Motion` is a variable, not a function (F-012).
20. Manifest Parse takes id from the filename stem; strict decode
    rejects undecoded keys — new manifest keys must land in the
    file struct + validation + round-trip test together.

## Just finished (M8 P0)

- `docs/adr/0012-host-agent-clis-as-optin-runtimes.md`: declared
  runtime category, user-owned (ADR-0005 exception), OS sandbox is
  the boundary, declared env (never ambient), runs not turns.
- `docs/features/F-013-cli-runtimes.md` (core), `F-014-run-
  observability.md`, `F-015-autopilot.md`, `F-016-inbox.md` — each
  with acceptance criteria + "inspired by Multica" citations +
  deferred lists.
- ROADMAP M8 section (P0 checked, P1–P4 listed); STATE updated.

## Next up

1. **P1 wave 1 (next session):** `internal/agentkit/clirun` registry
   + `CLI` shape + claude adapter + fixture harness (scripted stub
   CLIs, canned stream-json); manifest `runtime`/`timeout`/`retries`
   keys; `[[run]]` record on task cards; doctor `runtime/<cli>` rows.
2. P1 wave 1b: sandbox-wrapped spawn + worktree cwd + transcript
   persistence + thread streaming + Turn branch.
3. P1 wave 2: codex + opencode adapters + retry/timeout policy.
4. P1 wave 3: cursor-agent, copilot, gemini + reviewer handoff.
5. P2 → P3 → P4 per spec.

## Open questions for user

- DHI LICENSE: still none. Decide before any upstream sharing of
  DHI code (see session 10 gotcha 3).
- Wave-3 CLIs (cursor-agent, copilot, gemini) aren't installed on the
  dev machine — do you have accounts/installs for live verification,
  or should wave 3 stay fixture-only until you install them?
