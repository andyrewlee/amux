#!/usr/bin/env bash
# Informational staleness tripwire for file-pinned dev/release tools.
# Prints "NOTE <tool>: pinned <pin>, latest <upstream>" for each pin it can
# resolve. Always exits 0 — a stale pin is not a merge blocker (pinned tools
# keep working); run `make ci-nightly` to see the report, and bump pins
# deliberately via the documented per-tool process.
set -uo pipefail

cd "$(dirname "$0")/.."

note() { echo "NOTE $1: pinned ${2:-?}, latest ${3:-unresolved}"; }

gh_latest() { # gh_latest <owner/repo> — latest release tag, or empty
	curl -fsSL --max-time 15 \
		"https://api.github.com/repos/$1/releases/latest" 2>/dev/null |
		sed -n 's/.*"tag_name": *"\([^"]*\)".*/\1/p' | head -1
}

go_latest() { # go_latest <module> — latest version from the Go proxy, or empty
	go list -m "$1@latest" 2>/dev/null | awk '{print $2}'
}

mk_pin() { # mk_pin <VAR> — read a FOO_VERSION ?= vX.Y.Z pin from the Makefile
	sed -n "s/^$1 ?= \(.*\)$/\1/p" Makefile | head -1
}

pin="$(cat .golangci-version 2>/dev/null)"
note golangci-lint "$pin" "$(gh_latest golangci/golangci-lint)"

pin="$(cat .goreleaser-version 2>/dev/null)"
note goreleaser "$pin" "$(gh_latest goreleaser/goreleaser)"

note govulncheck "$(mk_pin GOVULNCHECK_VERSION)" "$(go_latest golang.org/x/vuln)"
note air "$(mk_pin AIR_VERSION)" "$(go_latest github.com/air-verse/air)"

pin="$(sed -n 's/^MINISIGN_VERSION="\(.*\)"$/\1/p' scripts/install_minisign.sh | head -1)"
latest="$(gh_latest jedisct1/minisign)"
note minisign "$pin" "$latest"
