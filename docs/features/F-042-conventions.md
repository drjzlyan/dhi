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
default. `settings.Config.Conventions` embeds it. Conventions are a
**team contract**, so they do not live in the personal, gitignored
`.dhi/config.toml`. Layers, lowest first:

1. built-in defaults
2. `~/.config/dhi/config.toml`, then `~/.config/dhi/conventions.toml` (personal)
3. **`.dhi/conventions.toml` — tracked, shared with the team**
4. `.dhi/config.toml` (personal override, may also carry `[conventions.*]`)

`conventions.toml` may only contain `[conventions.*]`. `Config.Save` never
serialises conventions (so saving a theme cannot freeze team values into a
personal file); `settings.SaveConventions` writes the tracked file in
full. Strict unknown-key refusal (ADR-0011) applies, and `UnknownKeys`
now recurses so a typo under `conventions.commit.*` is named.

```toml
[conventions.branch]
task   = "task/{slug}"      # placeholders: {slug} {id} {user} {date}
review = "review/{id}"

[conventions.commit]
format         = "free"     # free | conventional | ticket
ticket_pattern = '[A-Z][A-Z0-9]+-\d+'
max_subject    = 72
co_author      = ""         # "Name <email>" — no default: DHI drives several CLIs
co_author_enabled = false   # trailer added once when true (a layer can switch it off)

[conventions.copyright]
enabled = false
holder  = ""                # required when enabled
license = ""                # SPDX id
year    = ""                # "" = current year

[conventions.pr]
title = "{title}"                                   # agent-chosen titles: {title} {slug}
body  = "Created from DHI worktree `{branch}`."     # every PR DHI opens: {branch} {member} {title}
```

## Enforcement points

| Rule | Where |
|---|---|
| Branch patterns | task attach and review worktree seams in `cmd/dhi/main.go` (`branchVars`); bad pattern is a visible error, never a fallback |
| Commit format + subject length | `git_commit` validates in `parse` (before an approval is spent) with a message telling the author the fix |
| Co-author trailer | `git_commit` appends once (idempotent), only when `co_author_enabled`; the value must be set explicitly |
| Agent awareness | `Config.Guidance()` joins the system prompt (`cliPrompt`) so agents comply before a gate refuses |
| New-file copyright header | the served `write` tool prepends `copyright` to a file that does not yet exist (language-aware, shebang-safe, idempotent); overwrites are left exactly as given |
| PR title / body | `pr_open` renders `pr.title`; `review.Service.CreatePRForBranch` renders `pr.body` for every PR (agent or Reviewer); unknown placeholders are refused at load |
| Settings UI | rows for branch preset, commit format, co-author toggle (needs a value), copyright toggle (needs `holder`); saved to `conventions.toml`, flash says changes are live (F-052) |
| Preview | Settings "effective standards" shows standards + conventions, in the order `cliPrompt` appends them |

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
- **A CLI agent's own file edits.** The header is applied by DHI's served `write` tool.
  Coding CLIs (claude, codex…) write files with their *own* tools, which DHI cannot
  intercept, so for them the header remains an instruction in the system prompt only.
- Live reload: edits apply on restart (the wizard relaunches; Settings says so).
- Free-text editing of patterns inside the TUI (hand-edit `config.toml`).
