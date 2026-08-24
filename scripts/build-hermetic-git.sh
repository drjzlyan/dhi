#!/bin/sh
# build-hermetic-git.sh — produce DHI's pinned hermetic git artifacts
# (ADR-0009): transport-free git built from the upstream source tarball,
# packaged as a single bin/git per platform for the registry manifest.
#
# Usage:
#   scripts/build-hermetic-git.sh [version]        # default 2.55.0
#   DHI_GIT_SRC_SHA256=<hex> enforce upstream digest check
#
# Maintainer tooling only (network + C compiler); never invoked by the
# shipped product, which downloads verified registry artifacts instead.
set -eu

VERSION="${1:-${DHI_GIT_VERSION:-2.55.0}}"
SRC_URL="https://www.kernel.org/pub/software/scm/git/git-${VERSION}.tar.xz"

work="$(mktemp -d)"
trap 'rm -rf "$work"' EXIT
mkdir -p "$work/dist"

echo "==> downloading $SRC_URL"
curl -fsSL "$SRC_URL" -o "$work/src.tar.xz"

if [ -n "${DHI_GIT_SRC_SHA256:-}" ]; then
    echo "$DHI_GIT_SRC_SHA256  $work/src.tar.xz" | shasum -a 256 -c -
else
    echo "!! no DHI_GIT_SRC_SHA256 set — cross-check this digest against" \
         "the kernel.org release before pinning:"
    shasum -a 256 "$work/src.tar.xz"
fi

echo "==> extracting"
mkdir "$work/src"
tar -xJf "$work/src.tar.xz" -C "$work/src" --strip-components=1

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
