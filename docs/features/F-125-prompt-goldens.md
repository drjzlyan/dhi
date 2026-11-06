# F-125 — Golden snapshots of the assembled prompt

Status: done · gap G-A3

The prompt DHI hands a CLI is built from `behavior.Compose` plus the runtime's own sections
(`cliPromptAt`). Tests asserted fragments of it, so a reworded rule, a moved section or a lost
section passed unnoticed. The whole assembled prompt is now a reviewed golden.

## Behaviour
- `TestPromptGoldens` (`internal/agentkit/runtime/promptgolden_test.go`) renders system block and
  prompt for a fixture with identity, grounding, voice, standards, a member's `AGENTS.md`, a
  workflow and thread history.
- `prompt_fresh` is the level-0 prompt; `prompt_compacted` is the level-1 retry after a context
  overflow.
- Every adapter in the registry must assemble the same prompt. One that differs legitimately gets
  its own `prompt_fresh_<engine>` golden, created deliberately. Today none differs (all six
  adapters have `ReadsAgentsMD`, and the fixture's cwd is the workspace root).
- Regenerate with `DHI_UPDATE_GOLDENS=1 go test ./internal/agentkit/runtime`; review the diff
  like code (AGENTS.md rule 3).

## Acceptance
- Changing any rule text, section order or section tag fails `TestPromptGoldens`.
- Output is deterministic (no temp paths, ids or times); stable under `-count=3`.

## Not covered
- Memory, knowledge, lessons and episode sections (they depend on stores the harness leaves
  empty). Prompt delivery above 96 KiB is G-A4.
