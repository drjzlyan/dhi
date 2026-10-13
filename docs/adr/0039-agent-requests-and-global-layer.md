# ADR-0039: Agents ask; DHI grants at the next spawn; one global layer

Date: 2026-10-14 · Status: accepted · Extends: ADR-0012 §3–4, ADR-0028, ADR-0022, ADR-0035. M32.

## Context

Agents run inside an OS sandbox (ADR-0012 §3), with the CLI's own
permission prompts switched off. When the sandbox denied something, all
the agent saw was an error line. It had no way to ask for access, for a
secret, or for a skill. Everything DHI knows about you was per workspace
(skills, registry trust, standards, grants), so each new workspace had to
be set up again. The settings screen also copied every user-level value
into `.dhi/config.toml`.

## Decision

1. **The sandbox must fit the engines.** Each spawn gets a private temp
   dir inside its writable roots, through `TMPDIR` and the engine's own
   variable (`CLI.TempEnv`). We do not widen the sandbox to `/tmp`.
2. **Code work happens on its own branch.** `git_commit` refuses the
   default branch. A chat turn gets a card and a worktree through
   `work_branch`.
3. **A request becomes a card you answer.** Permission, secret and skill
   requests go through the approvals queue or the proposals screen.
   Sandbox denials found in a run's output become the same cards, so an
   agent does not have to remember to ask.
4. **Grants take effect at the next spawn.** The sandbox profile and the
   CLI's working dir are fixed when the CLI starts. A tool that changes
   either (`work_branch`, an approved permission) asks the turn to
   continue: when the spawn ends, DHI posts `@agent continue: …` in the
   thread and starts a fresh run.
5. **Grant duration** is once, this workspace (the default), or
   everywhere. "Everywhere" lives in the user config dir.
6. **Secrets are referenced by name.** Values live in the user-level
   credential store, fenced from the sandbox. They are injected only into
   DHI's `run` tool, for names that agent was granted, and never into the
   CLI process. Values are redacted from tool results, transcripts, traces
   and the bus. This keeps values out of prompts, chat and logs. It does
   not stop a determined agent from transforming a value it can use; the
   redaction catches only the literal value. Host env stays inherited (the
   user's choice), and doctor names exported variables that look like
   tokens.
7. **One precedence everywhere:** builtin < user (global) < workspace <
   team < agent. This covers settings, standards and instructions, skills,
   registry trust and grants. Each edit is saved to the one layer you
   chose. Skills come only from the DHI registry (digest-pinned, signed
   when a key is pinned). Installing a skill globally never attaches it to
   agents, because attached skills are part of every prompt.

## Consequences

- One relaunch mechanism serves `work_branch` and permission grants.
- New user-level files: `grants.json`, `standards.toml`, `skills/` and
  `registry/` under `$XDG_CONFIG_HOME/dhi`.
- A workspace that sets nothing locally inherits everything, so new
  workspaces need no setup.
