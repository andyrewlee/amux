#!/usr/bin/env bash
# openclaw-step.sh — Bounded amux step runner for chat/orchestrator flows.
#
# Usage:
#   openclaw-step.sh run  --workspace <id> --assistant <name> --prompt <text> [--wait-timeout 60s] [--idle-threshold 10s]
#   openclaw-step.sh send --agent <id> --text <text> [--enter] [--wait-timeout 60s] [--idle-threshold 10s]
#
# Emits a normalized JSON object for easy chat orchestration:
# {
#   "ok": true|false,
#   "mode": "run"|"send",
#   "status": "idle|needs_input|timed_out|session_exited|command_error|agent_error",
#   "summary": "...",
#   ...
# }
#
# Notes:
# - Always performs exactly one bounded --wait step.
# - Uses short, bounded internal recovery polling only when a timeout returns no visible output.
# - Surfaces permission-mode input gates with explicit hints.

set -euo pipefail

shell_quote() {
  printf '%q' "$1"
}

SCRIPT_SOURCE="${BASH_SOURCE[0]:-$0}"
SCRIPT_DIR="$(cd "$(dirname "$SCRIPT_SOURCE")" >/dev/null 2>&1 && pwd -P)"
SCRIPT_PATH="$SCRIPT_DIR/$(basename "$SCRIPT_SOURCE")"
STEP_SCRIPT_REF="${OPENCLAW_STEP_CMD_REF:-skills/amux/scripts/openclaw-step.sh}"
STEP_SCRIPT_CMD="$(shell_quote "$STEP_SCRIPT_REF")"
OPENCLAW_PRESENT_SCRIPT="${OPENCLAW_PRESENT_SCRIPT:-$SCRIPT_DIR/openclaw-present.sh}"

usage() {
  cat >&2 <<'EOF'
Usage:
  openclaw-step.sh run  --workspace <id> --assistant <name> --prompt <text> [--wait-timeout 60s] [--idle-threshold 10s]
  openclaw-step.sh send --agent <id> --text <text> [--enter] [--wait-timeout 60s] [--idle-threshold 10s]
EOF
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

hash_text() {
  local value="$1"
  if command -v sha256sum >/dev/null 2>&1; then
    printf '%s' "$value" | sha256sum | awk '{print $1}'
    return
  fi
  if command -v shasum >/dev/null 2>&1; then
    printf '%s' "$value" | shasum -a 256 | awk '{print $1}'
    return
  fi
  if command -v openssl >/dev/null 2>&1; then
    printf '%s' "$value" | openssl dgst -sha256 -r | awk '{print $1}'
    return
  fi
  # Last-resort fallback if hash tools are unavailable.
  printf '%s' "$value" | awk '{print length($0)}'
}

json_escape() {
  if command -v jq >/dev/null 2>&1; then
    printf '%s' "$1" | jq -Rsa .
    return
  fi
  local escaped
  escaped="$(printf '%s' "$1" | sed -e 's/\\/\\\\/g' -e 's/"/\\"/g' -e 's/\r/\\r/g' -e ':a;N;$!ba;s/\n/\\n/g')"
  printf '"%s"' "$escaped"
}

run_with_deadline() {
  local timeout_sec="$1"
  shift
  local out_file
  local exit_file
  out_file="$(mktemp -t openclaw-step.out.XXXXXX)"
  exit_file="$(mktemp -t openclaw-step.exit.XXXXXX)"

  local cmd_pid
  local watchdog_pid
  local wait_status
  local recorded_exit

  (
    set +e
    "$@" >"$out_file" 2>&1
    cmd_status=$?
    printf '%s' "$cmd_status" >"$exit_file"
    exit 0
  ) &
  cmd_pid=$!

  (
    sleep "$timeout_sec"
    if kill -0 "$cmd_pid" 2>/dev/null; then
      kill -TERM "$cmd_pid" 2>/dev/null || true
      sleep 2
      kill -KILL "$cmd_pid" 2>/dev/null || true
    fi
  ) >/dev/null 2>&1 &
  watchdog_pid=$!

  set +e
  wait "$cmd_pid" 2>/dev/null
  wait_status=$?
  set -e
  kill "$watchdog_pid" >/dev/null 2>&1 || true
  set +e
  wait "$watchdog_pid" >/dev/null 2>&1
  set -e

  RAW_OUTPUT="$(cat "$out_file" 2>/dev/null || true)"
  recorded_exit=""
  if [[ -s "$exit_file" ]]; then
    recorded_exit="$(cat "$exit_file" 2>/dev/null || true)"
  fi
  rm -f "$out_file" "$exit_file"

  if [[ -z "$recorded_exit" ]]; then
    COMMAND_TIMED_OUT=true
    CMD_EXIT="$wait_status"
    return
  fi

  COMMAND_TIMED_OUT=false
  CMD_EXIT="$recorded_exit"
}

print_json_error() {
  local mode="$1"
  local status="$2"
  local summary="$3"
  local detail="$4"
  printf '{'
  printf '"ok":false,'
  printf '"mode":%s,' "$(json_escape "$mode")"
  printf '"status":%s,' "$(json_escape "$status")"
  printf '"summary":%s,' "$(json_escape "$summary")"
  printf '"error":%s' "$(json_escape "$detail")"
  printf '}\n'
}

strip_ansi_text() {
  local input="$1"
  printf '%s' "$input" | sed \
    -e 's/\x1b\[[0-9;]*[a-zA-Z]//g' \
    -e 's/\x1b\][^\x07]*\x07//g' \
    -e 's/\x1b\][^\x1b]*\x1b\\//g' \
    -e 's/\x1b[()][0-9A-B]//g' \
    -e 's/\x1b[=>]//g' \
    -e 's/\r//g'
}

trim_line() {
  local line="$1"
  line="${line#"${line%%[![:space:]]*}"}"
  line="${line%"${line##*[![:space:]]}"}"
  printf '%s' "$line"
}

is_chrome_line() {
  local line="$1"
  case "$line" in
    ""|"|"|✻|"╭"*|"╰"*|"│"*|"─"*|"└ "*|"⎿ "*|"↳ Interacted with "*|"› "*|"❯ "*|"? for shortcuts"*|"✶ "*|"✻ "*|"▟"*|"▐"*|"▝"*|"▘"*|"Tip:"*|"model:"*|"directory:"*|"cwd:"*|"workspace:"*|"• Explored"|"• Exploring"|"• Working ("*|"Working ("*|"Thinking "*|*" no sandbox "*|*"/model "*|"~/.amux/"*|*"sandbox   "*|*"sandbox "*")"|"shift+tab to accept edits"*|"/ commands · @ files · ! shell"*|*"? for help"*|*"▄▄▄▄"*|*"███"*|*"▀▀▀"*|">   Type your message or @path/to/file"*)
      return 0
      ;;
    *)
      return 1
      ;;
  esac
}

extract_latest_useful_line() {
  local raw="$1"
  local cleaned line
  local latest=""
  cleaned="$(strip_ansi_text "$raw")"
  while IFS= read -r line; do
    line="$(trim_line "$line")"
    if [[ -z "$line" ]]; then
      continue
    fi
    if is_chrome_line "$line"; then
      continue
    fi
    latest="$line"
  done < <(printf '%s\n' "$cleaned")
  if [[ -n "${latest:-}" ]]; then
    printf '%s' "$latest"
    return
  fi

  # Fallback to the last non-empty trimmed line, even if chrome.
  while IFS= read -r line; do
    line="$(trim_line "$line")"
    if [[ -z "$line" ]]; then
      continue
    fi
    latest="$line"
  done < <(printf '%s\n' "$cleaned")
  printf '%s' "${latest:-}"
}

compact_agent_text() {
  local raw="$1"
  local cleaned line out
  out=""
  cleaned="$(strip_ansi_text "$raw")"
  while IFS= read -r line; do
    line="$(trim_line "$line")"
    if [[ -z "$line" ]]; then
      continue
    fi
    if is_chrome_line "$line"; then
      continue
    fi
    if [[ -n "$out" ]]; then
      out+=$'\n'
    fi
    out+="$line"
  done < <(printf '%s\n' "$cleaned")
  printf '%s' "$out"
}

last_nonempty_line() {
  local raw="$1"
  local line last
  last=""
  while IFS= read -r line; do
    line="$(trim_line "$line")"
    if [[ -z "$line" ]]; then
      continue
    fi
    last="$line"
  done < <(printf '%s\n' "$raw")
  printf '%s' "$last"
}

is_agent_progress_line() {
  local line="$1"
  case "$line" in
    "Search "*|"Read "*|"List "*|"Working "*|"Thinking "*|"• I "*|"• I'll "*|"• I’ll "*|"If you want"*|"No explicit TODO/FIXME debt markers were found"*)
      return 0
      ;;
    *)
      return 1
      ;;
  esac
}

is_jsonish_fragment() {
  local line="$1"
  local trimmed
  trimmed="$(trim_line "$line")"
  case "$trimmed" in
    "- {"*|"{"*)
      if [[ "$trimmed" == *'":"'* || "$trimmed" == *"{\""* || "$trimmed" == *",\""* ]]; then
        return 0
      fi
      ;;
  esac
  return 1
}

is_wrapped_fragment_line() {
  local line="$1"
  local trimmed
  trimmed="$(trim_line "$line")"
  if [[ -z "$trimmed" ]]; then
    return 1
  fi
  case "$trimmed" in
    "- "*|"• "*)
      return 1
      ;;
  esac
  if [[ "$trimmed" =~ ^[a-z0-9] ]] && line_has_file_signal "$trimmed"; then
    return 0
  fi
  if [[ "$trimmed" =~ ^[a-z0-9] ]] && [[ "$trimmed" == *"): "* ]]; then
    return 0
  fi
  return 1
}

is_file_only_bullet() {
  local line="$1"
  local trimmed value
  trimmed="$(trim_line "$line")"
  case "$trimmed" in
    "- "*|"• "*)
      ;;
    *)
      return 1
      ;;
  esac
  value="${trimmed#- }"
  value="${value#• }"
  value="$(trim_line "$value")"
  if [[ -z "$value" || "$value" == *" "* || "$value" == *":"* ]]; then
    return 1
  fi
  case "$value" in
    *".go"|*".md"|*".sh"|*".py"|*".ts"|*".tsx"|*".js"|*".jsx"|*".json"|*".yaml"|*".yml"|*".toml"|*"Makefile"|*"/"*)
      return 0
      ;;
  esac
  return 1
}

summary_is_weak() {
  local summary="$1"
  local trimmed lower
  trimmed="$(trim_line "$summary")"
  if [[ -z "$trimmed" ]]; then
    return 0
  fi
  if [[ "${#trimmed}" -lt 24 ]]; then
    return 0
  fi
  lower="$(printf '%s' "$trimmed" | tr '[:upper:]' '[:lower:]')"
  case "$lower" in
    "output tracking."|"effort to fix these."|"these."|"done"|"done."|"ok"|"ok."|"complete"|"complete.")
      return 0
      ;;
  esac
  return 1
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

extract_delta_summary_candidate() {
  local raw="$1"
  local line candidate fragment
  candidate=""
  fragment=""
  while IFS= read -r line; do
    line="$(trim_line "$line")"
    if [[ -z "$line" ]]; then
      continue
    fi
    if is_agent_progress_line "$line"; then
      continue
    fi
    if is_jsonish_fragment "$line"; then
      continue
    fi
    if is_wrapped_fragment_line "$line"; then
      if [[ -z "$candidate" ]]; then
        candidate="$line"
      fi
      if [[ -z "$fragment" ]]; then
        fragment="$line"
      fi
      continue
    fi
    if [[ -n "$fragment" && ( "$line" == "- "* || "$line" == "• "* ) ]]; then
      line="$(trim_line "$line $fragment")"
      if [[ "$line" == *":" ]]; then
        if [[ -z "$candidate" ]]; then
          candidate="$line"
        fi
        continue
      fi
      printf '%s' "$line"
      return
    fi
    if [[ "$line" == "- "* || "$line" == "• "* ]]; then
      if [[ "$line" == *"/"* || "$line" == *".go"* || "$line" == *".md"* || "$line" == *".sh"* || "$line" == *":"* ]]; then
        if is_file_only_bullet "$line"; then
          if [[ -z "$candidate" ]]; then
            candidate="$line"
          fi
          continue
        fi
        if [[ "$line" == *":" ]]; then
          if [[ -z "$candidate" ]]; then
            candidate="$line"
          fi
          continue
        fi
        printf '%s' "$line"
        return
      fi
      if [[ -z "$candidate" ]]; then
        candidate="$line"
      fi
      continue
    fi
    if [[ -z "$candidate" && "${#line}" -ge 32 ]]; then
      candidate="$line"
    fi
  done < <(printf '%s\n' "$raw" | awk 'NF { lines[++n]=$0 } END { for (i=n; i>=1; i--) print lines[i] }')
  if [[ -n "$candidate" && "$candidate" == *":" ]]; then
    candidate="$(trim_line "${candidate%:}")"
  fi
  printf '%s' "$candidate"
}

sanitize_summary_text() {
  local raw="$1"
  local text lower
  text="$(trim_line "$(strip_ansi_text "$raw")")"
  if [[ -z "${text// }" ]]; then
    printf ''
    return
  fi

  # Drop known hosted-UI landing chrome that sometimes leaks into captures.
  lower="$(printf '%s' "$text" | tr '[:upper:]' '[:lower:]')"
  case "$lower" in
    *"visit https://chatgpt.com/codex"*|*"app-landing-page=true"*|*"continue in your browser"*)
      printf ''
      return
      ;;
  esac
  if is_jsonish_fragment "$text"; then
    printf ''
    return
  fi

  # Trim escaped/plain JSON tails that can leak from tool/event payload fragments.
  text="$(printf '%s' "$text" | sed -E \
    -e 's/\\",[[:space:]]*\\\"(ok|mode|status|summary|latest_line|next_action|suggested_command|agent_id|workspace_id|assistant|message|delta|needs_input|input_hint|timed_out|session_exited|changed|response|data|error)\\\"[[:space:]]*:[[:space:]].*$//' \
    -e 's/",[[:space:]]*"(ok|mode|status|summary|latest_line|next_action|suggested_command|agent_id|workspace_id|assistant|message|delta|needs_input|input_hint|timed_out|session_exited|changed|response|data|error)"[[:space:]]*:[[:space:]].*$//' \
    -e 's/\\"/"/g' \
    -e 's/[[:space:]]+$//')"
  text="$(trim_line "$text")"
  if is_chrome_line "$text"; then
    printf ''
    return
  fi
  printf '%s' "$text"
}

build_delta_excerpt() {
  local raw="$1"
  local max_lines="$2"
  if ! [[ "$max_lines" =~ ^[0-9]+$ ]] || [[ "$max_lines" -le 0 ]]; then
    max_lines=3
  fi
  printf '%s\n' "$raw" | awk -v max_lines="$max_lines" '
    function trim(s) {
      sub(/^[[:space:]]+/, "", s)
      sub(/[[:space:]]+$/, "", s)
      return s
    }
    {
      line = trim($0)
      if (line == "") next
      if (line ~ /^(Search |Read |List |Working |Thinking )/) next
      if (line ~ /^• I[[:space:]]/) next
      lines[++n] = line
    }
    END {
      if (n == 0) exit
      start = n - max_lines + 1
      if (start < 1) start = 1
      for (i = start; i <= n; i++) print lines[i]
      if (start > 1) print "..."
    }
  '
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
  # Best-effort masking for common token/key patterns before OpenClaw delivery.
  printf '%s' "$input" | sed -E \
    -e 's/(sk-ant-api[0-9]*-[A-Za-z0-9_-]{10})[A-Za-z0-9_-]*/\1***/g' \
    -e 's/(sk-[A-Za-z0-9_-]{20})[A-Za-z0-9_-]*/\1***/g' \
    -e 's/(ghp_[A-Za-z0-9]{5})[A-Za-z0-9]*/\1***/g' \
    -e 's/(gho_[A-Za-z0-9]{5})[A-Za-z0-9]*/\1***/g' \
    -e 's/(github_pat_[A-Za-z0-9_]{5})[A-Za-z0-9_]*/\1***/g' \
    -e 's/(ghs_[A-Za-z0-9]{5})[A-Za-z0-9]*/\1***/g' \
    -e 's/(glpat-[A-Za-z0-9_-]{5})[A-Za-z0-9_-]*/\1***/g' \
    -e 's/(xoxb-[A-Za-z0-9]{5})[A-Za-z0-9-]*/\1***/g' \
    -e 's/(AKIA[0-9A-Z]{4})[0-9A-Z]{12}/\1************/g' \
    -e 's/(Bearer )[A-Za-z0-9+/_=.-]{8,}/\1***/g' \
    -e 's/((TOKEN|SECRET|PASSWORD|API_KEY|APIKEY|AUTH_TOKEN|PRIVATE_KEY|ACCESS_KEY|CLIENT_SECRET|WEBHOOK_SECRET)=)[^[:space:]'"'"'"]{8,}/\1***/g'
}

if [[ $# -lt 1 ]]; then
  usage
  exit 2
fi

MODE="$1"
shift

WAIT_TIMEOUT="60s"
IDLE_THRESHOLD="10s"
IDEMPOTENCY_KEY=""

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
    --idempotency-key)
      IDEMPOTENCY_KEY="$2"; shift 2 ;;
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
      print_json_error "$MODE" "command_error" "Invalid flag" "unknown flag: $1"
      exit 2 ;;
  esac
done

AUTO_IDEMPOTENCY="${OPENCLAW_STEP_AUTO_IDEMPOTENCY:-true}"
if [[ -z "$IDEMPOTENCY_KEY" && "$AUTO_IDEMPOTENCY" != "false" ]]; then
  idempotency_base="$MODE|$WAIT_TIMEOUT|$IDLE_THRESHOLD|$WORKSPACE|$ASSISTANT|$PROMPT|$AGENT_ID|$TEXT|$ENTER"
  idempotency_hash="$(hash_text "$idempotency_base")"
  IDEMPOTENCY_KEY="tgstep-${idempotency_hash:0:20}"
fi

if ! command -v amux >/dev/null 2>&1; then
  print_json_error "$MODE" "command_error" "amux is not installed" "missing binary: amux"
  exit 127
fi

if ! command -v jq >/dev/null 2>&1; then
  print_json_error "$MODE" "command_error" "jq is required" "missing binary: jq"
  exit 127
fi

cmd=(amux --json)
case "$MODE" in
  run)
    if [[ -z "$WORKSPACE" || -z "$ASSISTANT" || -z "$PROMPT" ]]; then
      usage
      print_json_error "$MODE" "command_error" "Missing required flags" "run requires --workspace, --assistant, --prompt"
      exit 2
    fi
    cmd+=(agent run --workspace "$WORKSPACE" --assistant "$ASSISTANT" --prompt "$PROMPT" --wait --wait-timeout "$WAIT_TIMEOUT" --idle-threshold "$IDLE_THRESHOLD")
    ;;
  send)
    if [[ -z "$AGENT_ID" || -z "$TEXT" ]]; then
      usage
      print_json_error "$MODE" "command_error" "Missing required flags" "send requires --agent and --text"
      exit 2
    fi
    cmd+=(agent send --agent "$AGENT_ID" --text "$TEXT" --wait --wait-timeout "$WAIT_TIMEOUT" --idle-threshold "$IDLE_THRESHOLD")
    if [[ "$ENTER" == "true" ]]; then
      cmd+=(--enter)
    fi
    ;;
  *)
    usage
    print_json_error "$MODE" "command_error" "Invalid mode" "mode must be run or send"
    exit 2
    ;;
esac

if [[ -n "$IDEMPOTENCY_KEY" ]]; then
  cmd+=(--idempotency-key "$IDEMPOTENCY_KEY")
fi

WAIT_TIMEOUT_SECONDS="$(duration_to_seconds "$WAIT_TIMEOUT" 60)"
# Allow bounded extra headroom for agent startup/prompt readiness.
HARD_TIMEOUT_BUFFER_SECONDS="$(duration_to_seconds "${OPENCLAW_STEP_HARD_TIMEOUT_BUFFER:-180}" 180)"
if ! [[ "$HARD_TIMEOUT_BUFFER_SECONDS" =~ ^[0-9]+$ ]] || [[ "$HARD_TIMEOUT_BUFFER_SECONDS" -lt 0 ]]; then
  HARD_TIMEOUT_BUFFER_SECONDS=180
fi
HARD_TIMEOUT_SECONDS=$((WAIT_TIMEOUT_SECONDS + HARD_TIMEOUT_BUFFER_SECONDS))
HARD_TIMEOUT_CAP_SECONDS="$(duration_to_seconds "${OPENCLAW_STEP_HARD_TIMEOUT_CAP:-600}" 600)"
if [[ "$HARD_TIMEOUT_CAP_SECONDS" -gt 0 && "$HARD_TIMEOUT_SECONDS" -gt "$HARD_TIMEOUT_CAP_SECONDS" ]]; then
  HARD_TIMEOUT_SECONDS="$HARD_TIMEOUT_CAP_SECONDS"
fi
RAW_OUTPUT=""
CMD_EXIT=0
COMMAND_TIMED_OUT=false
run_with_deadline "$HARD_TIMEOUT_SECONDS" "${cmd[@]}"

if [[ "$COMMAND_TIMED_OUT" == "true" ]]; then
  detail="hard timeout (${HARD_TIMEOUT_SECONDS}s) exceeded while running amux step"
  if [[ -n "${RAW_OUTPUT// }" ]]; then
    detail="$detail"$'\n'"$RAW_OUTPUT"
  fi
  print_json_error "$MODE" "command_error" "amux command exceeded hard timeout" "$detail"
  exit 124
fi

if [[ $CMD_EXIT -ne 0 ]]; then
  print_json_error "$MODE" "command_error" "amux command failed" "$RAW_OUTPUT"
  exit "$CMD_EXIT"
fi

if ! jq -e . >/dev/null 2>&1 <<<"$RAW_OUTPUT"; then
  print_json_error "$MODE" "command_error" "amux returned non-JSON output" "$RAW_OUTPUT"
  exit 65
fi

OK="$(jq -r '.ok // false' <<<"$RAW_OUTPUT")"
if [[ "$OK" != "true" ]]; then
  ERR_CODE="$(jq -r '.error.code // "unknown_error"' <<<"$RAW_OUTPUT")"
  ERR_MSG="$(jq -r '.error.message // "agent step failed"' <<<"$RAW_OUTPUT")"
  print_json_error "$MODE" "agent_error" "$ERR_CODE" "$ERR_MSG"
  exit 1
fi

STATUS="$(jq -r '.data.response.status // "unknown"' <<<"$RAW_OUTPUT")"
SESSION_NAME="$(jq -r '.data.session_name // ""' <<<"$RAW_OUTPUT")"
AGENT_ID_OUT="$(jq -r '.data.agent_id // ""' <<<"$RAW_OUTPUT")"
WORKSPACE_ID_OUT="$(jq -r '.data.workspace_id // .data.id // ""' <<<"$RAW_OUTPUT")"
ASSISTANT_OUT="$(jq -r '.data.assistant // ""' <<<"$RAW_OUTPUT")"
LATEST_LINE="$(jq -r '.data.response.latest_line // ""' <<<"$RAW_OUTPUT")"
RESPONSE_SUMMARY="$(jq -r '.data.response.summary // ""' <<<"$RAW_OUTPUT")"
DELTA="$(jq -r '.data.response.delta // ""' <<<"$RAW_OUTPUT")"
NEEDS_INPUT="$(jq -r '.data.response.needs_input // false' <<<"$RAW_OUTPUT")"
INPUT_HINT="$(jq -r '.data.response.input_hint // ""' <<<"$RAW_OUTPUT")"
TIMED_OUT="$(jq -r '.data.response.timed_out // false' <<<"$RAW_OUTPUT")"
SESSION_EXITED="$(jq -r '.data.response.session_exited // false' <<<"$RAW_OUTPUT")"
CHANGED="$(jq -r '.data.response.changed // false' <<<"$RAW_OUTPUT")"

# `agent send` responses may omit workspace_id. Derive it from agent id when possible.
if [[ -z "${WORKSPACE_ID_OUT// }" ]]; then
  if [[ -n "${AGENT_ID_OUT// }" && "$AGENT_ID_OUT" == *:* ]]; then
    WORKSPACE_ID_OUT="${AGENT_ID_OUT%%:*}"
  elif [[ -n "${AGENT_ID// }" && "$AGENT_ID" == *:* ]]; then
    WORKSPACE_ID_OUT="${AGENT_ID%%:*}"
  fi
fi

if [[ "$STATUS" == "timed_out" ]]; then
  if [[ "$LATEST_LINE" == "(no output yet)" ]]; then
    LATEST_LINE=""
  fi
  if [[ "$RESPONSE_SUMMARY" == "(no output yet)" ]]; then
    RESPONSE_SUMMARY=""
  fi
fi

LATEST_LINE="$(redact_secrets_text "$LATEST_LINE")"
RESPONSE_SUMMARY="$(redact_secrets_text "$RESPONSE_SUMMARY")"
DELTA="$(redact_secrets_text "$DELTA")"
INPUT_HINT="$(redact_secrets_text "$INPUT_HINT")"

LATEST_LINE="$(sanitize_summary_text "$LATEST_LINE")"
RESPONSE_SUMMARY="$(sanitize_summary_text "$RESPONSE_SUMMARY")"

LATEST_LINE_TRIMMED="$(trim_line "$LATEST_LINE")"
if is_chrome_line "$LATEST_LINE_TRIMMED"; then
  LATEST_LINE=""
fi
RESPONSE_SUMMARY_TRIMMED="$(trim_line "$RESPONSE_SUMMARY")"
if is_chrome_line "$RESPONSE_SUMMARY_TRIMMED"; then
  RESPONSE_SUMMARY=""
fi

DELTA_COMPACT="$(compact_agent_text "$DELTA")"
SUMMARY="$RESPONSE_SUMMARY"
if [[ -z "${SUMMARY// }" ]]; then
  SUMMARY="$LATEST_LINE"
fi
if [[ -z "${SUMMARY// }" && -n "${DELTA_COMPACT// }" ]]; then
  SUMMARY="$(last_nonempty_line "$DELTA_COMPACT")"
fi
SUMMARY_TRIMMED="$(trim_line "$SUMMARY")"
if is_chrome_line "$SUMMARY_TRIMMED"; then
  SUMMARY=""
fi
if [[ -z "${LATEST_LINE// }" && -n "${DELTA_COMPACT// }" ]]; then
  LATEST_LINE="$(last_nonempty_line "$DELTA_COMPACT")"
fi
if [[ -z "${RESPONSE_SUMMARY// }" && -n "${SUMMARY// }" ]]; then
  RESPONSE_SUMMARY="$SUMMARY"
fi

SUBSTANTIVE_OUTPUT=false
if [[ -n "${SUMMARY// }" || -n "${LATEST_LINE// }" || -n "${DELTA_COMPACT// }" ]]; then
  SUBSTANTIVE_OUTPUT=true
fi

# Some assistants report needs_input after producing a substantive answer, with no
# actionable input hint (or a generic conversational re-prompt). For mobile DX,
# treat these as completed step output instead of blocked state.
if [[ "$STATUS" == "needs_input" && "$NEEDS_INPUT" == "true" ]]; then
  INPUT_HINT_TRIMMED="$(trim_line "$INPUT_HINT")"
  INPUT_HINT_LOWER="$(printf '%s' "$INPUT_HINT_TRIMMED" | tr '[:upper:]' '[:lower:]')"
  NEEDS_INPUT_IS_GENERIC=false
  if [[ "$SUBSTANTIVE_OUTPUT" == "true" && -z "${INPUT_HINT_TRIMMED// }" ]]; then
    NEEDS_INPUT_IS_GENERIC=true
  fi
  case "$INPUT_HINT_LOWER" in
    "what can i do for you?"*|"anything else?"*|"how would you like to proceed?"*)
      NEEDS_INPUT_IS_GENERIC=true
      ;;
  esac
  if [[ "$NEEDS_INPUT_IS_GENERIC" == "true" ]]; then
    STATUS="idle"
    NEEDS_INPUT=false
    INPUT_HINT=""
  fi
fi

RECOVERED_FROM_CAPTURE=false
RECOVERY_ATTEMPTED=false
RECOVERY_POLLS_USED=0
if [[ "$STATUS" == "timed_out" && "$SUBSTANTIVE_OUTPUT" != "true" && -n "$SESSION_NAME" ]]; then
  RECOVERY_ATTEMPTED=true
  RECOVERY_POLLS="${OPENCLAW_STEP_TIMEOUT_RECOVERY_POLLS:-6}"
  RECOVERY_INTERVAL="${OPENCLAW_STEP_TIMEOUT_RECOVERY_INTERVAL:-5}"
  RECOVERY_LINES="${OPENCLAW_STEP_TIMEOUT_RECOVERY_LINES:-160}"
  if ! [[ "$RECOVERY_POLLS" =~ ^[0-9]+$ ]]; then
    RECOVERY_POLLS=6
  fi
  if ! [[ "$RECOVERY_INTERVAL" =~ ^[0-9]+$ ]]; then
    RECOVERY_INTERVAL=5
  fi
  if ! [[ "$RECOVERY_LINES" =~ ^[0-9]+$ ]]; then
    RECOVERY_LINES=160
  fi

  for ((i=1; i<=RECOVERY_POLLS; i++)); do
    RECOVERY_POLLS_USED="$i"
    if [[ "$RECOVERY_INTERVAL" -gt 0 ]]; then
      sleep "$RECOVERY_INTERVAL"
    fi
    capture_json="$(amux --json agent capture "$SESSION_NAME" --lines "$RECOVERY_LINES" 2>/dev/null || true)"
    if ! jq -e '.ok == true' >/dev/null 2>&1 <<<"$capture_json"; then
      continue
    fi
    capture_content="$(jq -r '.data.content // ""' <<<"$capture_json")"
    capture_compact="$(compact_agent_text "$capture_content")"
    recovered_line="$(last_nonempty_line "$capture_compact")"
    if [[ -z "${recovered_line// }" ]]; then
      continue
    fi

    RECOVERED_FROM_CAPTURE=true
    SUBSTANTIVE_OUTPUT=true
    if [[ -z "${SUMMARY// }" ]]; then
      SUMMARY="$recovered_line"
    fi
    if [[ -z "${LATEST_LINE// }" ]]; then
      LATEST_LINE="$recovered_line"
    fi
    if [[ -z "${RESPONSE_SUMMARY// }" ]]; then
      RESPONSE_SUMMARY="$recovered_line"
    fi
    if [[ -z "${DELTA_COMPACT// }" ]]; then
      DELTA_COMPACT="$capture_compact"
    fi
    if [[ -z "${DELTA// }" ]]; then
      DELTA="$capture_compact"
    fi
    CHANGED=true
    break
  done
fi

DELTA_SUMMARY_CANDIDATE="$(extract_delta_summary_candidate "$DELTA_COMPACT")"
DELTA_SUMMARY_CANDIDATE="$(sanitize_summary_text "$DELTA_SUMMARY_CANDIDATE")"
if [[ -n "${DELTA_SUMMARY_CANDIDATE// }" ]]; then
  if line_has_file_signal "$DELTA_SUMMARY_CANDIDATE" && ! line_has_file_signal "$SUMMARY"; then
    SUMMARY="$DELTA_SUMMARY_CANDIDATE"
  elif summary_is_weak "$SUMMARY"; then
    SUMMARY="$DELTA_SUMMARY_CANDIDATE"
  fi
  if line_has_file_signal "$DELTA_SUMMARY_CANDIDATE" && ! line_has_file_signal "$RESPONSE_SUMMARY"; then
    RESPONSE_SUMMARY="$DELTA_SUMMARY_CANDIDATE"
  elif summary_is_weak "$RESPONSE_SUMMARY"; then
    RESPONSE_SUMMARY="$DELTA_SUMMARY_CANDIDATE"
  fi
  if line_has_file_signal "$DELTA_SUMMARY_CANDIDATE" && ! line_has_file_signal "$LATEST_LINE"; then
    LATEST_LINE="$DELTA_SUMMARY_CANDIDATE"
  elif summary_is_weak "$LATEST_LINE"; then
    LATEST_LINE="$DELTA_SUMMARY_CANDIDATE"
  fi
fi

SUMMARY="$(sanitize_summary_text "$SUMMARY")"
RESPONSE_SUMMARY="$(sanitize_summary_text "$RESPONSE_SUMMARY")"
LATEST_LINE="$(sanitize_summary_text "$LATEST_LINE")"

if [[ -z "${SUMMARY// }" ]]; then
  case "$STATUS" in
    timed_out) SUMMARY="Timed out waiting for first visible output; agent may still be starting." ;;
    session_exited) SUMMARY="Agent session exited while waiting." ;;
    needs_input) SUMMARY="Agent needs input." ;;
    idle) SUMMARY="Agent step completed." ;;
    *) SUMMARY="Agent step completed with status: $STATUS." ;;
  esac
fi

BLOCKED_PERMISSION_MODE=false
NEXT_ACTION=""
SUGGESTED_COMMAND=""
if [[ "$NEEDS_INPUT" == "true" && "$INPUT_HINT" == "Assistant is waiting for local permission-mode selection." ]]; then
  BLOCKED_PERMISSION_MODE=true
  NEXT_ACTION="Switch to a non-interactive assistant (e.g. codex) for this step."
elif [[ "$STATUS" == "timed_out" ]]; then
  if [[ "$SUBSTANTIVE_OUTPUT" == "true" ]]; then
    NEXT_ACTION="Send one focused follow-up prompt on the same agent and continue from the latest output."
  else
    NEXT_ACTION="Agent may still be starting. Run one bounded follow-up send on the same agent to force a short status update."
  fi
  if [[ -n "$AGENT_ID_OUT" ]]; then
    SUGGESTED_COMMAND="$STEP_SCRIPT_CMD send --agent $(shell_quote "$AGENT_ID_OUT") --text \"Continue from current state and provide a one-line status update.\" --enter --wait-timeout 60s --idle-threshold 10s"
  fi
elif [[ "$STATUS" == "session_exited" ]]; then
  NEXT_ACTION="Restart the agent in the same workspace, then continue with a focused follow-up prompt."
  if [[ -n "$WORKSPACE_ID_OUT" && -n "$ASSISTANT_OUT" ]]; then
    SUGGESTED_COMMAND="$STEP_SCRIPT_CMD run --workspace $(shell_quote "$WORKSPACE_ID_OUT") --assistant $(shell_quote "$ASSISTANT_OUT") --prompt \"Continue from where you left off and provide a concise progress update.\" --wait-timeout 60s --idle-threshold 10s"
  fi
elif [[ "$STATUS" == "idle" && "$SUBSTANTIVE_OUTPUT" != "true" ]]; then
  NEXT_ACTION="No substantive output captured yet. Run one bounded follow-up send step on the same agent."
  if [[ -n "$AGENT_ID_OUT" ]]; then
    SUGGESTED_COMMAND="$STEP_SCRIPT_CMD send --agent $(shell_quote "$AGENT_ID_OUT") --text \"Provide a one-line progress status.\" --enter --wait-timeout 60s --idle-threshold 10s"
  fi
elif [[ "$STATUS" == "needs_input" ]]; then
  NEXT_ACTION="Ask the user to answer the pending prompt, then run one follow-up send step."
fi

SUMMARY="$(redact_secrets_text "$SUMMARY")"
LATEST_LINE="$(redact_secrets_text "$LATEST_LINE")"
RESPONSE_SUMMARY="$(redact_secrets_text "$RESPONSE_SUMMARY")"
DELTA_COMPACT="$(redact_secrets_text "$DELTA_COMPACT")"
NEXT_ACTION="$(redact_secrets_text "$NEXT_ACTION")"
SUGGESTED_COMMAND="$(redact_secrets_text "$SUGGESTED_COMMAND")"
INPUT_HINT="$(redact_secrets_text "$INPUT_HINT")"

STEP_VERBOSITY="$(normalize_verbosity_level "${OPENCLAW_STEP_VERBOSITY:-normal}")"
STEP_DETAIL_LINES="${OPENCLAW_STEP_DETAIL_LINES:-}"
if [[ -z "$STEP_DETAIL_LINES" ]]; then
  case "$STEP_VERBOSITY" in
    quiet) STEP_DETAIL_LINES=0 ;;
    normal) STEP_DETAIL_LINES=3 ;;
    detailed) STEP_DETAIL_LINES=8 ;;
  esac
fi
if ! [[ "$STEP_DETAIL_LINES" =~ ^[0-9]+$ ]]; then
  case "$STEP_VERBOSITY" in
    quiet) STEP_DETAIL_LINES=0 ;;
    normal) STEP_DETAIL_LINES=3 ;;
    detailed) STEP_DETAIL_LINES=8 ;;
  esac
fi

TIMED_OUT_STARTUP=false
if [[ "$STATUS" == "timed_out" && "$SUBSTANTIVE_OUTPUT" != "true" ]]; then
  TIMED_OUT_STARTUP=true
fi

STATUS_EMOJI="ℹ️"
case "$STATUS" in
  idle) STATUS_EMOJI="✅" ;;
  needs_input) STATUS_EMOJI="❓" ;;
  timed_out) STATUS_EMOJI="⏱️" ;;
  session_exited) STATUS_EMOJI="🛑" ;;
esac

OPENCLAW_DELTA_EXCERPT=""
if [[ "$STEP_DETAIL_LINES" -gt 0 && -n "${DELTA_COMPACT// }" ]]; then
  OPENCLAW_DELTA_EXCERPT="$(build_delta_excerpt "$DELTA_COMPACT" "$STEP_DETAIL_LINES")"
fi
OPENCLAW_MESSAGE="$STATUS_EMOJI $SUMMARY"
case "$STEP_VERBOSITY" in
  quiet)
    if [[ "$NEEDS_INPUT" == "true" && -n "${INPUT_HINT// }" ]]; then
      OPENCLAW_MESSAGE+=$'\n'"Input: $INPUT_HINT"
    fi
    ;;
  normal)
    if [[ -n "${NEXT_ACTION// }" ]]; then
      OPENCLAW_MESSAGE+=$'\n'"Next: $NEXT_ACTION"
    fi
    if [[ "$NEEDS_INPUT" == "true" && -n "${INPUT_HINT// }" ]]; then
      OPENCLAW_MESSAGE+=$'\n'"Input: $INPUT_HINT"
    fi
    if [[ -n "${OPENCLAW_DELTA_EXCERPT// }" ]]; then
      OPENCLAW_MESSAGE+=$'\n'"Details:"$'\n'"$OPENCLAW_DELTA_EXCERPT"
    fi
    if [[ -n "${SUGGESTED_COMMAND// }" ]]; then
      OPENCLAW_MESSAGE+=$'\n'"Command: $SUGGESTED_COMMAND"
    fi
    ;;
  detailed)
    if [[ -n "${NEXT_ACTION// }" ]]; then
      OPENCLAW_MESSAGE+=$'\n'"Next: $NEXT_ACTION"
    fi
    if [[ "$NEEDS_INPUT" == "true" && -n "${INPUT_HINT// }" ]]; then
      OPENCLAW_MESSAGE+=$'\n'"Input: $INPUT_HINT"
    fi
    if [[ -n "${OPENCLAW_DELTA_EXCERPT// }" ]]; then
      OPENCLAW_MESSAGE+=$'\n'"Details:"$'\n'"$OPENCLAW_DELTA_EXCERPT"
    fi
    if [[ -n "${SUGGESTED_COMMAND// }" ]]; then
      OPENCLAW_MESSAGE+=$'\n'"Command: $SUGGESTED_COMMAND"
    fi
    OPENCLAW_MESSAGE+=$'\n'"Meta: status=$STATUS changed=$CHANGED agent=${AGENT_ID_OUT:-none} workspace=${WORKSPACE_ID_OUT:-none}"
    if [[ "$RECOVERY_ATTEMPTED" == "true" ]]; then
      OPENCLAW_MESSAGE+=$'\n'"Recovery: attempted=true polls=$RECOVERY_POLLS_USED"
    fi
    ;;
esac
OPENCLAW_CHUNK_CHARS="${OPENCLAW_STEP_CHUNK_CHARS:-1200}"
if ! [[ "$OPENCLAW_CHUNK_CHARS" =~ ^[0-9]+$ ]]; then
  OPENCLAW_CHUNK_CHARS=1200
fi
if [[ "$OPENCLAW_CHUNK_CHARS" -le 0 ]]; then
  OPENCLAW_CHUNK_CHARS=1200
fi

INLINE_BUTTONS_SCOPE="$(normalize_inline_buttons_scope "${OPENCLAW_INLINE_BUTTONS_SCOPE:-allowlist}")"
INLINE_BUTTONS_ENABLED=true
if [[ "$INLINE_BUTTONS_SCOPE" == "off" ]]; then
  INLINE_BUTTONS_ENABLED=false
fi

CONTEXT_LOWER="$(printf '%s\n%s\n%s' "$SUMMARY" "$RESPONSE_SUMMARY" "$DELTA_COMPACT" | tr '[:upper:]' '[:lower:]')"
TEST_REMEDIATION_COMMAND=""
LINT_REMEDIATION_COMMAND=""
SECURITY_REVIEW_COMMAND=""
REVIEW_CHANGES_COMMAND=""
if [[ -n "$AGENT_ID_OUT" ]]; then
  if [[ "$CONTEXT_LOWER" == *"test"* ]] && [[ "$CONTEXT_LOWER" == *"fail"* || "$CONTEXT_LOWER" == *"panic"* || "$CONTEXT_LOWER" == *"error"* ]]; then
    TEST_REMEDIATION_COMMAND="$STEP_SCRIPT_CMD send --agent $(shell_quote "$AGENT_ID_OUT") --text \"Investigate failing tests, fix root causes, and report changed files plus exact test command/results.\" --enter --wait-timeout 60s --idle-threshold 10s"
  fi
  if [[ "$CONTEXT_LOWER" == *"lint"* || "$CONTEXT_LOWER" == *"format"* || "$CONTEXT_LOWER" == *"gofumpt"* || "$CONTEXT_LOWER" == *"style"* ]]; then
    LINT_REMEDIATION_COMMAND="$STEP_SCRIPT_CMD send --agent $(shell_quote "$AGENT_ID_OUT") --text \"Resolve lint and formatting issues, then provide a concise summary of fixes.\" --enter --wait-timeout 60s --idle-threshold 10s"
  fi
  if [[ "$CONTEXT_LOWER" == *"secret"* || "$CONTEXT_LOWER" == *"token"* || "$CONTEXT_LOWER" == *"credential"* || "$CONTEXT_LOWER" == *"key leak"* ]]; then
    SECURITY_REVIEW_COMMAND="$STEP_SCRIPT_CMD send --agent $(shell_quote "$AGENT_ID_OUT") --text \"Run a focused security pass for exposed credentials/secrets and propose concrete remediation.\" --enter --wait-timeout 60s --idle-threshold 10s"
  fi
  if [[ "$CHANGED" == "true" ]] && { line_has_file_signal "$SUMMARY" || line_has_file_signal "$DELTA_COMPACT" || [[ "$CONTEXT_LOWER" == *"changed file"* || "$CONTEXT_LOWER" == *"modified"* || "$CONTEXT_LOWER" == *"refactor"* || "$CONTEXT_LOWER" == *"patched"* ]]; }; then
    REVIEW_CHANGES_COMMAND="$STEP_SCRIPT_CMD send --agent $(shell_quote "$AGENT_ID_OUT") --text \"Summarize changed files, rationale, and any remaining risks in 5 bullets.\" --enter --wait-timeout 60s --idle-threshold 10s"
  fi
fi

STATUS_SEND_COMMAND=""
if [[ -n "$AGENT_ID_OUT" ]]; then
  STATUS_SEND_COMMAND="$STEP_SCRIPT_CMD send --agent $(shell_quote "$AGENT_ID_OUT") --text \"Provide a one-line progress status.\" --enter --wait-timeout 60s --idle-threshold 10s"
fi

RESTART_COMMAND=""
if [[ "$STATUS" == "session_exited" && -n "$WORKSPACE_ID_OUT" && -n "$ASSISTANT_OUT" ]]; then
  RESTART_COMMAND="$STEP_SCRIPT_CMD run --workspace $(shell_quote "$WORKSPACE_ID_OUT") --assistant $(shell_quote "$ASSISTANT_OUT") --prompt \"Continue from where you left off and provide a concise progress update.\" --wait-timeout 60s --idle-threshold 10s"
fi

DELIVERY_KEY="mode:${MODE}"
if [[ -n "$AGENT_ID_OUT" ]]; then
  DELIVERY_KEY="agent:${AGENT_ID_OUT}"
elif [[ -n "$SESSION_NAME" ]]; then
  DELIVERY_KEY="session:${SESSION_NAME}"
elif [[ -n "$WORKSPACE_ID_OUT" ]]; then
  DELIVERY_KEY="workspace:${WORKSPACE_ID_OUT}"
fi

DELIVERY_ACTION="send"
DELIVERY_PRIORITY=1
DELIVERY_RETRY_AFTER_SECONDS=0
DELIVERY_REPLACE_PREVIOUS=false
DELIVERY_DROP_PENDING=false

case "$STATUS" in
  timed_out)
    DELIVERY_ACTION="edit"
    DELIVERY_PRIORITY=2
    DELIVERY_REPLACE_PREVIOUS=true
    DELIVERY_RETRY_AFTER_SECONDS=5
    if [[ "$TIMED_OUT_STARTUP" == "true" ]]; then
      DELIVERY_RETRY_AFTER_SECONDS=8
    fi
    ;;
  needs_input)
    DELIVERY_ACTION="send"
    DELIVERY_PRIORITY=0
    DELIVERY_DROP_PENDING=true
    ;;
  session_exited)
    DELIVERY_ACTION="send"
    DELIVERY_PRIORITY=0
    DELIVERY_DROP_PENDING=true
    ;;
  idle)
    if [[ "$SUBSTANTIVE_OUTPUT" == "true" ]]; then
      DELIVERY_ACTION="send"
      DELIVERY_PRIORITY=1
      DELIVERY_DROP_PENDING=true
    else
      DELIVERY_ACTION="edit"
      DELIVERY_PRIORITY=2
      DELIVERY_REPLACE_PREVIOUS=true
      DELIVERY_RETRY_AFTER_SECONDS=5
    fi
    ;;
esac

OPENCLAW_PAYLOAD="$(jq -n \
  --arg mode "$MODE" \
  --arg status "$STATUS" \
  --arg summary "$SUMMARY" \
  --arg session_name "$SESSION_NAME" \
  --arg agent_id "$AGENT_ID_OUT" \
  --arg workspace_id "$WORKSPACE_ID_OUT" \
  --arg latest_line "$LATEST_LINE" \
  --arg response_summary "$RESPONSE_SUMMARY" \
  --arg delta "$DELTA" \
  --arg delta_compact "$DELTA_COMPACT" \
  --arg input_hint "$INPUT_HINT" \
  --argjson needs_input "$NEEDS_INPUT" \
  --argjson timed_out "$TIMED_OUT" \
  --argjson timed_out_startup "$TIMED_OUT_STARTUP" \
  --argjson session_exited "$SESSION_EXITED" \
  --argjson changed "$CHANGED" \
  --argjson substantive_output "$SUBSTANTIVE_OUTPUT" \
  --argjson blocked_permission_mode "$BLOCKED_PERMISSION_MODE" \
  --argjson recovered_from_capture "$RECOVERED_FROM_CAPTURE" \
  --argjson recovery_attempted "$RECOVERY_ATTEMPTED" \
  --argjson recovery_polls_used "$RECOVERY_POLLS_USED" \
  --arg next_action "$NEXT_ACTION" \
  --arg suggested_command "$SUGGESTED_COMMAND" \
  --arg status_emoji "$STATUS_EMOJI" \
  --arg channel_message "$OPENCLAW_MESSAGE" \
  --arg idempotency_key "$IDEMPOTENCY_KEY" \
  --arg assistant "$ASSISTANT_OUT" \
  --arg delivery_key "$DELIVERY_KEY" \
  --arg delivery_action "$DELIVERY_ACTION" \
  --argjson delivery_priority "$DELIVERY_PRIORITY" \
  --argjson delivery_retry_after_seconds "$DELIVERY_RETRY_AFTER_SECONDS" \
  --argjson delivery_replace_previous "$DELIVERY_REPLACE_PREVIOUS" \
  --argjson delivery_drop_pending "$DELIVERY_DROP_PENDING" \
  --arg step_verbosity "$STEP_VERBOSITY" \
  --arg inline_buttons_scope "$INLINE_BUTTONS_SCOPE" \
  --argjson inline_buttons_enabled "$INLINE_BUTTONS_ENABLED" \
  --argjson channel_chunk_chars "$OPENCLAW_CHUNK_CHARS" \
  --arg status_send_command "$STATUS_SEND_COMMAND" \
  --arg restart_command "$RESTART_COMMAND" \
  --arg test_remediation_command "$TEST_REMEDIATION_COMMAND" \
  --arg lint_remediation_command "$LINT_REMEDIATION_COMMAND" \
  --arg security_review_command "$SECURITY_REVIEW_COMMAND" \
  --arg review_changes_command "$REVIEW_CHANGES_COMMAND" \
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
    def quick_action($id; $lbl; $command; $style; $prompt):
      {
        id: $id,
        label: $lbl,
        command: $command,
        style: $style,
        callback_data: ("qa:" + $id),
        prompt: $prompt
      };
    (
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
          then quick_action("suggested"; "Continue"; $suggested_command; "primary"; "Continue from current state")
          else empty end),
        (if ($status_send_command | length) > 0
          then quick_action("status"; "Status"; $status_send_command; "primary"; "Request a one-line status update")
          else empty end),
        (if ($restart_command | length) > 0
          then quick_action(
            "restart";
            "Restart";
            $restart_command;
            "danger";
            "Restart the agent in the current workspace"
          )
          else empty end)
      ]
    ) as $quick_actions
    | {
      ok: true,
      mode: $mode,
      status: $status,
      status_emoji: $status_emoji,
      verbosity: $step_verbosity,
      summary: $summary,
      session_name: $session_name,
      agent_id: $agent_id,
      workspace_id: $workspace_id,
      idempotency_key: $idempotency_key,
      response: {
        latest_line: $latest_line,
        summary: $response_summary,
        delta: $delta,
        delta_compact: $delta_compact,
        needs_input: $needs_input,
        input_hint: $input_hint,
        timed_out: $timed_out,
        timed_out_startup: $timed_out_startup,
        session_exited: $session_exited,
        changed: $changed,
        substantive_output: $substantive_output
      },
      blocked_permission_mode: $blocked_permission_mode,
      recovered_from_capture: $recovered_from_capture,
      recovery: {
        attempted: $recovery_attempted,
        polls_used: $recovery_polls_used
      },
      delivery: {
        key: $delivery_key,
        action: $delivery_action,
        priority: $delivery_priority,
        retry_after_seconds: $delivery_retry_after_seconds,
        replace_previous: $delivery_replace_previous,
        drop_pending: $delivery_drop_pending,
        coalesce: true
      },
      next_action: $next_action,
      suggested_command: $suggested_command,
      quick_actions: $quick_actions,
      quick_action_map: ($quick_actions | map({key: .callback_data, value: .command}) | from_entries),
      quick_action_prompts: ($quick_actions | map({key: .callback_data, value: .prompt}) | from_entries),
      channel: (
        smart_split($channel_message; $channel_chunk_chars) as $chunks_raw
        | annotate_chunks($chunks_raw) as $chunks_meta
        | {
            message: $channel_message,
            verbosity: $step_verbosity,
            chunk_chars: $channel_chunk_chars,
            chunks: ($chunks_meta | map(.text)),
            chunks_meta: $chunks_meta,
            inline_buttons_scope: $inline_buttons_scope,
            inline_buttons_enabled: $inline_buttons_enabled,
            callback_data_max_bytes: 64,
            inline_buttons: (
              if $inline_buttons_enabled then
                build_action_rows($quick_actions; 2)
              else
                []
              end
            ),
            action_tokens: ($quick_actions | map(.callback_data)),
            actions_fallback: (
              if ($quick_actions | length) == 0 then
                ""
              else
                "Actions: " + action_tokens_text($quick_actions)
              end
            )
          }
      )
    }')"

if [[ "${OPENCLAW_STEP_SKIP_PRESENT:-false}" == "true" ]]; then
  printf '%s\n' "$OPENCLAW_PAYLOAD"
elif [[ -x "$OPENCLAW_PRESENT_SCRIPT" ]]; then
  "$OPENCLAW_PRESENT_SCRIPT" <<<"$OPENCLAW_PAYLOAD"
else
  printf '%s\n' "$OPENCLAW_PAYLOAD"
fi
