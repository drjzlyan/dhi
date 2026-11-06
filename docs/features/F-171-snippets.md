# F-171 — Snippets

Status: done · gap G-E18

LSP snippet items (`insertTextFormat: 2`) were flattened to plain text (`Printf(format)`), the client
did not advertise `snippetSupport`, and there was no tab-stop navigation or user snippets.

## Decision
Standard formats only (ADR-0043): user snippets are VS Code `<repo>/.vscode/*.code-snippets`
(JSON with comments; `prefix`, `body`, `description`, `scope`). No DHI snippet format.
`${name/regex/format/}` transforms are not supported: the transform is dropped, the value is used.

## Behaviour
- Grammar (`textbuf.ExpandSnippet`): `$1`, `${1}`, `${1:default}`, `${1|a,b|}` (first choice),
  `$0`, nested placeholders, `\$ \} \\` escapes, `$NAME`/`${NAME:default}` variables
  (TM_FILENAME, TM_FILENAME_BASE, TM_DIRECTORY, TM_FILEPATH, RELATIVE_FILEPATH, WORKSPACE_NAME,
  TM_LINE_INDEX/NUMBER, TM_CURRENT_LINE/WORD, TM_SELECTED_TEXT, CURRENT_YEAR..SECOND). Unknown
  variable: its default, else its name. A bare `$1` mirrors the first `${1:text}`. Continuation
  lines take the indent of the line the snippet lands on.
- `lsp` advertises `snippetSupport: true`; items carry `Snippet` and the raw syntax.
- After expansion a session starts on the first stop; the placeholder acts as selected (typing or
  backspace replaces it). `tab` / `shift+tab` move between stops; mirrored spans update as you
  type; `esc`, reaching `$0`, or moving the cursor out of the stop ends it.
- Insert-mode `tab` precedence: completion popup open (accept) > live snippet session (jump) >
  indent.
- User snippets are offered in the completion menu by prefix (case-insensitive), filtered by
  `scope` against the file's language id, with or without a language server. A snippet file that
  fails to parse is named on the status line.

## Acceptance
- Grammar table, indent, session tabs/mirrors/nesting/end rules: `TestExpandSnippetTable`,
  `TestExpandSnippetIndentsContinuationLines`, `TestSnippetSession*`, `TestSnippetMirrorsUpdateTogether`,
  `TestSnippetNestedAndGrowth` (`internal/textbuf`).
- `.code-snippets` parsing and scope: `internal/vscsnippets` tests.
- Fake-server snippet item expands and tabs, `snippetSupport` advertised, tab precedence, user
  snippets with mirrors, no-server and bad-file cases: `TestLSPSnippetCompletionExpandsAndTabs`,
  `TestSnippetTabPrecedence`, `TestUserSnippetsFromVSCodeFile`,
  `TestUserSnippetsWithoutServerAndBadFile` (`internal/tui/surfaces/editor`).
