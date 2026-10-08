#!/bin/sh
# Build DHI release artifacts: one tar.gz per platform in
# scripts/release-platforms.txt plus checksums.txt.
#
#   scripts/build-release.sh <version> <outdir>
#
# Artifacts are named dhi_<os>_<arch>.tar.gz (stable names so
# releases/latest/download/... URLs work) and contain `dhi` and LICENSE.
# The build is reproducible-ish: CGO off, -trimpath, stamped identity.
set -eu

version="${1:?usage: build-release.sh <version> <outdir>}"
outdir="${2:?usage: build-release.sh <version> <outdir>}"
root="$(cd "$(dirname "$0")/.." && pwd)"
version="${version#v}"

commit="$(git -C "$root" rev-parse --short HEAD 2>/dev/null || echo unknown)"
date="$(date -u +%Y-%m-%dT%H:%M:%SZ)"
pkg="github.com/drjzlyan/dhi/internal/version"
ldflags="-s -w -X $pkg.Version=$version -X $pkg.Commit=$commit -X $pkg.Date=$date"

mkdir -p "$outdir"
outdir="$(cd "$outdir" && pwd)"
work="$(mktemp -d)"
trap 'rm -rf "$work"' EXIT

sha256() {
	if command -v sha256sum >/dev/null 2>&1; then sha256sum "$@"; else shasum -a 256 "$@"; fi
}

: >"$outdir/checksums.txt"
grep -v '^#' "$root/scripts/release-platforms.txt" | grep -v '^$' | while IFS=/ read -r goos goarch; do
	name="dhi_${goos}_${goarch}"
	stage="$work/$name"
	mkdir -p "$stage"
	echo "build $goos/$goarch"
	(cd "$root" && CGO_ENABLED=0 GOOS="$goos" GOARCH="$goarch" \
		go build -trimpath -ldflags "$ldflags" -o "$stage/dhi" ./cmd/dhi)
	cp "$root/LICENSE" "$stage/LICENSE"
	tar -C "$stage" -czf "$outdir/$name.tar.gz" dhi LICENSE
	(cd "$outdir" && sha256 "$name.tar.gz" >>checksums.txt)
done
echo "wrote $(wc -l <"$outdir/checksums.txt" | tr -d ' ') artifact(s) to $outdir"
