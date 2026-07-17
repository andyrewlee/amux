#!/bin/sh
set -e

# amux installer script
# Usage: curl -fsSL https://raw.githubusercontent.com/andyrewlee/amux/main/install.sh | sh

REPO="andyrewlee/amux"
BINARY="amux"
INSTALL_DIR="${INSTALL_DIR:-/usr/local/bin}"

# Minisign public key used to verify the detached signature over checksums.txt
# before any checksum is trusted. Must match minisignPublicKey in
# internal/update/pubkey.go (both halves of the release signing keypair
# provisioned 2026-07-16).
MINISIGN_PUBKEY="RWQtWAYbPCb3AH9id1CLjcfy/KdeL6Uyo68qC6oKRJz1gyBOZS3Xm54P"

# AMUX_ALLOW_UNVERIFIED=1 lets the install proceed on checksum-only
# verification when signature verification cannot run (minisign not installed,
# signature asset missing, or no public key embedded above). It never bypasses
# an actual failed signature check. Default: fail closed.

# Detect OS
OS=$(uname -s | tr '[:upper:]' '[:lower:]')
case "$OS" in
  darwin) OS="darwin" ;;
  linux) OS="linux" ;;
  *)
    echo "Error: Unsupported operating system: $OS"
    exit 1
    ;;
esac

# Detect architecture
ARCH=$(uname -m)
case "$ARCH" in
  x86_64|amd64) ARCH="amd64" ;;
  arm64|aarch64) ARCH="arm64" ;;
  *)
    echo "Error: Unsupported architecture: $ARCH"
    exit 1
    ;;
esac

# Called when signature verification cannot run at all; honors
# AMUX_ALLOW_UNVERIFIED (returns to continue on checksum-only) and otherwise
# fails closed.
skip_or_die() {
  if [ "${AMUX_ALLOW_UNVERIFIED:-0}" = "1" ]; then
    echo "Warning: $1; continuing with checksum-only verification (AMUX_ALLOW_UNVERIFIED=1)." >&2
    return 0
  fi
  echo "Error: $1." >&2
  echo "Refusing to install without signature verification." >&2
  echo "Install minisign (https://jedisct1.github.io/minisign/) or set AMUX_ALLOW_UNVERIFIED=1 to proceed with checksum-only verification, at your own risk." >&2
  exit 1
}

sha256_of() {
  if command -v sha256sum >/dev/null 2>&1; then
    sha256sum "$1" | awk '{print $1}'
  elif command -v shasum >/dev/null 2>&1; then
    shasum -a 256 "$1" | awk '{print $1}'
  else
    echo ""
  fi
}

# Get latest version from GitHub API
get_latest_version() {
  curl -fsSL "https://api.github.com/repos/${REPO}/releases/latest" | 
    grep '"tag_name":' | 
    sed -E 's/.*"([^"]+)".*/\1/'
}

VERSION="${VERSION:-$(get_latest_version)}"

if [ -z "$VERSION" ]; then
  echo "Error: Could not determine latest version"
  exit 1
fi

# Remove 'v' prefix if present for filename
VERSION_NUM="${VERSION#v}"

FILENAME="${BINARY}_${VERSION_NUM}_${OS}_${ARCH}.tar.gz"
DOWNLOAD_URL="https://github.com/${REPO}/releases/download/${VERSION}/${FILENAME}"

echo "Installing ${BINARY} ${VERSION} (${OS}/${ARCH})..."

# Create temp directory
TMP_DIR=$(mktemp -d)
trap 'rm -rf "$TMP_DIR"' EXIT

# Download and extract
echo "Downloading ${DOWNLOAD_URL}..."
curl -fsSL "$DOWNLOAD_URL" -o "${TMP_DIR}/${FILENAME}"

# Verify checksum against the release's published checksums.txt
CHECKSUMS_URL="https://github.com/${REPO}/releases/download/${VERSION}/checksums.txt"
echo "Fetching checksums..."
curl -fsSL "$CHECKSUMS_URL" -o "${TMP_DIR}/checksums.txt"

# Verify the minisign signature over checksums.txt before trusting any
# checksum in it. The checksum below stays as a second layer; the signature is
# what proves the checksums came from the release signing key rather than
# from whoever controls the release assets.
MINISIG_URL="https://github.com/${REPO}/releases/download/${VERSION}/checksums.txt.minisig"
echo "Fetching signature..."
if ! curl -fsSL "$MINISIG_URL" -o "${TMP_DIR}/checksums.txt.minisig"; then
  skip_or_die "could not download release signature (checksums.txt.minisig)"
elif [ -z "$MINISIGN_PUBKEY" ]; then
  skip_or_die "no release signing public key embedded in this installer"
elif ! command -v minisign >/dev/null 2>&1; then
  skip_or_die "minisign not found; cannot verify the release signature"
elif ! minisign -V -P "$MINISIGN_PUBKEY" -m "${TMP_DIR}/checksums.txt" -x "${TMP_DIR}/checksums.txt.minisig" >/dev/null 2>&1; then
  echo "Error: signature verification FAILED for checksums.txt — the release may have been tampered with." >&2
  exit 1
else
  echo "Signature verified."
fi

EXPECTED=$(grep " ${FILENAME}\$" "${TMP_DIR}/checksums.txt" | awk '{print $1}')
if [ -z "$EXPECTED" ]; then
  echo "Error: no checksum entry for ${FILENAME} in checksums.txt"
  exit 1
fi

ACTUAL=$(sha256_of "${TMP_DIR}/${FILENAME}")
if [ -z "$ACTUAL" ]; then
  echo "Error: neither sha256sum nor shasum found; cannot verify download"
  exit 1
fi

if [ "$EXPECTED" != "$ACTUAL" ]; then
  echo "Error: checksum mismatch for ${FILENAME}"
  echo "  expected: $EXPECTED"
  echo "  actual:   $ACTUAL"
  exit 1
fi
echo "Checksum verified."

echo "Extracting..."
tar -xzf "${TMP_DIR}/${FILENAME}" -C "$TMP_DIR"

# Install binary
echo "Installing to ${INSTALL_DIR}/${BINARY}..."
if [ -w "$INSTALL_DIR" ]; then
  mv "${TMP_DIR}/${BINARY}" "${INSTALL_DIR}/${BINARY}"
else
  sudo mv "${TMP_DIR}/${BINARY}" "${INSTALL_DIR}/${BINARY}"
fi

chmod +x "${INSTALL_DIR}/${BINARY}"

echo ""
echo "✓ ${BINARY} ${VERSION} installed successfully!"
echo ""
echo "Run '${BINARY}' to get started."
