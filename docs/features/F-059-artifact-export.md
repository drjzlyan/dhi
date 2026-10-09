# F-059 — Export ideation artifacts

Status: done (M27 P7; the M7 "export to repo paths / MCP issue trackers" deferral)

## Behaviour
On the Ideator CANVAS, `X` exports the artifact under the cursor. The form
toggles the destination with `←/→`, and the target updates to a sensible
default:

| Destination | Target | Result |
|---|---|---|
| repo file | `member/path` (default `<first member>/docs/<name>`) | the artifact is written there and opened in the editor; an existing file needs a second `enter` to overwrite |
| task card | slug (default from the file name) | a board card titled by the artifact's first heading, with the text (up to 4 KB) and its path as the first comment |
| tracker (via agent) | agent id (default: the moderator) | a request in the session channel asking that agent to file it as an issue with its MCP tools and reply with the link; the agent's turn is dispatched |

DHI never calls an issue tracker itself. Filing goes through an agent that
holds the tracker's MCP tools (F-047 catalog: Jira, Linear, GitHub, …),
under its scopes and approvals, and shows up in the session like any other
agent work. Real tracker calls cannot be verified without credentials, so
the test covers the request and the dispatch, not a vendor API.

## Acceptance
- [x] file export, overwrite guard, task card with comment, tracker request
      + dispatched turn (ideator test)
