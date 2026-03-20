#!/usr/bin/env bash
# assistant-present.sh — compatibility wrapper for `amux assistant present`.

set -euo pipefail

SCRIPT_SOURCE="${BASH_SOURCE[0]:-$0}"
SCRIPT_DIR="$(cd "$(dirname "$SCRIPT_SOURCE")" >/dev/null 2>&1 && pwd -P)"
REPO_ROOT="$(cd "$SCRIPT_DIR/../../.." >/dev/null 2>&1 && pwd -P)"
ORIG_PWD="$(pwd -P)"
export AMUX_ASSISTANT_PRESENT_CMD_REF="${AMUX_ASSISTANT_PRESENT_CMD_REF:-skills/amux/scripts/assistant-present.sh}"

# shellcheck source=lib/wrapper.sh
source "$SCRIPT_DIR/lib/wrapper.sh"
amux_discover_bin

run_native_from_amux() {
  "${AMUX_BIN:-amux}" assistant present "$@"
}

run_native_from_checkout() {
  amux_run_from_checkout "$REPO_ROOT" "$ORIG_PWD" "assistant present" \
    -- "$@"
}

run_native() {
  if [[ -n "${AMUX_ASSISTANT_NATIVE_BIN:-}" ]]; then
    exec "$AMUX_ASSISTANT_NATIVE_BIN" --cwd "$ORIG_PWD" assistant present "$@"
  fi
  if command -v go >/dev/null 2>&1 && [[ -f "$REPO_ROOT/go.mod" && -d "$REPO_ROOT/cmd/amux" ]]; then
    local status=0
    run_native_from_checkout "$@" || status=$?
    if [[ "$status" -eq 0 ]]; then
      return 0
    fi
    if [[ "$status" -ne 97 ]]; then
      return "$status"
    fi
  fi
  exec "${AMUX_BIN:-amux}" assistant present "$@"
}

run_native "$@"
