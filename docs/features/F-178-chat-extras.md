# F-178 — Chat extras: skills as commands, /export, image attachments

Status: done · gap G-X3

Three independent additions to the workspace DM/channel composer and the editor chat. All of the
parsing sits in `internal/agentkit/skills` (`slash.go`) and `internal/agentkit/runtime`
(`chatextras.go`); the composers are thin.

## 1. User-invocable skills as `/<skill> [args]`
- Field names are Claude Code's (ADR-0043, no DHI-only field): `user-invocable` and
  `disable-model-invocation`. Claude Code lists every skill unless `user-invocable: false`; DHI
  offers a skill only when it declares itself (`user-invocable: true`, or `disable-model-invocation: true`
  without `user-invocable: false`, since only the user can run it), so a library of reference skills
  does not flood the completion. The fields survive a Settings edit (`Render`).
- Completion: `tab` on a leading `/word` completes built-ins and skills; the palette lists
  `Skill /name` rows (workspace channels, editor) that fill the composer.
- Built-ins win: `model fork new compact plan review ask yes no export` are reserved
  (`skills.Builtins`). A skill of that name is never offered or run; completion, and the palette, say
  "built-in /plan wins over the skill of that name".
- A `/word` that is neither a built-in nor a declared skill is refused by name in the composer (it is
  not posted as prose); the text stays. A skill that does not declare itself says so.
- Running: the message posts as typed; for a human trigger the runtime replaces the turn text with the
  `skill_activate` block (`skills.Activation`) plus the arguments. Any library skill that declares
  itself works (the human chose it by name), not only the agent's assigned ones. An agent's message
  never activates a skill.

## 2. `/export [md|json]`
- `Runtime.ExportConversation` writes `<workspace>/.dhi/exports/<channel>-t<thread>-<UTC>.<fmt>`
  (mode 0600; write site recorded) and the composer replies with the path.
- Pipeline: `cfg.Redact` first; then text from any non-human author is wrapped in the F-122 untrusted
  fence. The header has agent, CLI, model (and whether it is the agent default or the `/model`
  override), start/last-activity dates, and the episode's turns, tokens and cost.
- Workspace pane: needs an open thread in a DM (like `/fork`). Editor chat: the channel's top level
  (tokens and cost are per thread, so the header says so).

## 3. Image attachments: `@image:<path>`
- Chosen over `/image <path>`: the mention travels in the message text through the bus, forks and
  replays like `@file`, needs no composer state, and works in every composer. Paths with spaces:
  `@image:"my dir/a.png"`.
- Capability `ImagesOK` (+ `ImagesFlag`, `ImagesWhy`) per adapter. Verified from the docs: claude
  opens an image given by path with its Read tool (path named in the prompt); `codex exec -i/--image`
  and `exec resume --image` (one flag per file). cursor-agent, copilot, antigravity and opencode have
  no verified headless image input: a turn with an image is refused by name with the reason
  (ADR-0011), nothing is started.
- Only a human-authored trigger attaches. Relative paths must stay inside the workspace; an absolute
  or `~` path is the user's explicit choice. Refused: not png/jpg/gif/webp, missing, over 20 MiB, a
  comma in the path (codex splits on it), or inside a credential folder. The file is made readable to
  the wrapped CLI (`WithExtraReadOnly`). DHI uploads nothing; the CLI the user already runs gets the path.

## Acceptance
- `TestSlashDeclaration`, `TestSlashFieldsRoundTripThroughWrite`,
  `TestInvocationBuiltinsWinAndOnlyDeclaredSkillsRun`, `TestPromptCarriesTheActivationAndArgs` (skills).
- `TestUserInvocableSkillRunsActivated`, `TestSkillWithoutDeclarationOrFromAnAgentIsNotActivated` (runtime).
- `TestBuiltinSetCoversEveryComposerParser`, `TestSlashTabCompletesSkillsAndBuiltinsWin`,
  `TestSlashSkillPostsAndUnknownIsRefusedByName` (workspace); `TestChatSlashSkillCompletionAndRefusal` (editor).
- `TestExportWritesRedactedFencedMarkdownAndJSON`, `TestExportUsesConversationModelOverride`,
  `TestExportCommand`, `TestChatExportWritesUnderDhiExports`.
- `TestEveryAdapterWithoutImagesSaysWhy`, `TestCodexImagesAreRepeatedFlagsBeforeAnyPositional`,
  `TestOnlyClaudeAndCodexTakeImages`, `TestImageMentionParsing`, `TestImageAttachesForClaudeByPath`,
  `TestImageRefusedByNameWhereTheCLIHasNone`, `TestImagePathRules`.
