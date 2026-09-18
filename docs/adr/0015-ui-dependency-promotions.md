# ADR-0015: Promote chroma + go-runewidth to direct UI dependencies

Date: 2026-09-18 · Status: accepted · Serves: F-026 (UI beauty).
Bounds: ADR-0005 (hermetic, no system dependencies) unchanged.

## Context

F-026 needs two capabilities in `internal/tui`:

1. **Syntax highlighting in editor buffers.** The engine had no
   in-process highlighter; the editor renders buffers as plain fg text
   with a reverse-video cursor (M2 deferral). Glamour already pulls
   `alecthomas/chroma/v2` into the module graph as an *indirect*
   dependency for its fenced-code blocks.
2. **Display-cell width math.** `internal/ansi` + kit count runes, not
   display cells — CJK and wide glyphs mis-measure every clip/pad in
   the TUI. `mattn/go-runewidth` is already *indirect* (via x/ansi).

Promoting both to *direct* adds zero new code to the module graph:
the packages are already fetched, built, and vetted by the existing
dependency set. No alternative (in-house lexer, rune-width tables)
was considered — it would duplicate a mature library at the cost of
regressions exactly where this codebase has the least coverage.

## Decision

1. `internal/tui` may import `github.com/alecthomas/chroma/v2` —
   promoted to direct — for buffer token colorization only. Colors map
   through `internal/tui/theme` (chroma emits styles; theme maps its
   token kinds to DHI tokens so dark/light themes stay the single
   source of truth). No chroma imports outside the editor's renderer
   seam.
2. `github.com/mattn/go-runewidth` — promoted to direct — is the
   display-cell width function for `internal/ansi` and kit. Rune-count
   width math is deleted from kit.
3. Both stay pure Go, no system dependencies: ADR-0005 intact. No
   network: hermetic. License check: MIT (runewidth), MIT (chroma) —
   consistent with the MIT project decision.

## Consequences

- `go.mod` require blocks move both packages from `// indirect` to the
  direct block; no vendoring (plain Go build).
- The editor gains a highlighter seam (textbuf renderer) — highlighting
  is a render-time pass; textbuf state and keys are untouched.
- Theme lint (`TestNoRawColorsOutsideTheme`) extends to the chroma
  mapping: token colors never hardcode chroma style names at use
  sites.
- Kit's rune-width helpers die; go-runewidth's table-based measurement
  becomes the one width truth (ANSI-stripped strings only — measure
  after ansi.Strip).
