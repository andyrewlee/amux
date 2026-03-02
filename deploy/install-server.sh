#!/bin/bash
set -euo pipefail

# Medusa Server Install Script
# Usage: curl -fsSL https://raw.githubusercontent.com/andyrewlee/medusa/main/deploy/install-server.sh | bash

REPO="andyrewlee/medusa"
INSTALL_DIR="/usr/local/bin"
CONFIG_DIR="$HOME/.medusa"

echo "=== Medusa Server Installer ==="
echo

# Detect OS and architecture
OS=$(uname -s | tr '[:upper:]' '[:lower:]')
ARCH=$(uname -m)
case "$ARCH" in
    x86_64) ARCH="amd64" ;;
    aarch64|arm64) ARCH="arm64" ;;
    *) echo "Unsupported architecture: $ARCH"; exit 1 ;;
esac

echo "Detected: $OS/$ARCH"

# Check dependencies
for cmd in git tmux; do
    if ! command -v "$cmd" &>/dev/null; then
        echo "Missing required dependency: $cmd"
        echo "Install with: sudo apt install $cmd  (or equivalent)"
        exit 1
    fi
done

if ! command -v claude &>/dev/null; then
    echo "Claude Code CLI not found. Installing..."
    if command -v npm &>/dev/null; then
        npm install -g @anthropic-ai/claude-code
    else
        echo "npm not found. Please install Node.js first:"
        echo "  curl -fsSL https://deb.nodesource.com/setup_22.x | sudo -E bash -"
        echo "  sudo apt install -y nodejs"
        echo "  npm install -g @anthropic-ai/claude-code"
        exit 1
    fi
fi

# Download latest release
echo "Downloading medusa-server..."
LATEST=$(curl -fsSL "https://api.github.com/repos/$REPO/releases/latest" | grep -o '"tag_name": "[^"]*"' | cut -d'"' -f4)
if [ -z "$LATEST" ]; then
    echo "Could not determine latest release. Building from source..."
    TMPDIR=$(mktemp -d)
    git clone --depth 1 "https://github.com/$REPO.git" "$TMPDIR/medusa"
    cd "$TMPDIR/medusa"

    # Build web UI
    if command -v npm &>/dev/null; then
        cd web && npm install && npm run build && cd ..
    fi

    # Build Go binary
    go build -o medusa-server ./cmd/medusa-server/
    sudo mv medusa-server "$INSTALL_DIR/"
    rm -rf "$TMPDIR"
else
    DOWNLOAD_URL="https://github.com/$REPO/releases/download/$LATEST/medusa-server-${OS}-${ARCH}"
    curl -fsSL "$DOWNLOAD_URL" -o /tmp/medusa-server
    chmod +x /tmp/medusa-server
    sudo mv /tmp/medusa-server "$INSTALL_DIR/"
fi

# Create config directory
mkdir -p "$CONFIG_DIR"

echo
echo "=== Installation Complete ==="
echo
echo "Start the server:"
echo "  medusa-server --port 8420"
echo
echo "Or install as a systemd service:"
echo "  sudo cp deploy/medusa-server.service /etc/systemd/system/"
echo "  sudo systemctl enable --now medusa-server"
echo
echo "The server will print an auth token on first start."
echo "Use it to connect from web, mobile, or TUI clients."
