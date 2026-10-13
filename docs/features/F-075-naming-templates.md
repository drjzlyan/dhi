# F-075 — Naming templates: branch, worktree, commit, PR

Status: done (M30 P4)

## Problem
Conventions held one pattern per rule. Settings only cycled a few branch
presets, worktree folders were always `.dhi/tasks/<slug>/<member>`, there
was no commit-subject shape or ticket key, and a team could not save its
own scheme.

## Behaviour
- **Template.** A named bundle of: task and review branch, worktree
  folder, commit format, ticket regexp, commit subject shape, PR title
  and PR body (the description). Placeholders: `{slug}` `{member}`
  `{ticket}` `{user}` `{date}` `{title}` `{assignee}` `{type}` `{subject}`
  (each field accepts the ones that make sense; unknown ones are refused).
- **Ticket key.** The first match of the ticket regexp in the card's slug,
  title, epic or labels (`proj-42-login` → `PROJ-42`); else the slug.
- **Built-ins.** `default` (task/<slug>), `feature` (feature/<slug>,
  Conventional Commits), `ticket` (`{ticket}/{slug}`, `{ticket}: {subject}`,
  `[{ticket}] {title}`, structured PR body), `personal` (<user>/<slug>).
- **User templates.** Settings → CONVENTIONS lists built-ins and the
  team's templates with a live preview for a sample card. `enter` uses
  one (its rules are copied into the flat conventions every consumer
  reads, and `active` records it), `n` copies the selected template into
  a new one, `e` edits a user template, `d` deletes it. Everything
  validates before saving and saves at once to the tracked
  `.dhi/conventions.toml` (the user file outside a workspace); live
  reload applies it on the next action.
- **Where it applies.** Branches (board attach and take, runtime
  provisioning), worktree folders (`worktrees.Layout.TaskAt`, still under
  the tasks directory so the sandbox reopen holds), agent guidance (the
  subject shape), `pr_open` titles (`{ticket}`), PR bodies (`{slug}`,
  `{ticket}`, `{assignee}`), and the board's commit form, which starts
  from the subject template (`PROJ-42: `).
- Worktree values are made path-safe (no separators or dots from a
  value) and the pattern must contain `{member}` and stay inside.

## Acceptance
- [x] Template merge across layers, validation (per template), built-ins
      valid, shadowing, activation.
- [x] Worktree expansion safety; `TaskAt` placement.
- [x] Settings section golden and the use / copy / edit-refusal / delete
      flow, reloading from disk.
- [x] Commit form prefix.

## Not done
- The setup wizard still offers branch presets, not templates.
