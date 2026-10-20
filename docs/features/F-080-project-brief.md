# F-080 — Project brief: what we build and how we work

Status: done (M31 P2)

## Problem
Employees knew the code but not the project: what it is for, who uses it,
how the user wants work to flow, what to never do. Each user had to repeat
it in chat, and nothing carried it to every employee.

## Behaviour
- **File.** `.dhi/project.toml` (tracked, schema 1) holds one `[project]`
  table: `name`, `summary`, `goals`, `users`, `stack`, `process`, `review`,
  `testing`, `dont`. All free text; multi-line allowed. Unknown keys and
  other schemas are refused by name.
- **Detect.** `brief.Detect(root, members)` drafts a brief with file reads
  only: the name from package.json / pyproject / Cargo / go.mod (one member)
  or the folder; the README's first prose paragraph (badges, headings and
  code skipped, links flattened, capped at 400 runes); the stack from
  manifests (Go, Node + frameworks from dependencies, Python + Django /
  FastAPI / Flask / pytest, Rust, JVM, .NET, Ruby, Docker); the test
  commands from `verifyplan.Detect`; CI (GitHub Actions, GitLab, CircleCI,
  Jenkins, Azure). Process and review get sensible defaults.
- **Prompt.** A `project` section (priority 1, floor 800) right after the
  identity, before grounding, read fresh each turn. It tells employees the
  brief is ground truth from the person they work for. The Settings standards
  preview shows it in the same order.
- **Wizard.** A `project` step after the workspace step, shown while no brief
  exists, prefilled with the draft; enter keeps it. `\n` in a field is a new
  line.
- **Settings → PROJECT** (second section): the brief at a glance; `e` edits,
  `d` redrafts from the repo keeping what is written, `i` asks the first
  employee team lead (in the team channel) to interview you.
- **Interview.** The lead asks questions and calls `project_update`
  (`field`, whole new `text`, `rationale`): a harness tool every employee
  has. It records a pending proposal in the `project` layer of the F-070
  store and @mentions you; Settings → PROPOSALS shows the old and new text,
  and `y` replaces that one field.
- **Doctor.** `project/brief`: missing warns, malformed fails (and is left
  out of prompts until fixed).

## Also
- `kit.Form.ViewWidth(w)`: a form that fits a width; the active text field
  scrolls to keep its cursor visible. Settings form dialogs and the wizard
  project step use it, so long values no longer widen the dialog.

## Acceptance
- [x] Load/Save round trip, strict decode, field Set/Get
- [x] Detect: Go repo, polyglot members, empty dir
- [x] Prompt carries the brief before grounding; absent when no file
- [x] Wizard step: enter saves the draft, newlines survive, hidden when a brief exists; golden
- [x] Settings PROJECT: draft, edit, malformed named, interview posts to the lead; goldens
- [x] project_update proposes without touching the brief; PROPOSALS applies one field
- [x] doctor project/brief

## Live (session 59)
Run in tmux with a sandboxed DHI config, real Claude CLI, and a two-file Go
repo. Settings → PROJECT → `i` posted the request to #atlas-team. Atlas read
the brief and the repo, then asked four grounded questions. Atlas also spotted
that the branch was `master` while the tooling expects `main`, and that the
module path looked like a placeholder. The user answered all four in one
message. Atlas called `project_update` three times and summarised the changes.
PROPOSALS showed each change, and `y` wrote only that field to
`project.toml`.

Fixed along the way:
- **Proposal dialog clipped long text.** The proposed text ended in "…", so
  the user could not read all of what they were approving. It now wraps to
  the dialog width (`SettingProposal.lines(w)`).
- **Double period in the mention.** The agent's rationale ended with a
  period and the template added another (`… go install.. Review`). The
  rationale's own period is now trimmed.
