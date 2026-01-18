#!/usr/bin/env bash
set -euo pipefail

root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
cd "$root"

git config core.hooksPath .githooks
chmod +x .githooks/pre-push

echo "Installed git hooks to .githooks (pre-push enabled)."
