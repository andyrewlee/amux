#!/usr/bin/env bash
# lib/wrapper.sh — shared wrapper utilities for assistant scripts.
#
# Provides:
#   amux_discover_bin          — find AMUX_BIN if not already set
#   amux_go_run_should_fallback — check if go-run stderr indicates fallback needed
#   amux_run_from_checkout     — run a subcommand via `go run` from a repo checkout

# Discover AMUX_BIN if not set.
amux_discover_bin() {
  if [[ -n "${AMUX_BIN:-}" ]]; then
    return
  fi
  if command -v amux >/dev/null 2>&1; then
    export AMUX_BIN="$(command -v amux)"
  else
    IFS=':' read -r -a amux_bin_fallbacks <<<"${AMUX_BIN_FALLBACKS:-/usr/local/bin/amux:/opt/homebrew/bin/amux}"
    for candidate in "${amux_bin_fallbacks[@]}"; do
      if [[ -n "$candidate" && -x "$candidate" ]]; then
        export AMUX_BIN="$candidate"
        break
      fi
    done
  fi
}

# Check if go run output indicates a fallback is needed.
amux_go_run_should_fallback() {
  local stderr_file="${1:-}"
  if [[ -z "$stderr_file" || ! -f "$stderr_file" ]]; then
    return 1
  fi
  if sed \
    -e '/^exit status [0-9][0-9]*$/d' \
    -e '/^go: downloading /d' \
    -e '/^go: finding module for package /d' \
    -e '/^go: found /d' \
    "$stderr_file" | grep -Eiq \
    'cannot find main module|go\.mod file not found|main module .* does not contain package|no required module provides package|package .* is not in std|directory not found|no such file or directory|cannot load module|build constraints exclude all Go files'
  then
    return 0
  fi
  return 1
}

# Run via `go run` from a repo checkout.
# Usage: amux_run_from_checkout REPO_ROOT ORIG_PWD SUBCOMMAND [EXTRA_ENV...] -- [ARGS...]
#
# SUBCOMMAND is the full subcommand string, e.g. "assistant step".
# EXTRA_ENV entries are KEY=VALUE strings passed to env(1) before `go run`.
# Everything after "--" is forwarded as arguments to the subcommand.
# Returns 0 on success, 97 if fallback is needed, or the go-run exit status.
amux_run_from_checkout() {
  local repo_root="$1" orig_pwd="$2" subcmd="$3"
  shift 3
  local extra_env=()
  while [[ $# -gt 0 && "$1" != "--" ]]; do
    extra_env+=("$1")
    shift
  done
  [[ "${1:-}" == "--" ]] && shift

  local stderr_file status
  stderr_file="$(mktemp)"
  if (
    cd "$repo_root"
    # Use ${arr[@]+...} to safely expand empty arrays under set -u (bash 3.2).
    env ${extra_env[@]+"${extra_env[@]}"} \
      go run ./cmd/amux --cwd "$orig_pwd" $subcmd "$@" 2>"$stderr_file"
  ); then
    sed '/^exit status [0-9][0-9]*$/d' "$stderr_file" >&2
    rm -f "$stderr_file"
    return 0
  fi
  status=$?
  sed '/^exit status [0-9][0-9]*$/d' "$stderr_file" >&2
  if amux_go_run_should_fallback "$stderr_file"; then
    rm -f "$stderr_file"
    return 97
  fi
  rm -f "$stderr_file"
  return "$status"
}
