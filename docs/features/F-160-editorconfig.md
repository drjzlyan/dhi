# F-160 — EditorConfig

Status: done · gap G-E21

Projects already ship `.editorconfig` (a standard format, ADR-0043). DHI ignored it, so Tab, tab
width and save-time whitespace disagreed with the team's rules.

## Behaviour
- Lookup: `.editorconfig` files from the file's folder up to its member root, stopping at
  `root = true`. Nearer files win, later sections win. Cached per folder; saving any
  `.editorconfig` in DHI drops the cache and re-derives open buffers.
- Globs: `*`, `**`, `?`, `[seq]`, `[!seq]`, `{a,b}`, `{n..m}`; no `/` in a glob means any depth.
- `indent_style`, `indent_size`, `tab_width` (`indent_size = tab` uses `tab_width`; `tab_width`
  defaults to a numeric `indent_size`). Indent precedence for Tab/Enter (`indentFor`, F-119):
  **.editorconfig > file's detected style > language > Settings**. Tab display width is per
  buffer: `tab_width` overrides `editor.tab_width` for that buffer.
- `end_of_line` (`lf`, `crlf`) applies only to a new (empty) file; existing endings are never
  rewritten (F-115). `cr` is ignored.
- `insert_final_newline = true` and `trim_trailing_whitespace = true` run in the before-save
  hook after format-on-save. `false` or unset changes nothing from today's byte-exact save.
- `charset`: ignored. Only utf-8 is supported; no transcoding.

## Acceptance
- Glob table, merge/root/member-stop, cache invalidation: `internal/editorconfig` tests
  (`TestMatch`, `TestLookupPrecedenceAndRoot`, `TestLookupStopsAtMemberRoot`, `TestInvalidateRereads`).
- Buffer hooks: `TestTrimAndFinalNewlineUndo`, `TestSetCRLFOnlyForNewFiles`.
- Surface: `TestEditorConfigIndentPrecedence`, `TestEditorConfigSaveRulesOnlyWhenTrue`,
  `TestEditorConfigEndOfLineOnlyForNewFile`, `TestEditorConfigSaveInvalidatesCache`.
