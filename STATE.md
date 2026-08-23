# STATE — current position

Updated: 2026-08-23 (session: M2 started — editor nav tree + fuzzy find)

## Where we are

M1 fully committed (latest `4a2d2e5`, pins + ADR-0008). **This session
(uncommitted — commit next): M2 first slice** — Editor surface with
multi-repo nav tree, fuzzy find overlay, and the `internal/fuzzy` matcher.
`make verify` green; 13 packages pass under `-race`; three editor goldens.

## Just finished

- `internal/fuzzy`: case-insensitive subsequence match + scoring
  (contiguity > boundaries > camel humps; leading-gap penalty), stable
  ranking. Generic — reusable by Ideator/Workspace later.
- `surfaces/editor` component 1 (F-002):
  - tree grouped by member repo; lazy per-dir loading; dirs-first sort;
    depth-based indentation; `.git`/`node_modules`/dot-dirs pruned.
  - keys: j/k/g/G move · enter/l expand-or-open · h collapse · `/` finder.
  - finder: indexes all member files as vpaths (cap 20k), live filter,
    enter opens + reveals path in tree with cursor on the file.
  - opened file shows vpath in main pane (placeholder until buffers).
  - nil workspace → centered empty-state hint.
- `cmd/dhi`: Editor replaces its placeholder; loads `internal/workspace`
  from cwd when it is a DHI workspace.
- Goldens: collapsed tree / open-file / find overlay. Opened-file golden
  deliberately excludes absolute paths (portable across machines).

## Gotchas learned (do not re-learn these)

1. **Map iteration order leaked into UI**: workspace.Load appended
   members from a map — alpha/beta flipped between runs and broke tests +
   goldens. Fixed by sorting names at load. Any new map→slice must sort.
2. joinH measured widths on raw ANSI strings → huge gaps. Always measure
   `ansi.Strip`ped text (same gotcha as centering).
3. kit.Panel Width/Height are FIELDS — no method chaining after SetContent.
4. Dirs-first sorting means test key sequences go down→dir before files.
5. Golden files must never embed absolute temp paths or machine-specific
   data; assert those via Contains instead.

## Next up (rest of M2)

1. Commit this session (user approves diff first).
2. ripgrep fan-out search in editor (spawn via Manager.Env shim PATH,
   stream results into a results list; sandbox Guard for cwd scoping).
3. Modal buffer MVP: normal/insert modes, motions, d/c/y, :w :q :wq :e.
4. Then terminal drawer (PTY), markdown preview, git view via go-git,
   LSP foundation, settings skeleton — see ROADMAP M2.

## Open questions for user

- None blocking.
