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

has() { grep -q "  $1\$" "$sums"; }

# platform_block OS: on_arm / on_intel for whichever builds the release has;
# a single-arch OS pins that arch so the other gets a clear Homebrew error.
platform_block() {
	os=$1
	arm="dhi_${os}_arm64.tar.gz" intel="dhi_${os}_amd64.tar.gz"
	has "$arm" || has "$intel" || return 0
	case $os in darwin) echo "  on_macos do" ;; linux) echo "  on_linux do" ;; esac
	if has "$arm" && has "$intel"; then
		printf '    on_arm do\n      url "%s"\n      sha256 "%s"\n    end\n' "$base/$arm" "$(sum "$arm")"
		printf '    on_intel do\n      url "%s"\n      sha256 "%s"\n    end\n' "$base/$intel" "$(sum "$intel")"
	elif has "$arm"; then
		printf '    depends_on arch: :arm64\n    url "%s"\n    sha256 "%s"\n' "$base/$arm" "$(sum "$arm")"
	else
		printf '    depends_on arch: :x86_64\n    url "%s"\n    sha256 "%s"\n' "$base/$intel" "$(sum "$intel")"
	fi
	echo "  end"
	echo
}

base="https://github.com/${repo}/releases/download/v${version}"

cat <<RUBY
class Dhi < Formula
  desc "The agentic workspace IDE"
  homepage "https://github.com/${repo}"
  version "${version}"
  license "${license}"

$(platform_block darwin)
$(platform_block linux)
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
