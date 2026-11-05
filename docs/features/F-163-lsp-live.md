# F-163 — LSP features verified live against real gopls

Status: done · gap G-LSP-live

The LSP features were only covered by a scripted fake server. `TestLiveGopls`
(`internal/tui/surfaces/editor/lsp_live_test.go`) drives the editor's real key
paths against a real gopls on PATH: a temp Go module (go.mod, a.go declaring
`Add`, b.go using it, a scratch d.go, sub/c.go and sub/e.go with errors).
The editor starts gopls itself through the Manager (lazy start, F-116). The
test is skipped unless `DHI_LIVE_LSP=1` and `gopls` is on PATH. No trust gate
is configured (New without WithTrust trusts everything), so restricted mode
(F-148) does not intervene.

Run: `DHI_LIVE_LSP=1 go test -run LiveGopls -v ./internal/tui/surfaces/editor`

## Result (gopls v0.24.0)

| Feature | Result |
|---|---|
| open file, diagnostics arrive ("declared and not used: x") | pass |
| code actions `ga` (F-158 absent; existing flow): "Add import" quick fix applied | pass |
| hover `K` (signature and doc) | pass |
| `gd` across files (b.go to a.go, exact position) | pass |
| `gR` references (a.go and b.go) | pass |
| workspace symbols `:wsym Add` (F-154) | pass |
| completion (ctrl+space, accept) | pass |
| signature help (F-128), active parameter moves on "," | pass |
| format-on-save through gopls (`:w` rewrites `var z   =   1`) | pass |
| rename `gr` edits the unopened a.go on disk and the open b.go (F-116) | pass |
| inlay hints | pass (re-run 2026-11-05, gopls v0.24.0) |
| call hierarchy (F-173) | pass (re-run 2026-11-05, gopls v0.24.0) |
| semantic tokens (F-173) | pass (re-run 2026-11-05, gopls v0.24.0) |
| breadcrumbs and sticky scroll (F-175) | pass (re-run 2026-11-05, gopls v0.24.0) |
| didClose (F-132; no didSave step) | pass (no error, server stays healthy; gopls does not expose receipt) |

No DHI bug was found, so no code fix and no new fake-server test were needed.

## Notes

- Esc closes an open signature popup before leaving insert mode (two presses);
  the test tolerates this.
- A file that is open in a tab is renamed in its buffer, not on disk; the test
  closes a.go first to exercise the unopened path.

## Acceptance

- `TestLiveGopls` and its subtests (diagnostics, code actions, hover, gd across files, gR references,
  workspace symbols, inlay hints, call hierarchy, semantic tokens, breadcrumbs and sticky, completion,
  signature help, didClose, format on save, rename across files) are in the test file; the first rows above passed against gopls v0.24.0.
