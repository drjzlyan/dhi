# AGENTS.md — conventions for humans & AI agents working on DHI

Read this before changing anything. It keeps multi-session work coherent.

## Where things live

| Path | Purpose |
|---|---|
| `cmd/dhi/` | entrypoint only (no logic; minimal CLI per ADR-0004) |
| `internal/tui/theme/` | ALL colors/metrics/glyphs. Raw `lipgloss.Color(` outside this package fails tests |
| `internal/tui/kit/` | reusable primitives; deterministic render; no app knowledge |
| `internal/tui/app/` | shell: routing, global keys, help overlay |
| `internal/tui/surfaces/` | `Surface` contract + concrete surfaces |
| `internal/testutil/golden/` | snapshot harness (ANSI-stripped goldens) |
| `docs/adr/` | decision records — append-only; supersede, never edit history |

Dependency rule: `domain` (future `internal/domain`) imports nothing;
services (`runtime/gitcore/workspace/toolchain/...`) never import `tui`;
`tui` depends on service *interfaces* injected from `cmd`.

## Workflow rules

1. **Resuming work:** read `STATE.md` → then `ROADMAP.md`. Update both at the
   end of your session (STATE = exact position + gotchas; ROADMAP = checkbox).
2. **Features:** each milestone item gets a `docs/features/F-###-slug.md`
   spec with acceptance criteria before implementation; flip its status when done.
3. **Tests are the contract:** every component change ships with unit tests;
   every visual change regenerates goldens deliberately:
   `DHI_UPDATE_GOLDENS=1 go test ./...` — review diffs like code.
4. **Verify before declaring done:** `make verify` (fmt+vet+build+test+race).
5. **No system dependencies:** DHI must build with plain Go and run hermetic
   (ADR-0005); don't add code that shells out to unmanaged tools outside
   `internal/toolchain` seams.

## Commands

```
make test            # all tests
make verify          # fmt check + vet + build + race tests
DHI_UPDATE_GOLDENS=1 make test   # regenerate snapshots
go run ./cmd/dhi     # launch IDE locally
```
