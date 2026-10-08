# F-044: Distribution — release pipeline, installer, first-run consent

Status: complete (M25 P3) except the parts only a published tag can exercise
(see "Not verified here") · Part of the install → setup → first-use programme.

## Problem

The only way to get DHI was `go run ./cmd/dhi` with Go 1.26+. There was no
release, installer, checksum or version stamp; and the very first launch
silently downloaded ~157 MB with no consent and no size shown.

## What ships

| Piece | Where |
|---|---|
| Version identity | `internal/version` (`Version`/`Commit`/`Date` vars stamped by `-ldflags`), `dhi version` (listed in ADR-0004, previously unimplemented) |
| Release platforms | `scripts/release-platforms.txt` — **darwin/arm64, linux/amd64** |
| Build | `scripts/build-release.sh <version> <dir>` → `dhi_<os>_<arch>.tar.gz` + `checksums.txt` (CGO off, `-trimpath`) |
| Installer | `scripts/install.sh` (attached to each release as `install.sh`) |
| Release workflow | `.github/workflows/release.yml` on `v*` tags |
| Homebrew | `scripts/gen-formula.sh` → `dhi.rb` on the release; tap push is optional |
| Download sizes | `size` per manifest pin (`scripts/pin-sizes.py`; the git/gh pin scripts record it too) |
| First-run consent | `bootgate` first-run mode: tools, blurbs, sizes, total, install path; `enter` installs, `esc` skips |
| Install progress | overall `ProgressBar` + `n/N tools` in the bootstrap view |

## Decisions

- **Release platforms must equal toolchain platforms.** The manifest pins tools
  for darwin/arm64 and linux/amd64 only; a binary on another platform would
  refuse to boot (ADR-0011). `TestEveryReleasePlatformIsPinnedForEveryTool`
  fails when `release-platforms.txt` lists a platform the manifest does not
  fully pin (or sizes are missing). To add a platform: pin all six tools, extend
  the hermetic-git matrix in `release-git.yml`, then add the line.
- **No keys to manage.** Checksums are signed with cosign *keyless* (GitHub
  OIDC); `install.sh` always verifies SHA-256 and additionally verifies the
  signature when `cosign` is installed (`DHI_REQUIRE_SIGNATURE=1` makes that
  mandatory).
- **Homebrew tap is opt-in.** Set the repo variable `HOMEBREW_TAP_REPO` and the
  secret `HOMEBREW_TAP_TOKEN`; unset, the formula is still attached to the release.
- **First run asks.** The old unattended bootstrap remains only for
  `DHI_REGISTRY` (pipeline testing). Skipping leaves capabilities refusing by
  name until installed (ADR-0011).

## Verified

- `build-release.sh` builds both platforms; the stamped binary reports its version.
- `install.sh` against a local server: success, upgrade over an existing
  install (reports `old → new`), tampered artifact refused (nothing installed),
  unsupported platform refused with a named reason, `DHI_REQUIRE_SIGNATURE=1`
  without cosign refused. `shellcheck` clean.
- `gen-formula.sh` output passes `ruby -c`.
- Whole journey in tmux with an empty data dir: install.sh → first-run
  consent → real 157 MB download with the progress bar → setup wizard →
  relaunch → `dhi doctor` green for toolchain, identity and workspace.

## Not verified here

- `release.yml` itself (needs a pushed tag): the cosign signing step, `gh
  release create`, and the optional tap push have not run. The workflow YAML
  parses; its steps are thin wrappers over the scripts above, which were run.
- cosign signature verification inside `install.sh` (no cosign locally).
- Windows is not supported (no manifest pins, ADR-0005 targets mac/linux).

## Bugs found on the way
- `bootgate.wrapLine` emitted an empty leading line whenever text wrapped,
  wasting rows and pushing the key hint out of the panel.
- The confirm hint was clipped by the panel (`…until instal`); it now wraps.
