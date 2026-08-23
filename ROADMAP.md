# DHI ROADMAP

Status legend: `[ ]` planned · `[~]` in progress · `[x]` done.
Update this file **and** [STATE.md](STATE.md) at the end of every session.

---

## M0 — Skeleton & design system ✅ (2026-08-23)

- [x] Repo scaffold `github.com/drjzlyan/dhi`, Go 1.26, Bubble Tea/Lip Gloss v2 (`charm.land`)
- [x] Theme token system (`internal/tui/theme`) + raw-color lint enforcement test
- [x] Kit primitives: Panel, Tabs, StatusLine, List (`internal/tui/kit`)
- [x] App shell: surface registry, router, global keys, help overlay, focus seam (`internal/tui/app`)
- [x] Surfaces: Home dashboard + placeholders for Editor/Files/Term/Trees/Agents/Tasks/Review/Market
- [x] Golden-file snapshot harness (`internal/testutil/golden`, ANSI-stripped, `DHI_UPDATE_GOLDENS=1`)
- [x] Unit + component tests green; goldens locked
- [x] Docs: README, product/architecture/testing, ADRs 0001–0007, feature F-001
- [x] CI (vet + race tests + golangci-lint), Makefile, `.gitignore`

## M1 — Hermetic toolchain & bootstrap

- [ ] `toolchain.Manager`: resolve → download → sha256 verify → extract → activate → lockfile
- [ ] Registry manifest (pinned URLs per platform: git, ripgrep, node, uv)
- [ ] XDG-isolated prefix `~/.local/share/dhi` (+ cache/config dirs); shims PATH for child processes only
- [ ] Animated Bootstrap surface (event-driven; deterministic in tests; `animations=false` config)
- [ ] `dhi doctor [--json]` check suite shared with in-app health panel
- [ ] Path-jail sandbox + permission policy engine (`internal/sandbox`) with OS-sandbox adapter seam
- [ ] Workspace domain: multi-repo model, `.dhi/workspace.toml`, VPath resolver, dir-schema reservation for `memory/` + `knowledge/`
- [ ] Tests: httptest fixture server, tamper cases, doctor JSON assertions, bootstrap event-driven goldens

## M2 — IDE core

- [ ] Multi-repo nav tree grouped by member repo; fuzzy find; ripgrep fan-out search
- [ ] Modal editor MVP: normal/insert/visual/command, motions, d/c/y, registers(min), `:w :q :wq :e`
- [ ] PTY terminal panel, per-repo tabs
- [ ] Worktrees surface: list/create/switch/remove, per-worktree status, stage (space), commit (c)
- [ ] Command palette incl. `:ws add <path>` flows (replaces any CLI subcommands)
- [ ] E2E teatest scenario: edit across two repos, branch, worktree create/switch, stage/commit

## M3 — Agent runtime & PairChat

- [ ] `agentkit` manifest spec (skills/knowledge/mcp/tools/model) + validation
- [ ] Provider interface: Anthropic adapter + scripted MockProvider (all tests offline)
- [ ] Tool executors (namespaced VPath fs/shell/git ops) behind path-jail policies
- [ ] MCP client (stdio/http) + server registry
- [ ] Message bus: DMs/channels/threads, mention-triggered turns, JSONL persistence
- [ ] Memory: per-agent journal + notes; shared KB store w/ rg-based retrieval behind `KnowledgeStore`
- [ ] PairChat surface streaming; apply-suggestion→editor buffer

## M4 — Workplace

- [ ] Teams roster CRUD; task assignment (team or agent)
- [ ] TaskBoard channels/threads UI (Slack-like)
- [ ] ChangeSet: task ↔ multi-worktree binding
- [ ] Mention orchestration incl. agent↔agent handoff scripted e2e

## M5 — Review & Ideation

- [ ] Review flow: PR/worktree → auto review-worktree → diff viewer + hunk nav
- [ ] Inline comment threads; comment→agent roundtrip ("ask reviewer-agent")
- [ ] Ideation canvas: markdown preview + artifact blocks + agent contributions

## M6 — Marketplace

- [ ] Packaging (`manifest` bundle), install from local path/git URL
- [ ] Index/search UI; example agents repo (3 packs)
