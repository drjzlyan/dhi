# DHI — Architecture

## Layer map (target state)

```
cmd/dhi ──────────────► composition root: builds services, injects into TUI
   │
internal/
  tui/            ◄─ depends only on service interfaces + domain
    theme/          design tokens; the ONLY place colors are constructed
    kit/            reusable primitives (Panel/Tabs/StatusLine/List/…)
    app/            shell: registry, router, global keys, overlays
    surfaces/       Surface contract + home/editor/files/term/trees/…
  runtime/          agent orchestration: provider adapters, tools, MCP, bus
  agentkit/         agent+skill manifest spec, load/validate/package
  gitcore/          Git interface + CLI-backed impl (+fake) — worktree-first
  workspace/        multi-repo model, .dhi/ management, VPath resolver
  toolchain/        hermetic tool manager (M1): registry, verify, activate
  sandbox/          path-jail + permission policies (OS-sandbox seam later)
  marketplace/      pack install/index (M6)
  doctor/           environment check suite shared by CLI + health panel
  ansi/ testutil/   shared utilities
```

## Dependency rules (enforced by review; lint rules as layers land)

1. `domain` types import nothing internal.
2. Services never import `tui`.
3. `tui` never imports concrete services — only interfaces, wired in `cmd`.
4. `lipgloss.Color(` may appear **only** inside `internal/tui/theme`
   (enforced by `theme/theme_test.go` today).

## Shell ↔ Surface contract

`surfaces.Surface` = `Meta / Init / Resize / Update(msg) / HandleKey(key) bool / View()`.
The shell owns: tab bar, global keys (`1-9`, `tab`, `?`, `ctrl+c`), help
overlay, statusline. Surfaces own everything below their body area and must
render deterministically from `(state, width, height)`.

Key-routing order: **global bindings → active surface `HandleKey`**.
Non-key messages go to the active surface's `Update`; resizes broadcast to all.

## Rendering & testing stance

- Components render via explicit cell math where exactness matters (Panel),
  lipgloss styles otherwise. No component reads wall-clock time; animation
  drivers (M1) advance on injected tick messages so tests stay deterministic.
- Golden snapshots are ANSI-stripped text under each package's
  `testdata/goldens/`; regenerate with `DHI_UPDATE_GOLDENS=1`.

## Multi-repo model (lands M1)

- `workspace.Repo{Key, Path, DefaultBranch}`; keys unique per workspace.
- `VPath = repo:key + "/" + relpath`; single resolver maps to absolute paths.
- `.dhi/workspace.toml` is the source of truth; discovery walks up to the
  nearest `.dhi/`, else singleton-wraps a bare repo.

## Agent system shape (lands M3+)

- Agent = manifest(skills[], knowledge[], mcp[], tools allowlist, model).
- Runtime turn loop: context build → provider stream → tool exec (sandboxed)
  → post message; scripted MockProvider drives all tests offline.
- Memory: private per-agent journal+notes; shared KB with rg-based retrieval
  behind a `KnowledgeStore` interface (embedding impl can replace later).

## Isolation story

- Installs: XDG-prefixed toolchain managed solely by DHI (ADR-0005).
- Execution: path-jail restricts fs/shell ops to registered repos;
  deny-by-default dangerous ops; OS-level sandbox adapters reserved (ADR-0006).
