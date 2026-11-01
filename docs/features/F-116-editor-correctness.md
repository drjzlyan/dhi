# F-116 — Editor correctness and shell robustness

Status: done (M37 P1) · amends F-009 (LSP), F-040, F-050, F-084

These items come from the session-63 audit, items 4–11 and 21–25.

## Behaviour
- **Search lands on the match.** Enter on a search result opens the file at the hit's line and
  column (rg's byte column converted to the editor's character column). Before this fix it
  opened at line 1.
- **Completion**
  - The popup opens on its own while you type: after a letter, digit, `_` or `.` in insert
    mode, once typing pauses for 150 ms. ctrl+space still opens it on demand.
  - Accepting with enter or tab replaces what you typed of the word, or the range the server
    names. Before, it appended the whole label, so `Pri` became `PriPrintln`.
  - The server's `textEdit` and `insertText` are used, and snippet placeholders are turned into
    plain text.
  - j and k type normally. Move in the list with ↑/↓ or ctrl+n/ctrl+p.
- **Positions use UTF-16 columns, as LSP specifies.** `lsp.UTF16Col` and `lsp.RuneCol` convert
  every position DHI sends or receives: completion, hover, rename, code actions, edits,
  definitions and problems.
- **Rename across files**
  - A rename also changes files that aren't open: DHI applies the edits to the file on disk and
    saves it through textbuf's byte-exact save.
  - The message lists those files, or what could not be changed.
  - A failed rename says so instead of doing nothing silently.
- **Go to definition and references**
  - `gd` jumps to the definition. If there are several, they open as a list.
  - `gR` lists references in the results pane. Enter jumps to one; `r` (replace) is off for
    these lists.
  - The hint row now reads `gd definition · gR references · K hover · ]d problem · gr rename ·
    ga actions`. It used to call `gr` "refs".
- **Problems**
  - The cursor line's problem shows on the command line (✗ error, ⚠ warning).
  - `]d` and `[d` jump to the next and previous problem, wrapping around.
  - `:diag` (or `:problems`) lists every reported problem.
- **Code actions** apply to the whole cursor line. `workspace/executeCommand` is now sent as a
  request, so a server error reaches the user.
- **Language servers start off the UI loop.**
  - A missing server is reported right away. One that is installed starts in the background,
    and the files that waited for it are opened once it is up.
  - After 10 s DHI says the server is slow to start.
  - Each client gets exactly one event pump; before, a new one started for every file opened.
  - The agents' LSP tool uses the server for the file it names and no longer names gopls in
    every error.
- **Settings now reach the editor.** `editor.tab_width`, `editor.line_numbers` and
  `terminal.scrollback` used to do nothing.
  - They are read through `settings.Live`, which re-reads the config only when its files change.
  - Tab width and line numbers apply at the next frame. Scrollback applies to new terminal tabs.
- **Palette "Save file"** saves directly. Before, it typed `:w` into the buffer when in insert
  mode.
- **Format-on-save waits at most 1 s for a language server or 2 s for an external formatter**,
  as VS Code does. After that the file saves unformatted, with a note. An explicit `:fmt` still
  waits 3 s or 10 s.
- **Ideator preview** is cached by size and modification time, not by reading and hashing the
  file every frame. Files over 1 MB are not previewed, and the cache is capped at 32 entries.
- **Shell robustness**
  - If `.dhi/org.toml` fails to load, Settings still loads everything else and names the error,
    both in the TEAMS section and in a flash.
  - Errors are no longer swallowed in these places:
    - "Save everywhere" that leaves this workspace's own value in effect
    - recording a PR on its card
    - moving a card when you take it
    - saving setup progress (the error shows on the wizard card)
    - saving tutorial progress (shown in a toast)
  - The palette offers every theme (dhi-dusk, dhi-dawn, high-contrast). Picking one saves it the
    same way the Settings row does.
  - Settings booleans read "✓ on" / "○ off".
  - `dhi --help`, `-h` and `help` print the usage and exit 0.
  - A build without ldflags reports its module version and VCS stamp, or `0.1.0-dev`.
  - `ctrl+q` is documented in the help overlay and the README.
- Tests that skipped when their fixture had no data now fail instead. Two of them had been
  passing without testing anything; they now get real fixtures.

## Acceptance
- [x] Search enter lands on the hit's line and column (`search_test.go`).
- [x] Completion replaces the typed prefix; completion decoding and snippets are covered;
      UTF-16 conversions are tested on an emoji line.
- [x] `gd` jumps and `gR` lists; a rename changes an unopened file on disk; the problem message
      shows; `]d` and `:diag` work (`lspnav_test.go`).
- [x] A missing server is reported once per language. A server starts asynchronously, and its
      waiting buffers are opened.
- [x] Tab width and line numbers follow the settings; `settings.Live` picks up changes and keeps
      the last valid config.
- [x] Palette Save works from insert mode.
- [x] `make verify` passes.

## Notes
- `workspace/applyEdit` is still accepted with `applied: true` before the UI applies it. The
  answer is now accurate, because edits to unopened files are applied too.
- Live LSP checks (`gd`, rename across files, auto-completion) still need gopls on this machine.
  They are covered by the scripted fake server.
