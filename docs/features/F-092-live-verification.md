# F-092 — Live verification of M32, and what it found

Status: done (M32 P7)

## How it was run
- DHI was built from `m32` and driven through tmux in a scratch repo
  shaped like the yume-forge case: a Go module whose `Add` subtracts, a
  failing test, history on `main`.
- One agent, `anvil`, on Claude Code 2.1.285 (Haiku), with an isolated
  `XDG_CONFIG_HOME`, on macOS with the Seatbelt sandbox.
- The request was the one from the screenshot, sent in the DM: "can you
  analyse the current repository and fix it."

## Run log (what each live run exposed, and the fix)
1. **Bash ran** (the private `CLAUDE_CODE_TMPDIR` works), but:
   - writes to `/dev/null` were denied, which broke every redirect, git
     and `go vet`;
   - `go test` could not write `~/Library/Caches/go-build`;
   - the denial card offered `/dev`;
   - the continuation threw away the session as "197k tokens": Claude's
     result usage sums every call of a run.

   Fixes:
   - Seatbelt allows writes to harmless device nodes;
   - `BuildEnv` sets GOCACHE/GOPATH/GOMODCACHE under the toolchain
     (`go-cache`, `go-path`);
   - `DeniedPath` never offers `/dev` or `.git` internals;
   - per-call usage (`EventUsage`) gives `Run.Context`, which `episode.Fold`
     prefers.
2. **Everything ran, but the agent committed straight to `main` with
   native `git commit`.** Fix: the sandbox guard denies writing each
   member's default-branch ref and its `.lock` (`gitcore.DefaultBranchRefs`).
   Live-checked: a commit on main fails `cannot lock ref`, and one on a
   task branch succeeds. zsh heredocs failed on `/tmp`: `TMPPREFIX` now
   points at the agent's dir.
3. **The agent called `work_branch`, but the call timed out.** It waited
   on a git-scope approval, and Claude's MCP client gave up. Fixes:
   - `work_branch` runs without an approval (it only isolates the agent's
     own work);
   - adapters declare `WaitEnv` (claude `MCP_TOOL_TIMEOUT`), which DHI sets
     to one hour, because DHI's tools can wait on you.
4. **`work_branch` worked and the commit landed on `task/fix-add-function`,
   but:**
   - the agent kept editing the shared checkout in the same run;
   - the carry stash took DHI's own untracked `.dhi/.gitignore`.

   Fixes: a pending relaunch now ends the spawn at once, and the carry
   excludes `.dhi`.
5. **The clean pass.** Main is locked. `work_branch` creates card, branch
   and worktree, the edit is carried, the run is stopped and continued in
   the worktree, and the commit lands on the task branch. DHI then
   verifies on its own: "verified: go test ./... passed (round 1)". Main
   and the shared checkout are untouched.
6. **Secrets.**
   - `secret_request` raised a card; `y` opened the masked field (bullets
     only) and saved the secret for this workspace.
   - DHI's `run` then answered "command runner unavailable". That is a
     pre-existing bug: the runtime never passed `cfg.Run` to the served
     tools, so `run` and `skill_run` have never worked for CLI agents.
     Fixed (`toolDeps`, with a regression test).
   - Retried: the test printed the value inside `run`, and the agent saw
     `key=‹secret:STRIPE_TEST_KEY@yf›`. A grep of the workspace, the
     config dir and Claude's own session logs finds the value only in
     `credentials.toml`.
   - The chat pane showed ask answers as errors (✗); they are now neutral
     notes.

Also earlier: run 1 showed the agent asking `permission_request` for the
Go cache, unprompted. `o` granted it, and DHI continued the agent in a
fresh run with the path in its sandbox.

## Not run live
- Skill suggestion from a signed registry, and the global layer across
  two workspaces. Unit and surface tests cover them end to end
  (registry fetch, digest refusal, global install visible in another
  workspace, per-key saves).
- Codex and the other adapters: their `TempEnv`/`WaitEnv` are not
  declared yet, because nobody has observed their temp or timeout
  behaviour.
- Linux/bubblewrap: the `/tmp` failure was macOS-only; the ref guard and
  credential masks are covered by unit tests only.

## Round 2: the "not done or not verified" list

### Code
- **The session survives a relaunch.** Claude's `system`/`init` line
  carries `session_id`, so a run DHI stops still records its session
  (`EventSession`). A grant's continuation (same thread and cwd) now
  resumes instead of re-analysing.
- **`.dhi/tmp` cleanup.** At launch, entries untouched for 24h are removed
  (`runtime.CleanTmp`); recent ones stay, in case another DHI is open on
  the same workspace.
- **Linux packed refs.** `gitcore.LooseDefaultRefs` writes a loose ref
  file for a default branch that exists only in `packed-refs`, so
  bubblewrap has a file to guard.
- **Remote default branch.** Agent CLIs get a config-defined pre-push hook
  (`hook.dhi-default-branch.*` via `GIT_CONFIG_*`, git ≥ 2.47). It runs
  alongside the repo's own hooks, and the script lives in `.dhi/hooks/`,
  which agents cannot write. It refuses pushes to main, master or the
  remote's HEAD branch. `git push --no-verify` bypasses it; GitHub branch
  protection remains the real fence.
- **Exec grants.** `permission_request {kind: exec, target: /abs/program}`
  grants the program's folder read and execute, never write
  (`exec:<dir>`, `ReadOnlyExtender`), from the next spawn.
- **Codex:** the dhi MCP server's `tool_timeout_sec` is 3600.
- **A new workspace's wizard** no longer asks "you" or "instructions" once
  you have set them for every workspace.
- **Identity:** a `~/.gitconfig` that links to a missing file is named in
  the error, instead of git's "could not lock config file".
- **Editor tests:** polling deadlines are 15s; they return as soon as the
  condition holds. The earlier one-off failure did not reproduce in 16
  stress runs.

### Live
- **Codex (0.162.1):**
  - First run: it would not start inside Seatbelt ("Failed to synchronize
    managed preferences"). Probed: it needs read-only POSIX shared memory
    plus preference reads; the profile now allows both, reads only.
  - Then: it called `work_branch` before editing, continued in the
    worktree, and committed on `task/fix-addition`.
  - Its push to a local file remote hit the exec gap above.
- **Push guard:** an agent's `git push origin task/x:main` was refused by
  the hook; `git push origin task/x` went through; the remote's `main` was
  unchanged.
- **Skills, from a local registry signed with `scripts/registry-sign`:**
  1. The key was pinned and the registry refreshed for every workspace.
  2. anvil `skill_search`ed and `skill_suggest`ed it ("signed by a
     publisher you trust, digest verified, ships a script").
  3. PROPOSALS showed the skill and script in full.
  4. It was installed for every workspace and added to anvil: files in
     `<config>/dhi/skills`, script executable.
  - MARKETPLACE now shows an inherited registry and keys as yours.
- **Global layer:** with an instruction saved for every workspace, a brand
  new second repo was set up through the wizard. It had the skill (LIBRARY),
  the instruction (STANDARDS preview) and the engine (CONFIG, unmarked),
  and no `.dhi/config.toml` was written. This run is what showed the
  wizard asking "you" and "instructions" again (fixed above).
- **Linux (Docker, golang:1.27 arm64, bubblewrap):**
  - Every spawn failed at first: `/lib64` does not exist on arm64 Debian,
    and DHI bound it unconditionally. Now `--ro-bind-try`.
  - Then `TestLiveBubblewrap` (`DHI_LIVE_BWRAP=1`) passed: a commit on a
    packed-only `main` was refused, the task branch committed, the
    credentials file was masked, and `/dev/null` and `/tmp` worked.
- **Full test suite on Linux** (first time): everything passed except the
  mcp stdio test. Its fake server was an awk one-liner, and Debian's mawk
  buffers input, so it never answered and the dial had no deadline. It is
  now a portable sh read loop with a timeout. The suite is green on Linux.

### Still open
- Codex adapters other than claude and codex (opencode, cursor,
  copilot, antigravity) declare no TempEnv/WaitEnv; none has been run
  live.
- `git push --no-verify` bypasses the push guard; use GitHub branch
  protection.

## Round 3: opencode, Cursor, Copilot and Antigravity, live

The same DM ("can you analyse the current repository and fix it.") was
sent to each CLI on its default model, in one scratch repo, macOS Seatbelt
sandbox. Each now ends with the fix committed on its own task branch and
`main` untouched. What each exposed:

- **opencode 1.19.0:** failed at start, writing `$XDG_CONFIG_HOME/opencode`.
  Adapters now declare state dirs through `xdgDir` (config, data, cache,
  state follow `XDG_*`). Its tool failures showed only the tool name; they
  now carry `state.error` / `output`, and feed the denial card.
- **cursor-agent 2026.09.26:**
  - "env: bash: Operation not permitted": it is a `#!/usr/bin/env bash`
    script, and `PATH` found Homebrew's bash outside the exec roots. The
    runtime now adds each script CLI's interpreter folder
    (`interpreterDirs`) read+exec.
  - It then hit `$XDG_CONFIG_HOME/cursor` (now a root).
  - Its tool lines read "tool: ": the 2026 stream nests calls as
    `{"<kind>ToolCall": {args, result}}`, now parsed, with failure reasons
    and denials.
  - DHI's per-turn `.cursor/mcp.json` leaked into a commit. In a linked
    worktree the exclude went to `.git/worktrees/<name>/info/exclude`,
    which git never reads; it now follows `commondir`. The line stays,
    because it is shared and removing it would race other turns. The empty
    `.cursor/` DHI created is removed.
- **copilot 1.0.88:**
  - Its state is `~/.copilot` (the adapter declared
    `~/.config/github-copilot`); fixed, with XDG fallbacks.
  - It then reported DHI's tools missing: the workspace `.mcp.json` is no
    longer loaded. The endpoint now rides `--additional-mcp-config` for the
    session only, so no file lands in the worktree.
  - It asks before editing (its own style); with that approval it fixed,
    committed, and DHI verified.
- **antigravity (agy 1.2.13):**
  - It said it had committed, but the fix was only staged. **DHI's
    `git_commit` was losing every commit made in a task worktree:** go-git
    opened the linked worktree without common-dir support, wrote the
    branch ref into `.git/worktrees/<name>`, and reported success. It now
    opens with `EnableDotGitCommonDir`; a real-git test pins it. This also
    explains why opencode and Claude fell back to native `git commit`
    earlier.
  - Its text arrived as deltas, one chat line each (15 fragments in one
    run); it is now coalesced into sentences (0 fragments on the rerun).
