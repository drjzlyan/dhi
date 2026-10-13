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
