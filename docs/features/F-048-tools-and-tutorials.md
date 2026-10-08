# F-048: Source-built tools (gopls, dlv) + interactive tutorials

Status: complete (M25 P7)

## A. Debugger provisioning
`:debug` refused until a user built delve by hand (`dlv` was the one open item
from F-039). DHI now builds it itself, like `gopls`, with its pinned Go
(`GOTOOLCHAIN=local`, CGO off, private GOPATH):

- `toolchain.Delve()` (v1.27.2, `toolchain.DelveVersion`) and `toolchain.SourceBuilt()`
  = `[gopls, dlv]`. A test pins `dap.VerifiedDelve == toolchain.DelveVersion`.
- `boot` offers each missing one (`dlv (built from source)`); the boot gate builds the
  queue one after another with `building dlv from source (2/2)…`.
- **Fixed on the way:** a failed build used to release the shell and throw the error
  away (the text was set, then never shown). Failures now stop on a screen naming each
  tool and why, and wait for a key; tools then refuse by name at use.

Verified: delve v1.27.2 built through `BuildInstall` in 6 s with DHI's Go and reported
its version; the repo's live smoke test (`TestLiveDelve`) passed against that binary —
breakpoint stop with frames and locals, step, evaluate, run to exit.

## B. Interactive tutorials
A lesson is a short declarative TOML (`internal/tutorial/lessons/*.toml`): steps with a
title, a body and an optional **await**: `view:<surface id>`, `palette`, `help`, or none
(read, try, continue). The shell plays it as a **four-row coach strip** under the live UI —
the UI above stays fully usable (its height shrinks by the strip) and the strip advances
when the shell *observes* the action (`App.observe`: view switch, palette open, help open).

- Lessons: **Find your way around** (`tour`), **Work with your team**, **Edit with an agent
  beside you**, **Debug Go with breakpoints**. Every key they teach is taken from the
  app's own help text.
- Controls: `ctrl+]` continue/skip step, `ctrl+\` end lesson (chosen to avoid `ctrl+n`,
  which the palette and finders use as "down").
- Palette: `Tutorial: <name>` (✓ once completed), `End this lesson` while one runs.
- The wizard's last screen offers `t` for the tour: it starts at once when nothing needs a
  relaunch, otherwise a marker (`<config>/dhi/tour.pending`) starts it on the next launch.
- Progress: `tutorial:<slug>` in the user setup state (`setup.json`), only on completion.

## Acceptance criteria
- [x] dlv/gopls queue builds in order, skips ones already present, shows failures and waits
- [x] boot offers `dlv (built from source)`; a full install offers nothing
- [x] real delve built with DHI's Go; live smoke test passes
- [x] lessons parse strictly; awaits are in the grammar; teaching order fixed; bodies fit two rows
- [x] the strip takes exactly its rows and returns them; advances only on the awaited action; an already-true step advances at once; a gate pauses it; a new lesson ends the old one early
- [x] strip rows never exceed the terminal width; long text clips to two rows
- [x] palette entries, completion check, end-lesson entry
- [x] wizard tour offer (in-process vs queued across a relaunch), absent without hooks
- [x] played through in tmux: each step advanced on the real action; completion recorded

## Deferred (honest limits)
- **Language servers for other languages.** The editor wires LSP for Go only
  (`lspsurface.go`), so provisioning other servers would add binaries nothing can use.
  The right order is: teach the editor to pick a server per file type, then add
  provisioning per server.
- **Surface-level awaits** (the board's `n`, the editor's `:w`). Lessons for those steps
  are read-and-continue; observing them needs the surfaces to emit events.
- Formatters beyond `gofmt` (format-on-save is Go-only today).
