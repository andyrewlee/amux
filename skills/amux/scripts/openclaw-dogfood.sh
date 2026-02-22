#!/usr/bin/env bash
set -euo pipefail

SCRIPT_SOURCE="${BASH_SOURCE[0]:-$0}"
SCRIPT_DIR="$(cd "$(dirname "$SCRIPT_SOURCE")" >/dev/null 2>&1 && pwd -P)"
REPO_ROOT="$(cd "$SCRIPT_DIR/../../.." >/dev/null 2>&1 && pwd -P)"
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

require_flag_value() {
  local flag="$1"
  local value="${2-}"
  if [[ -z "${value// }" || "$value" == --* ]]; then
    echo "missing value for $flag" >&2
    exit 2
  fi
}

REPO_PATH=""
WORKSPACE_NAME="mobile-dogfood"
ASSISTANT="codex"
REPORT_DIR=""
KEEP_TEMP=false
REPORT_DIR_CREATED=false

while [[ $# -gt 0 ]]; do
  case "$1" in
    --repo)
      require_flag_value "--repo" "${2-}"
      REPO_PATH="$2"; shift 2 ;;
    --workspace)
      require_flag_value "--workspace" "${2-}"
      WORKSPACE_NAME="$2"; shift 2 ;;
    --assistant)
      require_flag_value "--assistant" "${2-}"
      ASSISTANT="$2"; shift 2 ;;
    --report-dir)
      require_flag_value "--report-dir" "${2-}"
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
  TMP_ROOT="$(mktemp -d "${TMPDIR:-/tmp}/amux-openclaw-dogfood-script.XXXXXX")"
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
  REPORT_DIR="$(mktemp -d "${TMPDIR:-/tmp}/amux-openclaw-dogfood-report.XXXXXX")"
  REPORT_DIR_CREATED=true
fi
mkdir -p "$REPORT_DIR"
DX_CONTEXT_FILE="$REPORT_DIR/openclaw-dx-context.json"
CHANNEL_AGENT_CREATED=false
CHANNEL_AGENT_ID="${OPENCLAW_DOGFOOD_OPENCLAW_AGENT:-amux-dx}"
CHANNEL_AGENT_WORKSPACE="${OPENCLAW_DOGFOOD_CHANNEL_AGENT_WORKSPACE:-}"
CHANNEL_AGENT_MODEL="${OPENCLAW_DOGFOOD_CHANNEL_AGENT_MODEL:-openai-codex/gpt-5.3-codex}"
CHANNEL_EPHEMERAL_ENABLED="${OPENCLAW_DOGFOOD_CHANNEL_EPHEMERAL_AGENT:-true}"
CHANNEL_AGENT_ISOLATED_WORKSPACE="${OPENCLAW_DOGFOOD_CHANNEL_AGENT_ISOLATED_WORKSPACE:-true}"

prepare_channel_agent() {
  if [[ "$CHANNEL_EPHEMERAL_ENABLED" != "true" ]]; then
    export OPENCLAW_DOGFOOD_OPENCLAW_AGENT="$CHANNEL_AGENT_ID"
    return 0
  fi

  local base_id candidate add_json workspace_path
  base_id="${CHANNEL_AGENT_ID:-amux-dx}"
  candidate="${base_id}-dogfood-${RUN_TAG}"
  add_json="$REPORT_DIR/openclaw-channel-agent-add.json"
  workspace_path="$CHANNEL_AGENT_WORKSPACE"
  if [[ "$CHANNEL_AGENT_ISOLATED_WORKSPACE" == "true" ]]; then
    workspace_path="$REPORT_DIR/openclaw-channel-agent-workspace"
    mkdir -p "$workspace_path"
    cat >"$workspace_path/AGENTS.md" <<'EOF'
# AGENTS
- You are a strict terminal command runner for amux workflows.
- For command requests, execute the exact shell command via the exec tool.
- Return only raw stdout/stderr from that command.
- Do not summarize, paraphrase, or fabricate output.
- If execution did not happen, output exactly: EXEC_NOT_RUN
EOF
  fi
  if [[ -z "${workspace_path// }" ]]; then
    workspace_path="$REPORT_DIR/openclaw-channel-agent-workspace"
    mkdir -p "$workspace_path"
    cat >"$workspace_path/AGENTS.md" <<'EOF'
# AGENTS
- You are a strict terminal command runner for amux workflows.
- Execute exact shell commands and return only raw stdout/stderr.
EOF
  fi
  if openclaw agents add "$candidate" \
    --workspace "$workspace_path" \
    --model "$CHANNEL_AGENT_MODEL" \
    --non-interactive \
    --json >"$add_json" 2>&1; then
    CHANNEL_AGENT_ID="$candidate"
    CHANNEL_AGENT_CREATED=true
  fi
  export OPENCLAW_DOGFOOD_OPENCLAW_AGENT="$CHANNEL_AGENT_ID"
}

cleanup() {
  if [[ "$CHANNEL_AGENT_CREATED" == "true" ]]; then
    openclaw agents delete "$CHANNEL_AGENT_ID" --force --json >"$REPORT_DIR/openclaw-channel-agent-delete.json" 2>&1 || true
  fi
  if [[ "$KEEP_TEMP" == "true" ]]; then
    return
  fi
  if [[ -n "${TMP_ROOT// }" && -d "$TMP_ROOT" ]]; then
    rm -rf "$TMP_ROOT"
  fi
  if [[ "$REPORT_DIR_CREATED" == "true" && -n "${REPORT_DIR// }" && -d "$REPORT_DIR" ]]; then
    rm -rf "$REPORT_DIR"
  fi
}
trap cleanup EXIT

prepare_channel_agent

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
  printf '%s\n' "$out" | sed -n '/^[[:space:]]*{/,$p' >"$json_file"
  if ! jq -e . >/dev/null 2>&1 <"$json_file"; then
    printf '%s\n' "$out" | awk '/^[[:space:]]*\\{/{line=$0} END{print line}' >"$json_file"
  fi
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

run_openclaw_channel_command() {
  local slug="$1"
  local session_id="$2"
  local channel="$3"
  local command_text="$4"
  local expected_token="${5:-}"
  local retry_on_missing_markers="${6:-true}"
  local primary_agent="${OPENCLAW_DOGFOOD_OPENCLAW_AGENT:-amux-dx}"
  local fallback_agent="${OPENCLAW_DOGFOOD_CHANNEL_FALLBACK_AGENT:-main}"
  local agent_used="$primary_agent"
  local require_nonce="${OPENCLAW_DOGFOOD_CHANNEL_REQUIRE_NONCE:-false}"
  local require_proof="${OPENCLAW_DOGFOOD_CHANNEL_REQUIRE_PROOF:-true}"
  local nonce_token nonce_file
  local proof_token proof_file
  nonce_token=""
  nonce_file=""
  proof_token=""
  proof_file=""
  if [[ "$require_nonce" == "true" ]]; then
    nonce_token="$(date +%s)-$RANDOM-$RANDOM"
    nonce_file="$(mktemp "${TMPDIR:-/tmp}/openclaw-dogfood-nonce.XXXXXX")"
    printf '%s\n' "$nonce_token" >"$nonce_file"
  fi
  local out_file="$REPORT_DIR/$slug.raw"
  local json_file="$REPORT_DIR/$slug.json"
  local status_file="$REPORT_DIR/$slug.status"
  local start_ts end_ts elapsed
  start_ts="$(date +%s)"
  local message_text
  local command_with_nonce
  if [[ "$require_nonce" == "true" ]]; then
    command_with_nonce="cat $(shell_quote "$nonce_file"); $command_text"
  else
    command_with_nonce="$command_text"
  fi
  if [[ "$require_proof" == "true" ]]; then
    proof_token="proof-$(date +%s)-$RANDOM-$RANDOM"
    proof_file="$REPORT_DIR/$slug.proof"
    rm -f "$proof_file" >/dev/null 2>&1 || true
    command_with_nonce="$command_with_nonce; printf '%s\n' $(shell_quote "$proof_token") > $(shell_quote "$proof_file")"
  fi
  message_text=$'Run exactly this shell command.\nDo not substitute workspace IDs or paths.\nReturn only the raw command output.\n\n'"$command_with_nonce"
  local out
  run_channel_once() {
    local agent_id="$1"
    local sid="$2"
    local prompt="$3"
    openclaw agent --agent "$agent_id" --channel "$channel" --thinking off --session-id "$sid" --json --timeout "${OPENCLAW_DOGFOOD_CHANNEL_TIMEOUT_SECONDS:-180}" --message "$prompt" 2>&1 || true
  }
  render_channel_status() {
    local file_path="$1"
    local elapsed_label="$2"
    jq -r --arg elapsed "$elapsed_label" '
      ((.result.payloads[0].text // .payloads[0].text // "") | tostring) as $txt
      | ($txt | fromjson? // null) as $inner
      | if ($inner != null and ($inner | type) == "object" and (($inner.status // "") | length) > 0) then
          (($inner.status // "ok") + "|" + (($inner.summary // "openclaw channel command completed") | gsub("[\r\n]+"; " ")) + "|latency=" + $elapsed)
        elif ((.result.payloads // []) | length) > 0 then
          ((.status // "ok") + "|" + ((.result.payloads[0].text // "openclaw channel command completed") | gsub("[\r\n]+"; " ")) + "|latency=" + $elapsed)
        elif ((.payloads // []) | length) > 0 then
          ("ok|" + ((.payloads[0].text // "openclaw channel command completed") | gsub("[\r\n]+"; " ")) + "|latency=" + $elapsed)
        else
          ((.status // "ok") + "|" + ((.summary // "openclaw channel command completed") | gsub("[\r\n]+"; " ")) + "|latency=" + $elapsed)
        end
    ' <"$file_path"
  }

  out="$(run_channel_once "$agent_used" "$session_id" "$message_text")"
  if [[ "$agent_used" != "$fallback_agent" ]] && [[ "$out" == *"not found"* && "$out" == *"agent"* ]]; then
    agent_used="$fallback_agent"
    out="$(run_channel_once "$agent_used" "$session_id" "$message_text")"
  fi
  end_ts="$(date +%s)"
  elapsed="$((end_ts - start_ts))"
  printf '%s\n' "$out" >"$out_file"
  printf '%s\n' "$out" | sed -n '/^{/,$p' >"$json_file"
  if jq -e . >/dev/null 2>&1 <"$json_file"; then
    render_channel_status "$json_file" "${elapsed}s" >"$status_file"
  else
    printf 'command_error|non-json terminal output|latency=%ss' "$elapsed" >"$status_file"
  fi
  local out_blob missing_markers
  out_blob="$(cat "$json_file" 2>/dev/null || true)"
  missing_markers=false
  if [[ "$require_nonce" == "true" ]] && [[ "$out_blob" != *"$nonce_token"* ]]; then
    missing_markers=true
  fi
  if [[ -n "${expected_token// }" ]] && [[ "$out_blob" != *"$expected_token"* ]]; then
    missing_markers=true
  fi
  if [[ "$missing_markers" == "true" ]]; then
    if [[ "$retry_on_missing_markers" != "true" ]]; then
      printf 'attention|channel output missing execution markers|latency=%ss' "$elapsed" >"$status_file"
      printf '%s|agent=%s\n' "$(cat "$status_file")" "$agent_used" >"$status_file"
      if [[ -n "${nonce_file// }" ]]; then
        rm -f "$nonce_file" >/dev/null 2>&1 || true
      fi
      if [[ -n "${proof_file// }" ]]; then
        rm -f "$proof_file" >/dev/null 2>&1 || true
      fi
      printf '%s\t%s\n' "$slug" "$(cat "$status_file")"
      return
    fi
    local retry_sid retry_prompt retry_out retry_elapsed retry_json
    retry_sid="${session_id}-retry"
    retry_prompt="$message_text"$'\n\n'"Previous output was invalid because expected execution markers were missing. Run the exact command now and return only raw output."
    start_ts="$(date +%s)"
    retry_out="$(run_channel_once "$agent_used" "$retry_sid" "$retry_prompt")"
    retry_elapsed="$(( $(date +%s) - start_ts ))"
    printf '%s\n' "$retry_out" >>"$out_file"
    retry_json="$(printf '%s\n' "$retry_out" | sed -n '/^{/,$p')"
    if { [[ "$require_nonce" != "true" ]] || [[ "$retry_json" == *"$nonce_token"* ]]; } && { [[ -z "${expected_token// }" ]] || [[ "$retry_json" == *"$expected_token"* ]]; }; then
      printf '%s\n' "$retry_json" >"$json_file"
      elapsed=$((elapsed + retry_elapsed))
      if jq -e . >/dev/null 2>&1 <"$json_file"; then
        render_channel_status "$json_file" "${elapsed}s" >"$status_file"
      fi
    else
      local fallback_elapsed fallback_out fallback_json
      if [[ "$agent_used" != "$fallback_agent" ]]; then
        start_ts="$(date +%s)"
        fallback_out="$(run_channel_once "$fallback_agent" "${session_id}-fallback" "$retry_prompt")"
        fallback_elapsed="$(( $(date +%s) - start_ts ))"
        printf '%s\n' "$fallback_out" >>"$out_file"
        fallback_json="$(printf '%s\n' "$fallback_out" | sed -n '/^{/,$p')"
        if { [[ "$require_nonce" != "true" ]] || [[ "$fallback_json" == *"$nonce_token"* ]]; } && { [[ -z "${expected_token// }" ]] || [[ "$fallback_json" == *"$expected_token"* ]]; }; then
          agent_used="$fallback_agent"
          printf '%s\n' "$fallback_json" >"$json_file"
          elapsed=$((elapsed + retry_elapsed + fallback_elapsed))
          if jq -e . >/dev/null 2>&1 <"$json_file"; then
            render_channel_status "$json_file" "${elapsed}s" >"$status_file"
          fi
        else
          printf 'attention|channel output missing execution markers|latency=%ss' "$((elapsed + retry_elapsed + fallback_elapsed))" >"$status_file"
        fi
      else
        printf 'attention|channel output missing execution markers|latency=%ss' "$((elapsed + retry_elapsed))" >"$status_file"
      fi
    fi
  fi
  if [[ "$require_proof" == "true" ]]; then
    local proof_value
    proof_value=""
    if [[ -f "$proof_file" ]]; then
      proof_value="$(cat "$proof_file" 2>/dev/null || true)"
    fi
    if [[ "$proof_value" != "$proof_token" ]]; then
      printf 'attention|channel output unverified: command execution proof missing|latency=%ss' "$elapsed" >"$status_file"
    fi
  fi
  printf '%s|agent=%s\n' "$(cat "$status_file")" "$agent_used" >"$status_file"
  if [[ -n "${nonce_file// }" ]]; then
    rm -f "$nonce_file" >/dev/null 2>&1 || true
  fi
  if [[ -n "${proof_file// }" ]]; then
    rm -f "$proof_file" >/dev/null 2>&1 || true
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
CHANNEL_STATUS_TOKEN="ch-status-${RUN_TAG}-${WS1_ID}"
CHANNEL_STATUS_CMD="cd $(shell_quote "$REPO_ROOT") && $(shell_quote "$DX_SCRIPT") status --workspace $(shell_quote "$WS1_ID") | jq -c --arg token $(shell_quote "$CHANNEL_STATUS_TOKEN") --arg ws $(shell_quote "$WS1_ID") '{status:(.status // \"\"),summary:(.summary // \"\"),workspace:(.data.workspaces[0].id // .data.context.workspace.id // \"\"),dogfood_channel_status_token:\$token,dogfood_expected_workspace:\$ws}'"
run_openclaw_channel_command openclaw_channel_status "dogfood-channel-${WS1_ID}-$RUN_TAG" "${OPENCLAW_DOGFOOD_CHANNEL:-telegram}" "$CHANNEL_STATUS_CMD" "$CHANNEL_STATUS_TOKEN"

run_dx start_ws1 start --workspace "$WS1_ID" --assistant "$ASSISTANT" --prompt "Update README with run instructions and add NOTES.md with one mobile DX tip." --max-steps 2 --turn-budget 120 --wait-timeout 80s --idle-threshold 10s
run_dx continue_ws1 continue --workspace "$WS1_ID" --auto-start --text "Add one concise status line to NOTES.md and finish." --enter --max-steps 1 --turn-budget 90 --wait-timeout 70s --idle-threshold 10s

run_dx workspace2_create workspace create --name "$SECONDARY_WORKSPACE" --project "$REPO_PATH" --assistant "$ASSISTANT"
WS2_ID="$(jq -r '.data.workspace.id // .data.workspace_id // ""' <"$REPORT_DIR/workspace2_create.json")"
if [[ -n "${WS2_ID// }" ]]; then
  CHANNEL_WS2_CMD="cd $(shell_quote "$REPO_ROOT") && $(shell_quote "$DX_SCRIPT") terminal run --workspace $(shell_quote "$WS2_ID") --text \"echo channel-smoke > CHANNEL_SMOKE.txt\" --enter"
  run_openclaw_channel_command openclaw_channel_terminal_ws2 "dogfood-channel-ws2-${WS2_ID}-$RUN_TAG" "${OPENCLAW_DOGFOOD_CHANNEL:-telegram}" "$CHANNEL_WS2_CMD" "" false
  run_dx start_ws2 start --workspace "$WS2_ID" --assistant "$ASSISTANT" --prompt "Create TODO.md with three concise next steps for this repo." --max-steps 1 --turn-budget 90 --wait-timeout 70s --idle-threshold 10s
fi

run_dx terminal_run_ws1 terminal run --workspace "$WS1_ID" --text "go run main.go" --enter
sleep 1
run_dx terminal_logs_ws1 terminal logs --workspace "$WS1_ID" --lines 40

run_dx workflow_dual_ws1 workflow dual --workspace "$WS1_ID" --implement-assistant "$ASSISTANT" --implement-prompt "Append one concise mobile-coding tip to README.md and proceed even if there are unrelated uncommitted changes." --review-assistant "$ASSISTANT" --review-prompt "Review for clarity and correctness." --max-steps 1 --turn-budget 100 --wait-timeout 70s --idle-threshold 10s --auto-continue-impl true

run_dx git_ship_ws1 git ship --workspace "$WS1_ID" --message "dogfood: scripted openclaw pass"
run_dx status_ws1 status --workspace "$WS1_ID" --capture-agents 8 --capture-lines 80
if [[ -n "${WS2_ID// }" ]]; then
  run_dx status_ws2 status --workspace "$WS2_ID" --capture-agents 8 --capture-lines 80
fi
run_dx alerts_project alerts --project "$REPO_PATH" --capture-agents 8 --capture-lines 80

SUMMARY_FILE="$REPORT_DIR/summary.txt"
channel_unverified_count=0
if ls "$REPORT_DIR"/*.status >/dev/null 2>&1; then
  channel_unverified_count="$(grep -hE "channel output (unverified: command execution proof missing|missing execution markers)" "$REPORT_DIR"/*.status 2>/dev/null | wc -l | tr -d ' ' || true)"
  if [[ -z "${channel_unverified_count// }" ]]; then
    channel_unverified_count=0
  fi
fi
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
  echo "channel_unverified_count=$channel_unverified_count"
} >"$SUMMARY_FILE"

echo "dogfood_complete summary_file=$(shell_quote "$SUMMARY_FILE")"
cat "$SUMMARY_FILE"
if [[ "${OPENCLAW_DOGFOOD_REQUIRE_CHANNEL_EXECUTION:-true}" == "true" ]] && [[ "$channel_unverified_count" =~ ^[0-9]+$ ]] && [[ "$channel_unverified_count" -gt 0 ]]; then
  echo "dogfood_fail reason=channel_execution_unverified count=$channel_unverified_count"
  exit 2
fi
