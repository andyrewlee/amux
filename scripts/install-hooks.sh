#!/usr/bin/env bash
set -euo pipefail

root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
cd "$root"

existing="$(git config --get core.hooksPath || true)"
if [[ "$existing" == ".githooks" ]]; then
  echo "core.hooksPath already set to .githooks — nothing to do."
elif [[ -n "$existing" ]]; then
  echo "WARNING: core.hooksPath is currently '$existing' — replacing it." >&2
  echo "         Existing hooks there will stop running; merge manually if needed." >&2
  git config core.hooksPath .githooks
else
  git config core.hooksPath .githooks
fi

chmod +x .githooks/pre-commit
chmod +x .githooks/pre-push

echo "Installed git hooks to .githooks (pre-commit, pre-push enabled)."
