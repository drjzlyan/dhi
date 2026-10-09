#!/bin/sh
# DHI installer.
#
#   curl -fsSL https://github.com/drjzlyan/dhi/releases/latest/download/install.sh | sh
#
# Downloads the release binary for this machine, verifies its SHA-256
# against the release's checksums.txt (and its cosign signature when
# `cosign` is installed), and installs it to ~/.local/bin. DHI itself
# fetches its own pinned toolchain on first run — nothing else is
# installed here, and no sudo is used.
#
# Environment:
#   DHI_VERSION            install this release (e.g. 0.2.0) instead of the latest
#   DHI_INSTALL_DIR        target directory (default: $HOME/.local/bin)
#   DHI_BASE_URL           download from here instead of GitHub (mirrors, tests)
#   DHI_REQUIRE_SIGNATURE  1 = fail unless the cosign signature verifies
#   NO_COLOR               disable colour
set -eu

REPO="drjzlyan/dhi"
SIGNER_RE="^https://github.com/${REPO}/\.github/workflows/release\.yml@"
OIDC_ISSUER="https://token.actions.githubusercontent.com"

# ---- presentation -----------------------------------------------------
if [ -t 1 ] && [ -z "${NO_COLOR:-}" ]; then
	C_BRAND="$(printf '\033[1;36m')"; C_OK="$(printf '\033[32m')"
	C_ERR="$(printf '\033[31m')"; C_DIM="$(printf '\033[2m')"; C_OFF="$(printf '\033[0m')"
else
	C_BRAND=""; C_OK=""; C_ERR=""; C_DIM=""; C_OFF=""
fi
step() { printf '%s→%s %s\n' "$C_BRAND" "$C_OFF" "$1"; }
ok()   { printf '%s✓%s %s\n' "$C_OK" "$C_OFF" "$1"; }
note() { printf '  %s%s%s\n' "$C_DIM" "$1" "$C_OFF"; }
die()  { printf '%s✗ %s%s\n' "$C_ERR" "$1" "$C_OFF" >&2; exit 1; }

banner() {
	printf '%s' "$C_BRAND"
	cat <<'ART'
  ██████╗ ██╗  ██╗██╗
  ██╔══██╗██║  ██║██║
  ██║  ██║███████║██║
  ██║  ██║██╔══██║██║
  ██████╔╝██║  ██║██║
  ╚═════╝ ╚═╝  ╚═╝╚═╝
ART
	printf '%s  the agentic workspace IDE%s\n\n' "$C_OFF$C_DIM" "$C_OFF"
}

# ---- helpers ----------------------------------------------------------
have() { command -v "$1" >/dev/null 2>&1; }

fetch() { # fetch <url> <dest> [quiet]
	if have curl; then
		if [ "${3:-}" = quiet ]; then curl -fsSL "$1" -o "$2"; else curl -fSL --progress-bar "$1" -o "$2"; fi
	elif have wget; then
		wget -q -O "$2" "$1"
	else
		die "need curl or wget to download DHI"
	fi
}

sha256_of() {
	if have sha256sum; then sha256sum "$1" | cut -d' ' -f1
	elif have shasum; then shasum -a 256 "$1" | cut -d' ' -f1
	else die "need sha256sum or shasum to verify the download"; fi
}

# ---- platform ---------------------------------------------------------
detect_platform() {
	os="$(uname -s | tr '[:upper:]' '[:lower:]')"
	arch="$(uname -m)"
	case "$arch" in
		x86_64|amd64) arch=amd64 ;;
		arm64|aarch64) arch=arm64 ;;
	esac
	case "$os" in
		darwin|linux) ;;
		*) die "DHI does not ship a build for ${os} (macOS and Linux only)." ;;
	esac
}

# supported lists the platforms a release's checksums.txt carries
# ("darwin/arm64, linux/amd64"): the release itself is the source of truth.
supported() {
	sed -n 's/.*  dhi_\([a-z]*\)_\([a-z0-9]*\)\.tar\.gz$/\1\/\2/p' "$1" | paste -sd, - | sed 's/,/, /g'
}

# ---- main -------------------------------------------------------------
main() {
	banner
	detect_platform
	step "platform ${os}/${arch}"

	if [ -n "${DHI_BASE_URL:-}" ]; then
		base="${DHI_BASE_URL%/}"
	elif [ -n "${DHI_VERSION:-}" ]; then
		base="https://github.com/${REPO}/releases/download/v${DHI_VERSION#v}"
	else
		base="https://github.com/${REPO}/releases/latest/download"
	fi
	dest="${DHI_INSTALL_DIR:-$HOME/.local/bin}"
	art="dhi_${os}_${arch}.tar.gz"

	tmp="$(mktemp -d)"
	trap 'rm -rf "$tmp"' EXIT INT TERM

	# The checksums come first: they say which platforms this release has,
	# so an unsupported machine is refused by name before any download.
	fetch "${base}/checksums.txt" "${tmp}/checksums.txt" quiet
	want="$(grep "  ${art}\$" "${tmp}/checksums.txt" | cut -d' ' -f1 || true)"
	[ -n "$want" ] || die "this release has no build for ${os}/${arch}. It ships: $(supported "${tmp}/checksums.txt")."

	step "downloading ${art}"
	fetch "${base}/${art}" "${tmp}/${art}"
	got="$(sha256_of "${tmp}/${art}")"
	[ "$want" = "$got" ] || die "checksum mismatch for ${art} (expected ${want}, got ${got}) — refusing to install"
	ok "sha256 verified"

	verify_signature "$base"

	tar -xzf "${tmp}/${art}" -C "$tmp" dhi || die "could not unpack ${art}"
	mkdir -p "$dest"
	prev=""
	if [ -x "${dest}/dhi" ]; then prev="$("${dest}/dhi" version 2>/dev/null | cut -d' ' -f2 || true)"; fi
	# Move into place atomically so a running dhi is never half-overwritten.
	cp "${tmp}/dhi" "${dest}/.dhi.new" && chmod 755 "${dest}/.dhi.new" && mv -f "${dest}/.dhi.new" "${dest}/dhi"
	now="$("${dest}/dhi" version 2>/dev/null | cut -d' ' -f2 || true)"
	if [ -n "$prev" ] && [ "$prev" != "$now" ]; then ok "upgraded dhi ${prev} → ${now} in ${dest}"
	else ok "installed dhi ${now:-} to ${dest}/dhi"; fi

	case ":${PATH}:" in
		*":${dest}:"*) ;;
		*)
			printf '\n%s! %s is not on your PATH.%s Add this to your shell profile:\n' "$C_ERR" "$dest" "$C_OFF"
			# shellcheck disable=SC2016  # the literal $PATH is what the user should paste
			printf '    export PATH="%s:$PATH"\n' "$dest"
			;;
	esac
	printf '\n%sNext:%s run %sdhi%s in a project directory — it sets everything else up.\n' "$C_BRAND" "$C_OFF" "$C_BRAND" "$C_OFF"
	note "On first run DHI downloads its own pinned toolchain (no sudo, nothing outside ~/.local/share/dhi)."
	note "Uninstall: rm -rf ${dest}/dhi ~/.local/share/dhi ~/.config/dhi"
}

verify_signature() {
	if [ "${DHI_REQUIRE_SIGNATURE:-0}" != 1 ] && ! have cosign; then
		note "cosign not found — skipped signature check (sha256 verified)"
		return 0
	fi
	have cosign || die "DHI_REQUIRE_SIGNATURE=1 but cosign is not installed"
	fetch "$1/checksums.txt.sigstore.json" "${tmp}/checksums.txt.sigstore.json" quiet ||
		die "no signature bundle published for this release"
	cosign verify-blob \
		--bundle "${tmp}/checksums.txt.sigstore.json" \
		--certificate-identity-regexp "$SIGNER_RE" \
		--certificate-oidc-issuer "$OIDC_ISSUER" \
		"${tmp}/checksums.txt" >/dev/null 2>&1 || die "cosign signature verification failed — refusing to install"
	ok "cosign signature verified"
}

main "$@"
