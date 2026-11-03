# ADR-0046: Untrusted content in prompts is fenced

Date: 2026-11-03 · Status: accepted · Companion to: ADR-0022 (install trust), ADR-0044
Serves: F-122 (gap G-A1).

## Context
ADR-0022 decides what may be installed. It says nothing about the text an installed or running
thing hands to an agent. Knowledge entries, channel posts, task comments, notes and MCP replies
can all be written by someone other than the human, and they were placed in prompts and tool
results as if the human had written them.

## Decision
1. Text the human did not write is wrapped in `<untrusted_data source="…">` where it enters a
   prompt or a tool result. The human's request, the human's own messages and DHI's state lines
   stay unwrapped.
2. A standing operating rule tells the agent what the fence means.
3. The wrapper defuses the tag inside the content, and `joinTagged` closes any fence the budget
   trimmed open.
4. This is a mitigation. It lowers the chance an injected instruction is followed; it does not
   stop one. Containment is the sandbox, scopes and approvals (ADR-0006, ADR-0044), and native
   CLI tool containment is G-C1.

## Consequences
Prompts grow by a few tokens per fenced block. Prompt goldens that include these blocks change.
Wrapping at the source (tool or section builder), not in the dispatcher, keeps DHI's own results
unfenced.
