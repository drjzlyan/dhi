# F-117 — The agent chat loop: approvals, routing, stop, progress, suggestions, notices

Status: done (M37 P2) · amends F-007, F-016, F-036, F-038, F-072

These items come from the session-63 audit, items 12–17, plus the S2 agent-loop items for stop,
progress and notices.

## Behaviour
- **Typing never answers an approval.**
  - A focused composer, in both the editor sidebar and the Workspace chat, answers with
    alt+y, alt+o, alt+a, alt+g or alt+n. You can also type `/yes`, `/once`, `/always`,
    `/everywhere` or `/no` (or just the first letter) as the whole message.
  - On a Mac without Option as Meta, ⌥y, ⌥o, ⌥a and ⌥g type ¥, ø, å and ©. These answer too,
    but only while the box is empty. ⌥n is a dead key on a US layout, so "no" goes through `/no`
    or the buttons.
  - A blurred Workspace composer still answers with bare letters, as before. The editor
    sidebar's blurred state belongs to the buffer, so letters there edit the buffer.
  - Answers apply to the current channel's agent in a DM, and to the oldest request in a
    shared channel.
  - Before this change, a message beginning with y, n, a, o or g (for example "no, try…")
    answered `list[0]` from any agent.
- **A message in #general with no @mention is answered.**
  - It goes to the default agent: the first team lead (teams by name), else the only agent,
    else the first agent by name (`Runtime.DefaultAgent`). The sidebar shows "@x is on it ·
    @name asks someone else" until that agent replies.
  - Only human posts are routed this way. An agent's reply without a mention wakes nobody, so
    agents can't loop.
- **Stop a turn.** ctrl+x in either chat cancels every turn running in the channel
  (`Runtime.CancelChannel`) and reports how many it stopped.
- **Live progress.** While a turn runs, the sidebar shows the agent's latest progress line
  (`● @x: running go test… · 2 queued  ^x stop`). `Runtime.Busy` counts the turns running and
  the turns queued behind a busy agent.
- **A failed post keeps your text** and says why ("not sent: …").
- **Apply (ctrl+f)**
  - The fence's info string is dropped, so a ```` ```python ```` block no longer inserts the
    word `python`.
  - With a visual selection, the suggestion replaces the selection (an inline edit). Without
    one, it is inserted at the cursor as whole lines.
- **Agent suggestions (`editor_propose_edit`)**
  - A suggestion never takes over the keyboard. You get a toast ("@ana suggests a change ·
    ctrl+y") and the buffer chip, and ctrl+y opens the review.
  - The review steps through the changes one hunk at a time (`linediff.Hunks`), with two lines
    of context:
    - y keeps the change and n drops it.
    - N drops the rest of the suggestion.
    - A accepts every pending suggestion.
    - esc defers the decision.
  - The kept changes are applied as one undo step.
  - When the agent proposed several changes, it is told which ones it kept
    ("kept 1 of 2 changes…"). A rejection now wakes the agent (`rt.Handle`), so it can adjust;
    before, the rejection was posted but nobody was woken.
- **Notices**
  - An agent's reply in a channel you are not reading, or while the sidebar is closed, raises
    a toast. This uses a new `bus.AllChannels` subscription.
  - While the terminal window is not focused (focus reporting), every toast is also sent as an
    OSC 9 desktop notification, which iTerm2, Ghostty, WezTerm and kitty show. Control
    characters are stripped from the text.

## Acceptance
- [x] With an approval pending, typing "no thanks" stays text. alt+y, `/no` and `¥` answer
      it (`chat_test.go`).
- [x] A bare #general post goes to the default agent, and an agent's bare post wakes nobody
      (`dispatch_test.go`). Live: "@atlas is on it" appeared and atlas answered.
- [x] `CancelChannel` and `Busy` are covered, race-clean (`nil_test.go`).
- [x] Apply drops a ```` ```python title=x ```` info line and replaces the selection.
- [x] A suggestion doesn't open on its own. Per-hunk review: n then y keeps one change and
      tells the agent (`pair_test.go`).
- [x] A reply toast fires for another channel and not for the one on screen. OSC 9 text is
      sanitised.
- [x] `make verify` passes.

## Notes
- A real turn's live progress line and the stop key still need a signed-in CLI to check live.
  The sandboxed HOME used in the audit has no Claude login.
- OSC 9 is sent whenever the terminal has reported a blur, and there is no setting for it yet.
  Terminals that don't support OSC 9 ignore the sequence.
