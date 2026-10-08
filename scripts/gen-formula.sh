#!/bin/sh
# Print a Homebrew formula for a DHI release.
#
#   scripts/gen-formula.sh <version> <checksums.txt> [license-spdx]
#
# The formula installs the prebuilt binary from the GitHub release, so the
# checksums in it are exactly the ones build-release.sh produced.
set -eu

version="${1:?usage: gen-formula.sh <version> <checksums.txt> [spdx]}"
sums="${2:?usage: gen-formula.sh <version> <checksums.txt> [spdx]}"
license="${3:-MIT}"
version="${version#v}"
repo="drjzlyan/dhi"

sum() { # sum <artifact>
	s="$(grep "  $1\$" "$sums" | cut -d' ' -f1)"
	[ -n "$s" ] || { echo "gen-formula: no checksum for $1" >&2; exit 1; }
	printf '%s' "$s"
}

cat <<RUBY
class Dhi < Formula
  desc "The agentic workspace IDE"
  homepage "https://github.com/${repo}"
  version "${version}"
  license "${license}"

  on_macos do
    depends_on arch: :arm64
    url "https://github.com/${repo}/releases/download/v${version}/dhi_darwin_arm64.tar.gz"
    sha256 "$(sum dhi_darwin_arm64.tar.gz)"
  end

  on_linux do
    depends_on arch: :x86_64
    url "https://github.com/${repo}/releases/download/v${version}/dhi_linux_amd64.tar.gz"
    sha256 "$(sum dhi_linux_amd64.tar.gz)"
  end

  def install
    bin.install "dhi"
  end

  def caveats
    <<~EOS
      Run \`dhi\` in a project directory. On first run DHI downloads its own
      pinned toolchain under ~/.local/share/dhi and walks you through setup.
    EOS
  end

  test do
    assert_match version.to_s, shell_output("#{bin}/dhi version")
  end
end
RUBY
