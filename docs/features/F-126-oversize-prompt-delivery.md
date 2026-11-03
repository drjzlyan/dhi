# F-126 — Say how an oversized prompt is delivered

Status: done · gap G-A4

Above 96 KiB (`clirun.MaxPromptArg`) the assembled system block and prompt cannot ride argv.
Adapters with a verified stdin path (`StdinOK`: claude, codex) get one tagged block on stdin, and
the system block loses its system-prompt slot. Adapters without one refuse the turn by name
(ADR-0011). Neither was visible afterwards.

## Decision
Record and report; do not trim. Trimming earlier would silently drop context the user budgeted
(`memory.prompt_tokens`, up to 400k tokens). The default budget (24k tokens, about 96k runes)
sits at the limit, so the stdin path is rare but real, and multibyte text reaches it sooner.

## Behaviour
- `tasks.Run.Delivery` is `"stdin"` when the prompt moved to stdin; empty for argv delivery.
- `dhi doctor` has a `runtime/<cli>/prompt-size` row per installed CLI: OK with the stdin
  behaviour, or Warn for a CLI without stdin delivery, naming the refusal and
  `memory.prompt_tokens`.

## Acceptance
- A run with a prompt over the limit has `Delivery == "stdin"`; a small one has `""`
  (`TestRunRecordsStdinDelivery`).
- Doctor's claude row names stdin (`runtimes_test.go`).
