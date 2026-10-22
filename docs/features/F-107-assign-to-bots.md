# F-107 — DHI picks the right bots

Status: done (session 61, M35 P7, landed with P5)

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
- The agent form refuses an unknown skill by name (install it in SKILLS first). "New bot with
  this…" (`a` in SKILLS and INTEGRATIONS) prefills the form.

## Acceptance
- [x] Scoring table over the starter-team roles.
- [x] All-bots policy reaches a newly created agent.
- [x] A failing validation writes nothing.
