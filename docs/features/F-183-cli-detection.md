# F-183 — Onboarding finds every installed agent CLI

Status: done · onboarding (wizard CLI and team steps), doctor

## Problem
On one machine the Copilot CLI was installed, but setup showed it as "not installed". Detection had
four weak spots:
1. **Where it looked.** `gh copilot` downloads the Copilot CLI into gh's data folder
   (`~/.local/share/gh/copilot`), which is never on PATH. Node CLIs installed through volta, bun,
   pnpm or `~/.npm-global` were found only when the login-shell PATH resolved.
2. **How it read the version.** The probe read stdout only and accepted only an exact `X.Y.Z`. A
   banner on stderr, colour codes, a prerelease (`1.0.90-1`) or a non-zero exit after printing the
   version all meant "no version".
3. **What it reported.** A binary that was found but gave no version was shown as not installed.
4. **Timeout.** 10 s, which a cold npm or volta shim can exceed.

## Behaviour
- `hosttools.installDirs` also searches:
  - `$NPM_CONFIG_PREFIX/bin`, `$VOLTA_HOME/bin` / `~/.volta/bin`, `$PNPM_HOME`, `~/Library/pnpm`,
    `~/.local/share/pnpm`, `$BUN_INSTALL/bin` / `~/.bun/bin` and `~/.npm-global/bin`;
  - gh's data folder: `$GH_DATA_DIR`, `$XDG_DATA_HOME/gh`, `%LOCALAPPDATA%\GitHub CLI` or
    `~/.local/share/gh`, both `<data>/copilot` and `<data>/copilot/copilot`;
  - `/opt/homebrew/bin` and `/usr/local/bin`.
- `probeVersion` reads stdout and stderr, strips ANSI, and keeps the output when it contains a version
  even if the exit code is non-zero. The timeout is 20 s.
- One parser (`firstVersionToken`/`versionCore`) for all six adapters: it accepts a leading `v`,
  surrounding punctuation and a prerelease or build suffix, and returns the `X.Y[.Z]` core.
- `Registry.DetectAll` returns `Probe{Path, Version, Err}`. The wizard row shows
  "✗ found, but --version failed", and under the row it says where the binary is, the reason, and
  that the CLI's runtime (node) is often missing from PATH. It never offers to install a CLI that is
  already there.

## Acceptance
- [x] Banners: copilot 1.0.88 / 1.0.90-1 / 0.0.354, claude, codex, cursor's date+hash, `v1.2.3,`, `(1.18.25+build.7)` (`TestFirstVersionTokenTolerance`)
- [x] Version on stderr with colour and a non-zero exit (`TestCopilotVersionFromStderrWithColourAndNonZeroExit`)
- [x] Found but failing binary keeps its path and reason (`TestDetectAllKeepsAFoundBinaryWhoseProbeFails`)
- [x] `gh copilot` download found in both layouts (`TestFindCopilotInGhDataFolder`)
- [x] Wizard shows found-but-broken, explains it, offers no install (`TestCLIStepShowsAFoundBinaryWhoseProbeFailed`)
- [ ] Live: re-run setup on the machine where Copilot was missed
