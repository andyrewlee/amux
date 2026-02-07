#!/usr/bin/env bash

set -euo pipefail

if [ "$#" -ne 3 ]; then
	echo "usage: $0 <base-sha> <head-sha> <pr-body-file>" >&2
	exit 2
fi

base_sha="$1"
head_sha="$2"
body_file="$3"

if [ ! -f "$body_file" ]; then
	echo "error: PR body file not found: $body_file" >&2
	exit 2
fi

changed_files="$(git diff --name-only "${base_sha}...${head_sha}")"

declare -a missing=()

is_checked() {
	local id="$1"
	grep -Eiq "^[[:space:]]*-[[:space:]]*\\[[xX]\\][[:space:]]*${id}([[:space:]]|$)" "$body_file"
}

require_checked() {
	local id="$1"
	local reason="$2"
	if ! is_checked "$id"; then
		missing+=("${id} (${reason})")
	fi
}

changed_matches() {
	local pattern="$1"
	printf '%s\n' "$changed_files" | grep -Eq "$pattern"
}

require_checked "devcheck" "run make devcheck locally"
require_checked "lint_strict_new" "run make lint-strict-new locally"

if changed_matches '^(internal/ui/|internal/vterm/|cmd/amux-harness/)'; then
	require_checked "harness_presets" "UI/rendering paths changed"
fi

if changed_matches '^(internal/tmux/|internal/e2e/|internal/pty/)'; then
	require_checked "tmux_e2e" "tmux/pty/e2e paths changed"
fi

if changed_matches '^(\.golangci\.yml|\.golangci\.strict\.yml|LINTING\.md|Makefile|\.github/workflows/ci\.yml)$'; then
	require_checked "lint_policy_review" "lint policy or gates changed"
fi

if [ "${#missing[@]}" -gt 0 ]; then
	echo "Missing required PR checklist items:" >&2
	for item in "${missing[@]}"; do
		echo "  - $item" >&2
	done
	exit 1
fi

echo "PR checklist validation passed."
