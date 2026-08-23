# F-002: Editor surface — the traditional IDE core

Status: done (M2, 2026-08-23) · Chat sidebar tracks M3 · LSP rich
features track M7

## Summary

One integrated IDE surface: file navigation, modal editor, multi-tab
terminal, lazygit-style git view, agent chat sidebar, and GitHub-style
preview — over the whole multi-repo workspace.

## Components

1. **File navigation pane** — left rail: workspace tree grouped by member
   repo (`repo-key:` prefixed paths), fuzzy find, cross-repo ripgrep search,
   open-in-editor. ✅ shipped
2. **Editor buffer** — modal (nvim-inspired): normal/insert/visual/command;
   motions/operators/registers; `:w :q :wq :e`; syntax highlighting; multiple
   buffers/tabs. ✅ shipped except syntax highlighting (M7 polish)
3. **Terminal drawer** — bottom panel, tabbed: one default tab per member
   repo (cwd pinned to that repo) + ability to open more tabs; PTY-backed
   with scrollback; DHI-managed toolchain PATH. ✅ shipped (ANSI-stripped
   scrollback MVP; VT emulation M7)
4. **Git view** — lazygit-inspired center tab: per-repo or whole-workspace
   status, hunk stage/unstage, commit, branch list, log; worktree ops
   (create/switch/remove) — deep-dive lives in Trees flows. ✅ status/
   stage/unstage/commit/log shipped via go-git; worktree ops → M5
5. **Chat sidebar** — right rail: conversation with any rostered agent;
   switchable mid-session; agent gets scoped tools (buffer, files, git);
   apply-suggestions directly into buffers. *(requires M3 runtime)*
6. **Preview** — toggle rendered view for supported formats (markdown first,
   GitHub-style rendering); diagram formats later via Ideator renderer reuse.
   ✅ markdown shipped (glamour)
7. **LSP** — language servers installable by user through DHI's managed
   toolchain (no system installs); foundation: install/wire/diagnostics +
   completion; hover/rename/refactor flow later. ✅ foundation shipped
   (client + gopls hermetic build path); hover/rename/refactor → M7

## Acceptance criteria

- ✅ Edit a file across two repos in one session; terminal tabs follow repo cwd.
- ✅ Stage/commit from git view without leaving editor surface.
- ✅ Markdown file renders in preview pane; edit→preview updates.
- ✅ All interactions covered by scripted-key component/e2e tests.
