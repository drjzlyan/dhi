# F-083 — Git identity: confirm, suggest, change

Status: done (M31 P5)

## Problem
The setup wizard showed an identity it found but could not change it. When
none was set it asked for a name and email with empty fields, though the
machine usually knows both. There was no way to change it later inside DHI.

## Behaviour
- **Found.** The identity step shows the identity: `enter` uses it, `e`
  opens the form prefilled to change it.
- **Suggested.** When git has no identity, `gitcore.SuggestIdentity` gathers
  candidates, best first: each member repo's local config, then its most
  frequent human author over the last 200 commits (bots dropped), then the
  signed-in GitHub account (managed `gh` only: `gh api user`, falling back to
  GitHub's noreply email), then the computer account's full name
  (`os/user`). `BestSuggestion` takes the first name and the first email and
  names both sources. The form is prefilled with it, and every candidate is
  listed with where it came from.
- **Settings → CONFIG → YOU.** A `git identity` row shows the current
  identity (or "not set"). `enter` opens the same form (suggested when
  unset), then shows the exact `git config --global` commands; `enter` runs
  them.
- Nothing is written without that confirmation; validation is unchanged
  (`gitcore.ValidateIdentity`).

## Acceptance
- [x] SuggestIdentity order, dedupe, bots; BestSuggestion sources
- [x] Wizard: suggested prefill + candidates (golden); found → e → change
- [x] Settings row: unavailable, unset → suggested → confirm → set → refreshed; bad email refused

## Live (session 59)
Run with a sandboxed HOME that had no `.gitconfig` and the git author/committer
env vars unset. The wizard opened the form with the name suggested from the
computer account and an empty email. It showed the exact
`git config --global` commands and wrote nothing until enter. Afterwards
`~/.gitconfig` held the name and email, and `git var GIT_AUTHOR_IDENT` used
them.
