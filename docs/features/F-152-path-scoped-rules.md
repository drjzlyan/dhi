# F-152 — Path-scoped rules and skills

Status: done · gap G-B7

Every standing instruction and every assigned skill rode every turn, whether or not the turn touched
the files it was written for. Claude Code (`paths:` in `.claude/rules`) and Cursor (`globs`) scope
rules by file; ADR-0043 says to use the standard field rather than invent one.

## Behaviour
- Skills: `SKILL.md` frontmatter may carry `paths:` (YAML list, or one comma-separated string). A
  scoped skill is in the catalog only when the turn concerns a matching file. DHI round-trips it.
- Rules: the standard Claude Code location, `.claude/rules/**/*.md` in each workspace member repo,
  Markdown with optional frontmatter. No DHI-only location: the files are committed with the repo,
  so a team shares them. `paths:` scopes a rule; a rule without it always applies. Matching rules
  form a "Rules" section, each labelled `<member>/.claude/rules/<name>.md`. A file whose
  frontmatter does not parse still applies to every turn and says so in the prompt (not dropped
  silently).
- Like F-124's AGENTS.md: `clirun.CLI.ReadsClaudeRules` marks a CLI that loads `.claude/rules` from
  its cwd. Set for claude only, from Claude Code's documented project-rules behaviour (not a live
  canary). For it, the cwd member's rules are left out and the other members' are carried; other
  CLIs get every member's rules. A task worktree's copy stands in for its member.
- Matching (`internal/agentkit/pathscope`): `**` spans directories; `*`, `?`, `[..]` as
  `path.Match`; a glob without `/` matches the base name at any depth. A leading member name on the
  file is also tried off, since globs are written relative to a repo.
- "Files the turn concerns" are the path-like tokens in the trigger text. `@file` and `@selection`
  expand to the open buffer's path before the message is sent (F-120), so they are covered. A task
  card records no touched-file list, so none is used.
- Settings preview (where F-124 shows repo AGENTS.md) lists every rule; scoped ones are labelled
  `(applies to <globs>)`.
- Unscoped skills and the default prompt are unchanged (`TestPromptGoldens` untouched).

## Acceptance
- Glob semantics: `TestMatch`, `TestAnyStripsMember`, `TestFiles` (pathscope).
- Skill `paths:` parse and round trip: `TestPathsFrontmatter`; catalog filter: `TestComposeScopedSkills`.
- Rule included and excluded, preview, cwd skip, malformed: `TestRulesScopedIncludedAndExcluded`,
  `TestRepoRulesSkipsTheCwdMemberWhenCLIReadsThem`, `TestRulesMalformedFrontmatterIsVisible`; in the assembled prompt: `TestPathScopedRules`.
