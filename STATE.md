# STATE — current position

Updated: 2026-09-08 (session 10: M8 opened — P0 specs + ADR-0012 landed)

## Where we are

**M7 closed (F-012, 47af9b7). M8 "Roster any agent" opened — P0
done, implementation next.** The user picked Multica
(multica-ai/multica, agents-as-teammates platform driving 26 host
CLIs) as the north star. P0 shipped: ADR-0012 (host agent CLIs as
opt-in runtimes — the one named exception to ADR-0005) + specs
F-013 (CLI runtimes), F-014 (run observability), F-015 (autopilots),
F-016 (inbox), + ROADMAP M8 section. Scope agreed: full M8 (P1–P4),
CLI roster = claude, codex, opencode (waves 1–2) + cursor-agent,
copilot, gemini (wave 3). Next: P1 wave 1.

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
6. runtime.Config already has Providers map[string]provider.Provider
   (per-agent override) + Turn(ctx, agentID, trigger) — the CLI
   branch lands inside Turn; anthropic path must stay byte-identical.
7. provider.Event has NO usage field — F-014 records anthropic runs
   with tokens -1 / cost false ("n/a") until the seam extension
   (deferred, needs the conformance suite to carry it).

## Gotchas carried (still load-bearing)

1. go-git Push needs a REGISTERED remote; fixtures use bare local origins.
2. Test fakes must fully implement seams; event pumps must NOT re-arm.
3. Read form fields BEFORE closeForm(); waitReply before provider.Calls().
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
