#!/usr/bin/env bash
# openclaw-turn.sh — Multi-step bounded OpenClaw coding turn for amux.
#
# Usage:
#   openclaw-turn.sh run  --workspace <id> --assistant <name> --prompt <text> [--max-steps 3] [--turn-budget 180] [--wait-timeout 60s] [--idle-threshold 10s]
#   openclaw-turn.sh send --agent <id> --text <text> [--enter] [--max-steps 3] [--turn-budget 180] [--wait-timeout 60s] [--idle-threshold 10s]
#
# Behavior:
# - Executes bounded steps via openclaw-step.sh.
# - Coalesces duplicate milestone summaries by default.
# - Stops on needs_input/session_exited, repeated timeouts, step cap, or turn budget.
# - Emits a final normalized JSON object with events/milestones/next action.

set -euo pipefail

usage() {
  cat >&2 <<'EOF'
Usage:
  openclaw-turn.sh run  --workspace <id> --assistant <name> --prompt <text> [--max-steps 3] [--turn-budget 180] [--wait-timeout 60s] [--idle-threshold 10s]
  openclaw-turn.sh send --agent <id> --text <text> [--enter] [--max-steps 3] [--turn-budget 180] [--wait-timeout 60s] [--idle-threshold 10s]
EOF
}

shell_quote() {
  printf '%q' "$1"
}

duration_to_seconds() {
  local value="$1"
  local fallback="$2"
  if [[ "$value" =~ ^[[:space:]]*([0-9]+)[[:space:]]*$ ]]; then
    echo "${BASH_REMATCH[1]}"
    return
  fi
  if [[ "$value" =~ ^[[:space:]]*([0-9]+)s[[:space:]]*$ ]]; then
    echo "${BASH_REMATCH[1]}"
    return
  fi
  if [[ "$value" =~ ^[[:space:]]*([0-9]+)m[[:space:]]*$ ]]; then
    echo "$(( ${BASH_REMATCH[1]} * 60 ))"
    return
  fi
  if [[ "$value" =~ ^[[:space:]]*([0-9]+)h[[:space:]]*$ ]]; then
    echo "$(( ${BASH_REMATCH[1]} * 3600 ))"
    return
  fi
  echo "$fallback"
}

normalize_verbosity_level() {
  local value="${1:-normal}"
  case "$value" in
    quiet|normal|detailed)
      printf '%s' "$value"
      ;;
    *)
      printf 'normal'
      ;;
  esac
}

normalize_inline_buttons_scope() {
  local value="${1:-allowlist}"
  case "$value" in
    off|dm|group|all|allowlist)
      printf '%s' "$value"
      ;;
    *)
      printf 'allowlist'
      ;;
  esac
}

redact_secrets_text() {
  local input="$1"
  printf '%s' "$input" | sed -E \
    -e 's/(sk-ant-api[0-9]*-[A-Za-z0-9_-]{10})[A-Za-z0-9_-]*/\1***/g' \
    -e 's/(sk-[A-Za-z0-9_-]{20})[A-Za-z0-9_-]*/\1***/g' \
    -e 's/(ghp_[A-Za-z0-9]{5})[A-Za-z0-9]*/\1***/g' \
    -e 's/(gho_[A-Za-z0-9]{5})[A-Za-z0-9]*/\1***/g' \
    -e 's/(github_pat_[A-Za-z0-9_]{5})[A-Za-z0-9_]*/\1***/g' \
    -e 's/(xoxb-[A-Za-z0-9]{5})[A-Za-z0-9-]*/\1***/g' \
    -e 's/(AKIA[0-9A-Z]{4})[0-9A-Z]{12}/\1************/g' \
    -e 's/(Bearer )[A-Za-z0-9+/_=.-]{8,}/\1***/g' \
    -e 's/((TOKEN|SECRET|PASSWORD|API_KEY|APIKEY|AUTH_TOKEN|PRIVATE_KEY|ACCESS_KEY|CLIENT_SECRET|WEBHOOK_SECRET)=)[^[:space:]'"'"'"]{8,}/\1***/g'
}

line_has_file_signal() {
  local value="$1"
  case "$value" in
    *".go"*|*".md"*|*".sh"*|*"internal/"*|*"cmd/"*|*"skills/"*|*"README."*|*"Makefile"*)
      return 0
      ;;
    *)
      return 1
      ;;
  esac
}

workspace_root_for_turn() {
  local workspace_id="$1"
  if [[ -z "$workspace_id" ]]; then
    printf ''
    return 0
  fi
  if ! command -v amux >/dev/null 2>&1 || ! command -v jq >/dev/null 2>&1; then
    printf ''
    return 0
  fi
  local ws_json root
  ws_json="$(amux --json workspace list --archived 2>/dev/null || true)"
  if ! jq -e '.ok == true' >/dev/null 2>&1 <<<"$ws_json"; then
    printf ''
    return 0
  fi
  root="$(jq -r --arg id "$workspace_id" '.data // [] | map(select(.id == $id)) | .[0].root // ""' <<<"$ws_json")"
  printf '%s' "$root"
}

if [[ $# -lt 1 ]]; then
  usage
  exit 2
fi

MODE="$1"
shift

WAIT_TIMEOUT="60s"
IDLE_THRESHOLD="10s"
MAX_STEPS="${OPENCLAW_TURN_MAX_STEPS:-3}"
TURN_BUDGET_SECONDS="${OPENCLAW_TURN_BUDGET_SECONDS:-180}"
FOLLOWUP_TEXT="${OPENCLAW_TURN_FOLLOWUP_TEXT:-Continue from current state and provide a concise status update and next action.}"
TIMEOUT_STREAK_LIMIT="${OPENCLAW_TURN_TIMEOUT_STREAK_LIMIT:-2}"
COALESCE_MILESTONES="${OPENCLAW_TURN_COALESCE_MILESTONES:-true}"
FINAL_RESERVE_SECONDS="${OPENCLAW_TURN_FINAL_RESERVE_SECONDS:-20}"
OPENCLAW_CHUNK_CHARS="${OPENCLAW_TURN_CHUNK_CHARS:-1200}"
TURN_VERBOSITY="$(normalize_verbosity_level "${OPENCLAW_TURN_VERBOSITY:-normal}")"

SCRIPT_SOURCE="${BASH_SOURCE[0]:-$0}"
SCRIPT_DIR="$(cd "$(dirname "$SCRIPT_SOURCE")" >/dev/null 2>&1 && pwd -P)"
SCRIPT_PATH="$SCRIPT_DIR/$(basename "$SCRIPT_SOURCE")"
TURN_SCRIPT_REF="${OPENCLAW_TURN_CMD_REF:-skills/amux/scripts/openclaw-turn.sh}"
TURN_SCRIPT_CMD="$(shell_quote "$TURN_SCRIPT_REF")"
STEP_SCRIPT="${OPENCLAW_TURN_STEP_SCRIPT:-$SCRIPT_DIR/openclaw-step.sh}"
if [[ ! -x "$STEP_SCRIPT" ]]; then
  STEP_SCRIPT="$SCRIPT_DIR/openclaw-step.sh"
fi
STEP_SCRIPT_REF="${OPENCLAW_TURN_STEP_CMD_REF:-skills/amux/scripts/openclaw-step.sh}"
STEP_SCRIPT_CMD="$(shell_quote "$STEP_SCRIPT_REF")"
OPENCLAW_PRESENT_SCRIPT="${OPENCLAW_PRESENT_SCRIPT:-$SCRIPT_DIR/openclaw-present.sh}"

WORKSPACE=""
ASSISTANT=""
PROMPT=""
AGENT_ID=""
TEXT=""
ENTER=false

while [[ $# -gt 0 ]]; do
  case "$1" in
    --wait-timeout)
      WAIT_TIMEOUT="$2"; shift 2 ;;
    --idle-threshold)
      IDLE_THRESHOLD="$2"; shift 2 ;;
    --max-steps)
      MAX_STEPS="$2"; shift 2 ;;
    --turn-budget)
      TURN_BUDGET_SECONDS="$2"; shift 2 ;;
    --followup-text)
      FOLLOWUP_TEXT="$2"; shift 2 ;;
    --workspace)
      WORKSPACE="$2"; shift 2 ;;
    --assistant)
      ASSISTANT="$2"; shift 2 ;;
    --prompt)
      PROMPT="$2"; shift 2 ;;
    --agent)
      AGENT_ID="$2"; shift 2 ;;
    --text)
      TEXT="$2"; shift 2 ;;
    --enter)
      ENTER=true; shift ;;
    *)
      usage
      echo "unknown flag: $1" >&2
      exit 2 ;;
  esac
done

if ! command -v jq >/dev/null 2>&1; then
  echo '{"ok":false,"status":"command_error","summary":"jq is required","error":"missing binary: jq"}'
  exit 0
fi

if [[ ! -x "$STEP_SCRIPT" ]]; then
  echo '{"ok":false,"status":"command_error","summary":"openclaw-step.sh is not executable","error":"invalid step script path"}'
  exit 0
fi

if ! [[ "$MAX_STEPS" =~ ^[0-9]+$ ]] || [[ "$MAX_STEPS" -le 0 ]]; then
  MAX_STEPS=3
fi
if ! [[ "$TIMEOUT_STREAK_LIMIT" =~ ^[0-9]+$ ]] || [[ "$TIMEOUT_STREAK_LIMIT" -le 0 ]]; then
  TIMEOUT_STREAK_LIMIT=2
fi
if ! [[ "$FINAL_RESERVE_SECONDS" =~ ^[0-9]+$ ]] || [[ "$FINAL_RESERVE_SECONDS" -lt 0 ]]; then
  FINAL_RESERVE_SECONDS=20
fi
if ! [[ "$OPENCLAW_CHUNK_CHARS" =~ ^[0-9]+$ ]] || [[ "$OPENCLAW_CHUNK_CHARS" -le 0 ]]; then
  OPENCLAW_CHUNK_CHARS=1200
fi
INLINE_BUTTONS_SCOPE="$(normalize_inline_buttons_scope "${OPENCLAW_INLINE_BUTTONS_SCOPE:-allowlist}")"
INLINE_BUTTONS_ENABLED=true
if [[ "$INLINE_BUTTONS_SCOPE" == "off" ]]; then
  INLINE_BUTTONS_ENABLED=false
fi

TURN_BUDGET_SECONDS="$(duration_to_seconds "$TURN_BUDGET_SECONDS" 180)"
if ! [[ "$TURN_BUDGET_SECONDS" =~ ^[0-9]+$ ]] || [[ "$TURN_BUDGET_SECONDS" -le 0 ]]; then
  TURN_BUDGET_SECONDS=180
fi

case "$MODE" in
  run)
    if [[ -z "$WORKSPACE" || -z "$ASSISTANT" || -z "$PROMPT" ]]; then
      usage
      echo '{"ok":false,"status":"command_error","summary":"Missing required flags","error":"run requires --workspace, --assistant, --prompt"}'
      exit 0
    fi
    ;;
  send)
    if [[ -z "$AGENT_ID" || -z "$TEXT" ]]; then
      usage
      echo '{"ok":false,"status":"command_error","summary":"Missing required flags","error":"send requires --agent and --text"}'
      exit 0
    fi
    ;;
  *)
    usage
    echo '{"ok":false,"status":"command_error","summary":"Invalid mode","error":"mode must be run or send"}'
    exit 0
    ;;
esac

TURN_ID="tgturn-$(date +%s)-$$"
START_TS="$(date +%s)"
STEPS_USED=0
TIMEOUT_STREAK=0
BUDGET_EXHAUSTED=false

EVENTS_JSON='[]'
MILESTONES_JSON='[]'
LAST_MILESTONE_SUMMARY=""

CURRENT_MODE="$MODE"
CURRENT_WORKSPACE="$WORKSPACE"
CURRENT_ASSISTANT="$ASSISTANT"
CURRENT_PROMPT="$PROMPT"
CURRENT_AGENT="$AGENT_ID"
CURRENT_TEXT="$TEXT"
CURRENT_ENTER="$ENTER"

LAST_STEP_JSON='{}'
LAST_STATUS="unknown"
LAST_SUMMARY=""
LAST_NEXT_ACTION=""
LAST_SUGGESTED_COMMAND=""
LAST_AGENT_ID="$AGENT_ID"
LAST_WORKSPACE_ID="$WORKSPACE"
LAST_ASSISTANT_OUT="$ASSISTANT"
LAST_SUBSTANTIVE_OUTPUT=false
LAST_NEEDS_INPUT=false
STEP_EVENT_JSON='{}'

while [[ "$STEPS_USED" -lt "$MAX_STEPS" ]]; do
  NOW_TS="$(date +%s)"
  ELAPSED="$((NOW_TS - START_TS))"
  REMAINING="$((TURN_BUDGET_SECONDS - ELAPSED))"
  # Always allow at least one step even on tight budgets.
  if [[ "$REMAINING" -le "$FINAL_RESERVE_SECONDS" && "$STEPS_USED" -gt 0 ]]; then
    BUDGET_EXHAUSTED=true
    break
  fi

  STEP_INDEX="$((STEPS_USED + 1))"
  STEP_IDEMPOTENCY_KEY="${TURN_ID}-step-${STEP_INDEX}"

  if [[ "$CURRENT_MODE" == "run" ]]; then
    STEP_JSON="$(OPENCLAW_STEP_SKIP_PRESENT=true "$STEP_SCRIPT" run \
      --workspace "$CURRENT_WORKSPACE" \
      --assistant "$CURRENT_ASSISTANT" \
      --prompt "$CURRENT_PROMPT" \
      --wait-timeout "$WAIT_TIMEOUT" \
      --idle-threshold "$IDLE_THRESHOLD" \
      --idempotency-key "$STEP_IDEMPOTENCY_KEY")"
  else
    STEP_ARGS=(
      "$STEP_SCRIPT" send
      --agent "$CURRENT_AGENT"
      --text "$CURRENT_TEXT"
      --wait-timeout "$WAIT_TIMEOUT"
      --idle-threshold "$IDLE_THRESHOLD"
      --idempotency-key "$STEP_IDEMPOTENCY_KEY"
    )
    if [[ "$CURRENT_ENTER" == "true" ]]; then
      STEP_ARGS+=(--enter)
    fi
    STEP_JSON="$(OPENCLAW_STEP_SKIP_PRESENT=true "${STEP_ARGS[@]}")"
  fi

  if ! jq -e . >/dev/null 2>&1 <<<"$STEP_JSON"; then
    STEP_JSON="$(jq -cn --arg raw "$STEP_JSON" '{
      ok: false, status: "command_error",
      summary: "Step script produced invalid JSON output.",
      raw_output: ($raw | .[0:2000])
    }')"
  fi

  STEP_EVENT_JSON="$(jq -c '{
    ok: (.ok // false),
    mode: (.mode // ""),
    status: (.status // "unknown"),
    summary: (.summary // ""),
    next_action: (.next_action // ""),
    suggested_command: (.suggested_command // ""),
    agent_id: (.agent_id // ""),
    workspace_id: (.workspace_id // ""),
    assistant: (.assistant // ""),
    response: {
      substantive_output: (.response.substantive_output // false),
      needs_input: (.response.needs_input // false),
      timed_out: (.response.timed_out // false),
      session_exited: (.response.session_exited // false),
      changed: (.response.changed // false)
    }
  }' <<<"$STEP_JSON")"

  STEPS_USED="$STEP_INDEX"
  LAST_STEP_JSON="$STEP_JSON"
  EVENTS_JSON="$(jq -cn --argjson events "$EVENTS_JSON" --argjson step "$STEP_EVENT_JSON" '$events + [$step]')"

  LAST_STATUS="$(jq -r '.status // "unknown"' <<<"$STEP_JSON")"
  LAST_SUMMARY="$(jq -r '.summary // ""' <<<"$STEP_JSON")"
  LAST_NEXT_ACTION="$(jq -r '.next_action // ""' <<<"$STEP_JSON")"
  LAST_SUGGESTED_COMMAND="$(jq -r '.suggested_command // ""' <<<"$STEP_JSON")"
  LAST_AGENT_ID="$(jq -r '.agent_id // empty' <<<"$STEP_JSON")"
  LAST_WORKSPACE_ID="$(jq -r '.workspace_id // empty' <<<"$STEP_JSON")"
  LAST_ASSISTANT_OUT="$(jq -r '.assistant // empty' <<<"$STEP_JSON")"
  LAST_SUBSTANTIVE_OUTPUT="$(jq -r '.response.substantive_output // false' <<<"$STEP_JSON")"
  LAST_NEEDS_INPUT="$(jq -r '.response.needs_input // false' <<<"$STEP_JSON")"
  LAST_SUMMARY="$(redact_secrets_text "$LAST_SUMMARY")"
  LAST_NEXT_ACTION="$(redact_secrets_text "$LAST_NEXT_ACTION")"
  LAST_SUGGESTED_COMMAND="$(redact_secrets_text "$LAST_SUGGESTED_COMMAND")"

  ADD_MILESTONE=true
  if [[ "$COALESCE_MILESTONES" == "true" && -n "$LAST_SUMMARY" && "$LAST_SUMMARY" == "$LAST_MILESTONE_SUMMARY" ]]; then
    ADD_MILESTONE=false
  fi
  if [[ "$ADD_MILESTONE" == "true" ]]; then
    MILESTONES_JSON="$(
      jq -cn \
        --argjson milestones "$MILESTONES_JSON" \
        --argjson step "$STEP_INDEX" \
        --arg status "$LAST_STATUS" \
        --arg summary "$LAST_SUMMARY" \
        --arg next_action "$LAST_NEXT_ACTION" \
        --arg suggested_command "$LAST_SUGGESTED_COMMAND" \
        '$milestones + [{
          step: $step,
          status: $status,
          summary: $summary,
          next_action: $next_action,
          suggested_command: $suggested_command
        }]'
    )"
    LAST_MILESTONE_SUMMARY="$LAST_SUMMARY"
  fi

  if [[ "$LAST_STATUS" == "timed_out" ]]; then
    TIMEOUT_STREAK="$((TIMEOUT_STREAK + 1))"
  else
    TIMEOUT_STREAK=0
  fi

  if [[ "$LAST_STATUS" == "needs_input" || "$LAST_NEEDS_INPUT" == "true" ]]; then
    break
  fi
  if [[ "$LAST_STATUS" == "session_exited" ]]; then
    break
  fi
  if [[ "$LAST_STATUS" == "idle" && "$LAST_SUBSTANTIVE_OUTPUT" == "true" ]]; then
    break
  fi
  if [[ "$TIMEOUT_STREAK" -ge "$TIMEOUT_STREAK_LIMIT" ]]; then
    break
  fi
  if [[ -z "$LAST_AGENT_ID" ]]; then
    break
  fi

  CURRENT_MODE="send"
  CURRENT_AGENT="$LAST_AGENT_ID"
  CURRENT_TEXT="$FOLLOWUP_TEXT"
  CURRENT_ENTER=true
done

END_TS="$(date +%s)"
ELAPSED_FINAL="$((END_TS - START_TS))"

OVERALL_STATUS="partial"
if [[ "$LAST_STATUS" == "idle" && "$LAST_SUBSTANTIVE_OUTPUT" == "true" ]]; then
  OVERALL_STATUS="completed"
elif [[ "$LAST_STATUS" == "needs_input" || "$LAST_NEEDS_INPUT" == "true" ]]; then
  OVERALL_STATUS="needs_input"
elif [[ "$LAST_STATUS" == "session_exited" ]]; then
  OVERALL_STATUS="session_exited"
elif [[ "$LAST_STATUS" == "timed_out" ]]; then
  OVERALL_STATUS="timed_out"
fi
if [[ "$BUDGET_EXHAUSTED" == "true" && "$OVERALL_STATUS" != "completed" ]]; then
  OVERALL_STATUS="partial_budget"
fi

FINAL_SUMMARY="$LAST_SUMMARY"
if [[ -z "${FINAL_SUMMARY// }" ]]; then
  FINAL_SUMMARY="Turn ended with status: $LAST_STATUS."
fi
if [[ "$OVERALL_STATUS" == "completed" ]]; then
  FINAL_SUMMARY="Completed in $STEPS_USED step(s). $FINAL_SUMMARY"
else
  FINAL_SUMMARY="Partial after $STEPS_USED step(s). $FINAL_SUMMARY"
fi
if [[ "$OVERALL_STATUS" == "completed" && -n "$LAST_WORKSPACE_ID" ]] && line_has_file_signal "$LAST_SUMMARY"; then
  WORKSPACE_ROOT_CANDIDATE="$(workspace_root_for_turn "$LAST_WORKSPACE_ID")"
  if [[ -n "$WORKSPACE_ROOT_CANDIDATE" && -d "$WORKSPACE_ROOT_CANDIDATE" ]]; then
    if git -C "$WORKSPACE_ROOT_CANDIDATE" rev-parse --is-inside-work-tree >/dev/null 2>&1; then
      PORCELAIN_STATUS="$(git -C "$WORKSPACE_ROOT_CANDIDATE" status --porcelain --untracked-files=all 2>/dev/null || true)"
      if [[ -z "${PORCELAIN_STATUS// }" ]]; then
        OVERALL_STATUS="partial"
        FINAL_SUMMARY="Partial after $STEPS_USED step(s). Claimed file updates, but no workspace changes were detected."
        LAST_NEXT_ACTION="Ask for exact changed files and apply the requested edits."
        if [[ -n "$LAST_AGENT_ID" ]]; then
          LAST_SUGGESTED_COMMAND="$TURN_SCRIPT_CMD send --agent $(shell_quote "$LAST_AGENT_ID") --text \"List exact files changed and apply the missing edits now.\" --enter --max-steps 2 --turn-budget 180 --wait-timeout 60s --idle-threshold 10s"
        fi
      fi
    fi
  fi
fi
FINAL_SUMMARY="$(redact_secrets_text "$FINAL_SUMMARY")"
LAST_NEXT_ACTION="$(redact_secrets_text "$LAST_NEXT_ACTION")"
LAST_SUGGESTED_COMMAND="$(redact_secrets_text "$LAST_SUGGESTED_COMMAND")"
if [[ "$MAX_STEPS" -gt 0 ]]; then
  STEP_PROGRESS_PERCENT="$((STEPS_USED * 100 / MAX_STEPS))"
else
  STEP_PROGRESS_PERCENT=0
fi

STATUS_EMOJI="ℹ️"
case "$OVERALL_STATUS" in
  completed) STATUS_EMOJI="✅" ;;
  needs_input) STATUS_EMOJI="❓" ;;
  timed_out|partial_budget) STATUS_EMOJI="⏱️" ;;
  session_exited) STATUS_EMOJI="🛑" ;;
esac

OPENCLAW_MESSAGE="$STATUS_EMOJI $FINAL_SUMMARY"
case "$TURN_VERBOSITY" in
  quiet)
    ;;
  normal)
    if [[ -n "${LAST_NEXT_ACTION// }" ]]; then
      OPENCLAW_MESSAGE+=$'\n'"Next: $LAST_NEXT_ACTION"
    fi
    if [[ -n "${LAST_SUGGESTED_COMMAND// }" ]]; then
      OPENCLAW_MESSAGE+=$'\n'"Command: $LAST_SUGGESTED_COMMAND"
    fi
    OPENCLAW_MESSAGE+=$'\n'"Progress: $STEPS_USED/$MAX_STEPS steps ($STEP_PROGRESS_PERCENT%)"
    ;;
  detailed)
    if [[ -n "${LAST_NEXT_ACTION// }" ]]; then
      OPENCLAW_MESSAGE+=$'\n'"Next: $LAST_NEXT_ACTION"
    fi
    if [[ -n "${LAST_SUGGESTED_COMMAND// }" ]]; then
      OPENCLAW_MESSAGE+=$'\n'"Command: $LAST_SUGGESTED_COMMAND"
    fi
    OPENCLAW_MESSAGE+=$'\n'"Progress: $STEPS_USED/$MAX_STEPS steps ($STEP_PROGRESS_PERCENT%)"
    OPENCLAW_MESSAGE+=$'\n'"Meta: elapsed=${ELAPSED_FINAL}s budget=${TURN_BUDGET_SECONDS}s timeout_streak=${TIMEOUT_STREAK}/${TIMEOUT_STREAK_LIMIT}"
    ;;
esac

TURN_CONTEXT_LOWER="$(printf '%s\n%s\n%s' "$FINAL_SUMMARY" "$LAST_NEXT_ACTION" "$LAST_SUGGESTED_COMMAND" | tr '[:upper:]' '[:lower:]')"
TURN_TEST_REMEDIATION_COMMAND=""
TURN_LINT_REMEDIATION_COMMAND=""
TURN_SECURITY_REVIEW_COMMAND=""
TURN_REVIEW_CHANGES_COMMAND=""
if [[ -n "$LAST_AGENT_ID" ]]; then
  if [[ "$TURN_CONTEXT_LOWER" == *"test"* ]] && [[ "$TURN_CONTEXT_LOWER" == *"fail"* || "$TURN_CONTEXT_LOWER" == *"panic"* || "$TURN_CONTEXT_LOWER" == *"error"* ]]; then
    TURN_TEST_REMEDIATION_COMMAND="$TURN_SCRIPT_CMD send --agent $(shell_quote "$LAST_AGENT_ID") --text \"Investigate failing tests, fix root causes, and report changed files plus exact test command/results.\" --enter --max-steps 2 --turn-budget 180 --wait-timeout 60s --idle-threshold 10s"
  fi
  if [[ "$TURN_CONTEXT_LOWER" == *"lint"* || "$TURN_CONTEXT_LOWER" == *"format"* || "$TURN_CONTEXT_LOWER" == *"gofumpt"* || "$TURN_CONTEXT_LOWER" == *"style"* ]]; then
    TURN_LINT_REMEDIATION_COMMAND="$TURN_SCRIPT_CMD send --agent $(shell_quote "$LAST_AGENT_ID") --text \"Resolve lint and formatting issues, then provide a concise summary of fixes.\" --enter --max-steps 2 --turn-budget 180 --wait-timeout 60s --idle-threshold 10s"
  fi
  if [[ "$TURN_CONTEXT_LOWER" == *"secret"* || "$TURN_CONTEXT_LOWER" == *"token"* || "$TURN_CONTEXT_LOWER" == *"credential"* || "$TURN_CONTEXT_LOWER" == *"key leak"* ]]; then
    TURN_SECURITY_REVIEW_COMMAND="$TURN_SCRIPT_CMD send --agent $(shell_quote "$LAST_AGENT_ID") --text \"Run a focused security pass for exposed credentials/secrets and propose concrete remediation.\" --enter --max-steps 2 --turn-budget 180 --wait-timeout 60s --idle-threshold 10s"
  fi
  if [[ "$OVERALL_STATUS" == "completed" ]] && { line_has_file_signal "$FINAL_SUMMARY" || [[ "$TURN_CONTEXT_LOWER" == *"changed file"* || "$TURN_CONTEXT_LOWER" == *"modified"* || "$TURN_CONTEXT_LOWER" == *"refactor"* || "$TURN_CONTEXT_LOWER" == *"patched"* ]]; }; then
    TURN_REVIEW_CHANGES_COMMAND="$TURN_SCRIPT_CMD send --agent $(shell_quote "$LAST_AGENT_ID") --text \"Summarize changed files, rationale, and remaining risks in 5 bullets.\" --enter --max-steps 2 --turn-budget 180 --wait-timeout 60s --idle-threshold 10s"
  fi
fi

STATUS_PING_COMMAND=""
if [[ -n "$LAST_AGENT_ID" ]]; then
  STATUS_PING_COMMAND="$STEP_SCRIPT_CMD send --agent $(shell_quote "$LAST_AGENT_ID") --text \"Provide a one-line progress status.\" --enter --wait-timeout 60s --idle-threshold 10s"
fi

DELIVERY_KEY="turn:${TURN_ID}"
if [[ -n "$LAST_AGENT_ID" ]]; then
  DELIVERY_KEY="agent:${LAST_AGENT_ID}"
elif [[ -n "$LAST_WORKSPACE_ID" ]]; then
  DELIVERY_KEY="workspace:${LAST_WORKSPACE_ID}"
fi

DELIVERY_ACTION="send"
DELIVERY_PRIORITY=1
DELIVERY_RETRY_AFTER_SECONDS=0
DELIVERY_REPLACE_PREVIOUS=false
DELIVERY_DROP_PENDING=true

case "$OVERALL_STATUS" in
  timed_out|partial|partial_budget)
    DELIVERY_ACTION="edit"
    DELIVERY_PRIORITY=2
    DELIVERY_RETRY_AFTER_SECONDS=8
    DELIVERY_REPLACE_PREVIOUS=true
    DELIVERY_DROP_PENDING=false
    ;;
  needs_input|session_exited)
    DELIVERY_ACTION="send"
    DELIVERY_PRIORITY=0
    DELIVERY_DROP_PENDING=true
    ;;
  completed)
    DELIVERY_ACTION="send"
    DELIVERY_PRIORITY=1
    DELIVERY_DROP_PENDING=true
    ;;
esac

OPENCLAW_PAYLOAD="$(jq -n \
  --arg mode "$MODE" \
  --arg status "$LAST_STATUS" \
  --arg overall_status "$OVERALL_STATUS" \
  --arg summary "$FINAL_SUMMARY" \
  --arg status_emoji "$STATUS_EMOJI" \
  --arg agent_id "$LAST_AGENT_ID" \
  --arg workspace_id "$LAST_WORKSPACE_ID" \
  --arg assistant "$LAST_ASSISTANT_OUT" \
  --arg next_action "$LAST_NEXT_ACTION" \
  --arg suggested_command "$LAST_SUGGESTED_COMMAND" \
  --arg turn_id "$TURN_ID" \
  --arg channel_message "$OPENCLAW_MESSAGE" \
  --arg turn_verbosity "$TURN_VERBOSITY" \
  --arg delivery_key "$DELIVERY_KEY" \
  --arg delivery_action "$DELIVERY_ACTION" \
  --argjson delivery_priority "$DELIVERY_PRIORITY" \
  --argjson delivery_retry_after_seconds "$DELIVERY_RETRY_AFTER_SECONDS" \
  --argjson delivery_replace_previous "$DELIVERY_REPLACE_PREVIOUS" \
  --argjson delivery_drop_pending "$DELIVERY_DROP_PENDING" \
  --argjson events "$EVENTS_JSON" \
  --argjson milestones "$MILESTONES_JSON" \
  --argjson steps_used "$STEPS_USED" \
  --argjson max_steps "$MAX_STEPS" \
  --argjson elapsed_seconds "$ELAPSED_FINAL" \
  --argjson turn_budget_seconds "$TURN_BUDGET_SECONDS" \
  --argjson timeout_streak "$TIMEOUT_STREAK" \
  --argjson timeout_streak_limit "$TIMEOUT_STREAK_LIMIT" \
  --argjson budget_exhausted "$BUDGET_EXHAUSTED" \
  --argjson step_progress_percent "$STEP_PROGRESS_PERCENT" \
  --arg inline_buttons_scope "$INLINE_BUTTONS_SCOPE" \
  --argjson inline_buttons_enabled "$INLINE_BUTTONS_ENABLED" \
  --argjson channel_chunk_chars "$OPENCLAW_CHUNK_CHARS" \
  --arg test_remediation_command "$TURN_TEST_REMEDIATION_COMMAND" \
  --arg lint_remediation_command "$TURN_LINT_REMEDIATION_COMMAND" \
  --arg security_review_command "$TURN_SECURITY_REVIEW_COMMAND" \
  --arg review_changes_command "$TURN_REVIEW_CHANGES_COMMAND" \
  --arg status_ping_command "$STATUS_PING_COMMAND" \
  '
    def rindex_compat($s):
      indices($s) | if length == 0 then null else .[-1] end;
    def smart_split($txt; $size):
      def next_cut($source):
        ($source[0:$size]) as $head
        | ($head | rindex_compat("\n\n")) as $double
        | ($head | rindex_compat("\n")) as $single
        | ($head | rindex_compat(" ")) as $space
        | ($double // $single // $space) as $idx
        | if $idx == null or $idx < ($size / 3) then $size else ($idx + 1) end;
      def split_rec($source):
        if ($source | length) <= $size then
          [($source | ltrimstr("\n"))]
        else
          (next_cut($source)) as $cut
          | [($source[0:$cut])] + split_rec($source[$cut:])
        end;
      if ($txt | length) == 0 then
        []
      else
        split_rec($txt)
        | map(select(length > 0))
      end;
    def annotate_chunks($chunks):
      ($chunks | length) as $count
      | [range(0; $count) as $idx
          | {
              index: ($idx + 1),
              total: $count,
              text: (
                if $idx == 0 then
                  $chunks[$idx]
                else
                  "continued (" + (($idx + 1) | tostring) + "/" + ($count | tostring) + ")\n" + $chunks[$idx]
                end
              )
            }
        ];
    def build_action_rows($actions; $size):
      if ($actions | length) == 0 then
        []
      else
        [range(0; ($actions | length); $size) as $idx
          | ($actions[$idx:($idx + $size)] | map({text: .label, callback_data: .callback_data, style: .style}))
        ]
      end;
    def action_tokens_text($actions):
      ($actions | map(.callback_data) | join(" | "));
    def quick_action($id; $label; $command; $style; $prompt):
      {
        id: $id,
        label: $label,
        command: $command,
        style: $style,
        callback_data: ("qa:" + $id),
        prompt: $prompt
      };
    def quick_actions_list:
      [
        (if ($test_remediation_command | length) > 0
          then quick_action("fix_tests"; "Fix Tests"; $test_remediation_command; "success"; "Investigate and fix failing tests")
          else empty end),
        (if ($lint_remediation_command | length) > 0
          then quick_action("fix_lint"; "Fix Lint"; $lint_remediation_command; "success"; "Resolve lint and formatting issues")
          else empty end),
        (if ($security_review_command | length) > 0
          then quick_action("security"; "Security"; $security_review_command; "danger"; "Run a focused security remediation pass")
          else empty end),
        (if ($review_changes_command | length) > 0
          then quick_action("review"; "Review"; $review_changes_command; "primary"; "Review and summarize recent code changes")
          else empty end),
        (if ($suggested_command | length) > 0
          then quick_action("continue"; "Continue"; $suggested_command; "primary"; "Continue from current state")
          else empty end),
        (if ($status_ping_command | length) > 0
          then quick_action(
            "status";
            "Status";
            $status_ping_command;
            "primary";
            "Request a one-line status update"
          )
          else empty end)
      ];
    {
      ok: true,
      mode: $mode,
      turn_id: $turn_id,
      status: $status,
      overall_status: $overall_status,
      status_emoji: $status_emoji,
      verbosity: $turn_verbosity,
      summary: $summary,
      agent_id: $agent_id,
      workspace_id: $workspace_id,
      assistant: $assistant,
      steps_used: $steps_used,
      max_steps: $max_steps,
      elapsed_seconds: $elapsed_seconds,
      turn_budget_seconds: $turn_budget_seconds,
      budget_exhausted: $budget_exhausted,
      progress_percent: $step_progress_percent,
      timeout_streak: $timeout_streak,
      timeout_streak_limit: $timeout_streak_limit,
      next_action: $next_action,
      suggested_command: $suggested_command,
      delivery: {
        key: $delivery_key,
        action: $delivery_action,
        priority: $delivery_priority,
        retry_after_seconds: $delivery_retry_after_seconds,
        replace_previous: $delivery_replace_previous,
        drop_pending: $delivery_drop_pending,
        coalesce: true
      },
      events: $events,
      milestones: $milestones,
      progress_updates: (
        $milestones
        | to_entries
        | map(
            . as $entry
            | $entry.value as $m
            | {
                step: $m.step,
                status: $m.status,
                summary: $m.summary,
                next_action: $m.next_action,
                suggested_command: $m.suggested_command,
                progress: {
                  step: $m.step,
                  max_steps: $max_steps,
                  percent: (if $max_steps > 0 then (($m.step * 100 / $max_steps) | floor) else 0 end)
                },
                message: (
                  (if $m.status == "idle" then "✅"
                   elif $m.status == "needs_input" then "❓"
                   elif $m.status == "timed_out" then "⏱️"
                   elif $m.status == "session_exited" then "🛑"
                   else "ℹ️" end)
                  + " " + ($m.summary // "")
                ),
                delivery: {
                  key: $delivery_key,
                  action: (
                    if ($entry.key == (($milestones | length) - 1))
                      and ($overall_status == "completed" or $overall_status == "needs_input" or $overall_status == "session_exited")
                    then "send"
                    else "edit"
                    end
                  ),
                  priority: (
                    if $m.status == "needs_input" or $m.status == "session_exited" then 0
                    elif $m.status == "timed_out" then 2
                    else 1
                    end
                  ),
                  replace_previous: (
                    if ($entry.key == (($milestones | length) - 1))
                      and ($overall_status == "completed" or $overall_status == "needs_input" or $overall_status == "session_exited")
                    then false
                    else true
                    end
                  ),
                  coalesce: true
                }
              }
          )
      ),
      quick_actions: quick_actions_list,
      quick_action_map: (quick_actions_list | map({key: .callback_data, value: .command}) | from_entries),
      quick_action_prompts: (quick_actions_list | map({key: .callback_data, value: .prompt}) | from_entries),
      channel: (
        smart_split($channel_message; $channel_chunk_chars) as $chunks_raw
        | annotate_chunks($chunks_raw) as $chunks_meta
        | {
            message: $channel_message,
            verbosity: $turn_verbosity,
            chunk_chars: $channel_chunk_chars,
            chunks: ($chunks_meta | map(.text)),
            chunks_meta: $chunks_meta,
            inline_buttons_scope: $inline_buttons_scope,
            inline_buttons_enabled: $inline_buttons_enabled,
            callback_data_max_bytes: 64,
            inline_buttons: (
              if $inline_buttons_enabled then
                build_action_rows(quick_actions_list; 2)
              else
                []
              end
            ),
            action_tokens: (quick_actions_list | map(.callback_data)),
            actions_fallback: (
              if (quick_actions_list | length) == 0 then
                ""
              else
                "Actions: " + action_tokens_text(quick_actions_list)
              end
            ),
            progress_updates: (
              $milestones
              | map(
                  {
                    step,
                    status,
                    progress_percent: (if $max_steps > 0 then ((.step * 100 / $max_steps) | floor) else 0 end),
                    message: (
                      (if .status == "idle" then "✅"
                       elif .status == "needs_input" then "❓"
                       elif .status == "timed_out" then "⏱️"
                       elif .status == "session_exited" then "🛑"
                       else "ℹ️" end)
                      + " " + (.summary // "")
                    )
                  }
                )
            )
          }
      )
    }
  ')"

if [[ "${OPENCLAW_TURN_SKIP_PRESENT:-false}" == "true" ]]; then
  printf '%s\n' "$OPENCLAW_PAYLOAD"
elif [[ -x "$OPENCLAW_PRESENT_SCRIPT" ]]; then
  "$OPENCLAW_PRESENT_SCRIPT" <<<"$OPENCLAW_PAYLOAD"
else
  printf '%s\n' "$OPENCLAW_PAYLOAD"
fi
