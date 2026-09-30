#!/usr/bin/env bash
# Verify the GOFUMPT/GOIMPORTS pins in the Makefile still match the formatter
# versions vendored inside the pinned golangci-lint (.golangci-version).
# `golangci-lint fmt` runs its vendored gofumpt + goimports, so if `make fmt`'s
# pins drift from those versions, local fmt and the lint gate disagree about
# what "correct" looks like (fmt/lint split-brain). The Makefile comment that
# documented this contract is enforced by this check.
#
# Usage: check_fmt_versions.sh <expected-gofumpt-version> <expected-x-tools-version>
# e.g.   check_fmt_versions.sh v0.9.2 v0.44.0
#
# Network behavior: golangci-lint's go.mod is fetched from
# raw.githubusercontent.com. When it is unreachable this prints a NOTE and
# exits 0 by default (devcheck path); FMT_VERSION_STRICT=1 (exported by
# `make ci`) makes an unverifiable check exit 1. A real version mismatch
# always fails.
set -euo pipefail
cd "$(dirname "$0")/.."

want_fumpt="${1:-}"
want_tools="${2:-}"
strict="${FMT_VERSION_STRICT:-0}"

fail() { echo "check-fmt-versions: ERROR: $*" >&2; exit 1; }
unverifiable() {
  if [ "$strict" = "1" ]; then
    fail "$*"
  fi
  echo "check-fmt-versions: NOTE: $* — fmt-version parity unverified" >&2
  exit 0
}

[ -n "$want_fumpt" ] && [ -n "$want_tools" ] || fail "usage: $0 <gofumpt-version> <x-tools-version>"

tag="$(tr -d '[:space:]' < .golangci-version 2>/dev/null)" || tag=""
[ -n "$tag" ] || unverifiable "cannot read .golangci-version"

url="https://raw.githubusercontent.com/golangci/golangci-lint/${tag}/go.mod"
gomod="$(curl -fsSL --max-time 10 "$url" 2>/dev/null)" || unverifiable "cannot fetch golangci-lint ${tag} go.mod (offline?)"

have_fumpt="$(printf '%s\n' "$gomod" | awk '$1=="mvdan.cc/gofumpt" {print $2; exit}')"
have_tools="$(printf '%s\n' "$gomod" | awk '$1=="golang.org/x/tools" {print $2; exit}')"

[ -n "$have_fumpt" ] || fail "golangci-lint ${tag} go.mod has no mvdan.cc/gofumpt require — its vendoring layout changed; update this check"
[ -n "$have_tools" ] || fail "golangci-lint ${tag} go.mod has no golang.org/x/tools require — its vendoring layout changed; update this check"

[ "$want_fumpt" = "$have_fumpt" ] || \
  fail "GOFUMPT pin (${want_fumpt}) != gofumpt vendored by golangci-lint ${tag} (${have_fumpt}) — bump the Makefile pins to match"
[ "$want_tools" = "$have_tools" ] || \
  fail "GOIMPORTS pin (${want_tools}) != x/tools vendored by golangci-lint ${tag} (${have_tools}) — bump the Makefile pins to match"

echo "check-fmt-versions: gofumpt ${want_fumpt}, x/tools ${want_tools} match golangci-lint ${tag}"
