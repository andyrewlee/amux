#!/usr/bin/env bash
# Pinned minisign install for release builds. Downloads the upstream
# minisign binary archive for this OS and verifies it against a pinned
# sha256 before installing — the tool that verifies our release signatures
# must not itself be fetched unverified (previously: unpinned `apt-get
# install minisign`, whose version drifts with the distro archive).
#
# On version bump: update MINISIGN_VERSION and each archive's pinned sha256
# (hash = `shasum -a 256 minisign-<ver>-<os>.<ext>` of the release artifact).
set -euo pipefail

MINISIGN_VERSION="0.12"
MINISIGN_SHA256_LINUX="9a599b48ba6eb7b1e80f12f36b94ceca7c00b7a5173c95c3efc88d9822957e73"
MINISIGN_SHA256_MACOS="89000b19535765f9cffc65a65d64a820f433ef6db8020667f7570e06bf6aac63"
INSTALL_DIR="${INSTALL_DIR:-/usr/local/bin}"

sha256_check() {
	# sha256sum is GNU coreutils; macOS ships shasum instead.
	if command -v sha256sum >/dev/null 2>&1; then
		echo "$1  $2" | sha256sum -c -
	else
		echo "$1  $2" | shasum -a 256 -c -
	fi
}

tmp="$(mktemp -d)"
trap 'rm -rf "$tmp"' EXIT

os="$(uname -s | tr '[:upper:]' '[:lower:]')"
case "$os" in
linux)
	curl -fsSL \
		"https://github.com/jedisct1/minisign/releases/download/${MINISIGN_VERSION}/minisign-${MINISIGN_VERSION}-linux.tar.gz" \
		-o "$tmp/minisign.tar.gz"
	sha256_check "$MINISIGN_SHA256_LINUX" "$tmp/minisign.tar.gz"
	tar -xzf "$tmp/minisign.tar.gz" -C "$tmp"
	# The tarball ships one static binary per arch under
	# minisign-linux/<arch>/ with the same names `uname -m` reports.
	src="$tmp/minisign-linux/$(uname -m)/minisign"
	;;
darwin)
	# Upstream's macOS asset is a zip holding a single arm64 binary at the
	# root (plus an AppleDouble "._" stub to ignore). intel macs and older
	# macOS: `brew install minisign` is the documented path instead.
	if [ "$(uname -m)" != "arm64" ]; then
		echo "minisign-${MINISIGN_VERSION}-macos.zip ships arm64 only; on this arch use: brew install minisign" >&2
		exit 1
	fi
	curl -fsSL \
		"https://github.com/jedisct1/minisign/releases/download/${MINISIGN_VERSION}/minisign-${MINISIGN_VERSION}-macos.zip" \
		-o "$tmp/minisign.zip"
	sha256_check "$MINISIGN_SHA256_MACOS" "$tmp/minisign.zip"
	unzip -o -q "$tmp/minisign.zip" -d "$tmp/macos"
	src="$tmp/macos/minisign"
	;;
*)
	echo "unsupported OS for this installer: $os (use your package manager)" >&2
	exit 1
	;;
esac

[ -f "$src" ] || { echo "no minisign binary for $(uname -sm) in the pinned archive" >&2; exit 1; }
if [ ! -d "$INSTALL_DIR" ]; then
	mkdir -p "$INSTALL_DIR" 2>/dev/null || sudo mkdir -p "$INSTALL_DIR"
fi
if [ -w "$INSTALL_DIR" ]; then
	install -m 0755 "$src" "$INSTALL_DIR/minisign"
else
	sudo install -m 0755 "$src" "$INSTALL_DIR/minisign"
fi
