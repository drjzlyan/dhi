# F-107 — DHI picks the right bots

Status: planned (session 61, M35 P7)

## Why
Attaching a skill meant typing its slug in an agent form. The user wants DHI to decide which bots
should get a new skill or MCP server, with "all bots" as a choice.

## Behaviour
- After every install, and from SKILLS / INTEGRATIONS `a`, an Assign dialog offers
  "Recommended (N)" (preselected, each with a reason), "All bots", or "Choose…".
- Recommendations are deterministic: the skill or server's name, description, category and tool
  names scored against each agent's role, persona, system prompt, skills and tools.
- "All bots" is kept as a policy, so agents created later get it too.
- Saves are all-or-nothing across agents: skills into `skills`, servers as `mcp__<server>__*`
  grants plus the secret grants their variables need.
- Agent forms pick skills and tools from validated lists. "New agent with this…" prefills the form.

## Acceptance
- [ ] Scoring table over the starter-team roles.
- [ ] All-bots policy reaches a newly created agent.
- [ ] A failing validation writes nothing.
