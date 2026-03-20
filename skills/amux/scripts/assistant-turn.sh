#!/usr/bin/env bash
# assistant-turn.sh — compatibility wrapper for `amux assistant turn`.

set -euo pipefail

SCRIPT_SOURCE="${BASH_SOURCE[0]:-$0}"
SCRIPT_DIR="$(cd "$(dirname "$SCRIPT_SOURCE")" >/dev/null 2>&1 && pwd -P)"
PRESENT_SCRIPT="${AMUX_ASSISTANT_PRESENT_SCRIPT:-$SCRIPT_DIR/assistant-present.sh}"
ORIG_PWD="$(pwd -P)"

is_repo_root() {
  local dir="${1:-}"
  [[ -n "$dir" && -f "$dir/go.mod" && -d "$dir/cmd/amux" ]]
}

walk_to_repo_root() {
  local dir="${1:-}"
  [[ -n "$dir" ]] || return 1
  if [[ ! -d "$dir" ]]; then
    dir="$(dirname "$dir")"
  fi
  dir="$(cd "$dir" >/dev/null 2>&1 && pwd -P)" || return 1
  while true; do
    if is_repo_root "$dir"; then
      printf '%s\n' "$dir"
      return 0
    fi
    local parent
    parent="$(dirname "$dir")"
    [[ "$parent" != "$dir" ]] || break
    dir="$parent"
  done
  return 1
}

parent_cwd() {
  command -v lsof >/dev/null 2>&1 || return 1
  lsof -a -p "${PPID:-0}" -d cwd -Fn 2>/dev/null | sed -n 's/^n//p' | head -n 1
}

find_repo_root() {
  local candidate root
  local candidates=()

  if [[ -n "${AMUX_ASSISTANT_REPO_ROOT:-}" ]]; then
    candidates+=("${AMUX_ASSISTANT_REPO_ROOT}")
  fi
  candidates+=("$SCRIPT_DIR/../../..")
  candidates+=("$SCRIPT_DIR")
  candidates+=("$ORIG_PWD")
  if [[ -n "${PWD:-}" ]]; then
    candidates+=("${PWD}")
  fi
  if [[ -n "${OLDPWD:-}" ]]; then
    candidates+=("${OLDPWD}")
  fi
  if candidate="$(parent_cwd)"; [[ -n "${candidate:-}" ]]; then
    candidates+=("$candidate")
  fi

  for candidate in "${candidates[@]}"; do
    if root="$(walk_to_repo_root "$candidate")"; then
      printf '%s\n' "$root"
      return 0
    fi
  done
  return 1
}

REPO_ROOT="$(find_repo_root || true)"

# shellcheck source=lib/wrapper.sh
source "$SCRIPT_DIR/lib/wrapper.sh"
amux_discover_bin

run_native_from_amux() {
  "${AMUX_BIN:-amux}" assistant turn "$@"
}

run_native_from_checkout() {
  amux_run_from_checkout "$REPO_ROOT" "$ORIG_PWD" "assistant turn" \
    AMUX_ASSISTANT_REUSE_SELF_EXEC=true -- "$@"
}

run_native() {
  export AMUX_ASSISTANT_TURN_SCRIPT_DIR="$SCRIPT_DIR"
  export AMUX_ASSISTANT_TURN_STEP_CMD_REF="${AMUX_ASSISTANT_TURN_STEP_CMD_REF:-$SCRIPT_DIR/assistant-step.sh}"
  export AMUX_ASSISTANT_TURN_CMD_REF="${AMUX_ASSISTANT_TURN_CMD_REF:-$SCRIPT_DIR/assistant-turn.sh}"

  if [[ -n "${AMUX_ASSISTANT_NATIVE_BIN:-}" ]]; then
    "$AMUX_ASSISTANT_NATIVE_BIN" --cwd "$ORIG_PWD" assistant turn "$@"
    return
  fi
  if command -v go >/dev/null 2>&1 && [[ -n "$REPO_ROOT" ]] && is_repo_root "$REPO_ROOT"; then
    local status=0
    run_native_from_checkout "$@" || status=$?
    if [[ "$status" -eq 0 ]]; then
      return 0
    fi
    if [[ "$status" -ne 97 ]]; then
      return "$status"
    fi
  fi
  run_native_from_amux "$@"
}

if [[ "${AMUX_ASSISTANT_TURN_SKIP_PRESENT:-false}" == "true" || ! -x "$PRESENT_SCRIPT" ]]; then
  run_native "$@"
  exit $?
fi

run_native "$@" | "$PRESENT_SCRIPT"
