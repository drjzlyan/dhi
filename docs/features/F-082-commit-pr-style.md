# F-082 — Your commit and PR style, learned

Status: done (M31 P4)

## Problem
Commit format and PR text came from presets the user had to know about.
There was no commit body shape, nothing learned from the repo's own history
or PR template, and no way to say "write them like this one".

## Behaviour
- **Commit body.** `conventions.commit.body` (also on templates) is an
  example body. Agents follow its structure and tone, not its words; the
  guidance prints it indented under the commit rules. It survives template
  copy/apply.
- **Learn from history.** `conventions.InferCommits(subjects)` needs 5+
  non-merge subjects. When 60% agree it picks Conventional Commits or ticket
  keys (`{ticket}: {subject}` or `{ticket} {subject}`), otherwise free-form.
  The subject limit is raised from 72 to fit the 95th percentile (max 120).
  Every conclusion comes with a sentence of evidence.
  `gitcore.Runner.Subjects` reads `git log --no-merges` through hermetic git.
- **Learn from the PR template.** `.github/pull_request_template.md` (and
  the usual other places, or the first file of `PULL_REQUEST_TEMPLATE/`)
  becomes `pr.body`, with braces made safe.
- **Learn from an example.** `InferFromExample(commit, pr)`: the subject
  decides the format, the body (trailers removed) becomes the body example,
  and a PR is reduced to its skeleton (headings, unchecked checklist,
  template comments).
- **Wizard.** The conventions step learns from the repo when nothing is
  customised yet, preselects it ("detected") and shows the evidence. A new
  last field picks a naming template (the "wizard templates" item); choosing
  one updates the commit and branch fields to match.
- **Settings → CONVENTIONS.** `p` pastes a commit and/or PR you like; `g`
  learns from git history and the PR template. Both show what they learned
  and the resulting preview; enter saves the keys to the team conventions.
  The template form has a commit body field.

## Not done
- DHI does not write commit bodies itself: agents write them following the
  example. The human commit form still prefills only the subject.

## Acceptance
- [x] InferCommits: conventional, ticket, mixed, too few, long subjects
- [x] InferFromExample: ticket subject, body without trailers, PR skeleton; the learned config validates
- [x] PRTemplate; Guidance prints the body; template round trip keeps it
- [x] Settings: p (empty refused, confirm, saved), g (unavailable, nothing, ticket); golden
- [x] Wizard: detected preselected and saved; template syncs fields and saves; golden
