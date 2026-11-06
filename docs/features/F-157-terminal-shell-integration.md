# F-157 — Terminal shell integration

Status: done · gap G-M2

The drawer's terminal knew nothing about the shell inside it: no way to jump between commands, to see
which one failed, to open a tab where the shell had `cd`ed, or to hand it code from the editor.

## Decision
Read the marks the shell already sends; write nothing. Shells and prompt tools that emit the
FinalTerm / iTerm2 semantic-prompt marks (OSC 133 `A` prompt, `B` input, `C` output, `D;<exit>`
end) and the working-directory report (OSC 7 `file://host/path`) light the features up: fish,
starship, iTerm2 / WezTerm / VS Code shell integration, recent zsh and bash setups. DHI never
edits a shell rc file (ADR-0044). Without the marks everything stays idle; `alt+k` / `alt+j` then
say so in a toast that names OSC 133 and this spec.

To enable it: use a shell or prompt tool that emits OSC 133 (fish does by default; starship has
it behind its `[shell]`/integration setup; the iTerm2/WezTerm/VS Code integration scripts emit
it), and for OSC 7 a `precmd` that prints `\e]7;file://$HOSTNAME$PWD\a` (fish, zsh's
`add-zsh-hook`, and most integrations already do). The user adds this to their own rc file.

## Behaviour
- `vt.ParseMark` / `vt.ParseCwd` parse the payloads; `vt.Screen` records each prompt as an absolute
  row (stable while scrollback is trimmed). Terminators BEL and ST, and sequences split across
  reads, work (the emulator's parser). Marks on the alternate screen are ignored.
- Keys in a focused drawer: `alt+k` previous, `alt+j` next command prompt, scrolling it to the
  top row; repeated presses keep walking, `alt+j` past the last returns to live. Free of
  alt+s / alt+o (F-141 split), alt+n/w/r, alt+1..9. A full-screen program keeps the keys.
  (`alt+[` is the CSI prefix and `alt+.` is readline's yank-last-arg, so neither was used.)
- A prompt whose command exited non-zero shows a danger `✗` at the right edge of its row, live and
  in scrollback. `D` without a status is not a failure.
- New tabs (`alt+n`) and splits (`alt+s`) open in the folder OSC 7 last reported for the active
  tab. A report from another host (ssh) or a folder that no longer exists falls back to the tab's
  own folder.
- Send to terminal: `alt+enter` in visual mode, or `:send` (visual selection, else the cursor
  line) pastes into the active terminal tab (bracketed when the shell asked), opens and focuses
  the drawer, and adds no newline: the user reviews it and presses enter.

## Acceptance
- Parsers: `TestParseMark`, `TestParseCwd`; BEL/ST and 1..7-byte split reads:
  `TestMarksTerminatorsAndSplitReads`, `TestCwdFromOSC7`; no marks without integration:
  `TestMarksNotSeenWithoutShellIntegration`; rows stable under trimming:
  `TestMarkRowsSurviveScrollbackTrim`; alt screen: `TestMarksIgnoredOnAltScreen`.
- Jump: `TestJumpPromptWalksBackAndForward`, `TestJumpPromptStopsAtOldest`,
  `TestJumpPromptWithoutMarksSaysHow`.
- Gutter: `TestFailedCommandGetsGutterMark`, `TestFailedMarkFollowsScrollback`.
- cwd: `TestNewTabAndSplitOpenInReportedFolder`, `TestReportedFolderThatVanishedFallsBackToTabDir`.
- Send: `TestSendSelectionToTerminal`, `TestSendCommandWithoutSelectionSendsLine`,
  `TestSendRefusesWithoutLiveTerminal`.
