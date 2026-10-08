# Contributing to DHI

Thanks for helping. Bug reports, ideas, docs fixes and pull requests are all
welcome.

## Before you start

- **Bugs and ideas:** open an [issue](https://github.com/drjzlyan/dhi/issues/new/choose)
  using a template. Include the output of `dhi version` and `dhi doctor` for
  bugs.
- **Larger changes:** open an issue first, so we can agree on the approach
  before you write the code.
- **Security problems:** do not open a public issue. See
  [SECURITY.md](SECURITY.md).

## Set up

DHI builds with plain Go. Nothing else is needed for the code and the tests.

```sh
git clone https://github.com/drjzlyan/dhi && cd dhi
go run ./cmd/dhi          # run it (Go 1.26+)
make test                 # all tests
make verify               # what CI checks: gofmt, vet, build, race tests
make lint                 # golangci-lint (same version as CI)
```

[AGENTS.md](AGENTS.md) explains where things live and the rules every change
follows. These are the ones that matter most:

- **Tests are the contract.** Every change comes with tests. A visual change
  regenerates the golden snapshots on purpose
  (`DHI_UPDATE_GOLDENS=1 make test`); review the diff like code.
- **Every screen fits every window.** The layout contract
  (`internal/tui/layoutcontract`) renders each view from 40 to 200 columns.
- **Colours live in the theme.** Never use a raw `lipgloss.Color` outside
  `internal/tui/theme`; a test enforces it.
- **No system tools.** DHI runs on its own pinned toolchain (ADR-0005).
- **Features get a spec.** A new feature has a `docs/features/F-###-*.md`
  with acceptance criteria. A design decision gets an ADR in `docs/adr/`.

## Screenshots

`scripts/screenshots.sh` rebuilds the README images from a seeded demo
workspace (`scripts/demo`). Run it when a change affects what the README
shows. It needs `tmux`, `python3` and an installed DHI toolchain.

## Pull requests

- Keep each PR to one topic, and explain the why in the description.
- `make verify` and `make lint` must pass; CI runs the same on macOS and
  Linux.
- Update `ROADMAP.md`, `STATE.md` and the feature spec when your change
  moves them.

By contributing you agree that your work is licensed under the
[MIT License](LICENSE) and that you follow the
[Code of Conduct](CODE_OF_CONDUCT.md).
