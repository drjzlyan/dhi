# STATE — current position

Updated: 2026-09-09 (session 17: MIT license adopted; M9 true-unread
complete; M10 started — agent lifecycle + IDE parity specs landed;
wave-3 live-verify still pending installs)

## Where we are

**M8 and M9 are complete: `make verify` green.** M10 (agent lifecycle
& full IDE parity) is specced from the user's product goal: create
agents in Settings (full CRUD), add agents from GitHub (one flow,
auto-detects pack vs bare manifests), and give agents full IDE parity
(a documented matrix + a toolbridge so CLI agents can drive DHI
actions like task cards and PRs, allowlist-gated, approvals for
mutating ops). Survey finding baked into F-018: `runtime.Reload` has
no callers today — roster changes don't go live until restart; the
reload pump fixes that for every surface at once.
Phases: P1 settings CRUD + pump, P2 GitHub import, P3 parity bridge.
Next: M10 P1.

## Session 17 gotchas (F-017 / M9)

- **`unread.File` is `.dhi/unread.json`** (full relative path, the
  pack-provenance precedent). An early version used bare `unread.json`
  and wrote `<root>/unread.json`; the doctor suite caught it only
  because its fixture wrote `.dhi/unread.json` directly — keep the
  store and doctor on the ONE constant.
- **Doctor's unread row must stay read-only**: `unread.Open` SEEDS
  (writes) on a missing file. Doctor reads raw bytes + `unread.Decode`;
  the store's Open is the only writer. A read-only diagnostic that
  creates state would mask "fresh install" from "corrupted".
- **`signal()` must hold the mutex** while iterating `subs` — MarkRead
  unlocks BEFORE signaling, so an unlocked iteration raced with
  Subscribe's cancel delete (-race caught it in TestSubscribeFiresOnWrite).
- **Scan's thread rule**: a top-level message whose thread scope
  (`<channel>#<rootID>`) carries a watermark counts as read (max of the
  two limits). Without this, the inbox mention jump (which marks the
  THREAD scope) could never clear a top-level mention row.
- **Snooze keys are (channel, messageID)** — agent messages only.
  Approval/run_failed/in_review items refuse `z` with a named hint
  (snoozing them would need a different keying; noted as a scope cut).
- **`gotoInbox` in tests loops `for m.sec != secInbox`** — a fixed `]`
  count breaks when the test is ALREADY at inbox (form submit leaves
  you there; 8 more `]` lands on autopilots).
- **Snoozed rows stay dim even under the cursor** (parked ≠ urgent);
  the word-wrapped "snoozed until HH:MM" suffix can split across lines —
  assert "snoozed until" and the time separately.
- Editor chat shares the store via `editor.WithUnread` in main; the
  workspace Deps `Unread` field takes precedence, falling back to
  opening its own (tests) — one read state, two surfaces.

P4 (F-016 inbox) landed: `internal/inbox` is a pure aggregation
(`Build(apprs, bus, tasks)`, no state) that lists approvals, unreplied
@you mentions, failed runs on open tasks, and in-review tasks in
severity-then-age order. The INBOX 9th pane is a launcher: `enter`/`o`
jumps to the owning surface (editor chat approvals, CHANNELS thread,
run-replay, Reviewer) via narrow injected seams that degrade to a named
hint when absent. The `!N` statusline segment is recomputed per frame.

## Session 16 gotchas (F-016 / P4)

- **Inbox is pure + per-frame**: `m.inboxItems()` recomputes
  `inbox.Build` on every call (view + `AttentionCount`), so a row
  disappears the frame its home surface resolves it — no read-marks, no
  cache, nothing to invalidate. The `!N` statusline reads the same
  `AttentionCount()` via an interface assertion in `App.compose()`, so it
  clears in lockstep. Keep `Build` side-effect free (it is the
  table-tested contract).
- **Mention thread rule**: "replied" means a later `Author == bus.Human`
  message in the SAME thread — `repliedByYou(thread, i)` scans
  `thread[i+1:]`. A top-level DM message IS its own thread
  (`bus.ThreadOf` = own id), so a plain new top-level message does NOT
  close it; only a threaded reply (`Thread: dm.ID`) does. Tests assert
  both the close-by-threaded-reply and the not-closed-by-top-level cases.
- **Jump seams are injected closures**, not surface refs: workspace
  `Deps` carries `OpenChat func() bool` + `OpenReview func(id) bool`
  (main wires them to `app.FocusEditorChat()` / `app.SelectReviewer(id)`
  which assert `FocusChat()` / `SelectReview(string) bool` on the editor /
  reviewer surfaces). A nil/false seam degrades to a named `m.inboxHint`
  rendered via `theme.WarningText()` — visible, never silent. Mention +
  run jumps are internal (chatpane.openAt / openReplay), no seam needed.
- **INBOX is the 9th pane (secInbox)**; `TestSectionCyclingWraps` was
  updated so `[` from the first wraps to inbox and 8 `]` reach it.
  Inbox rows word-wrap at the pane width (`wordWrap` in view.go: break at
  spaces, hard-break overlong tokens); the kind glyph (◆ approve / ✗
  run_failed / ✓ in_review / @ mention) + cursor are prefixed per-line.
- `chatPane` must be built from the bus BEFORE the mention jump test
  asserts `openAt` — `newChatPane` starts with an empty channel rail;
  `refreshPaneRail()` populates it. Without that, `openAt` returns false.
- App `compose()` builds a LOCAL copy of the statusline (`status :=
  *a.status`, copies `Left`) before prepending the `!N` segment — never
  mutate `a.status.Left` in place or it re-prepends every frame.

## Session 15 gotchas (F-015 / P3)

- Autopilot **execution is a bus post**, not a direct agent call: run
  now / catch-up / ticks all post `bus.Message{Channel:"dm:<agent>",
  Author: bus.Human, Text:"[autopilot <slug>] <prompt>"}` then
  `rt.Handle(ctx,msg)` in a goroutine. Success = `MarkRan(slug, now)`
  ONLY after the turn returns, so a crash mid-turn never marks ran and
  catch-up re-fires exactly once (persist-before-visibility).
  `bus.Post` is synchronous and stamps `m.At = time.Now()` itself — tests
  observe the recorded posts (no goroutine sleeps).
- **Dangling agent** (not in Store.ids): run_autopilot_now refuses with
  `"agent <x> not on roster (recheck autopilot card or roster)"` and is
  NOT marked ran — the named fix is the ADR-0011 no-guess rule on the
  UI surface too. Catch-up skips (never refuses) dangling cards.
- `NextArm` is the ONLY arming input: interval → `now + every` (never
  ran → now), daily/weekly → next weekday instant since last mark ran,
  paused card → never arms. Ticks re-arm via `autopilotTickMsg`
  (armSeq guard = one chain). Paused/absent store → `nil` cmd = no
  chain. In-session ticks run ONLY while Workspace is the active surface
  (App routes async msgs to Active); catch-up covers boot.
- Launch catch-up = Workspace `Init()`, not "after gate release" — the
  gate may not exist at all (gate only on block/offer/bootstrap), and
  App.Init runs every surface's Init at boot. Catch-up runs the due set
  in slug order, exactly once (posts are synchronous, so slug-order is
  testable without sleeps).
- AUTOPILOTS is the 8th pane (secAutopilots, after secInspect); `[`
  from the first section wraps to it. Columns: name/agent/schedule/
  next-due/last-result; next-due shows "due" when due-last-checked,
  else `Next` formatted (interval "in 10m", daily "today 09:00",
  weekly "Fri 17:00"), last-result = newest agent run status else
  "ran HH:MM" (LastRun) else "-". Widths: 20/12/28/16 — keep content
  under them or pads collapse (golden `workspace_autopilots`).

## Session 14 gotchas (F-014 / P2)

- `tasks.Run.Runtime` is now stored prefixed (`cli:claude`); run decode
  is strict — a hand-edited card with a bad `[[run]]` status refuses the
  whole card (F-011 strict-data spirit) AND gets a line-precise warning
  from doctor `runs/store` via `tasks.CheckRuns` (which re-parses raw
  lines — validateRun messages map to the offending field's line).
- Rollup cost math: `HasCost` is the declared `cost:false` marker; older
  cards written before the field decode as cost-less → sums are marked
  "partial" rather than pretending to know the cost (ADR-0011: never
  guess). `-1` token runs never leak zeroes: `TokPartial` flags them.
- Replay pane is modal on `workspace.sectionKey` AFTER `[`/`]` handling
  so section switching still works; j/k/g/G scroll (G = bottom), esc
  closes; all other keys are swallowed so card state can't mutate
  behind the pane. Replay re-renders/wraps on geometry change
  (`width/height` cache on `runReplay`).
- `flashErr` messages are only visible while a modal is open (form.kind
  fNone = invisible) — test the `m.form.err` value, not the rendered
  output, for refused-key assertions.
- Golden gotcha: the missing-transcript golden embeds an absolute
  `t.TempDir()` path; it's stable because Go seeds temp dir names per
  test identity — regenerate goldens rather than hand-editing.

## Session 13 gotchas (wave 3)

1. All three wave-3 contracts are DOCUMENTED, not live-verified —
   cursor-agent's stream-json (system/assistant/tool_call/result),
   copilot's session JSONL envelope (data.content/toolRequests,
   tool.execution_end.result), gemini's stream-json timeline
   (init/message/tool_use/tool_result/error/result). Each adapter
   carries its live-verify checklist; the terminal event always gets
   `last_message` stitched on because none of the three put the final
   summary text on their terminal result event.
2. copilot's JSONL nests name/result under `data` (tool.execution_end
   has `data:{name,result}`), not top-level — reading top-level fields
   silently matched nothing.
3. Doctor + goldens needed no changes with 6 adapters: doctor iterates
   the registry generically and the golden surfaces never render the
   full runtime set.

## Session 12 gotchas (wave 2)

1. The `CLI` adapter is a struct of function fields (claude.go), not an
   interface — new adapters like codex.go must follow the struct shape
   exactly (`ParseStream func(io.Reader) <-chan StreamEvent`, `Finalize
   func(string) (string, Usage, error)`), and register in `allAdapters()`
   (declared once, in claude.go).
2. codex never puts the final-text and usage on one line:
   `turn.completed` has usage but no text, the last assistant reply is a
   separate `agent_message`. Adapters must stitch the summary onto the
   terminal event themselves (a normalized payload with `last_message`)
   so `Finalize` gets both (same for opencode's stop `step_finish`).
3. codex `item.started` AND `item.completed` both carry the same
   `command_execution` — emitting both would double the transcript
   rows, so the codex parser shows commands on `started` and only
   surfaces failures on `completed`.
4. opencode emits `tool_use` (not `tool`): `part.type == "tool"`,
   fields `part.tool`, `part.state.metadata.exit`. exit != 0 or
   status error ⇒ EventError.
5. codex reports no cost (HasCost=false); opencode reports cost only
   when the provider emits it (0 when unconfigured) → HasCost = cost > 0.
6. Retry state lives in a package var `cliRetryBackoff` (default 30s);
   runtime tests set it to 1ms + restore. The retry notice posted to the
   thread is prefix-matched in tests, not equality-matched (it embeds
   the backoff duration).
7. `rt.Handle` dispatches async goroutines; `rt.Turn` is the synchronous
   error-returning path — retry tests assert transcript files/history
   after `Turn` returns, not via Handle.
8. The "unknown runtime" manifest-test fixture previously used
   `runtime = "codex"` as its invalid value — every adapter addition
   turns a former negative fixture valid. Grep for the CLINames when
   adding adapters.

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

## Just finished (M9 — true unread, complete)

- `internal/unread` (new): strict `.dhi/unread.json` store — monotonic
  `MarkRead`, fresh-install seeding (history never floods), atomic
  writes, subscribe pump, prune-on-load with named notes,
  `AddressedToHuman` predicate, pure `Scan`/`Counts`.
- Read-on-open/post: chatPane `onRead` seam (channel switch, `t`, jump,
  post), editor chat `WithUnread` (open, `[/]`, post) — one store shared
  via main; `●`/`●N` markers on both rails; the inbox mention jump
  resolves its row (thread-scope Scan rule).
- Snooze: `z` toggle form (15m/1h/4h/tomorrow 09:00), `u`/`z` unpark,
  dimmed rows, `!N` + rail count exclusion, 30s `snoozeTickMsg` chain.
- Doctor `unread/store` row (read-only, strict-decode Fail names the
  value, dangling snooze Warn, expired counted). Goldens: channels-unread
  rail, inbox-snoozed. `make verify` + `-race` green.

## Open questions for user

- LICENSE: decided 2026-09-09 — **MIT** (LICENSE file + README; the
  go.mod `license` directive is dropped — this toolchain rejects it).
  No longer blocking upstream sharing.
- Wave-3 CLIs (cursor-agent, copilot, gemini) aren't installed on the
  dev machine — do you have accounts/installs for live verification,
  or should wave 3 stay fixture-only until you install them?

## Next up

1. **M10 P1 — agents in Settings (F-018):** Settings AGENTS section
   (full CRUD via `org.CreateAgent/UpdateAgent/ArchiveAgent/
   DeleteAgent` — DeleteAgent is NEW), strict manifest forms, and the
   live-reload pump (roster change → `LoadRoster` → `runtime.Reload`).
   Commit.
2. M10 P2 — add agents from GitHub (F-019): one source form in
   Settings (git URL or path, `#sub/path` fragment), auto-detect
   pack.toml vs bare manifests, validate-all-then-write, named skips.
   Commit.
3. M10 P3 — full IDE parity (F-020): parity matrix + `toolbridge`
   (DHI-namespaced tool calls from clirun adapters → task/PR actions,
   manifest allowlist gating, approvals for mutating ops). Commit.
4. **Wave-3 live verify** (needs installs): once cursor-agent/copilot/
   gemini are on the machine, run a real task per adapter, fill each
   adapter's live-verify checklist + set `Tested`, then doctor reports
   OK. Until then the adapters stay fixture-first and doctor marks a
   detected version untested (FAIL), never a guess.
5. **(Deferred, from F-017)** snooze-expiry push notifications,
   per-message read granularity, multi-human read states, bulk
   "mark all read"; inbox items from autopilot completions / doctor
   regressions (F-016 deferral).
