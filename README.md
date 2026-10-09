<div align="center">

# ◆ DHI

**The agentic workspace IDE — in your terminal.**

A keyboard-first IDE where you and a crew of AI agents share one workspace:
a board, channels, an editor, a review screen, all in one terminal window.

[![CI](https://github.com/drjzlyan/dhi/actions/workflows/ci.yml/badge.svg)](https://github.com/drjzlyan/dhi/actions/workflows/ci.yml)
[![Release](https://img.shields.io/github/v/release/drjzlyan/dhi?sort=semver&display_name=tag)](https://github.com/drjzlyan/dhi/releases)
[![Go](https://img.shields.io/github/go-mod/go-version/drjzlyan/dhi)](go.mod)
[![License: MIT](https://img.shields.io/badge/license-MIT-blue.svg)](LICENSE)

<img src="docs/assets/review.png" alt="DHI's review screen: changed files, a syntax-highlighted diff, and the review conversation side by side" width="100%">

</div>

## Why DHI

You ask agents for work in one window, review what they did in another, and
track it all somewhere else. DHI puts it in one place, inside the editor you
already work in:

- **A team, not a chat box.** Agents have names, roles, skills and memory.
  Assign them cards on a board, @-mention them in channels, or pair with
  one in a buffer.
- **Review that feels like a real review.** Every change lands in its own
  git worktree. You get a three-column diff, word-level highlights, agent
  suggestions you accept or dismiss, and one GitHub review sent as you.
- **Many repos, one workspace.** Navigate, search and replace across every
  member repo at once; changes that span services stay linked.
- **Your keyboard, your rules.** Vim-style editing, a command palette for
  everything, and every key shown where you need it. The mouse works too.
- **Nothing to install around it.** DHI brings its own pinned Go, Node, git,
  ripgrep and GitHub CLI under one folder. No sudo, and your system
  toolchain is never touched.
- **Fits any window.** Every screen adapts from a narrow split to an
  ultrawide monitor and is tested at each size.

## A look around

| Board and agents | Channels |
|---|---|
| <img src="docs/assets/board.png" alt="Kanban board with a selected task's details"> | <img src="docs/assets/channels.png" alt="The #general channel with messages from agents"> |
| **Editor: split panes** | **Project-wide regex replace, previewed** |
| <img src="docs/assets/editor.png" alt="Two files side by side in split panes"> | <img src="docs/assets/replace.png" alt="Preview of a regex replace across files"> |
| **Help that follows you** | **Narrow terminals get the full UI** |
| <img src="docs/assets/help.png" alt="Searchable keyboard help over the board"> | <img src="docs/assets/narrow.png" alt="The workspace at 72 columns"> |

## Install

```sh
curl -fsSL https://github.com/drjzlyan/dhi/releases/latest/download/install.sh | sh
```

The installer picks the build for your machine, **verifies its SHA-256** (and
its cosign signature when `cosign` is installed), and puts `dhi` in
`~/.local/bin` without sudo.

- Pin a version with `DHI_VERSION=0.1.0`.
- Choose the folder with `DHI_INSTALL_DIR`.
- Homebrew: a `dhi.rb` formula is attached to every release.

**Platforms:** macOS on Apple silicon and Linux x86_64, which are the
platforms DHI can pin a complete toolchain for. Other platforms are refused
at install time with the reason, rather than failing on first run.

<details>
<summary>Build from source</summary>

```sh
git clone https://github.com/drjzlyan/dhi && cd dhi
go run ./cmd/dhi      # Go 1.26+
make verify           # fmt + vet + race tests
```
</details>

## First run

```sh
cd your-project && dhi
```

1. **Toolchain.** DHI lists what it will download (about 157 MB: Go, Node,
   uv, ripgrep, git, gh) and where, then asks before installing. Everything
   lands in `~/.local/share/dhi`; delete that folder to uninstall.
2. **Setup.** A short wizard covers:
   - the workspace;
   - your git identity;
   - team conventions (commit format, branch names, co-author);
   - which coding agent to use;
   - a starter team.

   Skip anything; re-run it any time from `ctrl+p` → *Run setup wizard*.
3. **Learn by doing.** `ctrl+p` → *Tutorial* runs short lessons under the
   live UI; each step waits for you to do the real action.

Agents run on the coding CLI you already use: **Claude Code**, **Codex**,
**Cursor Agent**, **GitHub Copilot CLI**, **opencode** or **Antigravity**.
DHI detects it, installs most of them for you after asking, and keeps every
agent inside the workspace's tools, scopes and approvals.

## Keys

| Key | Does |
|---|---|
| `1`–`5`, `tab` | switch views: Workspace · Editor · Ideator · Reviewer · Settings |
| `[` `]` | switch sections inside a view |
| `ctrl+p` | command palette: search every action |
| `?` | help for where you are; `/` searches it |
| `n` | new task / session / review, depending on the view |
| `s`, `ctrl+r` | search the workspace; toggle regex |
| `ctrl+w v` | split the editor |
| `ctrl+t` | terminal drawer — vim, htop and friends run full screen |
| `ctrl+c` | quit |

Remap any key in `config.toml` (`[keys] "ctrl+k" = "ctrl+p"`); hints and help follow your keys.
`dhi version` prints the build; `dhi doctor` diagnoses an install.

## Docs

| | |
|---|---|
| [Product overview](docs/product.md) | vision, personas, the five views |
| [Architecture](docs/architecture.md) | modules and dependency rules |
| [Testing](docs/testing.md) | test pyramid, golden snapshots, the layout contract |
| [Decisions](docs/adr/) | architecture decision records |
| [Features](docs/features/) | one spec per feature, with acceptance criteria |
| [Roadmap](ROADMAP.md) | milestones, done and planned |

## Contributing

Bug reports, ideas and pull requests are welcome. See
[CONTRIBUTING.md](CONTRIBUTING.md) to get set up (`make verify` is the bar),
and please read the [Code of Conduct](CODE_OF_CONDUCT.md). To report a
security issue privately, follow [SECURITY.md](SECURITY.md).

## License

[MIT](LICENSE)
