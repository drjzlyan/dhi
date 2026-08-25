#!/bin/sh
# build-hermetic-git.sh — produce DHI's pinned hermetic git artifacts
# (ADR-0009): transport-free git built from the upstream source tarball,
# packaged as a single bin/git per platform for the registry manifest.
#
# Usage:
#   scripts/build-hermetic-git.sh [version]        # default 2.55.0
#   GIT_RELEASE_KEY_FP=<fpr> override release-signer fingerprint
#   DHI_GIT_SKIP_VERIFY=1 skip GPG (local experiments ONLY; CI never sets it)
#
# Source integrity: kernel.org publishes a detached signature over the
# gzip tarball, signed by the git release key. We verify against the
# fingerprint below (cross-check per pin against the release announce),
# then build from the very file we verified.
#
# Maintainer/CI tooling only (network + C compiler); never invoked by
# the shipped product, which downloads verified registry artifacts.
set -eu

VERSION="${1:-${DHI_GIT_VERSION:-2.55.0}}"
DEFAULT_KEY_FP="96E07AF25771955980DAD10020D04E5A713660A7"  # Junio C Hamano <gitster@pobox.com>
GIT_REPO="${GIT_REPO:-https://github.com/git/git}"

# Source integrity: we clone the upstream tag itself (shallow) and
# verify ITS GPG signature against the pinned release-key fingerprint —
# stronger than tarball+detached-sig, immune to kernel.org's blanket
# 404s for CI-provider ranges on /pub. The tag pins exact commit content.
#
# Maintainer/CI tooling only (network + C compiler); never invoked by
# the shipped product, which downloads verified registry artifacts.
set -eu

work="$(mktemp -d)"
trap 'rm -rf "$work"' EXIT
mkdir -p "$work/dist"

echo "==> fetching signed tag v${VERSION} from $GIT_REPO (depth 1)"
git init -q "$work/src"
git -C "$work/src" fetch -q --depth 1 "$GIT_REPO" \
    "refs/tags/v${VERSION}:refs/tags/v${VERSION}"

if [ "${DHI_GIT_SKIP_VERIFY:-0}" != "1" ]; then
    command -v gpg >/dev/null || { echo "gpg required (apt/brew install gnupg)" >&2; exit 1; }
    KEY_FP="${GIT_RELEASE_KEY_FP:-$DEFAULT_KEY_FP}"
    curl -fsSL "https://keyserver.ubuntu.com/pks/lookup?op=get&search=0x${KEY_FP}" \
        -o "$work/key.asc"
    export GNUPGHOME="$work/gnupg"
    mkdir -m 700 -p "$GNUPGHOME"
    gpg --quiet --import "$work/key.asc"

    STATUS="$work/status.txt"
    # git verify-tag splits the embedded signature correctly; --raw
    # surfaces gpg's status lines for fingerprint matching.
    if ! git -C "$work/src" verify-tag --raw "v${VERSION}" >"$STATUS" 2>&1; then
        cat "$STATUS"; echo "signature FAILED" >&2; exit 1
    fi
    # VALIDSIG's LAST field is the primary-key fingerprint; the first
    # is whichever subkey did the signing.
    grep "VALIDSIG" "$STATUS" | awk '{print $NF}' | grep -qx "$KEY_FP" ||
        { echo "signature valid but signed by unexpected key:" >&2
          grep -E "VALIDSIG|Good signature" "$STATUS" >&2; exit 1; }
    echo "==> tag signature OK (key $KEY_FP)"
else
    echo "!! GPG verification SKIPPED (DHI_GIT_SKIP_VERIFY=1) — never ship pins from unverified builds" >&2
fi

echo "==> checking out verified tree"
git -C "$work/src" checkout -q "tags/v${VERSION}"

echo "==> building (transport-free: NO_CURL NO_EXPAT NO_GETTEXT NO_PERL NO_TCLTK)"
make -C "$work/src" -j "$(getconf _NPROCESSORS_ONLN 2>/dev/null || echo 4)" \
    git \
    GIT_VERSION="v${VERSION}" \
    NO_CURL=1 NO_EXPAT=1 NO_GETTEXT=1 NO_PERL=1 NO_TCLTK=1 \
    NO_INSTALL_HARDLINKS=1 \
    >/dev/null

"$work/src/git" --version

os="$(uname -s)"; arch="$(uname -m)"
case "$os/$arch" in
    Darwin/arm64)  plat="darwin/arm64"  ;;
    Darwin/x86_64) plat="darwin/amd64"  ;;
    Linux/x86_64)  plat="linux/amd64"   ;;
    Linux/aarch64) plat="linux/arm64"   ;;
    *) echo "unsupported platform $os/$arch" >&2; exit 1 ;;
esac
plat_slug="$(printf '%s' "$plat" | tr '/' '-')"

stage="$work/pkg/bin"
mkdir -p "$stage"
cp "$work/src/git" "$stage/git"
chmod 0755 "$stage/git"

out="$work/dist/dhi-git-${VERSION}-${plat_slug}.tar.gz"
tar -czf "$out" -C "$work/pkg" bin
digest="$(shasum -a 256 "$out" | awk '{print $1}')"
cp "$out" .
shafile="dhi-git-${VERSION}-${plat_slug}.sha256"
echo "$digest  $(basename "$out")" > "$shafile"

cat <<EOF

==> artifact:   $(basename "$out")
==> sha256:     $digest
==> platform:   $plat

Registry manifest snippet (paste after reviewing provenance):

    "${plat}": {
      "url": "https://github.com/drjzlyan/dhi/releases/download/hermetic-git-v${VERSION}/$(basename "$out")",
      "sha256": "${digest}",
      "format": "tar.gz"
    },
EOF
