#!/usr/bin/env bash
# assistant-dx.sh
# Deterministic thin wrapper around typed amux JSON commands.
# No implicit retries, no implicit respawn loops, no hidden follow-ups.

set -euo pipefail

SELF_SCRIPT="skills/amux/scripts/assistant-dx.sh"
DEFAULT_REVIEW_PROMPT="Review current uncommitted changes. Return findings first ordered by severity with file references, then residual risks and test gaps."

AMUX_BIN_DEFAULT="$(command -v amux 2>/dev/null || true)"
if [[ -z "${AMUX_BIN_DEFAULT// }" ]]; then
  if [[ -x "/usr/local/bin/amux" ]]; then
    AMUX_BIN_DEFAULT="/usr/local/bin/amux"
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
  assistant-dx.sh workspace list [--project <repo> | --all]
  assistant-dx.sh workspace create --name <name> [--project <repo>] [--from-workspace <id>] [--scope project|nested] [--assistant <name>] [--base <ref>]

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
  jq -c --arg id "$id" --arg label "$label" --arg command "$command" --arg style "$style" --arg prompt "$prompt" \
    '. + [{id:$id,label:$label,command:$command,style:$style,prompt:$prompt}]' <<<"$actions_json"
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

amux_ok_json() {
  local command_name="$1"
  shift

  local raw
  if ! raw="$("$AMUX_BIN" --json "$@" 2>&1)"; then
    emit_error "$command_name" "amux command failed: $*" "$raw"
    return 1
  fi
  if ! jq -e '.' >/dev/null 2>&1 <<<"$raw"; then
    emit_error "$command_name" "amux returned invalid JSON" "$raw"
    return 1
  fi
  if [[ "$(jq -r '.ok // false' <<<"$raw")" != "true" ]]; then
    emit_error "$command_name" "$(jq -r '.error.message // "amux command failed"' <<<"$raw")" "$(jq -r '.error.code // "amux_error"' <<<"$raw")"
    return 1
  fi
  printf '%s' "$raw"
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
  local command_name="$1"
  local workspace="$2"
  local assistant="$3"
  local data_json="$4"
  local monitor_timeout="$5"
  local poll_interval="$6"

  local timeout_s poll_s started elapsed
  timeout_s="$(parse_duration_seconds "$monitor_timeout" || true)"
  poll_s="$(parse_duration_seconds "$poll_interval" || true)"
  [[ -z "${timeout_s// }" ]] && timeout_s=480
  [[ -z "${poll_s// }" ]] && poll_s=15
  (( timeout_s < 1 )) && { printf '%s' "$data_json"; return; }
  (( poll_s < 1 )) && poll_s=1

  started="$(date +%s)"
  while true; do
    if task_reached_terminal_state "$data_json"; then
      break
    fi
    elapsed=$(( $(date +%s) - started ))
    (( elapsed >= timeout_s )) && break
    sleep "$poll_s"

    local status_out next_data
    if ! status_out="$(amux_ok_json "$command_name" task status --workspace "$workspace" --assistant "$assistant")"; then
      break
    fi
    next_data="$(jq -c '.data // {}' <<<"$status_out")"
    data_json="$next_data"
  done

  printf '%s' "$data_json"
}

emit_task_result() {
  local command_name="$1"
  local workspace="$2"
  local assistant="$3"
  local prompt="$4"
  local data_json="$5"

  local task_status overall status summary next_action suggested quick_actions message
  task_status="$(jq -r '.status // ""' <<<"$data_json")"
  overall="$(jq -r '.overall_status // ""' <<<"$data_json")"
  status="$(map_task_status "$task_status" "$overall")"
  summary="$(jq -r '.summary // "Task completed."' <<<"$data_json")"
  next_action="$(jq -r '.next_action // "Check status and continue if needed."' <<<"$data_json")"
  suggested="$(jq -r '.suggested_command // ""' <<<"$data_json")"
  [[ -z "${suggested// }" ]] && suggested="$(build_task_status_cmd "$workspace" "$assistant")"
  quick_actions="$(jq -c '.quick_actions // []' <<<"$data_json")"
  if [[ "$(jq -r 'length' <<<"$quick_actions")" == "0" ]]; then
    quick_actions='[]'
    quick_actions="$(append_action "$quick_actions" "status" "Status" "$(build_task_status_cmd "$workspace" "$assistant")" "primary" "Check task status")"
  fi
  message="$summary"
  [[ -n "${next_action// }" ]] && message+=$'\n'"Next: $next_action"

  emit_result "true" "$command_name" "$status" "$summary" "$next_action" "$suggested" \
    "$(jq -cn --arg workspace "$workspace" --arg assistant "$assistant" --arg prompt "$prompt" --argjson task "$data_json" '{workspace:$workspace,assistant:$assistant,prompt:$prompt,task:$task}')" \
    "$quick_actions" \
    "$message"
}

parse_task_start_flags() {
  local -n workspace_ref=$1
  local -n assistant_ref=$2
  local -n prompt_ref=$3
  local -n wait_timeout_ref=$4
  local -n idle_threshold_ref=$5
  local -n start_lock_ttl_ref=$6
  local -n idempotency_ref=$7
  local -n allow_new_run_ref=$8
  shift 8

  workspace_ref=""
  assistant_ref="codex"
  prompt_ref=""
  wait_timeout_ref=""
  idle_threshold_ref=""
  start_lock_ttl_ref=""
  idempotency_ref=""
  allow_new_run_ref="false"

  while [[ $# -gt 0 ]]; do
    case "$1" in
      --workspace|--assistant|--prompt|--wait-timeout|--idle-threshold|--start-lock-ttl|--idempotency-key)
        [[ $# -ge 2 ]] || return 1
        case "$1" in
          --workspace) workspace_ref="$2" ;;
          --assistant) assistant_ref="$2" ;;
          --prompt) prompt_ref="$2" ;;
          --wait-timeout) wait_timeout_ref="$2" ;;
          --idle-threshold) idle_threshold_ref="$2" ;;
          --start-lock-ttl) start_lock_ttl_ref="$2" ;;
          --idempotency-key) idempotency_ref="$2" ;;
        esac
        shift 2
        ;;
      --allow-new-run)
        allow_new_run_ref="true"
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
  if ! out="$(amux_ok_json "$command_name" "${amux_args[@]}")"; then
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
  if ! out="$(amux_ok_json "task.status" task status --workspace "$workspace" --assistant "$assistant")"; then
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
  if ! out="$(amux_ok_json "review" "${amux_args[@]}")"; then
    return 0
  fi
  data_json="$(jq -c '.data // {}' <<<"$out")"
  if [[ "$monitor" == "true" ]]; then
    data_json="$(wait_for_task_terminal "review" "$workspace" "$assistant" "$data_json" "$monitor_timeout" "$poll_interval")"
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
    if ! status_out="$(amux_ok_json "continue" task status --workspace "$workspace" --assistant "$assistant")"; then
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
  if ! out="$(amux_ok_json "continue" "${send_args[@]}")"; then
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
  if ! out="$(amux_ok_json "project.list" project list)"; then
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
  if ! out="$(amux_ok_json "project.add" project add --path "$path")"; then
    return 0
  fi
  data_json="$(jq -c '.data // {}' <<<"$out")"
  emit_result "true" "project.add" "ok" "Project registered." "Create/list workspace next." "$SELF_SCRIPT workspace list --project $(quote_cmd "$path")" "$data_json" "[]" "✅ Project registered."
}

cmd_workspace_list() {
  local args=(workspace list)
  while [[ $# -gt 0 ]]; do
    case "$1" in
      --project|--workspace|--limit|--page)
        [[ $# -ge 2 ]] || { emit_error "workspace.list" "missing value for $1"; return 0; }
        args+=("$1" "$2")
        shift 2
        ;;
      --all)
        args+=(--all)
        shift
        ;;
      *) emit_error "workspace.list" "unknown flag: $1"; return 0 ;;
    esac
  done
  local out rows count first_ws suggested actions
  if ! out="$(amux_ok_json "workspace.list" "${args[@]}")"; then
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
  local args=(workspace create)
  local saw_name="false"
  while [[ $# -gt 0 ]]; do
    case "$1" in
      --name|--project|--from-workspace|--scope|--assistant|--base)
        [[ $# -ge 2 ]] || { emit_error "workspace.create" "missing value for $1"; return 0; }
        [[ "$1" == "--name" ]] && saw_name="true"
        args+=("$1" "$2")
        shift 2
        ;;
      *) emit_error "workspace.create" "unknown flag: $1"; return 0 ;;
    esac
  done
  [[ "$saw_name" != "true" ]] && { emit_error "workspace.create" "missing required flag: --name"; return 0; }

  local out data_json ws_id assistant
  if ! out="$(amux_ok_json "workspace.create" "${args[@]}")"; then
    return 0
  fi
  data_json="$(jq -c '.data // {}' <<<"$out")"
  ws_id="$(jq -r '.id // ""' <<<"$data_json")"
  assistant="$(jq -r '.assistant // "codex"' <<<"$data_json")"
  local suggested="$SELF_SCRIPT workspace list --all"
  [[ -n "${ws_id// }" ]] && suggested="$(build_task_start_cmd "$ws_id" "$assistant" "Continue from current state and report status plus next action.")"
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
    local out data_json task_status overall status summary next_action suggested actions msg
    if ! out="$(amux_ok_json "status" task status --workspace "$workspace" --assistant "$assistant")"; then
      return 0
    fi
    data_json="$(jq -c '.data // {}' <<<"$out")"
    task_status="$(jq -r '.status // ""' <<<"$data_json")"
    overall="$(jq -r '.overall_status // ""' <<<"$data_json")"
    status="$(map_task_status "$task_status" "$overall")"
    summary="$(jq -r '.summary // "Workspace status captured."' <<<"$data_json")"
    next_action="$(jq -r '.next_action // "Continue with the next focused step."' <<<"$data_json")"
    suggested="$(jq -r '.suggested_command // ""' <<<"$data_json")"
    [[ -z "${suggested// }" ]] && suggested="$(build_task_start_cmd "$workspace" "$assistant" "Continue from current state and report status plus next action.")"
    actions="$(jq -c '.quick_actions // []' <<<"$data_json")"
    if [[ "$(jq -r 'length' <<<"$actions")" == "0" ]]; then
      actions='[]'
      actions="$(append_action "$actions" "start" "Start" "$(build_task_start_cmd "$workspace" "$assistant" "Continue from current state and report status plus next action.")" "primary" "Start bounded task")"
    fi
    msg="$summary"
    msg+=$'\n'"Workspace: $workspace"
    msg+=$'\n'"Next: $next_action"
    emit_result "true" "status" "$status" "$summary" "$next_action" "$suggested" \
      "$(jq -cn --arg workspace "$workspace" --arg assistant "$assistant" --argjson include_stale "$include_stale" --argjson task "$data_json" '{workspace:$workspace,assistant:$assistant,include_stale:$include_stale,task:$task}')" \
      "$actions" \
      "$msg"
    return 0
  fi

  local ws_out sess_out ws_rows sess_rows total active first_active first_ws suggested actions summary next_action
  if ! ws_out="$(amux_ok_json "status" workspace list --all)"; then
    return 0
  fi
  if ! sess_out="$(amux_ok_json "status" session list)"; then
    return 0
  fi
  ws_rows="$(jq -c '.data // []' <<<"$ws_out")"
  sess_rows="$(jq -c '.data // []' <<<"$sess_out")"
  total="$(jq -r 'length' <<<"$ws_rows")"
  active="$(jq -r '[.[] | select((.type // "") == "agent" and (.status // "") != "stopped") | (.workspace_id // "") | select(length>0)] | unique | length' <<<"$sess_rows")"
  first_active="$(jq -r '[.[] | select((.type // "") == "agent" and (.status // "") != "stopped") | (.workspace_id // "") | select(length>0)] | unique | .[0] // ""' <<<"$sess_rows")"
  first_ws="$(jq -r '.[]?.id // ""' <<<"$ws_rows" | head -n 1)"
  actions='[]'

  if [[ "$active" -gt 0 ]]; then
    summary="$active active workspace(s), $total total"
    next_action="Check active workspace and continue there."
    suggested="$SELF_SCRIPT status --workspace $(quote_cmd "$first_active") --assistant $(quote_cmd "$assistant")"
    actions="$(append_action "$actions" "active" "Active Status" "$suggested" "success" "Open active workspace status")"
  else
    summary="No active workspace tasks. $total workspace(s) available."
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
  if ! out="$(amux_ok_json "terminal.run" "${args[@]}")"; then
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
  if ! out="$(amux_ok_json "terminal.logs" terminal logs --workspace "$workspace" --lines "$lines")"; then
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
  if ! out="$(amux_ok_json "cleanup" "${args[@]}")"; then
    return 0
  fi
  data_json="$(jq -c '.data // {}' <<<"$out")"
  emit_result "true" "cleanup" "ok" "Cleanup completed." "Refresh status." "$SELF_SCRIPT status" "$data_json" "[]" "✅ Cleanup completed."
}

cmd_workspace_root() {
  local workspace="$1"
  local out
  if ! out="$(amux_ok_json "git.ship" workspace list --all)"; then
    return 1
  fi
  jq -r --arg workspace "$workspace" '(.data // []) | map(select(.id == $workspace)) | .[0].root // ""' <<<"$out"
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
  if ! root="$(cmd_workspace_root "$workspace")"; then
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
