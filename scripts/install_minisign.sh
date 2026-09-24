#!/usr/bin/env bash
# Pinned minisign install for CI/release jobs. Downloads the upstream
# minisign linux binary tarball and verifies it against a pinned sha256
# before installing — the tool that verifies our release signatures must not
# itself be fetched unverified (previously: unpinned `apt-get install
# minisign`, whose version drifts with the distro archive).
#
# On version bump: update both MINISIGN_VERSION and MINISIGN_SHA256 (hash =
# `shasum -a 256 minisign-<ver>-linux.tar.gz` of the release artifact).
set -euo pipefail

MINISIGN_VERSION="0.12"
MINISIGN_SHA256="9a599b48ba6eb7b1e80f12f36b94ceca7c00b7a5173c95c3efc88d9822957e73"
INSTALL_DIR="${INSTALL_DIR:-/usr/local/bin}"

tmp="$(mktemp -d)"
trap 'rm -rf "$tmp"' EXIT

curl -fsSL \
	"https://github.com/jedisct1/minisign/releases/download/${MINISIGN_VERSION}/minisign-${MINISIGN_VERSION}-linux.tar.gz" \
	-o "$tmp/minisign.tar.gz"
echo "${MINISIGN_SHA256}  ${tmp}/minisign.tar.gz" | sha256sum -c -
tar -xzf "$tmp/minisign.tar.gz" -C "$tmp"

# The tarball ships one static binary per arch under minisign-linux/<arch>/
# with the same names `uname -m` reports (x86_64, aarch64).
src="$tmp/minisign-linux/$(uname -m)/minisign"
[ -f "$src" ] || { echo "no minisign binary for arch: $(uname -m)" >&2; exit 1; }
if [ -w "$INSTALL_DIR" ]; then
	install -m 0755 "$src" "$INSTALL_DIR/minisign"
else
	sudo install -m 0755 "$src" "$INSTALL_DIR/minisign"
fi
