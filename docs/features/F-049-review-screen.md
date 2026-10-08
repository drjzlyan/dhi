# F-049: GitHub-grade review screen — suggestions, one review as you, real diffs

Status: in progress (M26) · Builds on F-005 (Reviewer), F-029 (the user's identity is the only one that goes outward)

## Problem
The Reviewer worked, but fell short of reviewing on GitHub:
1. **A "review" did not go out as a review.** An own-PR review became N loose comments (replies mis-threaded
   because the root's remote id was never recorded); an external PR got one flat issue comment. No verdict,
   no summary, no inline anchors.
2. **Employees' findings were not curated.** A review an employee wrote landed as ordinary threads and
   would go out as-is.
3. **Diffs were basic**: no syntax colour, no word-level highlight inside a changed line, no way to see the
   code around a hunk, and files / diff / conversation lived in separate sections.
4. **A latent bug**: `gh api repos/<repo>/…` was given the git *remote URL*, which `gh` rejects
   (`unsupported protocol scheme`). Verified against the real `gh`; the fake used in tests hid it.

## Design

### R-A. One review, sent as the human
- `GH.SubmitReview` → `POST repos/{owner}/{repo}/pulls/{n}/reviews` with `commit_id`, `event`
  (`APPROVE` | `REQUEST_CHANGES` | `COMMENT`), `body`, and inline `comments[]` (line/side, or file-level via
  `subject_type: file`). All `gh api` calls normalise the remote to `owner/name` (`--hostname` for non-github.com).
- `Service.SubmitReview(ctx, review, verdict, summary)`:
  - sends **only the human's own comments** that have not been sent (`Author == you`, not suggestion, not dismissed, not already `Posted`);
  - a comment whose line is **not in the diff** (GitHub would reject the whole review with 422) is moved into the
    review body as `path:line — text` instead of failing;
  - replies to an **existing GitHub thread** go as replies (the reviews API cannot thread), after the review;
  - **no agent name or marker ever goes out** (F-029);
  - approving or requesting changes on **your own PR** is refused up front with the fix (use *Comment*);
  - on success each sent comment is marked `Posted`, the review records verdict, summary and URL;
    a second round sends only what is new.
- Schema 2 cards: comments gain `posted`, `suggested`, `dismissed`, `severity`; reviews gain `verdict`, `summary`. Schema 1 still loads.

### R-B. Employees suggest, you decide
- "Ask an employee to review" requests **structured findings** (a fenced `json` block: file, line, side,
  severity, comment, plus a summary). Each finding becomes a *suggestion* anchored to its line; text that
  cannot be parsed becomes one file-level suggestion with the raw reply (never dropped).
- A suggestion shows the employee's name and severity, and is **not sent** until you act:
  `y` accept (becomes your draft), `e` edit & accept, `x` dismiss. Replies an employee gives inside a thread
  *you* started stay conversation and are never sent.

### R-C. Submit dialog
`S` (from any review section) opens: verdict → summary → confirmation that says exactly what will be sent
("one review as you: 4 inline comments, 1 in the summary; 3 suggestions undecided will not be sent").

### R-D. Diffs
Syntax colour (chroma, via theme tokens) on code lines; **word-level** highlight of the changed span inside
a paired removed/added line; **collapsed context** between hunks ("⋯ 42 unchanged lines") that expands from
the review worktree's file.

### R-E. One screen
With a review open and ≥ 120 columns, FILES (tree with ± stats and viewed ticks) | DIFF | CONVERSATION
(threads and suggestions for the current file) are visible together; focus follows the existing section
handlers, so every key keeps working. Narrower terminals keep the section view.

## Acceptance criteria
(ticked as they land)
- [ ] gh repo normalisation: https / ssh / scp forms, `.git`, trailing slash, enterprise host
- [ ] SubmitReview payload shape (event, commit, inline + file-level comments)
- [ ] only the human's unsent comments go; suggestions/dismissed/posted never; no agent text
- [ ] off-diff comment falls back into the body; replies to remote threads thread correctly
- [ ] own-PR approve/request-changes refused with the fix; comment allowed
- [ ] second submit sends only new comments; schema 1 cards load
- [ ] structured findings parse into anchored suggestions; garbage becomes one file-level suggestion
- [ ] accept / edit / dismiss; undecided count shown at submit
- [ ] submit dialog end to end
- [ ] syntax + word-level + expandable context
- [ ] three-column screen at ≥ 120 cols, section view below
- [ ] walked in tmux; a live review against GitHub only with the user's go-ahead

## Not verified without your go-ahead
Posting a review to a real PR changes your repository. Everything up to the HTTP call is tested; the live
call needs a scratch PR you approve.
