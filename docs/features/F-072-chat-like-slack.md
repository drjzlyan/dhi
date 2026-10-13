# F-072 — Chat that reads like Slack

Status: done (M30 P1)

## Problem
The workspace CHANNELS floor showed raw `dm:<id>` labels, put the DIRECT
MESSAGES header above the channels, and could not find a channel by name.
Every relayed CLI line and runtime notice (tool calls, "retrying…",
verify output) landed in the conversation as if a teammate had said it,
and counted as unread. Agent replies with code fences or lists showed as
raw markdown. The user asked for a channel list like Slack's, searchable,
with only human-like messages, formatted properly.

## Behaviour
- **Rail.** A CHANNELS group (`#general`, then team channels) and a
  DIRECT MESSAGES group listing each agent by name — never `dm:`. A
  presence dot marks an agent that is working right now. Channel ids stay
  `dm:<id>` internally; one `kit.ChannelLabel` names them everywhere
  (workspace rail, header, narrow rail, switcher, editor chat).
- **Where it applies.** The workspace CHANNELS floor, the editor chat
  sidebar and the Ideator transcript (message ids move to a muted suffix
  so markdown is never mis-parsed).
- **Find by name.** With the rail focused, `/` filters the rail by fuzzy
  name; `enter` opens the top match. `ctrl+g` opens a quick switcher over
  every channel and DM, unread first; type to filter, `enter` opens.
- **Messages only.** Stream lines (`bus.KindStream`: tool calls, CLI
  progress, routine runtime notices) never render in the transcript or
  the thread pane and never count as unread or inbox attention. They stay
  on the bus for the trace view (F-074).
- **Live status line.** While an agent is working on the open
  conversation, a typing-style line above the composer says what it is
  doing in plain words ("worker is running tests…"), derived from its
  newest stream line. It disappears when the turn ends.
- **Alerts.** A runtime event the human must act on (budget stop, auth
  failure, a failed or stuck task, a missing toolchain) posts as
  `bus.KindAlert`: a muted system-styled line in chat that counts as
  attention, but is never replayed to the agent as its own words.
- **Teammate voice.** Agents are told their reply is posted to team chat:
  plain prose, no tool logs, fenced code for code. A conservative
  sanitizer drops leaked tool-echo lines from final replies.
- **Formatting.** Message bodies render markdown through a themed
  renderer (`kit.Markdown`): fenced code as a shaded block with a language
  label and syntax colours, lists, quotes, inline code, headings, links.
  Plain prose skips the renderer. Output is cached per text and width and
  keeps the row background intact (no holes).

## Acceptance
- [x] Rail shows CHANNELS then DIRECT MESSAGES, agent names without `dm:`,
      presence dot for a working agent; header and narrow rail match.
- [x] Rail `/` filter and `ctrl+g` switcher find channels and DMs by name.
- [x] Stream rows are absent from transcript, thread pane, unread counts
      and inbox; the status line shows during a turn and clears after.
- [x] Alerts render as system lines, count as attention, are not replayed.
- [x] Code fences, lists and inline code render formatted in chat, thread
      pane and editor chat; goldens and the background sweep cover them.
- [x] Reply sanitizer table-tested; prompt guidance present.

## Notes
- `bus.KindAlert` is the human-facing kind; `Runtime.alert` posts it.
  Final failures, budget stops, auth failures and refused turns alert;
  a turn error that was not alerted gets a generic alert in `Turn`.
  Routine notices (`Runtime.notice`) stay `KindStream`.
- Presence: `Runtime.AgentWorking` (nil-safe) via `Deps.AgentWorking`.
- Renderer: `kit.RenderMarkdown` / `kit.ChatMarkdown` (goldmark AST +
  `syntax.LexLang`), not glamour: it paints with theme tokens and keeps
  row backgrounds intact. `TrnRow.Suffix` carries pins, edits,
  reactions and reply counts outside the markdown body.
