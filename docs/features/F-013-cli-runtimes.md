# F-013: CLI runtimes — roster the agent CLIs you already run

Status: implemented (M8 P1; six adapters live-verified; more runtimes deferred) · Milestone: M8 · Closes: ADR-0012
Inspired by: Multica's daemon-runtimes model — an agent is a teammate
who works on a runtime you control; Multica drives 26 host agent CLIs
and DHI starts with six of the same, through DHI's own task/worktree/
sandbox/bus machinery.

## Summary

A CLI agent is engaged as one delegated **run**: the runtime assembles
the task prompt (same grounding + standards as the old in-house turns),
spawns the CLI headless in the task's worktree wrapped by the OS
sandbox, streams the transcript into the bound bus thread, and records
the outcome (exit, duration, tokens, cost, transcript path) as a `run`
on the task card. Failures stop with a posted reason; retries are
explicit. Nothing degrades silently (ADR-0011); the CLIs are
user-owned, so their absence is a named refusal + doctor row, never a
boot block (ADR-0012). Because ADR-0013 removed the in-house engine,
CLI runtimes are no longer opt-in on top of one — every rostered agent
thinks through a host CLI (wave 1 = claude), and waves 2–3 add
codex/opencode/cursor-agent/copilot/gemini.

## Part A — CLI registry (`internal/agentkit/clirun`)

- **Shape.** `CLI` = one adapter:
  - `Name` (registry key = manifest `runtime` value), `Bin` (binary
    name), `Tested` (version range verified against fixtures),
  - `EnvPass []string` — the EXACT env vars passed through (ADR-0012 §4;
    doctor reports this set),
  - `StateRoots()` — per-CLI rw roots for the sandbox profile (the
    CLI's own state dir, e.g. `~/.claude`, `~/.codex`),
  - `BuildArgs(task) []string` — headless invocation for one run
    (prompt + model + worktree cwd + permissions),
  - `ParseStream(r io.Reader) (<-chan StreamEvent, error)` — JSONL
    event stream → neutral events (progress text, tool command,
    error),
  - `Finalize(final) (Summary, Usage, error)` — terminal event →
    summary text + tokens/cost.
  - Registry: `All()`, `Get(name)`, `Names()`, `Detect(lookPath)
    map[string]string` (name → detected version, for doctor).
- **Invocations** (verified against the pinned versions 2026-09-08;
  each is fixture-tested, not host-tested, in CI):
  - `claude` (Claude Code 2.1.177):
    `claude -p <prompt> --output-format stream-json --verbose
    [--model M] --permission-mode bypassPermissions --max-turns 50
    --add-dir <worktree>`; terminal `result` event carries
    `total_cost_usd` + `usage` (in/out/cache tokens).
  - `codex` (codex-cli 0.147.0, live-verified 2026-09-09):
    `codex exec --json --skip-git-repo-check -C <worktree> [-m M]
    --sandbox danger-full-access --dangerously-bypass-approvals-and-
    sandbox <prompt>`; JSONL events (`thread.started`, `turn.started`,
    item `started`/`completed` with `command_execution`/`file_change`/
    `agent_message`, `turn.completed` carrying usage). The bypass flag
    is sanctioned here because the OS sandbox wraps the process
    (ADR-0012 §3). codex reports no US-dollar cost (HasCost=false);
    only stdout is parsed (stderr carries models-cache noise).
  - `opencode` (1.18.25, live-verified 2026-09-09):
    `opencode run --format json --dir <worktree> --auto
    [--model provider/model] [--title <task-slug>] <prompt>`; JSONL
    of `step_start`/`tool_use` (state.status, metadata.exit)/`text`/
    `step_finish` (tokens + cost); the stop step_finish is terminal.
  - Wave 3 (cursor-agent, copilot, gemini): LANDED fixture-first
    2026-09-09 (adapters + fixtures to the documented contracts), but
    NOT installed on the dev machine — each adapter ships with its
    fixture contract first and a live-verify checklist (version, flag
    set, stream shape, cost location) recorded in the adapter file
    before its doctor row may report OK. Until verified, their
    `Tested` pin is empty, so a detected-but-untested version is a
    doctor FAIL (never a guess).
- **Fixture harness.** Each adapter is tested against a scripted
  fixture binary (a tiny shell/Go stub on a temp PATH) emitting canned
  event streams: success-with-cost, mid-stream error, hang (timeout),
  wrong-version banner. The same scripted-Mock philosophy as the
  provider conformance suite; real CLIs are never executed in CI.
  Optional live smoke behind `DHI_SMOKE_CLIRUN=1` for the three
  installed CLIs (mirrors `DHI_SMOKE_NET` gating).

## Part B — manifest + runtime wiring

> Final state per ADR-0013 (2026-09-09): the in-house engine is removed,
> so the manifest and runtime below no longer branch on a native
> provider. `runtime` is **required and CLI-only** — an empty or unknown
> value is a parse error naming the valid set, and there is no implicit
> engine to fall back to.

- **Manifest** (`internal/agentkit/manifest`): new key
  `runtime = <registry name>` (required, strict enum against
  `clirun.Names()` — unknown value is a parse error listing the valid
  set). The reserved `""` default and `"anthropic"` no longer exist,
  and the `env_var` key is dropped — credentials are declared, never
  ambient: each CLI's own auth is reached only through the adapter's
  declared pass-through (ADR-0012 §4). `model` doubles as the CLI model
  arg (optional for CLI runtimes — the CLI default applies when
  absent). Optional per-run policy: `timeout` (duration, default 10m)
  and `retries` (int 0..3, default 0). `tools` stays as declared intent
  (validated for well-formedness; enforced later by the IDE-tool
  bridge), and `policy_json` still configures policy but now scopes the
  OS-sandbox roots around the spawn (ADR-0013 §4–5).
- **Runtime** (`internal/agentkit/runtime`): `Config.CLIs *clirun.Registry`
  (nil ⇒ CLI runtimes unavailable; a rostered CLI agent then refuses
  turns with the named fix and fails its doctor row — the roster
  still loads, so manifest validity and binary availability stay
  separate concerns). `Turn` is a thin CLI dispatcher (the former
  anthropic→provider branch is deleted). Prompt assembly reuses
  the in-house grounding path (layered standards + task context)
  plus a short "report format" tail: final summary + what changed +
  anything the human must know.
- **The executor** (one run):
  1. **Worktree.** The run's cwd is the task's attached worktree
     (existing tasks `ChangeSet`); no attachment ⇒ visible refusal
     naming the `w` attach fix (Multica's per-run workdir, DHI's
     existing worktree binding).
  2. **Env.** Hermetic toolchain PATH (`Manager.Env`) + the adapter's
     declared `EnvPass` set. Nothing else.
  3. **Sandbox.** argv wrapped via the existing `Sandbox` seam with
     worktree + `StateRoots()` as rw roots (ADR-0012 §3).
  4. **Spawn + stream.** Context with the run timeout; JSONL parsed
     to neutral events; progress posts into the bound bus thread,
     throttled (one post per tool command + errors + final usage;
     never per-token). Full transcript persisted to
     `.dhi/agents/<id>/runs/<utc-stamp>-<seq>.jsonl` (reserved dir,
     like memory journals).
  5. **Finalize.** Usage/cost from the terminal event, exit code,
     duration → `[[run]]` record appended to the task card (F-014's
     schema): `ts, cli, model, status` (running|succeeded|failed|
     timed_out), `exit`, `duration`, `tokens_in`, `tokens_out`,
     `cost_usd`, `transcript`, `attempt`.
  6. **Success.** Summary posted to the thread; if the worktree is
     dirty the task moves to `in-review` (existing status machine) so
     the human's decision is the ship gate — Multica's review gate,
     DHI's kanban.
  7. **Failure/timeout.** Post the reason to the thread; if
     `retries` remain, re-spawn (attempt + 1) after a 30s in-session
     backoff; exhausted ⇒ run marked failed, task stays `active`,
     the failure is inbox-visible (F-016). A timed-out run kills the
     whole process group (seatbelt/bubblewrap children included).

## Part C — doctor + wiring

- Doctor rows `runtime/<cli>` per registered CLI: OK + detected
  version when present and in the tested range; Warn "not detected
  (no agent needs it)" when absent with no CLI agent; Fail naming the
  agent(s) when a rostered agent needs it; Fail on version outside
  the tested range ("claude 9.9.9 untested — pin or adapt").
- Doctor row `runtime/env`: the declared pass-through set per
  detected CLI (auditable env, ADR-0012 §4).
- `cmd/dhi` builds the registry with the host `exec.LookPath` (the
  sanctioned host lookup for this category) and passes it to
  `runtime.Config.CLIs`.

### Acceptance criteria

- **Registry:** golden argv tests per adapter (fixed task → exact
  argv); parser table tests over fixture streams (success / mid-
  stream error / hang / cost footer) including malformed-line
  tolerance (bad line → error event, stream continues); Detect
  matrix (present / absent / wrong version).
- **Manifest:** unknown `runtime` value refused with the valid set
  named; no `env_var` key exists — CLI secrets reach an agent only
  through the adapter's declared pass-through; save/load round-trip
  with `runtime`, `timeout`, `retries`.
- **Executor:** a fixture CLI on a temp PATH runs end-to-end in a
  temp workspace — worktree cwd, sandbox Wrap observed with worktree
  + state roots (recording sandbox), transcript persisted, `[[run]]`
  recorded, summary posted, dirty tree ⇒ `in-review`.
- **Timeout/retry:** hanging fixture ⇒ `timed_out` after the timeout,
  reason posted, exactly 1 attempt at `retries=0` and 2 at
  `retries=1` (30s backoff compressed to ms in tests via injected
  clock).
- **Runs records:** task TOML round-trips `[[run]]`; a malformed run
  line warns like other malformed cards (doctor).
- **Doctor:** all four `runtime/<cli>` states + env row; JSON output
  includes them.
- `make verify` green; goldens for the task-detail run lines.

## Deferred

- The remaining ~20 of Multica's runtimes (kimi, grok, qoder, trae, …):
  the registry makes each one adapter file + fixtures.
- Resuming an interrupted CLI run (e.g. claude `--resume`, codex
  session files): run-scoped state dirs make this a parser problem
  per CLI; after the base contract proves out.
- Live-verify checklist automation for wave-3 CLIs (CI matrix with
  the real binaries) — needs the CLIs in CI images.
- Per-run network policy differentiation (F-010's ro-root
  differentiation deferred item covers the same policy gap).
