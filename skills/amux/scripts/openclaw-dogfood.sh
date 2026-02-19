#!/usr/bin/env bash
set -euo pipefail

SCRIPT_SOURCE="${BASH_SOURCE[0]:-$0}"
SCRIPT_DIR="$(cd "$(dirname "$SCRIPT_SOURCE")" >/dev/null 2>&1 && pwd -P)"
DX_SCRIPT="$SCRIPT_DIR/openclaw-dx.sh"

usage() {
  cat <<'EOF'
Usage:
  openclaw-dogfood.sh [--repo <path>] [--workspace <name>] [--assistant <name>] [--report-dir <path>] [--keep-temp] [--cleanup-temp]

Runs a real OpenClaw/amux dogfood flow end-to-end:
  - project add
  - start coding
  - continue coding
  - create second workspace + start
  - terminal run + logs
  - workflow dual
  - git ship
  - status
EOF
}

require_bin() {
  if ! command -v "$1" >/dev/null 2>&1; then
    echo "missing required binary: $1" >&2
    exit 1
  fi
}

shell_quote() {
  printf '%q' "$1"
}

REPO_PATH=""
WORKSPACE_NAME="mobile-dogfood"
ASSISTANT="codex"
REPORT_DIR=""
KEEP_TEMP=true

while [[ $# -gt 0 ]]; do
  case "$1" in
    --repo)
      REPO_PATH="$2"; shift 2 ;;
    --workspace)
      WORKSPACE_NAME="$2"; shift 2 ;;
    --assistant)
      ASSISTANT="$2"; shift 2 ;;
    --report-dir)
      REPORT_DIR="$2"; shift 2 ;;
    --keep-temp)
      KEEP_TEMP=true; shift ;;
    --cleanup-temp)
      KEEP_TEMP=false; shift ;;
    -h|--help)
      usage
      exit 0 ;;
    *)
      echo "unknown flag: $1" >&2
      usage
      exit 2 ;;
  esac
done

RUN_TAG="$(date +%m%d%H%M%S)-$RANDOM"
PRIMARY_WORKSPACE="${WORKSPACE_NAME}-${RUN_TAG}"
SECONDARY_WORKSPACE="${WORKSPACE_NAME}-parallel-${RUN_TAG}"

require_bin jq
require_bin git
require_bin amux
require_bin openclaw

if [[ ! -x "$DX_SCRIPT" ]]; then
  echo "missing executable script: $DX_SCRIPT" >&2
  exit 1
fi

TMP_ROOT=""
if [[ -z "${REPO_PATH// }" ]]; then
  TMP_ROOT="$(mktemp -d /tmp/amux-openclaw-dogfood-script.XXXXXX)"
  REPO_PATH="$TMP_ROOT/repo"
  mkdir -p "$REPO_PATH"
  cat >"$REPO_PATH/main.go" <<'EOF'
package main

import (
	"fmt"
	"time"
)

func main() {
	fmt.Printf("%s hello from openclaw dogfood\n", time.Now().Format("2006-01-02"))
}
EOF
  cat >"$REPO_PATH/README.md" <<'EOF'
# dogfood
EOF
  (
    cd "$REPO_PATH"
    git init -q
    git add .
    git -c user.name='Dogfood' -c user.email='dogfood@example.com' commit -qm 'init'
  )
fi

if [[ -z "${REPORT_DIR// }" ]]; then
  REPORT_DIR="$(mktemp -d /tmp/amux-openclaw-dogfood-report.XXXXXX)"
fi
mkdir -p "$REPORT_DIR"
DX_CONTEXT_FILE="$REPORT_DIR/openclaw-dx-context.json"

cleanup() {
  if [[ "$KEEP_TEMP" == "true" ]]; then
    return
  fi
  if [[ -n "${TMP_ROOT// }" && -d "$TMP_ROOT" ]]; then
    rm -rf "$TMP_ROOT"
  fi
}
trap cleanup EXIT

run_dx() {
  local slug="$1"
  shift
  local out_file="$REPORT_DIR/$slug.raw"
  local json_file="$REPORT_DIR/$slug.json"
  local status_file="$REPORT_DIR/$slug.status"
  local start_ts end_ts elapsed
  start_ts="$(date +%s)"
  local out
  out="$(OPENCLAW_DX_CONTEXT_FILE="$DX_CONTEXT_FILE" "$DX_SCRIPT" "$@" 2>&1 || true)"
  end_ts="$(date +%s)"
  elapsed="$((end_ts - start_ts))"
  printf '%s\n' "$out" >"$out_file"
  printf '%s\n' "$out" | tail -n 1 >"$json_file"
  if jq -e . >/dev/null 2>&1 <"$json_file"; then
    jq -r --arg elapsed "${elapsed}s" '.status + "|" + (.summary // "") + "|latency=" + $elapsed' <"$json_file" >"$status_file"
  else
    printf 'command_error|non-json terminal output|latency=%ss' "$elapsed" >"$status_file"
  fi
  printf '%s\t%s\n' "$slug" "$(cat "$status_file")"
}

run_openclaw_local_ping() {
  local slug="$1"
  local session_id="$2"
  local out_file="$REPORT_DIR/$slug.raw"
  local json_file="$REPORT_DIR/$slug.json"
  local status_file="$REPORT_DIR/$slug.status"
  local start_ts end_ts elapsed
  start_ts="$(date +%s)"
  local out
  out="$(openclaw agent --local --json --session-id "$session_id" --message "Dogfood ping: summarize current state in one line." 2>&1 || true)"
  end_ts="$(date +%s)"
  elapsed="$((end_ts - start_ts))"
  printf '%s\n' "$out" >"$out_file"
  printf '%s\n' "$out" | sed -n '/^{/,$p' >"$json_file"
  if jq -e . >/dev/null 2>&1 <"$json_file"; then
    jq -r --arg elapsed "${elapsed}s" '
      if ((.payloads // []) | length) > 0 then
        "ok|" + ((.payloads[0].text // "openclaw local ping completed") | gsub("[\r\n]+"; " ")) + "|latency=" + $elapsed
      elif (.status // "") | length > 0 then
        (.status + "|" + ((.summary // "") | gsub("[\r\n]+"; " ")) + "|latency=" + $elapsed)
      else
        "ok|openclaw local ping completed|latency=" + $elapsed
      end
    ' <"$json_file" >"$status_file"
  else
    printf 'command_error|non-json terminal output|latency=%ss' "$elapsed" >"$status_file"
  fi
  printf '%s\t%s\n' "$slug" "$(cat "$status_file")"
}

echo "dogfood_start repo=$(shell_quote "$REPO_PATH") report_dir=$(shell_quote "$REPORT_DIR")"
openclaw health --json >"$REPORT_DIR/openclaw-health.raw" 2>&1 || true

run_dx project_add project add --path "$REPO_PATH" --workspace "$PRIMARY_WORKSPACE" --assistant "$ASSISTANT"
WS1_ID="$(jq -r '.data.workspace.id // .data.workspace_id // .data.context.workspace.id // ""' <"$REPORT_DIR/project_add.json")"
if [[ -z "${WS1_ID// }" ]]; then
  echo "failed to resolve ws1 id from project_add" >&2
  exit 1
fi
run_openclaw_local_ping openclaw_local_ping "$WS1_ID"

run_dx start_ws1 start --workspace "$WS1_ID" --assistant "$ASSISTANT" --prompt "Update README with run instructions and add NOTES.md with one mobile DX tip." --max-steps 2 --turn-budget 120 --wait-timeout 80s --idle-threshold 10s
run_dx continue_ws1 continue --workspace "$WS1_ID" --text "Add one concise status line to NOTES.md and finish." --enter --max-steps 1 --turn-budget 90 --wait-timeout 70s --idle-threshold 10s

run_dx workspace2_create workspace create --name "$SECONDARY_WORKSPACE" --project "$REPO_PATH" --assistant "$ASSISTANT"
WS2_ID="$(jq -r '.data.workspace.id // .data.workspace_id // ""' <"$REPORT_DIR/workspace2_create.json")"
if [[ -n "${WS2_ID// }" ]]; then
  run_dx start_ws2 start --workspace "$WS2_ID" --assistant "$ASSISTANT" --prompt "Create TODO.md with three concise next steps for this repo." --max-steps 1 --turn-budget 90 --wait-timeout 70s --idle-threshold 10s
fi

run_dx terminal_run_ws1 terminal run --workspace "$WS1_ID" --text "go run main.go" --enter
sleep 1
run_dx terminal_logs_ws1 terminal logs --workspace "$WS1_ID" --lines 40

run_dx workflow_dual_ws1 workflow dual --workspace "$WS1_ID" --implement-assistant "$ASSISTANT" --implement-prompt "Make one small docs improvement in README." --review-assistant "$ASSISTANT" --review-prompt "Review for clarity and correctness." --max-steps 1 --turn-budget 100 --wait-timeout 70s --idle-threshold 10s

run_dx git_ship_ws1 git ship --workspace "$WS1_ID" --message "dogfood: scripted openclaw pass"
run_dx status_project status --project "$REPO_PATH" --capture-agents 8 --capture-lines 80

SUMMARY_FILE="$REPORT_DIR/summary.txt"
{
  echo "repo=$REPO_PATH"
  echo "report_dir=$REPORT_DIR"
  echo "dx_context_file=$DX_CONTEXT_FILE"
  echo "workspace_primary=$WS1_ID"
  echo "workspace_primary_name=$PRIMARY_WORKSPACE"
  if [[ -n "${WS2_ID// }" ]]; then
    echo "workspace_secondary=$WS2_ID"
    echo "workspace_secondary_name=$SECONDARY_WORKSPACE"
  fi
  echo "steps:"
  for f in "$REPORT_DIR"/*.status; do
    [[ -f "$f" ]] || continue
    echo "  $(basename "$f" .status): $(cat "$f")"
  done
} >"$SUMMARY_FILE"

echo "dogfood_complete summary_file=$(shell_quote "$SUMMARY_FILE")"
cat "$SUMMARY_FILE"
