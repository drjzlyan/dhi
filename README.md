# DHI

**The agentic workspace IDE — in your terminal.**

DHI is a TUI IDE where the human joins a *workspace* of AI agents as a
first-class member. Traditional IDE surfaces (editor, file tree, terminal,
git) meet an agent layer: pair-programming chat, a Slack-like task board,
ideation canvases, and a lazygit×GitHub-review×chat review screen.
Everything is **worktree-first**, **multi-repo aware**, and
**neovim-flavored**.

```
 ◆ DHI  1 Home › 2 Editor › 3 Files › 4 Term › 5 Trees › ...
 ██████╗ ██╗  ██╗██╗
 ██╔══██╗██║  ██║██║      the agentic workspace IDE
 ██║  ██║███████║██║
```

## Status

Foundation milestone **M0 complete** — see [ROADMAP.md](ROADMAP.md) and
[STATE.md](STATE.md). This is pre-alpha; surfaces beyond Home are scoped
placeholders.

## Run

```sh
go run ./cmd/dhi          # launch (requires Go 1.26+)
```

Keys: `1-9` switch workspace · `tab`/`shift+tab` cycle · `?` help · `ctrl+c` quit.

## Principles

- **Hermetic:** DHI manages its own toolchain (ADR-0005); no system installers.
- **Minimal CLI:** only `dhi` / `dhi doctor`; all work happens in-TUI (ADR-0004).
- **Tested UI:** golden snapshots + scripted key tests; no manual QA required (docs/testing.md).
- **Session-resumable knowledge:** ROADMAP/STATE/features/ADRs updated every session (AGENTS.md).

## Docs

| Doc | Content |
|---|---|
| [docs/product.md](docs/product.md) | vision, personas, journeys |
| [docs/architecture.md](docs/architecture.md) | module map, dependency rules |
| [docs/testing.md](docs/testing.md) | test pyramid, golden workflow |
| [docs/adr/](docs/adr/) | decision records |
| [AGENTS.md](AGENTS.md) | contributor conventions |

## License

TBD (pre-release).
