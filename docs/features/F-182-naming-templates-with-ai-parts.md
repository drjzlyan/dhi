# F-182 — Your own branch, commit and PR formats, with AI-written parts

Status: done · ADR-0052 · builds on F-042, F-075, F-082

## Problem
Teams could pick a naming template (F-075) and edit it, but only with a fixed set of placeholders that
differed per field. Nothing could say "an AI writes this part". The review also found these bugs in the
existing rules:
- Settings validated every template against one sample map that held every placeholder. So
  `branch.task = "feat/{subject}"` saved fine and then failed when a task was provisioned. `{member}`
  in a task branch was blank on the board's take and fell back to `task/<slug>` without saying so.
- `commit.subject` was guidance only. `git_commit` checked the commit *format*, never the subject
  template.
- `pr.title` applied only to `pr_open`. PRs opened from the board or the Reviewer went out with the
  raw title.
- The Reviewer's "create PR" pushed `review/<id>` even when `branch.review` named the branch
  something else.
- The board opened a PR for the card's first member only; `pr_open` opens one per member.
- A failed commit or push on the board showed nothing (the error was dropped).
- PR templates learned from a repo turned `{…}` into `(…)`.

## Behaviour
- **One template language** (`internal/conventions/template.go`) for `branch.task`, `branch.review`,
  `worktree.task`, `commit.subject`, the new `commit.body_format`, `pr.title` and `pr.body`:
  - `{name}`: a symbol DHI fills. The symbols are `{ticket}`, `{slug}`, `{id}`, `{title}`,
    `{member}`, `{branch}`, `{assignee}`, `{user}` and `{date}`.
  - `{name|kebab}`: a filter. The filters are `lower`, `upper`, `kebab` and `snake`, and they chain.
  - `{ai}` or `{ai:hint}`: text an AI writes. One key per hint, so the same hint used twice gets the
    same text.
  - `{type}` and `{subject}` in commit fields are AI parts too, as they were before.
  - `{{` and `}}` are literal braces.
  - Any other brace text stays literal, so code in a PR body needs no escaping.
- **Per-field rules** (`ValidateField`): each field has its own symbol list. A task branch may use
  `{member}`: the board's prefill keeps it, and attaching fills it with the member being attached.
  `{ai}` is refused in `branch.review` and `worktree.task`, which must come out the same every time. Validation fills in sample values and
  sample AI text, then checks the result is a valid ref.
- **Who writes the AI parts:**
  - *An agent is acting* (`git_commit`, `pr_open`, `work_branch`): the agent writes them. Each tool
    takes `fill: [{"key": "<key>", "text": "…"}]` (a list of objects, the shape every engine's tool
    calling takes; a plain `{"key": "text"}` object is accepted too) and DHI puts the text in place.
    `git_commit` also still accepts a whole `message`. A missing key is refused before any approval
    card (for `work_branch`, only when it creates the branch), and the error lists every key with its
    hint. With a body format and no subject template, the subject is the `subject` part. The prompt's conventions section shows each template with the task's own values
    filled in and the AI parts as `<hint>`, plus the key list. It is the same text on all six CLIs
    (`TestPromptConventionsSameOnEveryCLI`).
  - *Nobody is acting:*
    - a branch named before a task turn (`Provision`, the board's take or attach);
    - a commit from the board or the editor's message buffer;
    - a PR from the board or the Reviewer.

    In these cases DHI runs one read-only call on the default agent (`Runtime.WriteSlots`, built like
    `:suggest`, F-181). The call has no tools, resumes no session and posts nothing. The diff, commits
    or task go inside the untrusted fence. It counts toward budgets and is recorded in the ledger as
    `kind: "write"`. A CLI with no read-only mode (opencode, copilot, cursor, antigravity) is refused by
    name, the same as `:suggest`: write the parts yourself, or make a claude/codex agent the default.
- **Checking:** `CheckCommitFor(msg, vars)` turns the subject template and body format into an
  anchored pattern. The fixed parts must read exactly as rendered for this task; the AI parts match
  any text, and whitespace matching is lenient. Trailers at the end are ignored. So a wrong ticket is
  refused with the expected shape (`"PROJ-9: <imperative summary…>"`). A ticket-format team whose card
  has no ticket key gets the slug in `{ticket}`. The template's shape decides, the coarse "starts
  with a key" check is skipped, and the guidance says the slug stands in.
- **One rendering path for PRs:** `review.Service.OpenPR` renders both `pr.title` (the author's
  title becomes `{title}`) and `pr.body` for every caller: `pr_open`, the board and the Reviewer.
  Rendering happens before the push, so a refused PR pushes nothing. Symbols come from one source,
  `varsForBranch`: the card's values when a card owns the branch, otherwise what the branch name gives.
  A Reviewer PR uses the branch its worktree has checked out (falling back to the `branch.review` name).
  The board opens one PR per member.
- **Human forms:**
  - The editor's `COMMIT_EDITMSG` starts from the team's draft: symbols filled, AI parts as
    `{ai:…}` markers, and a note explaining them. On `:w`, any markers still there are written off the
    UI thread, then the commit runs. Replace a marker with your own text to write that part yourself.
  - The board's one-line commit form starts with the subject up to its first AI part (`PROJ-7: `),
    so typing never lands after a marker. On submit, any markers you typed, and the body format, are
    written off the UI thread, then the commit runs.
  - The board's branch prefill works the same way. Attaching runs off the UI when the name has
    markers.
  - Human commits are otherwise not checked against the conventions. Only agents are gated.
- **Settings → CONVENTIONS:** the template form has a `body format` field. The preview adds `review`
  and `body` lines and shows AI parts as `<hint>`. The help lists the symbols, filters and `{ai:…}`.
  The wizard preview shows AI parts too.
- **Built-in template `described`:** Conventional Commits `{type}: {subject}`; a body format
  `{ai:why the change is needed, in one or two sentences}\n\nRefs: {ticket}`; and a PR body with
  `## Summary` and `## Testing` sections written by the AI around the fixed parts.
- **Learned PR templates** double their braces, so they render back exactly as written.

## Not done / limits
- An agent that runs `git commit` or `gh pr create` in its own shell skips these rules on every CLI.
  The guidance tells agents to use `git_commit`/`pr_open` only. DHI does not install a `commit-msg`
  hook: that would mean writing into the user's repos or overriding their `core.hooksPath`
  (ADR-0044). The default branch is still guarded (F-086).
- AI parts in a branch name written before a turn depend on the default agent having a read-only mode.

## Acceptance
- [x] Ticket format + slug fallback: a composed message passes its own check; guidance says the slug stands in (`TestTicketFormatWithASlugFallbackStillCommits`)
- [x] `fill` schemas are a list of objects, never an open map (`TestFillSchemasArePortable`); the list form commits
- [x] Language: symbols, filters, `{ai}`/`{ai:hint}`, legacy `{type}`/`{subject}`, `{{ }}`, literal braces (`template_test.go`)
- [x] Per-field validation; every symbol has a sample (`TestValidateFieldRules`, `TestSampleVarsCoverEverySymbol`)
- [x] Match: fixed parts, wildcards, whitespace, multi-line bodies; CheckCommitFor + ComposeCommit round trip
- [x] Guidance shows task values, `<hint>` parts, the fill keys and the git_commit/pr_open rule; identical on all six CLIs (golden `prompt_conventions`)
- [x] git_commit `fill` composes with the task's symbols; a wrong ticket and missing parts are refused before an approval
- [x] pr_open refuses missing AI parts before an approval and passes title + fill to the review seam
- [x] work_branch refuses missing branch parts; the fill reaches Provision
- [x] Review service: pr.title on every path; agent PR with missing parts pushes nothing; generator fills a human's PR; Reviewer PR uses the configured review branch
- [x] WriteSlots: one read-only, fenced, metered spawn; refuses a CLI without read-only mode; reply parsing
- [x] Board: PR title template, one PR per member, commit writer off the UI, commit/push failures shown
- [x] Editor: draft from the team's shape; `:w` writes markers then commits
- [x] Settings: body format field, preview, help (goldens); learned braces render back as written
- [ ] Live: a claude agent fills a `described` PR through pr_open; the board writes a commit with the default agent (not run in this session)
