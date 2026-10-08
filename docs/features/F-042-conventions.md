# F-042: Layered conventions — branch, commit, copyright, PR text

Status: complete (M25 P1) · Part of the install → setup → first-use UX
programme (see the session-49 plan). Foundation for the setup wizard
(F-043), which will collect these values.

## Problem

How a team names branches, words commits and headers files was either
hardcoded (`task/<slug>`, `review/<id>` in `cmd/dhi/main.go`) or absent
(commit format, `Co-Authored-By`, copyright). Agents could only be told
in free text.

## Design

`internal/conventions` — pure, stdlib-only — holds the rules and every
default. `settings.Config.Conventions` embeds it, so the existing
layering (defaults < user `config.toml` < workspace `.dhi/config.toml`)
and strict unknown-key refusal (ADR-0011) apply unchanged. `UnknownKeys`
now recurses, so a typo under `conventions.commit.*` is named.

```toml
[conventions.branch]
task   = "task/{slug}"      # placeholders: {slug} {id} {user} {date}
review = "review/{id}"

[conventions.commit]
format         = "free"     # free | conventional | ticket
ticket_pattern = '[A-Z][A-Z0-9]+-\d+'
max_subject    = 72
co_author      = ""         # "Name <email>" → trailer added once

[conventions.copyright]
enabled = false
holder  = ""                # required when enabled
license = ""                # SPDX id
year    = ""                # "" = current year

[conventions.pr]
title = "{title}"
body  = "{summary}"
```

## Enforcement points

| Rule | Where |
|---|---|
| Branch patterns | task attach and review worktree seams in `cmd/dhi/main.go` (`branchVars`); bad pattern is a visible error, never a fallback |
| Commit format + subject length | `git_commit` validates in `parse` (before an approval is spent) with a message telling the author the fix |
| Co-author trailer | `git_commit` appends once (idempotent) |
| Agent awareness | `Config.Guidance()` joins the system prompt (`cliPrompt`) so agents comply before a gate refuses |
| Settings UI | rows for branch preset, commit format, co-author toggle, copyright toggle (needs `holder`) |

## Acceptance criteria

- [x] Zero config yields today's behaviour (`task/<slug>`, `review/<id>`, free commits)
- [x] Layering: workspace overrides user overrides default, per key
- [x] Invalid pattern/format/regex/co-author/holder refuses by key name
- [x] Conventional and ticket formats validated; non-conforming `git_commit` refused pre-approval
- [x] Co-author trailer is idempotent
- [x] Copyright headers per language, shebang-safe, idempotent
- [x] Settings rows persist and reload

## Deferred

- Worktree root location (`tasks.Dir` is the store layout; moving it is a migration).
- Copyright header injection on agent file writes, and `pr.title`/`pr.body`
  consumption by `pr_open` — the templates parse and validate today,
  the PR seam still takes the title agents pass. Both need the write/PR
  tools' seams; tracked for the tools phase.
- Free-text editing of patterns inside the TUI (hand-edit `config.toml`).
