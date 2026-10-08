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

## Install

```sh
curl -fsSL https://github.com/drjzlyan/dhi/releases/latest/download/install.sh | sh
```

The installer picks the build for your machine, **verifies its SHA-256**
(and its cosign signature when `cosign` is installed), and puts `dhi` in
`~/.local/bin` — no sudo. Pin a version with `DHI_VERSION=0.2.0`, or choose
the directory with `DHI_INSTALL_DIR`. Homebrew users: `dhi.rb` is attached to
every release (and published to a tap when one is configured).

Supported today: **macOS on Apple silicon** and **Linux x86_64** — exactly the
platforms DHI can pin a complete hermetic toolchain for. Others are refused at
install time with a named reason, not a broken first run.

## First run

```sh
cd your-project && dhi
```

1. **Toolchain** — DHI shows what it will download (about 157 MB: Go, Node, uv,
   ripgrep, git, gh), where it goes, and asks. Everything lands under
   `~/.local/share/dhi`; delete that folder to uninstall.
2. **Setup wizard** — workspace, git identity, team conventions (commit format,
   branch names, co-author, copyright). Skip anything; re-run any time from the
   command palette (`ctrl+p` → *Run setup wizard*).

`dhi version` prints the build identity; `dhi doctor` diagnoses an install.

## From source

```sh
go run ./cmd/dhi          # requires Go 1.26+
make verify               # fmt + vet + build + race tests
```

Keys: `1-9` switch views · `tab`/`shift+tab` cycle · `ctrl+p` palette · `?` help · `ctrl+c` quit.

## Status

Milestones M0–M24 are complete; see [ROADMAP.md](ROADMAP.md) (M25 is the
install → setup → first-use programme) and [STATE.md](STATE.md).

## Principles

- **Hermetic:** DHI manages its own toolchain (ADR-0005); no system installers.
- **Minimal CLI:** only `dhi` / `dhi doctor` / `dhi version`; all work happens in-TUI (ADR-0004).
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

MIT — see [LICENSE](LICENSE).
