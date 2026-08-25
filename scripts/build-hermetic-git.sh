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
BASE="https://www.kernel.org/pub/software/scm/git"
DEFAULT_KEY_FP="96E07AF2577195598DA0D6825D8D4F9305F6963A"  # git release key

work="$(mktemp -d)"
trap 'rm -rf "$work"' EXIT
mkdir -p "$work/dist"

echo "==> downloading $BASE/git-${VERSION}.tar.gz (+ detached signature)"
curl -fsSL "$BASE/git-${VERSION}.tar.gz" -o "$work/src.tar.gz"
curl -fsSL "$BASE/git-${VERSION}.tar.sign" -o "$work/src.tar.sign"

if [ "${DHI_GIT_SKIP_VERIFY:-0}" != "1" ]; then
    command -v gpg >/dev/null || { echo "gpg required (apt/brew install gnupg)" >&2; exit 1; }
    KEY_FP="${GIT_RELEASE_KEY_FP:-$DEFAULT_KEY_FP}"
    GNUPGHOME="$work/gnupg" mkdir -p "$work/gnupg"
    curl -fsSL "https://keyserver.ubuntu.com/pks/lookup?op=get&search=0x${KEY_FP}" \
        -o "$work/key.asc"
    GNUPGHOME="$work/gnupg" gpg --quiet --import "$work/key.asc"
    STATUS="$work/status.txt"
    GNUPGHOME="$work/gnupg" gpg --status-fd 1 --verify \
        "$work/src.tar.sign" "$work/src.tar.gz" >"$STATUS" 2>&1 ||
        { cat "$STATUS"; echo "signature FAILED" >&2; exit 1; }
    grep -q "^\[GNUPG:\] VALIDSIG $KEY_FP " "$STATUS" ||
        { echo "signature valid but signed by unexpected key:" >&2; grep VALIDSIG "$STATUS"; exit 1; }
    echo "==> signature OK (key $KEY_FP)"
else
    echo "!! GPG verification SKIPPED (DHI_GIT_SKIP_VERIFY=1) — never ship pins from unverified builds" >&2
fi

echo "==> extracting"
mkdir "$work/src"
tar -xzf "$work/src.tar.gz" -C "$work/src" --strip-components=1

echo "==> building (transport-free: NO_CURL NO_EXPAT NO_GETTEXT NO_PERL NO_TCLTK)"
make -C "$work/src" -j "$(getconf _NPROCESSORS_ONLN 2>/dev/null || echo 4)" \
    git \
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
