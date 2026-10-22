# ADR-0043: Agent Skills, mcpServers JSON and vendor plugins replace packs

Date: 2026-10-22 · Status: accepted · Supersedes: the pack/registry/marketplace decisions of
F-008, F-034 and F-062, ADR-0028's hardcoded catalog, and DHI's single-file skill format. M35,
F-105, F-107.

## Context

DHI invented its own skill file (`<slug>.md` with a private `script:` key), its own pack format,
registry and signing, and a hardcoded integrations catalog that ran community packages for
Atlassian and Slack. Meanwhile the ecosystem converged: Agent Skills (agentskills.io) is read by
Claude Code, Codex, Cursor, Copilot, opencode and others; MCP servers are configured with the
`mcpServers` JSON; and vendors publish official plugins (`.claude-plugin/plugin.json` bundling
`skills/` and `.mcp.json`) in marketplaces such as `anthropics/claude-plugins-official`. The user
asked for the standards only, and for skills and MCP access to go to the right bots.

## Decision

1. **Skills are Agent Skills folders**, parsed leniently per the agentskills.io client guide.
   Built-in skills ship in that format.
2. **MCP servers use the `mcpServers` JSON**, stored per scope as `.dhi/mcp.json` and
   `<config>/dhi/mcp.json`, exactly as vendors publish it.
3. **Plugins and marketplaces are read in the vendor format.** DHI uses a plugin's skills and MCP
   servers; it lists but does not run commands, agents or hooks.
4. **Packs, the registry, registry signing, the marketplace and the hardcoded catalog are removed.**
   Named repairs migrate existing installs.
5. **Progressive disclosure.** Agents get an `<available_skills>` catalog for their assigned skills
   and read SKILL.md on demand, instead of every body inlined into every prompt.
6. **Assignment is explicit and recommended.** Each install ends in an Assign step with a
   deterministic recommendation, "all bots" (kept as a policy), or a manual choice.

## Consequences

- Skills and servers installed for other tools work in DHI and vice versa (`.agents/skills`,
  `.claude/skills`, `.mcp.json` are discovered in trusted member repos).
- Unsigned sources replace signed registry entries, so installs pin a commit and show scripts and
  commands before they can run.
- Prompt shape changes for every skilled agent; the F-099 evals gate the change.
