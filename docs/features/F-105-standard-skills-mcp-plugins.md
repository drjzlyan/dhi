# F-105 — Standard skills, MCP servers and plugins (packs removed)

Status: planned (session 61, M35 P5) · ADR-0043 · supersedes F-008, F-034, F-062, F-091 (registry parts)

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
- [ ] Spec validation and lenient-parse tables.
- [ ] Real Slack/Zoom/Atlassian `.mcp.json` files parse and round-trip.
- [ ] plugin.json and marketplace.json parse; install from local git fixtures.
- [ ] Each repair; `skill_run` refuses unassigned skills.
- [ ] Prompt has `<available_skills>` only when skills are assigned.
- [ ] F-099 evals within noise of the M34 baseline.
