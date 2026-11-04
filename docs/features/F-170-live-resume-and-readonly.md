# F-170 — Live resume check and read-only flags

Status: done · gap G-C11 (and the G-C2 flag follow-up)

Resume was claimed only for claude and codex. Whether opencode, cursor and copilot resume
(F-066) and whether the read-only flags of F-144 hold had not been checked against the real CLIs.

## Method
Throwaway temp git repo, tiny prompts. Resume: turn 1 says "the secret word is PERSIMMON"; turn 2
passes the id captured from the stream and asks for the word. Read-only: argv from the adapter's
`BuildArgs` (not hand-typed), ask the CLI to create a file, check the file does not exist.

## Results (2026-11-04)

| CLI | Version | Check | Flags | Result |
|---|---|---|---|---|
| opencode | 1.19.0 | resume | `run --format json --dir . --auto` then `--session <sessionID>` (id is on every JSONL line) | pass, replied PERSIMMON |
| cursor-agent | 2026.10.12 | resume | `-p … --output-format stream-json --force --trust --workspace .` then `--resume <session_id>` (id on the first `system` line and on `result`) | pass, replied PERSIMMON |
| copilot | 1.0.88 | resume | not run | blocked: the permission classifier refused the run (it needs `--allow-all-tools`); left unverified, no `ResumeOK` |
| claude | 2.1.285 | resume | `--resume <session_id>` | blocked: classifier refused the bypass-permissions run; earlier live check (F-066) stands |
| codex | 0.162.1 | resume | `exec resume … <thread_id> <prompt>` | blocked, same; earlier live check stands |
| claude | 2.1.285 | read-only | `--disallowedTools WebFetch,WebSearch,Edit,Write,MultiEdit,NotebookEdit,Bash` | pass: `made.txt` not created, the Write attempt was refused |
| codex | 0.162.1 | read-only | `exec --json --skip-git-repo-check -C . --sandbox read-only -c web_search=disabled` | pass: `made.txt` not created, "workspace is read-only" |

## Bug found
opencode and cursor-agent had no resume at all: `RunInput.Resume` was ignored and no session id was
captured, so every turn started fresh although both CLIs resume.

## Behaviour
- opencode: `BuildArgs` adds `--session <id>` when `Resume` is set; the parser emits `EventSession`
  from the first line's `sessionID`, and `Finalize` reports it as `Usage.SessionID`. `ResumeOK`.
- cursor-agent: `--resume <id>`; the session comes from `system`/`result` `session_id`. `ResumeOK`.
- copilot stays fresh-per-turn (doctor says so) until a run is allowed and verified.

## Acceptance
- `TestOpenCodeResumeArgsAndSession`, `TestCursorResumeArgsAndSession` (argv, one `EventSession`,
  `Usage.SessionID`; no flag on a fresh run).
- `TestResumeOKOnlyVerified` lists claude, codex, opencode, cursor-agent.
