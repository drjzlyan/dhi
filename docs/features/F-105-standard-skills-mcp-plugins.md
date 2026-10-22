# F-105 — Standard skills, MCP servers and plugins (packs removed)

Status: done (session 61, M35 P5); live install checked in P9; eval gate open (fixtures carry no skills) · ADR-0043 · supersedes F-008, F-034, F-062, F-091 (registry parts)

## Why
DHI had its own skill file format, its own pack/registry/marketplace and a hardcoded integrations
catalog with community packages. The user wants widely used standards only: Agent Skills, the
`mcpServers` JSON, and the vendors' own plugins.

## Behaviour
- Skills are Agent Skills folders (`<name>/SKILL.md` + `scripts/`, `references/`, `assets/`),
  parsed leniently as the agentskills.io client guide describes. Built-ins ship as SKILL.md.
- MCP servers use the `mcpServers` JSON (`type`, `command/args/env`, `url/headers`,
  `oauth.clientId/callbackPort`, `${VAR}`).
- Plugins: `.claude-plugin/plugin.json` contributes its skills and MCP servers; marketplaces are
  `.claude-plugin/marketplace.json`. Default marketplace: `anthropics/claude-plugins-official`.
- Install from a marketplace plugin, a GitHub/git URL or local path (skills), or a URL / JSON
  snippet (MCP). Commits are pinned in `installed.toml`; unsigned sources show scripts and commands
  before anything runs.
- Scopes: workspace (`.dhi/skills`, `.dhi/mcp.json`) and user (`<config>/dhi/...`). Also read:
  `~/.agents/skills`, and `.agents/skills`, `.claude/skills`, `.mcp.json` in member repos.
- Agents see `<available_skills>` (name, description, location) and load SKILL.md when needed
  (`skill_activate` for runtimes without file reads). `skill_run` runs assigned skills only.
- Removed: packs, registry, marketplace, the hardcoded catalog, LIBRARY skills, AGENTS `g`.
  Named repairs migrate old skills, pack installs and `.dhi/mcp/*.toml`, and replace the community
  Atlassian/Slack servers with the official plugins.

## Acceptance
- [x] Spec validation and lenient-parse tables.
- [x] Real Slack/Zoom/Atlassian `.mcp.json` files parse and round-trip.
- [x] plugin.json and marketplace.json parse; install from local git fixtures.
- [x] Each repair; `skill_run` refuses unassigned skills.
- [x] Prompt has `<available_skills>` only when skills are assigned.
- [ ] F-099 evals within noise of the M34 baseline — not run: no fixture assigns a skill, so prompts are unchanged; a skill-activation fixture is still to write.

## Notes (session 61)
- Settings: SKILLS (i install, g give to bots, a new bot with it, w write one, s allow scripts, x
  remove, v view) and INTEGRATIONS (+ add a plugin / URL / snippet, g, a, x, v). LIBRARY keeps roles
  and personas. PACKS and MARKETPLACE are gone; AGENTS `g` imports agent manifests only.
- Marketplace catalogs on GitHub are read as one file (raw.githubusercontent.com) and cached a day;
  other hosts are cloned. Plugin sources: relative, url+sha, git-subdir, github.
- Built-in skills are written to `<cache>/dhi/builtin-skills/<version>/` so agents read them by
  path; those dirs, yours and `~/.agents/skills` are read-only sandbox roots.
- A stdio server from the standard config gets network (the format has no origin list); files stay
  sandboxed. Legacy SSE is refused by name.
