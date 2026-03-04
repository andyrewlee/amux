#!/usr/bin/env bash
# assistant-dx.sh
# Deterministic thin wrapper around typed amux JSON commands.
# No implicit retries, no implicit respawn loops, no hidden follow-ups.

set -euo pipefail

if ! command -v jq >/dev/null 2>&1; then
  preflight_channel="${AMUX_ASSISTANT_CHANNEL:-telegram}"
  preflight_channel="${preflight_channel//\\/\\\\}"
  preflight_channel="${preflight_channel//\"/\\\"}"
  cat <<JSON
{"ok":false,"command":"init","status":"command_error","summary":"assistant-dx requires jq in PATH.","next_action":"Install jq and retry.","suggested_command":"brew install jq","data":{"details":"jq_missing"},"quick_actions":[],"quick_action_by_id":{},"channel":{"message":"⚠️ assistant-dx requires jq in PATH.","chunks":["⚠️ assistant-dx requires jq in PATH."],"chunks_meta":[{"index":1,"total":1,"text":"⚠️ assistant-dx requires jq in PATH."}],"inline_buttons":[]},"assistant_ux":{"selected_channel":"${preflight_channel}"}}
JSON
  exit 0
fi

SELF_SCRIPT="skills/amux/scripts/assistant-dx.sh"
DEFAULT_REVIEW_PROMPT="Review current uncommitted changes. Return findings first ordered by severity with file references, then residual risks and test gaps."

AMUX_BIN_DEFAULT="$(command -v amux 2>/dev/null || true)"
if [[ -z "${AMUX_BIN_DEFAULT// }" ]]; then
  if [[ -x "/usr/local/bin/amux" ]]; then
    AMUX_BIN_DEFAULT="/usr/local/bin/amux"
  elif [[ -x "/opt/homebrew/bin/amux" ]]; then
    AMUX_BIN_DEFAULT="/opt/homebrew/bin/amux"
  else
    AMUX_BIN_DEFAULT="amux"
  fi
fi
AMUX_BIN="${AMUX_BIN:-$AMUX_BIN_DEFAULT}"

usage() {
  cat <<'USAGE'
Usage:
  assistant-dx.sh task start --workspace <id> [--assistant <name>] --prompt <text> [--wait-timeout <dur>] [--idle-threshold <dur>] [--start-lock-ttl <dur>] [--allow-new-run] [--idempotency-key <key>]
  assistant-dx.sh task status --workspace <id> [--assistant <name>]
  assistant-dx.sh start --workspace <id> [--assistant <name>] --prompt <text> [task flags...]
  assistant-dx.sh review --workspace <id> [--assistant <name>] [--prompt <text>] [task flags...] [--monitor-timeout <dur>] [--poll-interval <dur>] [--no-monitor]
  assistant-dx.sh continue [--agent <id> | --workspace <id>] [--assistant <name>] [--text <text>] [--enter]

  assistant-dx.sh status [--workspace <id>] [--assistant <name>] [--include-stale]
  assistant-dx.sh alerts [same flags as status]
  assistant-dx.sh guide [--workspace <id>] [--assistant <name>] [--task <text>]

  assistant-dx.sh project list [--query <text>]
  assistant-dx.sh project add [--path <repo> | --cwd]
  assistant-dx.sh workspace list [--project <repo> | --repo <repo> | --all] [--archived]
  assistant-dx.sh workspace create <name> --project <repo> [--assistant <name>] [--base <ref>]

  assistant-dx.sh terminal run --workspace <id> --text <cmd> [--enter]
  assistant-dx.sh terminal logs --workspace <id> [--lines <n>]
  assistant-dx.sh git ship --workspace <id> [--message <msg>] [--push]
  assistant-dx.sh assistants
  assistant-dx.sh cleanup [--older-than <dur>] [--yes]

Notes:
  - workflow commands were removed; use task start/status + continue explicitly.
USAGE
}

selected_channel() {
  local channel="${AMUX_ASSISTANT_CHANNEL:-telegram}"
  [[ -z "${channel// }" ]] && channel="telegram"
  printf '%s' "$channel"
}

quote_cmd() {
  printf '%q' "$1"
}

normalize_actions() {
  local actions_json="${1:-[]}"
  jq -c '
    if type != "array" then [] else
      map({
        id: (.id // .action_id // "action"),
        action_id: (.id // .action_id // "action"),
        label: (.label // "Action"),
        command: (.command // ""),
        style: (.style // "primary"),
        prompt: (.prompt // "")
      })
    end
  ' <<<"$actions_json"
}

actions_map() {
  jq -c 'reduce .[] as $a ({}; .[$a.action_id] = ($a.command // ""))'
}

append_action() {
  local actions_json="$1"
  local id="$2"
  local label="$3"
  local command="$4"
  local style="${5:-primary}"
  local prompt="${6:-}"
  jq -c --arg id "$id" --arg action_label "$label" --arg command "$command" --arg style "$style" --arg prompt "$prompt" \
    '. + [{id:$id,label:$action_label,command:$command,style:$style,prompt:$prompt}]' <<<"$actions_json"
}

emit_result() {
  local ok_json="$1"
  local command="$2"
  local status="$3"
  local summary="$4"
  local next_action="$5"
  local suggested_command="$6"
  local data_json="${7-}"
  local quick_actions_json="${8-}"
  local message="$9"

  [[ -z "${data_json// }" ]] && data_json='{}'
  [[ -z "${quick_actions_json// }" ]] && quick_actions_json='[]'

  local channel qa_norm qa_map
  channel="$(selected_channel)"
  qa_norm="$(normalize_actions "$quick_actions_json")"
  qa_map="$(actions_map <<<"$qa_norm")"

  jq -cn \
    --argjson ok "$ok_json" \
    --arg command "$command" \
    --arg status "$status" \
    --arg summary "$summary" \
    --arg next_action "$next_action" \
    --arg suggested_command "$suggested_command" \
    --arg message "$message" \
    --arg channel "$channel" \
    --argjson data "$data_json" \
    --argjson quick_actions "$qa_norm" \
    --argjson quick_action_by_id "$qa_map" \
    '{
      ok: $ok,
      command: $command,
      status: $status,
      summary: $summary,
      next_action: $next_action,
      suggested_command: $suggested_command,
      data: $data,
      quick_actions: $quick_actions,
      quick_action_by_id: $quick_action_by_id,
      channel: {
        message: $message,
        chunks: [$message],
        chunks_meta: [{index:1,total:1,text:$message}],
        inline_buttons: []
      },
      assistant_ux: {
        selected_channel: $channel
      }
    }'
}

emit_error() {
  local command="$1"
  local message="$2"
  local details="${3:-}"
  emit_result "false" "$command" "command_error" "$message" "Check command usage and retry." "" "$(jq -cn --arg details "$details" '{details:$details}')" "[]" "⚠️ $message"
}

amux_get_ok_json() {
  local out_ref="$1"
  local command_name="$2"
  shift 2

  local stdout_file stderr_file stdout_raw stderr_raw stdout_trim details rc=0
  stdout_file="$(mktemp "${TMPDIR:-/tmp}/amux_stdout.XXXXXX")" || {
    emit_error "$command_name" "amux wrapper failed to allocate temp file" "mktemp stdout failed"
    return 1
  }
  stderr_file="$(mktemp "${TMPDIR:-/tmp}/amux_stderr.XXXXXX")" || {
    rm -f "$stdout_file"
    emit_error "$command_name" "amux wrapper failed to allocate temp file" "mktemp stderr failed"
    return 1
  }
  if "$AMUX_BIN" --json "$@" >"$stdout_file" 2>"$stderr_file"; then
    rc=0
  else
    rc=$?
  fi
  stdout_raw="$(<"$stdout_file")"
  stderr_raw="$(<"$stderr_file")"
  rm -f "$stdout_file" "$stderr_file"

  # In --json mode, treat a non-empty valid envelope as authoritative even if exit code is non-zero.
  stdout_trim="${stdout_raw//[[:space:]]/}"
  if [[ -n "$stdout_trim" ]] && jq -e 'type == "object" and has("ok")' >/dev/null 2>&1 <<<"$stdout_raw"; then
    if [[ "$(jq -r '.ok // false' <<<"$stdout_raw")" != "true" ]]; then
      emit_error "$command_name" "$(jq -r '.error.message // "amux command failed"' <<<"$stdout_raw")" "$(jq -r '.error.code // "amux_error"' <<<"$stdout_raw")"
      return 1
    fi
    printf -v "$out_ref" '%s' "$stdout_raw"
    return 0
  fi

  details="$stderr_raw"
  [[ -z "${details// }" ]] && details="$stdout_raw"
  if (( rc != 0 )); then
    emit_error "$command_name" "amux command failed: $*" "$details"
  else
    emit_error "$command_name" "amux returned invalid JSON" "$details"
  fi
  return 1
}

map_task_status() {
  local status="$1"
  local overall="$2"
  case "$status" in
    needs_input) printf 'needs_input'; return ;;
    attention) printf 'attention'; return ;;
  esac
  case "$overall" in
    needs_input) printf 'needs_input' ;;
    in_progress|session_exited|partial|partial_budget|timed_out) printf 'attention' ;;
    *) printf 'ok' ;;
  esac
}

build_task_start_cmd() {
  local workspace="$1"
  local assistant="$2"
  local prompt="$3"
  printf '%s task start --workspace %s --assistant %s --prompt %s' \
    "$SELF_SCRIPT" "$(quote_cmd "$workspace")" "$(quote_cmd "$assistant")" "$(quote_cmd "$prompt")"
}

build_task_status_cmd() {
  local workspace="$1"
  local assistant="$2"
  printf '%s task status --workspace %s --assistant %s' \
    "$SELF_SCRIPT" "$(quote_cmd "$workspace")" "$(quote_cmd "$assistant")"
}

build_task_continue_cmd() {
  local workspace="$1"
  local assistant="$2"
  local text="${3:-}"
  local cmd
  cmd="$(printf '%s continue --workspace %s --assistant %s' \
    "$SELF_SCRIPT" "$(quote_cmd "$workspace")" "$(quote_cmd "$assistant")")"
  if [[ -n "${text// }" ]]; then
    cmd+=" --text $(quote_cmd "$text")"
  fi
  cmd+=" --enter"
  printf '%s' "$cmd"
}

build_task_followups() {
  local workspace="$1"
  local assistant="$2"
  local task_status="$3"
  local overall="$4"
  local prompt="$5"
  local input_hint="$6"
  local agent_id="$7"

  local status_cmd continue_cmd start_cmd suggested quick_actions followup_text start_prompt
  status_cmd="$(build_task_status_cmd "$workspace" "$assistant")"
  suggested="$status_cmd"
  quick_actions='[]'
  quick_actions="$(append_action "$quick_actions" "status" "Status" "$status_cmd" "primary" "Check task status")"
  start_prompt="$prompt"
  [[ -z "${start_prompt// }" ]] && start_prompt="Continue from current state and report status plus next action."

  if [[ "$task_status" == "needs_input" || "$overall" == "needs_input" ]]; then
    followup_text="$input_hint"
    [[ -z "${followup_text//[[:space:]]/}" ]] && followup_text="Reply with the exact option needed, then continue and report status plus blockers."
    continue_cmd="$(build_task_continue_cmd "$workspace" "$assistant" "$followup_text")"
    suggested="$continue_cmd"
    quick_actions="$(append_action "$quick_actions" "continue" "Continue" "$continue_cmd" "primary" "Send response and continue")"
  elif [[ "$overall" == "in_progress" ]]; then
    :
  elif [[ "$overall" == "session_exited" || -z "${agent_id// }" ]]; then
    start_cmd="$(build_task_start_cmd "$workspace" "$assistant" "$start_prompt")"
    suggested="$start_cmd"
    quick_actions="$(append_action "$quick_actions" "start" "Start" "$start_cmd" "primary" "Start another bounded run")"
  else
    continue_cmd="$(build_task_continue_cmd "$workspace" "$assistant" "Continue from current state and provide status plus next action.")"
    suggested="$continue_cmd"
    quick_actions="$(append_action "$quick_actions" "continue" "Continue" "$continue_cmd" "primary" "Send a follow-up instruction")"
    start_cmd="$(build_task_start_cmd "$workspace" "$assistant" "$start_prompt")"
    quick_actions="$(append_action "$quick_actions" "start" "Start" "$start_cmd" "secondary" "Start another bounded run")"
  fi

  jq -cn --arg suggested "$suggested" --argjson actions "$quick_actions" \
    '{suggested_command:$suggested,quick_actions:$actions}'
}

parse_duration_seconds() {
  local raw="${1:-}"
  raw="${raw//[[:space:]]/}"
  if [[ -z "${raw// }" ]]; then
    return 1
  fi
  if [[ "$raw" =~ ^[0-9]+$ ]]; then
    printf '%s' "$raw"
    return 0
  fi
  if [[ "$raw" =~ ^([0-9]+)(s|m|h)$ ]]; then
    local n="${BASH_REMATCH[1]}"
    local u="${BASH_REMATCH[2]}"
    case "$u" in
      s) printf '%s' "$n" ;;
      m) printf '%s' "$((n * 60))" ;;
      h) printf '%s' "$((n * 3600))" ;;
      *) return 1 ;;
    esac
    return 0
  fi
  return 1
}

task_reached_terminal_state() {
  local data_json="$1"
  local overall task_status
  overall="$(jq -r '.overall_status // ""' <<<"$data_json")"
  task_status="$(jq -r '.status // ""' <<<"$data_json")"
  if [[ "$task_status" == "needs_input" ]]; then
    return 0
  fi
  [[ "$overall" != "in_progress" ]]
}

wait_for_task_terminal() {
  local data_ref="$1"
  local command_name="$2"
  local workspace="$3"
  local assistant="$4"
  local monitor_timeout="$5"
  local poll_interval="$6"
  local current_data="${!data_ref-}"

  local timeout_s poll_s started elapsed
  timeout_s="$(parse_duration_seconds "$monitor_timeout" || true)"
  poll_s="$(parse_duration_seconds "$poll_interval" || true)"
  [[ -z "${timeout_s// }" ]] && timeout_s=480
  [[ -z "${poll_s// }" ]] && poll_s=15
  (( timeout_s < 1 )) && { printf -v "$data_ref" '%s' "$current_data"; return; }
  (( poll_s < 1 )) && poll_s=1

  started="$(date +%s)"
  while true; do
    if task_reached_terminal_state "$current_data"; then
      break
    fi
    elapsed=$(( $(date +%s) - started ))
    (( elapsed >= timeout_s )) && break
    sleep "$poll_s"

    local status_out next_data
    if ! amux_get_ok_json status_out "$command_name" task status --workspace "$workspace" --assistant "$assistant"; then
      return 1
    fi
    next_data="$(jq -c '.data // {}' <<<"$status_out")"
    current_data="$next_data"
  done

  printf -v "$data_ref" '%s' "$current_data"
}

emit_task_result() {
  local command_name="$1"
  local workspace="$2"
  local assistant="$3"
  local prompt="$4"
  local data_json="$5"

  local task_status overall status summary next_action followups_json suggested quick_actions message
  task_status="$(jq -r '.status // ""' <<<"$data_json")"
  overall="$(jq -r '.overall_status // ""' <<<"$data_json")"
  status="$(map_task_status "$task_status" "$overall")"
  summary="$(jq -r '.summary // "Task completed."' <<<"$data_json")"
  next_action="$(jq -r '.next_action // "Check status and continue if needed."' <<<"$data_json")"
  followups_json="$(build_task_followups "$workspace" "$assistant" "$task_status" "$overall" "$prompt" "$(jq -r '.input_hint // ""' <<<"$data_json")" "$(jq -r '.agent_id // ""' <<<"$data_json")")"
  suggested="$(jq -r '.suggested_command // ""' <<<"$followups_json")"
  quick_actions="$(jq -c '.quick_actions // []' <<<"$followups_json")"
  message="$summary"
  [[ -n "${next_action// }" ]] && message+=$'\n'"Next: $next_action"

  emit_result "true" "$command_name" "$status" "$summary" "$next_action" "$suggested" \
    "$(jq -cn --arg workspace "$workspace" --arg assistant "$assistant" --arg prompt "$prompt" --argjson task "$data_json" '{workspace:$workspace,assistant:$assistant,prompt:$prompt,task:$task}')" \
    "$quick_actions" \
    "$message"
}

parse_task_start_flags() {
  local workspace_ref="$1"
  local assistant_ref="$2"
  local prompt_ref="$3"
  local wait_timeout_ref="$4"
  local idle_threshold_ref="$5"
  local start_lock_ttl_ref="$6"
  local idempotency_ref="$7"
  local allow_new_run_ref="$8"
  shift 8

  printf -v "$workspace_ref" '%s' ""
  printf -v "$assistant_ref" '%s' "codex"
  printf -v "$prompt_ref" '%s' ""
  printf -v "$wait_timeout_ref" '%s' ""
  printf -v "$idle_threshold_ref" '%s' ""
  printf -v "$start_lock_ttl_ref" '%s' ""
  printf -v "$idempotency_ref" '%s' ""
  printf -v "$allow_new_run_ref" '%s' "false"

  while [[ $# -gt 0 ]]; do
    case "$1" in
      --workspace|--assistant|--prompt|--wait-timeout|--idle-threshold|--start-lock-ttl|--idempotency-key)
        [[ $# -ge 2 ]] || return 1
        case "$1" in
          --workspace) printf -v "$workspace_ref" '%s' "$2" ;;
          --assistant) printf -v "$assistant_ref" '%s' "$2" ;;
          --prompt) printf -v "$prompt_ref" '%s' "$2" ;;
          --wait-timeout) printf -v "$wait_timeout_ref" '%s' "$2" ;;
          --idle-threshold) printf -v "$idle_threshold_ref" '%s' "$2" ;;
          --start-lock-ttl) printf -v "$start_lock_ttl_ref" '%s' "$2" ;;
          --idempotency-key) printf -v "$idempotency_ref" '%s' "$2" ;;
        esac
        shift 2
        ;;
      --allow-new-run)
        printf -v "$allow_new_run_ref" '%s' "true"
        shift
        ;;
      --max-steps|--turn-budget)
        [[ $# -ge 2 ]] || return 1
        shift 2
        ;;
      *)
        return 1
        ;;
    esac
  done
  return 0
}

cmd_task_start_like() {
  local command_name="$1"
  shift

  local workspace assistant prompt wait_timeout idle_threshold start_lock_ttl idempotency allow_new_run
  if ! parse_task_start_flags workspace assistant prompt wait_timeout idle_threshold start_lock_ttl idempotency allow_new_run "$@"; then
    emit_error "$command_name" "invalid flags for $command_name"
    return 0
  fi
  if [[ -z "${workspace// }" || -z "${prompt// }" ]]; then
    emit_error "$command_name" "missing required flags: --workspace and --prompt"
    return 0
  fi

  local amux_args=(task start --workspace "$workspace" --assistant "$assistant" --prompt "$prompt")
  [[ -n "${wait_timeout// }" ]] && amux_args+=(--wait-timeout "$wait_timeout")
  [[ -n "${idle_threshold// }" ]] && amux_args+=(--idle-threshold "$idle_threshold")
  [[ -n "${start_lock_ttl// }" ]] && amux_args+=(--start-lock-ttl "$start_lock_ttl")
  [[ -n "${idempotency// }" ]] && amux_args+=(--idempotency-key "$idempotency")
  [[ "$allow_new_run" == "true" ]] && amux_args+=(--allow-new-run)

  local out data_json
  if ! amux_get_ok_json out "$command_name" "${amux_args[@]}"; then
    return 0
  fi
  data_json="$(jq -c '.data // {}' <<<"$out")"
  emit_task_result "$command_name" "$workspace" "$assistant" "$prompt" "$data_json"
}

cmd_task_status() {
  local workspace=""
  local assistant="codex"
  while [[ $# -gt 0 ]]; do
    case "$1" in
      --workspace|--assistant)
        [[ $# -ge 2 ]] || { emit_error "task.status" "missing value for $1"; return 0; }
        [[ "$1" == "--workspace" ]] && workspace="$2" || assistant="$2"
        shift 2
        ;;
      *) emit_error "task.status" "unknown flag: $1"; return 0 ;;
    esac
  done
  [[ -z "${workspace// }" ]] && { emit_error "task.status" "missing required flag: --workspace"; return 0; }

  local out data_json
  if ! amux_get_ok_json out "task.status" task status --workspace "$workspace" --assistant "$assistant"; then
    return 0
  fi
  data_json="$(jq -c '.data // {}' <<<"$out")"
  emit_task_result "task.status" "$workspace" "$assistant" "" "$data_json"
}

cmd_start() {
  cmd_task_start_like "start" "$@"
}

cmd_review() {
  local workspace=""
  local assistant="codex"
  local prompt="$DEFAULT_REVIEW_PROMPT"
  local wait_timeout=""
  local idle_threshold=""
  local start_lock_ttl=""
  local idempotency=""
  local allow_new_run="false"
  local monitor="true"
  local monitor_timeout="${AMUX_ASSISTANT_DX_MONITOR_TIMEOUT:-8m}"
  local poll_interval="${AMUX_ASSISTANT_DX_MONITOR_POLL_INTERVAL:-15s}"

  while [[ $# -gt 0 ]]; do
    case "$1" in
      --workspace|--assistant|--prompt|--wait-timeout|--idle-threshold|--start-lock-ttl|--idempotency-key|--max-steps|--turn-budget|--monitor-timeout|--poll-interval)
        [[ $# -ge 2 ]] || { emit_error "review" "missing value for $1"; return 0; }
        case "$1" in
          --workspace) workspace="$2" ;;
          --assistant) assistant="$2" ;;
          --prompt) prompt="$2" ;;
          --wait-timeout) wait_timeout="$2" ;;
          --idle-threshold) idle_threshold="$2" ;;
          --start-lock-ttl) start_lock_ttl="$2" ;;
          --idempotency-key) idempotency="$2" ;;
          --monitor-timeout) monitor_timeout="$2" ;;
          --poll-interval) poll_interval="$2" ;;
          --max-steps|--turn-budget) : ;;
        esac
        shift 2
        ;;
      --allow-new-run)
        allow_new_run="true"
        shift
        ;;
      --no-monitor)
        monitor="false"
        shift
        ;;
      *) emit_error "review" "unknown flag: $1"; return 0 ;;
    esac
  done

  if [[ -z "${workspace// }" ]]; then
    emit_error "review" "missing required flag: --workspace"
    return 0
  fi

  local amux_args=(task start --workspace "$workspace" --assistant "$assistant" --prompt "$prompt")
  [[ -n "${wait_timeout// }" ]] && amux_args+=(--wait-timeout "$wait_timeout")
  [[ -n "${idle_threshold// }" ]] && amux_args+=(--idle-threshold "$idle_threshold")
  [[ -n "${start_lock_ttl// }" ]] && amux_args+=(--start-lock-ttl "$start_lock_ttl")
  [[ -n "${idempotency// }" ]] && amux_args+=(--idempotency-key "$idempotency")
  [[ "$allow_new_run" == "true" ]] && amux_args+=(--allow-new-run)

  local out data_json
  if ! amux_get_ok_json out "review" "${amux_args[@]}"; then
    return 0
  fi
  data_json="$(jq -c '.data // {}' <<<"$out")"
  if [[ "$monitor" == "true" ]]; then
    if ! wait_for_task_terminal data_json "review" "$workspace" "$assistant" "$monitor_timeout" "$poll_interval"; then
      return 0
    fi
  fi

  emit_task_result "review" "$workspace" "$assistant" "$prompt" "$data_json"
}

cmd_continue() {
  local agent=""
  local workspace=""
  local assistant="codex"
  local text=""
  local enter="false"
  local wait_timeout="60s"
  local idle_threshold="10s"

  while [[ $# -gt 0 ]]; do
    case "$1" in
      --agent|--workspace|--assistant|--text|--wait-timeout|--idle-threshold)
        [[ $# -ge 2 ]] || { emit_error "continue" "missing value for $1"; return 0; }
        case "$1" in
          --agent) agent="$2" ;;
          --workspace) workspace="$2" ;;
          --assistant) assistant="$2" ;;
          --text) text="$2" ;;
          --wait-timeout) wait_timeout="$2" ;;
          --idle-threshold) idle_threshold="$2" ;;
        esac
        shift 2
        ;;
      --enter)
        enter="true"
        shift
        ;;
      --auto-start)
        # Intentionally ignore auto-start to prevent hidden respawn loops.
        shift
        ;;
      *) emit_error "continue" "unknown flag: $1"; return 0 ;;
    esac
  done

  if [[ -z "${agent// }" ]]; then
    [[ -z "${workspace// }" ]] && { emit_error "continue" "missing target: pass --agent or --workspace"; return 0; }
    local status_out status_data
    if ! amux_get_ok_json status_out "continue" task status --workspace "$workspace" --assistant "$assistant"; then
      return 0
    fi
    status_data="$(jq -c '.data // {}' <<<"$status_out")"
    agent="$(jq -r '.agent_id // ""' <<<"$status_data")"
    [[ -z "${agent// }" ]] && { emit_error "continue" "no active agent for workspace $workspace"; return 0; }
  fi

  local send_args=(agent send --agent "$agent")
  [[ -n "${text// }" ]] && send_args+=(--text "$text")
  [[ "$enter" == "true" ]] && send_args+=(--enter)
  send_args+=(--wait --wait-timeout "$wait_timeout" --idle-threshold "$idle_threshold")

  local out data_json resp_json raw_status status summary next_action suggested actions message
  if ! amux_get_ok_json out "continue" "${send_args[@]}"; then
    return 0
  fi
  data_json="$(jq -c '.data // {}' <<<"$out")"
  resp_json="$(jq -c '.response // {}' <<<"$data_json")"
  raw_status="$(jq -r '.status // empty' <<<"$resp_json")"
  [[ -z "${raw_status// }" ]] && raw_status="$(jq -r '.status // "idle"' <<<"$data_json")"

  case "$raw_status" in
    needs_input) status="needs_input" ;;
    timed_out|session_exited|partial|partial_budget|attention) status="attention" ;;
    *) status="ok" ;;
  esac

  summary="$(jq -r '.summary // empty' <<<"$resp_json")"
  [[ -z "${summary// }" ]] && summary="$(jq -r '.summary // "Continue completed."' <<<"$data_json")"
  next_action="$(jq -r '.next_action // "Check status and continue with the next step."' <<<"$resp_json")"

  actions='[]'
  suggested=""
  if [[ -n "${workspace// }" ]]; then
    suggested="$(build_task_status_cmd "$workspace" "$assistant")"
    actions="$(append_action "$actions" "status" "Status" "$suggested" "primary" "Check task status")"
  fi

  message="$summary"
  message+=$'\n'"Next: $next_action"

  emit_result "true" "continue" "$status" "$summary" "$next_action" "$suggested" \
    "$(jq -cn --arg agent "$agent" --arg workspace "$workspace" --arg assistant "$assistant" --argjson send "$data_json" '{agent:$agent,workspace:$workspace,assistant:$assistant,send:$send}')" \
    "$actions" \
    "$message"
}

cmd_project_list() {
  local query=""
  while [[ $# -gt 0 ]]; do
    case "$1" in
      --query)
        [[ $# -ge 2 ]] || { emit_error "project.list" "missing value for --query"; return 0; }
        query="$2"
        shift 2
        ;;
      --limit|--page)
        [[ $# -ge 2 ]] || { emit_error "project.list" "missing value for $1"; return 0; }
        shift 2
        ;;
      *) emit_error "project.list" "unknown flag: $1"; return 0 ;;
    esac
  done

  local out projects_json filtered_json count first_path suggested actions summary
  if ! amux_get_ok_json out "project.list" project list; then
    return 0
  fi
  projects_json="$(jq -c '.data // []' <<<"$out")"
  if [[ -n "${query// }" ]]; then
    filtered_json="$(jq -c --arg q "$query" '
      map(select((((.name // "") + " " + (.path // "")) | ascii_downcase) | contains($q | ascii_downcase)))
    ' <<<"$projects_json")"
  else
    filtered_json="$projects_json"
  fi
  count="$(jq -r 'length' <<<"$filtered_json")"
  first_path="$(jq -r '.[]?.path // ""' <<<"$filtered_json" | head -n 1)"
  suggested="$SELF_SCRIPT workspace list --all"
  actions='[]'
  if [[ -n "${first_path// }" ]]; then
    suggested="$SELF_SCRIPT workspace list --project $(quote_cmd "$first_path")"
    actions="$(append_action "$actions" "workspaces" "Workspaces" "$suggested" "primary" "List workspaces for first project")"
  else
    actions="$(append_action "$actions" "add" "Add Project" "$SELF_SCRIPT project add --cwd" "primary" "Register current repo as project")"
  fi
  summary="$count project(s)"

  emit_result "true" "project.list" "ok" "$summary" "Choose project/workspace and start task." "$suggested" \
    "$(jq -cn --arg query "$query" --argjson projects "$filtered_json" '{query:$query,projects:$projects}')" \
    "$actions" \
    "✅ $summary"
}

cmd_project_add() {
  local path=""
  local use_cwd="false"
  while [[ $# -gt 0 ]]; do
    case "$1" in
      --path)
        [[ $# -ge 2 ]] || { emit_error "project.add" "missing value for --path"; return 0; }
        path="$2"
        shift 2
        ;;
      --cwd)
        use_cwd="true"
        shift
        ;;
      --workspace|--assistant|--base)
        [[ $# -ge 2 ]] || { emit_error "project.add" "missing value for $1"; return 0; }
        shift 2
        ;;
      *) emit_error "project.add" "unknown flag: $1"; return 0 ;;
    esac
  done
  [[ "$use_cwd" == "true" && -z "${path// }" ]] && path="$(pwd)"
  [[ -z "${path// }" ]] && { emit_error "project.add" "missing required flag: --path (or --cwd)"; return 0; }

  local out data_json
  if ! amux_get_ok_json out "project.add" project add "$path"; then
    return 0
  fi
  data_json="$(jq -c '.data // {}' <<<"$out")"
  emit_result "true" "project.add" "ok" "Project registered." "Create/list workspace next." "$SELF_SCRIPT workspace list --project $(quote_cmd "$path")" "$data_json" "[]" "✅ Project registered."
}

cmd_workspace_list() {
  local args=(workspace list)
  while [[ $# -gt 0 ]]; do
    case "$1" in
      --project|--repo)
        [[ $# -ge 2 ]] || { emit_error "workspace.list" "missing value for $1"; return 0; }
        args+=("$1" "$2")
        shift 2
        ;;
      --all)
        args+=(--all)
        shift
        ;;
      --archived)
        args+=(--archived)
        shift
        ;;
      *) emit_error "workspace.list" "unknown flag: $1"; return 0 ;;
    esac
  done
  local out rows count first_ws suggested actions
  if ! amux_get_ok_json out "workspace.list" "${args[@]}"; then
    return 0
  fi
  rows="$(jq -c '.data // []' <<<"$out")"
  count="$(jq -r 'length' <<<"$rows")"
  first_ws="$(jq -r '.[]?.id // ""' <<<"$rows" | head -n 1)"
  suggested="$SELF_SCRIPT project list"
  actions='[]'
  if [[ -n "${first_ws// }" ]]; then
    suggested="$SELF_SCRIPT status --workspace $(quote_cmd "$first_ws")"
    actions="$(append_action "$actions" "status" "Status" "$suggested" "primary" "Check first workspace status")"
  fi
  emit_result "true" "workspace.list" "ok" "$count workspace(s)" "Start or continue task in target workspace." "$suggested" "$(jq -cn --argjson workspaces "$rows" '{workspaces:$workspaces}')" "$actions" "✅ $count workspace(s)"
}

cmd_workspace_create() {
  local name=""
  local project=""
  local assistant=""
  local base=""
  while [[ $# -gt 0 ]]; do
    case "$1" in
      --project|--assistant|--base)
        [[ $# -ge 2 ]] || { emit_error "workspace.create" "missing value for $1"; return 0; }
        case "$1" in
          --project) project="$2" ;;
          --assistant) assistant="$2" ;;
          --base) base="$2" ;;
        esac
        shift 2
        ;;
      --name)
        emit_error "workspace.create" "--name is no longer supported; use positional name: workspace create <name> --project <repo>"
        return 0
        ;;
      --from-workspace|--scope|--idempotency-key)
        emit_error "workspace.create" "$1 is not supported by amux workspace create"
        return 0
        ;;
      --*)
        emit_error "workspace.create" "unknown flag: $1"
        return 0
        ;;
      *)
        if [[ -n "${name// }" ]]; then
          emit_error "workspace.create" "unexpected positional argument: $1"
          return 0
        fi
        name="$1"
        shift
        ;;
    esac
  done

  [[ -z "${name// }" ]] && { emit_error "workspace.create" "missing required positional argument: <name>"; return 0; }
  [[ -z "${project// }" ]] && { emit_error "workspace.create" "missing required flag: --project"; return 0; }

  local args=(workspace create "$name" --project "$project")
  [[ -n "${assistant// }" ]] && args+=(--assistant "$assistant")
  [[ -n "${base// }" ]] && args+=(--base "$base")

  local out data_json ws_id ws_assistant
  if ! amux_get_ok_json out "workspace.create" "${args[@]}"; then
    return 0
  fi
  data_json="$(jq -c '.data // {}' <<<"$out")"
  ws_id="$(jq -r '.id // ""' <<<"$data_json")"
  ws_assistant="$(jq -r '.assistant // "codex"' <<<"$data_json")"
  local suggested="$SELF_SCRIPT workspace list --all"
  [[ -n "${ws_id// }" ]] && suggested="$(build_task_start_cmd "$ws_id" "$ws_assistant" "Continue from current state and report status plus next action.")"
  emit_result "true" "workspace.create" "ok" "Workspace created." "Start a bounded task." "$suggested" "$data_json" "[]" "✅ Workspace created."
}

cmd_status() {
  local workspace=""
  local assistant="codex"
  local include_stale="false"
  while [[ $# -gt 0 ]]; do
    case "$1" in
      --workspace|--assistant)
        [[ $# -ge 2 ]] || { emit_error "status" "missing value for $1"; return 0; }
        [[ "$1" == "--workspace" ]] && workspace="$2" || assistant="$2"
        shift 2
        ;;
      --include-stale)
        include_stale="true"
        shift
        ;;
      --project|--limit|--capture-lines|--capture-agents|--older-than|--alerts-only|--recent-workspaces)
        if [[ "$1" == "--alerts-only" ]]; then
          shift
        else
          [[ $# -ge 2 ]] || { emit_error "status" "missing value for $1"; return 0; }
          shift 2
        fi
        ;;
      *) emit_error "status" "unknown flag: $1"; return 0 ;;
    esac
  done

  if [[ -n "${workspace// }" ]]; then
    local out data_json task_status overall status summary next_action followups_json suggested actions msg
    if ! amux_get_ok_json out "status" task status --workspace "$workspace" --assistant "$assistant"; then
      return 0
    fi
    data_json="$(jq -c '.data // {}' <<<"$out")"
    task_status="$(jq -r '.status // ""' <<<"$data_json")"
    overall="$(jq -r '.overall_status // ""' <<<"$data_json")"
    status="$(map_task_status "$task_status" "$overall")"
    summary="$(jq -r '.summary // "Workspace status captured."' <<<"$data_json")"
    next_action="$(jq -r '.next_action // "Continue with the next focused step."' <<<"$data_json")"
    followups_json="$(build_task_followups "$workspace" "$assistant" "$task_status" "$overall" "" "$(jq -r '.input_hint // ""' <<<"$data_json")" "$(jq -r '.agent_id // ""' <<<"$data_json")")"
    suggested="$(jq -r '.suggested_command // ""' <<<"$followups_json")"
    actions="$(jq -c '.quick_actions // []' <<<"$followups_json")"
    msg="$summary"
    msg+=$'\n'"Workspace: $workspace"
    msg+=$'\n'"Next: $next_action"
    emit_result "true" "status" "$status" "$summary" "$next_action" "$suggested" \
      "$(jq -cn --arg workspace "$workspace" --arg assistant "$assistant" --argjson include_stale "$include_stale" --argjson task "$data_json" '{workspace:$workspace,assistant:$assistant,include_stale:$include_stale,task:$task}')" \
      "$actions" \
      "$msg"
    return 0
  fi

  local ws_args=(workspace list)
  [[ "$include_stale" == "true" ]] && ws_args+=(--all)

  local ws_out sess_out ws_rows sess_rows total agent_sessions first_ws first_agent_ws suggested actions summary next_action
  if ! amux_get_ok_json ws_out "status" "${ws_args[@]}"; then
    return 0
  fi
  if ! amux_get_ok_json sess_out "status" session list; then
    return 0
  fi
  ws_rows="$(jq -c '.data // []' <<<"$ws_out")"
  sess_rows="$(jq -c '.data // []' <<<"$sess_out")"
  total="$(jq -r 'length' <<<"$ws_rows")"
  agent_sessions="$(jq -r '[.[] | select((.type // "") == "agent")] | length' <<<"$sess_rows")"
  first_ws="$(jq -r '.[]?.id // ""' <<<"$ws_rows" | head -n 1)"
  first_agent_ws="$(jq -r '[.[] | select((.type // "") == "agent") | (.workspace_id // "") | select(length > 0)] | unique | .[0] // ""' <<<"$sess_rows")"
  actions='[]'

  summary="$total workspace(s), $agent_sessions agent session(s)."
  if [[ "$agent_sessions" -gt 0 ]]; then
    next_action="Check status on the target workspace."
    if [[ -n "${first_agent_ws// }" ]]; then
      suggested="$SELF_SCRIPT status --workspace $(quote_cmd "$first_agent_ws") --assistant $(quote_cmd "$assistant")"
      actions="$(append_action "$actions" "status" "Status" "$suggested" "primary" "Open active agent workspace status")"
    elif [[ -n "${first_ws// }" ]]; then
      suggested="$SELF_SCRIPT status --workspace $(quote_cmd "$first_ws") --assistant $(quote_cmd "$assistant")"
      actions="$(append_action "$actions" "status" "Status" "$suggested" "primary" "Open workspace status")"
    else
      suggested="$SELF_SCRIPT workspace list --all"
      actions="$(append_action "$actions" "workspaces" "Workspaces" "$suggested" "primary" "List all workspaces")"
    fi
  else
    next_action="Start a bounded task."
    if [[ -n "${first_ws// }" ]]; then
      suggested="$(build_task_start_cmd "$first_ws" "$assistant" "Continue from current state and report status plus next action.")"
      actions="$(append_action "$actions" "start" "Start" "$suggested" "primary" "Start task in first workspace")"
    else
      suggested="$SELF_SCRIPT workspace list --all"
      actions="$(append_action "$actions" "workspaces" "Workspaces" "$suggested" "primary" "List all workspaces")"
    fi
  fi

  emit_result "true" "status" "ok" "$summary" "$next_action" "$suggested" \
    "$(jq -cn --argjson workspaces "$ws_rows" --argjson sessions "$sess_rows" --argjson include_stale "$include_stale" '{workspaces:$workspaces,sessions:$sessions,include_stale:$include_stale}')" \
    "$actions" \
    "$summary"
}

cmd_alerts() {
  cmd_status "$@"
}

cmd_guide() {
  local workspace=""
  local assistant="codex"
  local task=""
  while [[ $# -gt 0 ]]; do
    case "$1" in
      --workspace|--assistant|--task)
        [[ $# -ge 2 ]] || { emit_error "guide" "missing value for $1"; return 0; }
        case "$1" in
          --workspace) workspace="$2" ;;
          --assistant) assistant="$2" ;;
          --task) task="$2" ;;
        esac
        shift 2
        ;;
      --project)
        [[ $# -ge 2 ]] || { emit_error "guide" "missing value for --project"; return 0; }
        shift 2
        ;;
      *) emit_error "guide" "unknown flag: $1"; return 0 ;;
    esac
  done

  local lower summary next_action suggested actions
  lower="$(printf '%s' "$task" | tr '[:upper:]' '[:lower:]')"
  actions='[]'

  if [[ "$lower" == *"review"* && -n "${workspace// }" ]]; then
    summary="Guide: run bounded review task"
    next_action="Run one bounded task step and inspect the result."
    suggested="$(build_task_start_cmd "$workspace" "$assistant" "$DEFAULT_REVIEW_PROMPT")"
  elif [[ "$lower" == *"status"* || "$lower" == *"active"* ]]; then
    summary="Guide: check status first"
    next_action="Use status to decide continue/start actions."
    if [[ -n "${workspace// }" ]]; then
      suggested="$SELF_SCRIPT status --workspace $(quote_cmd "$workspace") --assistant $(quote_cmd "$assistant")"
    else
      suggested="$SELF_SCRIPT status"
    fi
  elif [[ "$lower" == *"ship"* || "$lower" == *"commit"* || "$lower" == *"push"* ]]; then
    summary="Guide: ship workspace changes"
    next_action="Commit/push current workspace changes if clean."
    if [[ -n "${workspace// }" ]]; then
      suggested="$SELF_SCRIPT git ship --workspace $(quote_cmd "$workspace") --push"
    else
      suggested="$SELF_SCRIPT workspace list --all"
    fi
  elif [[ -n "${workspace// }" && -n "${task// }" ]]; then
    summary="Guide: run bounded task"
    next_action="Run one bounded task step."
    suggested="$(build_task_start_cmd "$workspace" "$assistant" "$task")"
  else
    summary="Guide: choose workspace then run task"
    next_action="Pick workspace, then run task start."
    suggested="$SELF_SCRIPT workspace list --all"
  fi

  if [[ -n "${workspace// }" ]]; then
    actions="$(append_action "$actions" "status" "Status" "$SELF_SCRIPT status --workspace $(quote_cmd "$workspace") --assistant $(quote_cmd "$assistant")" "primary" "Check workspace status")"
    actions="$(append_action "$actions" "review" "Review" "$(build_task_start_cmd "$workspace" "$assistant" "$DEFAULT_REVIEW_PROMPT")" "primary" "Run review task")"
  else
    actions="$(append_action "$actions" "workspaces" "Workspaces" "$SELF_SCRIPT workspace list --all" "primary" "List all workspaces")"
  fi

  emit_result "true" "guide" "ok" "$summary" "$next_action" "$suggested" "$(jq -cn --arg workspace "$workspace" --arg assistant "$assistant" --arg task "$task" '{workspace:$workspace,assistant:$assistant,task:$task}')" "$actions" "✅ $summary"
}

cmd_terminal_run() {
  local workspace=""
  local text=""
  local enter="false"
  while [[ $# -gt 0 ]]; do
    case "$1" in
      --workspace|--text)
        [[ $# -ge 2 ]] || { emit_error "terminal.run" "missing value for $1"; return 0; }
        [[ "$1" == "--workspace" ]] && workspace="$2" || text="$2"
        shift 2
        ;;
      --enter) enter="true"; shift ;;
      *) emit_error "terminal.run" "unknown flag: $1"; return 0 ;;
    esac
  done
  [[ -z "${workspace// }" || -z "${text// }" ]] && { emit_error "terminal.run" "missing required flags: --workspace and --text"; return 0; }

  local args=(terminal run --workspace "$workspace" --text "$text")
  [[ "$enter" == "true" ]] && args+=(--enter)
  local out data_json
  if ! amux_get_ok_json out "terminal.run" "${args[@]}"; then
    return 0
  fi
  data_json="$(jq -c '.data // {}' <<<"$out")"
  emit_result "true" "terminal.run" "ok" "Terminal command sent." "Inspect logs if needed." "$SELF_SCRIPT terminal logs --workspace $(quote_cmd "$workspace") --lines 120" "$data_json" "[]" "✅ Terminal command sent."
}

cmd_terminal_logs() {
  local workspace=""
  local lines="120"
  while [[ $# -gt 0 ]]; do
    case "$1" in
      --workspace|--lines)
        [[ $# -ge 2 ]] || { emit_error "terminal.logs" "missing value for $1"; return 0; }
        [[ "$1" == "--workspace" ]] && workspace="$2" || lines="$2"
        shift 2
        ;;
      *) emit_error "terminal.logs" "unknown flag: $1"; return 0 ;;
    esac
  done
  [[ -z "${workspace// }" ]] && { emit_error "terminal.logs" "missing required flag: --workspace"; return 0; }

  local out data_json
  if ! amux_get_ok_json out "terminal.logs" terminal logs --workspace "$workspace" --lines "$lines"; then
    return 0
  fi
  data_json="$(jq -c '.data // {}' <<<"$out")"
  emit_result "true" "terminal.logs" "ok" "Terminal logs captured." "Continue based on logs." "$SELF_SCRIPT status --workspace $(quote_cmd "$workspace")" "$data_json" "[]" "✅ Terminal logs captured."
}

cmd_assistants() {
  local config_path="${AMUX_CONFIG:-$HOME/.amux/config.json}"
  local assistants='[]'
  if [[ -f "$config_path" ]]; then
    assistants="$(jq -c '.assistants // {} | keys | sort' "$config_path" 2>/dev/null || printf '[]')"
  fi
  local count
  count="$(jq -r 'length' <<<"$assistants")"
  emit_result "true" "assistants" "ok" "$count configured assistant alias(es)" "Use configured alias with task start/status." "$SELF_SCRIPT status" \
    "$(jq -cn --arg config_path "$config_path" --argjson assistants "$assistants" '{config_path:$config_path,assistants:$assistants}')" \
    "[]" \
    "✅ $count configured assistant alias(es)"
}

cmd_cleanup() {
  local older_than="24h"
  local yes="false"
  while [[ $# -gt 0 ]]; do
    case "$1" in
      --older-than)
        [[ $# -ge 2 ]] || { emit_error "cleanup" "missing value for --older-than"; return 0; }
        older_than="$2"
        shift 2
        ;;
      --yes) yes="true"; shift ;;
      *) emit_error "cleanup" "unknown flag: $1"; return 0 ;;
    esac
  done
  local args=(session prune --older-than "$older_than")
  [[ "$yes" == "true" ]] && args+=(--yes)
  local out data_json
  if ! amux_get_ok_json out "cleanup" "${args[@]}"; then
    return 0
  fi
  data_json="$(jq -c '.data // {}' <<<"$out")"
  emit_result "true" "cleanup" "ok" "Cleanup completed." "Refresh status." "$SELF_SCRIPT status" "$data_json" "[]" "✅ Cleanup completed."
}

cmd_workspace_root() {
  local root_ref="$1"
  local workspace="$2"
  local out
  if ! amux_get_ok_json out "git.ship" workspace list --all; then
    return 1
  fi
  local root
  root="$(jq -r --arg workspace "$workspace" '(.data // []) | map(select(.id == $workspace)) | .[0].root // ""' <<<"$out")"
  printf -v "$root_ref" '%s' "$root"
}

cmd_git_ship() {
  local workspace=""
  local message=""
  local push="false"
  while [[ $# -gt 0 ]]; do
    case "$1" in
      --workspace|--message)
        [[ $# -ge 2 ]] || { emit_error "git.ship" "missing value for $1"; return 0; }
        [[ "$1" == "--workspace" ]] && workspace="$2" || message="$2"
        shift 2
        ;;
      --push)
        push="true"
        shift
        ;;
      *) emit_error "git.ship" "unknown flag: $1"; return 0 ;;
    esac
  done
  [[ -z "${workspace// }" ]] && { emit_error "git.ship" "missing required flag: --workspace"; return 0; }

  local root
  if ! cmd_workspace_root root "$workspace"; then
    return 0
  fi
  if [[ -z "${root// }" || ! -d "$root" ]]; then
    emit_error "git.ship" "workspace root unavailable" "$root"
    return 0
  fi

  local porcelain
  porcelain="$(git -C "$root" status --porcelain --untracked-files=all 2>/dev/null || true)"
  if [[ -z "${porcelain// }" ]]; then
    emit_result "true" "git.ship" "ok" "No changes to commit." "Run review or continue coding." "$(build_task_start_cmd "$workspace" "codex" "$DEFAULT_REVIEW_PROMPT")" \
      "$(jq -cn --arg workspace "$workspace" --arg root "$root" '{workspace:$workspace,root:$root,changed:false}')" "[]" "✅ No changes to commit."
    return 0
  fi

  [[ -z "${message// }" ]] && message="chore(amux): update ${workspace}"
  if ! git -C "$root" add -A >/dev/null 2>&1; then
    emit_error "git.ship" "git add failed" "$root"
    return 0
  fi
  local commit_out
  if ! commit_out="$(git -C "$root" commit -m "$message" 2>&1)"; then
    emit_error "git.ship" "git commit failed" "$commit_out"
    return 0
  fi

  local pushed="false"
  local push_error=""
  if [[ "$push" == "true" ]]; then
    if git -C "$root" push >/dev/null 2>&1; then
      pushed="true"
    else
      push_error="git push failed"
    fi
  fi

  local status="ok"
  [[ -n "${push_error// }" ]] && status="attention"
  emit_result "true" "git.ship" "$status" "Commit created." "Run review or continue implementation." "$(build_task_start_cmd "$workspace" "codex" "$DEFAULT_REVIEW_PROMPT")" \
    "$(jq -cn --arg workspace "$workspace" --arg root "$root" --arg message "$message" --argjson pushed "$pushed" --arg push_error "$push_error" '{workspace:$workspace,root:$root,message:$message,pushed:$pushed,push_error:$push_error,changed:true}')" \
    "[]" \
    "✅ Commit created."
}

cmd_workflow() {
  emit_error "workflow" "workflow commands were removed; use task start/status plus continue for explicit control"
}

cmd_help() {
  local text
  text="$(usage 2>/dev/null || true)"
  emit_result "true" "help" "ok" "assistant-dx help" "Run a command from usage." "$SELF_SCRIPT status" "$(jq -cn --arg usage "$text" '{usage:$usage}')" "[]" "assistant-dx help"
}

main() {
  [[ $# -lt 1 ]] && { emit_error "usage" "missing command"; return 0; }
  local command="$1"
  shift

  case "$command" in
    task)
      [[ $# -lt 1 ]] && { emit_error "task" "missing task subcommand"; return 0; }
      local task_sub="$1"
      shift
      case "$task_sub" in
        start|run) cmd_task_start_like "task.start" "$@" ;;
        status) cmd_task_status "$@" ;;
        *) emit_error "task" "unknown task subcommand: $task_sub" ;;
      esac
      ;;
    start) cmd_start "$@" ;;
    review) cmd_review "$@" ;;
    continue) cmd_continue "$@" ;;
    status) cmd_status "$@" ;;
    alerts) cmd_alerts "$@" ;;
    guide) cmd_guide "$@" ;;
    assistants) cmd_assistants "$@" ;;
    cleanup) cmd_cleanup "$@" ;;
    workflow) cmd_workflow "$@" ;;
    help|-h|--help) cmd_help ;;
    project)
      [[ $# -lt 1 ]] && { emit_error "project" "missing project subcommand"; return 0; }
      local psub="$1"
      shift
      case "$psub" in
        list) cmd_project_list "$@" ;;
        add) cmd_project_add "$@" ;;
        *) emit_error "project" "unknown project subcommand: $psub" ;;
      esac
      ;;
    workspace)
      [[ $# -lt 1 ]] && { emit_error "workspace" "missing workspace subcommand"; return 0; }
      local wsub="$1"
      shift
      case "$wsub" in
        list) cmd_workspace_list "$@" ;;
        create) cmd_workspace_create "$@" ;;
        *) emit_error "workspace" "unknown workspace subcommand: $wsub" ;;
      esac
      ;;
    terminal)
      [[ $# -lt 1 ]] && { emit_error "terminal" "missing terminal subcommand"; return 0; }
      local tsub="$1"
      shift
      case "$tsub" in
        run) cmd_terminal_run "$@" ;;
        logs) cmd_terminal_logs "$@" ;;
        *) emit_error "terminal" "unknown terminal subcommand: $tsub" ;;
      esac
      ;;
    git)
      [[ $# -lt 1 ]] && { emit_error "git" "missing git subcommand"; return 0; }
      local gsub="$1"
      shift
      case "$gsub" in
        ship) cmd_git_ship "$@" ;;
        *) emit_error "git" "unknown git subcommand: $gsub" ;;
      esac
      ;;
    *) emit_error "$command" "unknown command: $command" ;;
  esac
}

main "$@"
