# STATE — current position

Updated: 2026-08-23 (session: M2 — markdown preview)

## Where we are

PTY drawer committed (`2be7941`), workspace-mgmt spec committed
(`d219511`). **This session (uncommitted — commit next): markdown
preview.** `make verify` green; 17 packages under `-race` (glamour added;
x/cellbuf bumped to v0.0.15 for x/ansi compat).

## Just finished

- `internal/preview`: glamour wrapper — GFM (tables/tasks/strike),
  dark style, per-width cached renderers, deterministic output,
  IsMarkdown extension check.
- Editor: ctrl+g toggles preview for the active buffer when the file is
  markdown; non-md shows "not a markdown file" in the command line.
  Content-hash cache re-renders only when buffer text or width changes
  (edit → preview updates live, F-002 criterion).
- Tests: render/GFM/determinism units; surface toggle + live-update +
  non-md hint; editor_preview golden.

## Gotchas learned (do not re-learn these)

1. glamour's old x/cellbuf pin breaks against newer x/ansi — after
   adding charm deps run `go get github.com/charmbracelet/x/cellbuf@latest`
   then `go mod tidy`.
2. Test fixtures must resolve member files via ws.Resolve(ParseVPath)
   — hardcoded repo-relative paths silently miss (beta lives outside
   repos/ in fixtures).
3. feed(m, "/", "e","n","t","e","r") types LETTERS into finders; enter
   must be its own keystroke. Whole words need typeKeys.
4. Carried: nil-channel pump hang (create chans in New); drainTerm in
   poll loops; ANSI-strip before measuring.

## Next up (rest of M2)

1. Commit this session (user approves diff first).
2. Git view over go-git (status/stage/commit/log/worktrees) behind a
   gitcore service seam; then LSP foundation; settings skeleton.

## Open questions for user

- None blocking.
