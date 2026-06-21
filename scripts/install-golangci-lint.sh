#!/usr/bin/env bash
#
# install-golangci-lint.sh — self-bootstrap the pinned golangci-lint.
#
# The version is the single source of truth in .golangci-version. A system
# golangci-lint is often a different version from the repo pin, and prebuilt
# binaries can be rejected when they were built with an older Go than the go.mod
# target ("build Go < target Go"). Building the pinned version FROM SOURCE with
# the local Go toolchain into a repo-local, gitignored dir sidesteps both
# problems.
#
# This script is idempotent: if ./.cache/bin/golangci-lint already reports the
# pinned version it does nothing and never deletes an existing good binary.
set -euo pipefail

root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
cd "$root"

version_file=".golangci-version"
if [[ ! -f "$version_file" ]]; then
  echo "install-golangci-lint: $version_file not found" >&2
  exit 1
fi

want="$(tr -d '[:space:]' <"$version_file")"
if [[ -z "$want" ]]; then
  echo "install-golangci-lint: $version_file is empty" >&2
  exit 1
fi
want_bare="${want#v}"

bindir="$root/.cache/bin"
binary="$bindir/golangci-lint"

# Idempotent fast path: an existing binary that already reports the pinned
# version is good — do nothing and leave it untouched.
if [[ -x "$binary" ]]; then
  have_raw="$("$binary" version 2>/dev/null | grep -oE 'v?[0-9]+\.[0-9]+\.[0-9]+' | head -1 || true)"
  have_bare="${have_raw#v}"
  if [[ "$have_bare" == "$want_bare" ]]; then
    echo "install-golangci-lint: ./.cache/bin/golangci-lint already at $want; nothing to do"
    exit 0
  fi
  echo "install-golangci-lint: ./.cache/bin/golangci-lint is ${have_raw:-unknown}, rebuilding to $want"
fi

command -v go >/dev/null 2>&1 || {
  echo "install-golangci-lint: go toolchain is required to build golangci-lint" >&2
  exit 1
}

mkdir -p "$bindir"
major="${want_bare%%.*}"
module="github.com/golangci/golangci-lint/cmd/golangci-lint"
if [[ "$major" =~ ^[0-9]+$ && "$major" -ge 2 ]]; then
  module="github.com/golangci/golangci-lint/v${major}/cmd/golangci-lint"
fi

echo "install-golangci-lint: building $module@$want into $bindir"
GOBIN="$bindir" go install "$module@$want"

# Confirm the freshly built binary reports the pinned version.
got_raw="$("$binary" version 2>/dev/null | grep -oE 'v?[0-9]+\.[0-9]+\.[0-9]+' | head -1 || true)"
got_bare="${got_raw#v}"
if [[ "$got_bare" != "$want_bare" ]]; then
  echo "install-golangci-lint: built ${got_raw:-unknown} but expected $want" >&2
  exit 1
fi
echo "install-golangci-lint: ./.cache/bin/golangci-lint now at $want"
