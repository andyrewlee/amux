#!/usr/bin/env bash
# openclaw-dx.sh — OpenClaw-first control plane for amux coding workflows.
#
# Covers project/workspace/agent/terminal/session/git/review flows in one UX layer.

set -euo pipefail

usage() {
  cat >&2 <<'USAGE'
Usage:
  openclaw-dx.sh project add [--path <repo> | --cwd] [--workspace <name>] [--assistant <name>] [--base <branch>]
  openclaw-dx.sh project list [--limit <n>] [--page <n>] [--query <text>]
  openclaw-dx.sh project pick [--index <n> | --name <query> | --path <repo>] [--workspace <name>] [--assistant <name>] [--base <branch>]

  openclaw-dx.sh workspace create --name <name> [--project <repo>] [--from-workspace <id>] [--scope project|nested] [--assistant <name>] [--base <branch>]
  openclaw-dx.sh workspace list [--project <repo>] [--workspace <id>] [--limit <n>] [--page <n>]
  openclaw-dx.sh workspace decide [--project <repo>] [--from-workspace <id>] [--task <text>] [--assistant <name>] [--name <workspace-name>]

  openclaw-dx.sh start --workspace <id> --prompt <text> [--assistant <name>] [--max-steps <n>] [--turn-budget <sec>] [--wait-timeout <dur>] [--idle-threshold <dur>]
  openclaw-dx.sh continue [--agent <id> | --workspace <id>] [--text <text>] [--enter] [--auto-start] [--assistant <name>] [--max-steps <n>] [--turn-budget <sec>] [--wait-timeout <dur>] [--idle-threshold <dur>]

  openclaw-dx.sh status [--project <repo>] [--workspace <id>] [--limit <n>] [--capture-lines <n>] [--capture-agents <n>] [--older-than <dur>] [--alerts-only] [--include-stale] [--recent-workspaces <n>]
  openclaw-dx.sh alerts [same flags as status]

  openclaw-dx.sh terminal run --workspace <id> --text <command> [--enter]
  openclaw-dx.sh terminal preset --workspace <id> [--kind nextjs] [--port <n>] [--host <name>] [--manager auto|npm|pnpm|yarn|bun]
  openclaw-dx.sh terminal logs --workspace <id> [--lines <n>]

  openclaw-dx.sh cleanup [--older-than <dur>] [--yes]
  openclaw-dx.sh review --workspace <id> [--assistant <name>] [--prompt <text>] [--max-steps <n>] [--turn-budget <sec>] [--wait-timeout <dur>] [--idle-threshold <dur>]
  openclaw-dx.sh git ship --workspace <id> [--message <msg>] [--push]
  openclaw-dx.sh guide [--project <repo>] [--workspace <id>] [--task <text>] [--assistant <name>]

  openclaw-dx.sh workflow kickoff --name <workspace-name> [--project <repo>] [--from-workspace <id>] [--scope project|nested] [--assistant <name>] --prompt <text> [--base <branch>] [--max-steps <n>] [--turn-budget <sec>] [--wait-timeout <dur>] [--idle-threshold <dur>]
  openclaw-dx.sh workflow dual --workspace <id> [--implement-assistant <name>] [--implement-prompt <text>] [--review-assistant <name>] [--review-prompt <text>] [--max-steps <n>] [--turn-budget <sec>] [--wait-timeout <dur>] [--idle-threshold <dur>] [--auto-continue-impl <true|false>] [--auto-continue-impl-prompt <text>]

  openclaw-dx.sh assistants [--workspace <id> --probe] [--limit <n>] [--prompt <text>] [--max-steps <n>] [--turn-budget <sec>] [--wait-timeout <dur>] [--idle-threshold <dur>]
USAGE
}

json_escape() {
  printf '%s' "$1" | jq -Rsa .
}

shell_quote() {
  printf '%q' "$1"
}

is_positive_int() {
  [[ "${1:-}" =~ ^[0-9]+$ ]] && [[ "$1" -gt 0 ]]
}

is_valid_hostname() {
  [[ "${1:-}" =~ ^[-A-Za-z0-9.:]+$ ]]
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
    -e 's/(ghs_[A-Za-z0-9]{5})[A-Za-z0-9]*/\1***/g' \
    -e 's/(glpat-[A-Za-z0-9_-]{5})[A-Za-z0-9_-]*/\1***/g' \
    -e 's/(xoxb-[A-Za-z0-9]{5})[A-Za-z0-9-]*/\1***/g' \
    -e 's/(AKIA[0-9A-Z]{4})[0-9A-Z]{12}/\1************/g' \
    -e 's/(Bearer )[A-Za-z0-9+/_=.-]{8,}/\1***/g' \
    -e 's/((TOKEN|SECRET|PASSWORD|API_KEY|APIKEY|AUTH_TOKEN|PRIVATE_KEY|ACCESS_KEY|CLIENT_SECRET|WEBHOOK_SECRET)=)[^[:space:]'"'"'\"]{8,}/\1***/g'
}

sanitize_workspace_name() {
  local value="$1"
  value="$(printf '%s' "$value" | tr '[:upper:]' '[:lower:]' | sed -E 's/[^a-z0-9._-]+/-/g; s/-+/-/g; s/\.+/./g; s/^-+//; s/-+$//; s/^\.+//; s/\.+$//')"
  value="${value//../.}"
  if [[ -z "$value" ]]; then
    value="ws-$(date +%s)"
  fi
  if [[ ! "$value" =~ ^[a-z0-9] ]]; then
    value="w${value}"
  fi
  printf '%s' "$value"
}

compose_nested_workspace_name() {
  local parent_name="$1"
  local child_name="$2"
  local parent_norm child_norm
  parent_norm="$(sanitize_workspace_name "$parent_name")"
  child_norm="$(sanitize_workspace_name "$child_name")"
  if [[ "$child_norm" == "$parent_norm"* ]]; then
    printf '%s' "$child_norm"
    return
  fi
  printf '%s.%s' "$parent_norm" "$child_norm"
}

normalize_json_or_default() {
  local input="$1"
  local fallback="$2"
  if jq -e . >/dev/null 2>&1 <<<"$input"; then
    printf '%s' "$input"
  else
    printf '%s' "$fallback"
  fi
}

AMUX_ERROR_OUTPUT=""
AMUX_ERROR_CAPTURE_FILE=""
if AMUX_ERROR_CAPTURE_FILE="$(mktemp "${TMPDIR:-/tmp}/amux-openclaw-dx-error.XXXXXX" 2>/dev/null)"; then
  :
else
  AMUX_ERROR_CAPTURE_FILE="${TMPDIR:-/tmp}/amux-openclaw-dx-error.$$"
fi
amux_ok_json() {
  local out
  AMUX_ERROR_OUTPUT=""
  if [[ -n "${AMUX_ERROR_CAPTURE_FILE:-}" ]]; then
    : >"$AMUX_ERROR_CAPTURE_FILE" 2>/dev/null || true
  fi
  if ! out="$(amux --json "$@" 2>&1)"; then
    AMUX_ERROR_OUTPUT="$out"
    if [[ -n "${AMUX_ERROR_CAPTURE_FILE:-}" ]]; then
      printf '%s' "$out" >"$AMUX_ERROR_CAPTURE_FILE" 2>/dev/null || true
    fi
    return 1
  fi
  if ! jq -e . >/dev/null 2>&1 <<<"$out"; then
    AMUX_ERROR_OUTPUT="$out"
    if [[ -n "${AMUX_ERROR_CAPTURE_FILE:-}" ]]; then
      printf '%s' "$out" >"$AMUX_ERROR_CAPTURE_FILE" 2>/dev/null || true
    fi
    return 1
  fi
  local ok
  ok="$(jq -r '.ok // false' <<<"$out")"
  if [[ "$ok" != "true" ]]; then
    AMUX_ERROR_OUTPUT="$out"
    if [[ -n "${AMUX_ERROR_CAPTURE_FILE:-}" ]]; then
      printf '%s' "$out" >"$AMUX_ERROR_CAPTURE_FILE" 2>/dev/null || true
    fi
    return 1
  fi
  printf '%s' "$out"
}

# Result envelope globals.
RESULT_OK=true
RESULT_COMMAND=""
RESULT_STATUS="ok"
RESULT_SUMMARY=""
RESULT_MESSAGE=""
RESULT_NEXT_ACTION=""
RESULT_SUGGESTED_COMMAND=""
RESULT_DATA='{}'
RESULT_QUICK_ACTIONS='[]'
RESULT_DELIVERY_ACTION="send"
RESULT_DELIVERY_PRIORITY=1
RESULT_DELIVERY_RETRY_AFTER_SECONDS=0
RESULT_DELIVERY_REPLACE_PREVIOUS=false
RESULT_DELIVERY_DROP_PENDING=true

OPENCLAW_DX_CHUNK_CHARS="${OPENCLAW_DX_CHUNK_CHARS:-1200}"
if ! is_positive_int "$OPENCLAW_DX_CHUNK_CHARS"; then
  OPENCLAW_DX_CHUNK_CHARS=1200
fi

INLINE_BUTTONS_SCOPE="$(normalize_inline_buttons_scope "${OPENCLAW_INLINE_BUTTONS_SCOPE:-allowlist}")"
INLINE_BUTTONS_ENABLED=true
if [[ "$INLINE_BUTTONS_SCOPE" == "off" ]]; then
  INLINE_BUTTONS_ENABLED=false
fi

DX_CMD_REF="skills/amux/scripts/openclaw-dx.sh"
TURN_CMD_REF="skills/amux/scripts/openclaw-turn.sh"
STEP_CMD_REF="skills/amux/scripts/openclaw-step.sh"

normalize_command_refs() {
  local value="$1"
  value="${value//skills\/amux\/scripts\/openclaw-dx.sh/$DX_CMD_REF}"
  value="${value//skills\/amux\/scripts\/openclaw-turn.sh/$TURN_CMD_REF}"
  value="${value//skills\/amux\/scripts\/openclaw-step.sh/$STEP_CMD_REF}"
  printf '%s' "$value"
}

emit_result() {
  local data_json quick_actions_json message_clean summary_clean next_clean suggested_clean context_json
  data_json="$(normalize_json_or_default "$RESULT_DATA" '{}')"
  quick_actions_json="$(normalize_json_or_default "$RESULT_QUICK_ACTIONS" '[]')"
  context_json="$(context_read_json)"

  summary_clean="$(redact_secrets_text "$RESULT_SUMMARY")"
  message_clean="$(redact_secrets_text "$RESULT_MESSAGE")"
  next_clean="$(redact_secrets_text "$RESULT_NEXT_ACTION")"
  suggested_clean="$(redact_secrets_text "$RESULT_SUGGESTED_COMMAND")"

  summary_clean="$(normalize_command_refs "$summary_clean")"
  message_clean="$(normalize_command_refs "$message_clean")"
  next_clean="$(normalize_command_refs "$next_clean")"
  suggested_clean="$(normalize_command_refs "$suggested_clean")"

  quick_actions_json="$(jq -c --arg dx "$DX_CMD_REF" --arg turn "$TURN_CMD_REF" --arg step "$STEP_CMD_REF" '
    map(
      .command = ((.command // "")
        | gsub("skills/amux/scripts/openclaw-dx\\.sh"; $dx)
        | gsub("skills/amux/scripts/openclaw-turn\\.sh"; $turn)
        | gsub("skills/amux/scripts/openclaw-step\\.sh"; $step)
      )
    )
  ' <<<"$quick_actions_json")"

  data_json="$(jq -c --arg dx "$DX_CMD_REF" --arg turn "$TURN_CMD_REF" --arg step "$STEP_CMD_REF" '
    def rewrite:
      if type == "string" then
        gsub("skills/amux/scripts/openclaw-dx\\.sh"; $dx)
        | gsub("skills/amux/scripts/openclaw-turn\\.sh"; $turn)
        | gsub("skills/amux/scripts/openclaw-step\\.sh"; $step)
      elif type == "array" then
        map(rewrite)
      elif type == "object" then
        with_entries(.value |= rewrite)
      else
        .
      end;
    rewrite
  ' <<<"$data_json")"

  data_json="$(jq -cn --argjson payload "$data_json" --argjson context "$context_json" '
    if ($payload | type) == "object" then
      $payload + {context: $context}
    else
      {value: $payload, context: $context}
    end
  ')"

  local result_payload
  result_payload="$(jq -n \
    --argjson ok "$RESULT_OK" \
    --arg command "$RESULT_COMMAND" \
    --arg status "$RESULT_STATUS" \
    --arg summary "$summary_clean" \
    --arg message "$message_clean" \
    --arg next_action "$next_clean" \
    --arg suggested_command "$suggested_clean" \
    --argjson data "$data_json" \
    --argjson quick_actions "$quick_actions_json" \
    --arg inline_buttons_scope "$INLINE_BUTTONS_SCOPE" \
    --argjson inline_buttons_enabled "$INLINE_BUTTONS_ENABLED" \
    --argjson channel_chunk_chars "$OPENCLAW_DX_CHUNK_CHARS" \
    --arg delivery_action "$RESULT_DELIVERY_ACTION" \
    --argjson delivery_priority "$RESULT_DELIVERY_PRIORITY" \
    --argjson delivery_retry_after_seconds "$RESULT_DELIVERY_RETRY_AFTER_SECONDS" \
    --argjson delivery_replace_previous "$RESULT_DELIVERY_REPLACE_PREVIOUS" \
    --argjson delivery_drop_pending "$RESULT_DELIVERY_DROP_PENDING" \
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
      def status_emoji($status):
        if $status == "ok" then "✅"
        elif $status == "needs_input" then "❓"
        elif $status == "attention" then "⚠️"
        elif $status == "command_error" or $status == "agent_error" then "🛑"
        else "ℹ️"
        end;
      def action_token($id; $idx):
        (
          ($id | tostring | ascii_downcase | gsub("[^a-z0-9_-]"; "_") | gsub("_+"; "_") | .[0:40])
          | if length == 0 then "action" else . end
        ) as $clean
        | ("dx:" + $clean + ":" + (($idx + 1) | tostring));
      def normalize_actions($actions):
        ($actions // [])
        | to_entries
        | map(
            . as $entry
            | ($entry.value // {}) as $value
            | {
                id: ($value.id // "action"),
                label: ($value.label // "Action"),
                command: ($value.command // ""),
                style: (
                  ($value.style // "primary") as $style
                  | if ($style == "primary" or $style == "success" or $style == "danger") then
                      $style
                    else
                      "primary"
                    end
                ),
                prompt: ($value.prompt // ""),
                callback_data: (
                  if (($value.callback_data // "") | length) > 0 then
                    $value.callback_data
                  else
                    action_token(($value.id // "action"); $entry.key)
                  end
                )
              }
          )
        | map(. + {callback_data: (.callback_data[0:64])});

      normalize_actions($quick_actions) as $actions
      | (
          if ($message | length) > 0 then
            $message
          else
            (status_emoji($status) + " " + $summary)
          end
        ) as $channel_message
      | smart_split($channel_message; $channel_chunk_chars) as $chunks_raw
      | annotate_chunks($chunks_raw) as $chunks_meta
      | {
          ok: $ok,
          command: $command,
          status: $status,
          summary: $summary,
          next_action: $next_action,
          suggested_command: $suggested_command,
          data: $data,
          quick_actions: $actions,
          quick_action_map: ($actions | map({key: .callback_data, value: .command}) | from_entries),
          quick_action_prompts: ($actions | map({key: .callback_data, value: .prompt}) | from_entries),
          delivery: {
            key: ("dx:" + $command),
            action: $delivery_action,
            priority: $delivery_priority,
            retry_after_seconds: $delivery_retry_after_seconds,
            replace_previous: $delivery_replace_previous,
            drop_pending: $delivery_drop_pending,
            coalesce: true
          },
          channel: {
            message: $channel_message,
            chunk_chars: $channel_chunk_chars,
            chunks: ($chunks_meta | map(.text)),
            chunks_meta: $chunks_meta,
            inline_buttons_scope: $inline_buttons_scope,
            inline_buttons_enabled: $inline_buttons_enabled,
            callback_data_max_bytes: 64,
            inline_buttons: (
              if $inline_buttons_enabled then
                build_action_rows($actions; 2)
              else
                []
              end
            ),
            action_tokens: ($actions | map(.callback_data)),
            actions_fallback: (
              if ($actions | length) == 0 then
                ""
              else
                "Actions: " + action_tokens_text($actions)
              end
            )
          }
        }
    ')"

  if [[ -x "$OPENCLAW_PRESENT_SCRIPT" ]]; then
    "$OPENCLAW_PRESENT_SCRIPT" <<<"$result_payload"
  else
    printf '%s\n' "$result_payload"
  fi
}

emit_error() {
  local command_name="$1"
  local status="$2"
  local summary="$3"
  local detail="${4:-}"
  RESULT_OK=false
  RESULT_COMMAND="$command_name"
  RESULT_STATUS="$status"
  RESULT_SUMMARY="$summary"
  RESULT_MESSAGE="🛑 $summary"
  if [[ -n "${detail// }" ]]; then
    RESULT_MESSAGE+=$'\n'"$detail"
  fi
  RESULT_NEXT_ACTION="Fix the error and retry this command."
  RESULT_SUGGESTED_COMMAND=""
  RESULT_DATA="$(jq -cn --arg detail "$detail" '{error: $detail}')"
  RESULT_QUICK_ACTIONS='[]'
  RESULT_DELIVERY_ACTION="send"
  RESULT_DELIVERY_PRIORITY=0
  RESULT_DELIVERY_REPLACE_PREVIOUS=false
  RESULT_DELIVERY_DROP_PENDING=true
  emit_result
}

emit_amux_error() {
  local command_name="$1"
  local out="${2:-$AMUX_ERROR_OUTPUT}"
  if [[ -z "${out// }" ]] && [[ -n "${AMUX_ERROR_CAPTURE_FILE:-}" ]] && [[ -f "$AMUX_ERROR_CAPTURE_FILE" ]]; then
    out="$(cat "$AMUX_ERROR_CAPTURE_FILE" 2>/dev/null || true)"
  fi
  local err_code="command_error"
  local err_msg="amux command failed"
  local err_details='{}'
  if jq -e . >/dev/null 2>&1 <<<"$out"; then
    err_code="$(jq -r '.error.code // "command_error"' <<<"$out")"
    err_msg="$(jq -r '.error.message // "amux command failed"' <<<"$out")"
    err_details="$(jq -c '.error.details // {}' <<<"$out")"
  else
    err_msg="$out"
  fi
  if [[ -z "${err_code// }" ]]; then
    err_code="command_error"
  fi
  if [[ -z "${err_msg// }" ]]; then
    if [[ -n "${out// }" ]]; then
      err_msg="$(printf '%s' "$out" | tr '\n' ' ' | sed -E 's/[[:space:]]+/ /g' | cut -c 1-240)"
    else
      err_msg="amux command failed"
    fi
  fi
  local status="command_error"
  if [[ "$err_code" == *"agent"* ]]; then
    status="agent_error"
  fi
  RESULT_OK=false
  RESULT_COMMAND="$command_name"
  RESULT_STATUS="$status"
  RESULT_SUMMARY="$err_msg"
  RESULT_MESSAGE="🛑 $err_msg"
  RESULT_NEXT_ACTION="Fix the failing amux command input and retry."
  RESULT_SUGGESTED_COMMAND=""
  RESULT_DATA="$(jq -cn --arg code "$err_code" --arg message "$err_msg" --argjson details "$err_details" '{error: {code: $code, message: $message, details: $details}}')"
  RESULT_QUICK_ACTIONS='[]'
  RESULT_DELIVERY_ACTION="send"
  RESULT_DELIVERY_PRIORITY=0
  RESULT_DELIVERY_REPLACE_PREVIOUS=false
  RESULT_DELIVERY_DROP_PENDING=true
  emit_result
}

workspace_row_by_id() {
  local workspace_id="$1"
  local ws_out
  if ! ws_out="$(amux_ok_json workspace list --archived)"; then
    return 1
  fi
  jq -c --arg id "$workspace_id" '
    (.data // [])
    | if type == "array" then . else [] end
    | map(select(.id == $id))
    | .[0] // empty
  ' <<<"$ws_out"
}

workspace_require_exists() {
  local command_name="$1"
  local workspace_id="$2"
  local ws_row
  if ! ws_row="$(workspace_row_by_id "$workspace_id")"; then
    emit_amux_error "$command_name"
    return 1
  fi
  if [[ -z "${ws_row// }" ]]; then
    emit_error "$command_name" "command_error" "workspace not found" "$workspace_id"
    return 1
  fi
  return 0
}

agent_for_workspace() {
  local workspace_id="$1"
  local agents_out
  if ! agents_out="$(amux_ok_json agent list --workspace "$workspace_id")"; then
    printf ''
    return 0
  fi
  local agents_json agent_count first_agent
  agents_json="$(jq -c '.data // []' <<<"$agents_out")"
  agent_count="$(jq -r 'length' <<<"$agents_json")"
  first_agent="$(jq -r '.[0].agent_id // ""' <<<"$agents_json")"

  if [[ -z "$first_agent" ]]; then
    printf ''
    return 0
  fi
  if [[ ! "$agent_count" =~ ^[0-9]+$ ]] || [[ "$agent_count" -le 1 ]]; then
    printf '%s' "$first_agent"
    return 0
  fi

  local capture_limit
  capture_limit="${OPENCLAW_DX_AGENT_PICK_CAPTURE_LIMIT:-4}"
  if [[ ! "$capture_limit" =~ ^[0-9]+$ ]] || [[ "$capture_limit" -le 0 ]]; then
    capture_limit=4
  fi

  local best_agent fallback_needs_input_agent
  best_agent=""
  fallback_needs_input_agent=""
  while IFS=$'\t' read -r session_name agent_id; do
    [[ -z "${agent_id// }" ]] && continue
    [[ -z "${session_name// }" ]] && continue

    local capture_out capture_status capture_needs_input capture_hint capture_hint_trim
    if ! capture_out="$(amux_ok_json agent capture "$session_name" --lines 48)"; then
      continue
    fi
    capture_status="$(jq -r '.data.status // "captured"' <<<"$capture_out")"
    capture_needs_input="$(jq -r '.data.needs_input // false' <<<"$capture_out")"
    capture_hint="$(jq -r '.data.input_hint // ""' <<<"$capture_out")"
    capture_hint_trim="$(printf '%s' "$capture_hint" | tr -d '\r')"
    capture_hint_trim="${capture_hint_trim#"${capture_hint_trim%%[![:space:]]*}"}"
    capture_hint_trim="${capture_hint_trim%"${capture_hint_trim##*[![:space:]]}"}"

    if [[ "$capture_status" == "session_exited" ]]; then
      continue
    fi
    if [[ "$capture_needs_input" == "true" && "$capture_hint_trim" == "Assistant is waiting for local permission-mode selection." ]]; then
      continue
    fi
    if [[ "$capture_needs_input" == "false" ]]; then
      best_agent="$agent_id"
      break
    fi
    if [[ -z "$fallback_needs_input_agent" ]]; then
      fallback_needs_input_agent="$agent_id"
    fi
  done < <(jq -r --argjson cap "$capture_limit" '.[:$cap][] | [.session_name // "", .agent_id // ""] | @tsv' <<<"$agents_json")

  if [[ -n "$best_agent" ]]; then
    printf '%s' "$best_agent"
    return 0
  fi
  if [[ -n "$fallback_needs_input_agent" ]]; then
    printf '%s' "$fallback_needs_input_agent"
    return 0
  fi
  printf '%s' "$first_agent"
}

turn_reports_permission_mode_gate() {
  local turn_json="$1"
  if ! jq -e . >/dev/null 2>&1 <<<"$turn_json"; then
    return 1
  fi
  jq -e '
    ((.overall_status // .status // "") == "needs_input")
    and (
      ((.events // []) | any(
        (.response.needs_input // false) == true
        and ((.response.input_hint // "") == "Assistant is waiting for local permission-mode selection.")
      ))
      or ((.next_action // "") | test("permission-mode selection"; "i"))
      or ((.summary // "") | test("permission-mode selection"; "i"))
    )
  ' >/dev/null 2>&1 <<<"$turn_json"
}

turn_reports_no_workspace_change_claim() {
  local turn_json="$1"
  if ! jq -e . >/dev/null 2>&1 <<<"$turn_json"; then
    return 1
  fi
  jq -e '
    ((.summary // "") | test("Claimed file updates, but no workspace changes were detected\\."; "i"))
    or ((.events // []) | any((.summary // "") | test("Claimed file updates, but no workspace changes were detected\\."; "i")))
  ' >/dev/null 2>&1 <<<"$turn_json"
}

default_assistant_for_workspace() {
  local workspace_id="$1"
  local ws_row
  if ! ws_row="$(workspace_row_by_id "$workspace_id")"; then
    printf ''
    return 0
  fi
  if [[ -z "${ws_row// }" ]]; then
    printf ''
    return 0
  fi
  jq -r '.assistant // ""' <<<"$ws_row"
}

assistant_require_known() {
  local command_name="$1"
  local assistant="$2"
  local normalized
  normalized="$(printf '%s' "$assistant" | tr '[:upper:]' '[:lower:]')"
  normalized="${normalized#"${normalized%%[![:space:]]*}"}"
  normalized="${normalized%"${normalized##*[![:space:]]}"}"
  if [[ -z "${normalized// }" ]]; then
    emit_error "$command_name" "command_error" "invalid assistant" "$assistant"
    return 1
  fi
  if [[ "${#normalized}" -gt 100 ]]; then
    emit_error "$command_name" "command_error" "invalid assistant" "$assistant"
    return 1
  fi
  if [[ "$normalized" =~ ^[a-zA-Z0-9][a-zA-Z0-9._-]*$ ]]; then
    return 0
  fi
  emit_error "$command_name" "command_error" "invalid assistant" "$assistant"
  return 1
}

canonicalize_path() {
  local path="$1"
  if [[ -z "$path" ]]; then
    printf ''
    return 0
  fi
  if [[ -d "$path" ]]; then
    (
      cd "$path" >/dev/null 2>&1 && pwd -P
    ) || printf '%s' "$path"
    return 0
  fi
  printf '%s' "$path"
}

current_git_root() {
  if ! command -v git >/dev/null 2>&1; then
    printf ''
    return 0
  fi
  local root
  root="$(git rev-parse --show-toplevel 2>/dev/null || true)"
  if [[ -z "${root// }" ]]; then
    printf ''
    return 0
  fi
  canonicalize_path "$root"
}

default_project_path_hint() {
  local inferred
  inferred="$(current_git_root)"
  if [[ -n "$inferred" ]]; then
    printf '%s' "$inferred"
    return 0
  fi
  canonicalize_path "$(pwd -P)"
}

context_file_path() {
  local configured="${OPENCLAW_DX_CONTEXT_FILE:-}"
  if [[ -n "${configured// }" ]]; then
    printf '%s' "$configured"
    return 0
  fi
  local base="${XDG_STATE_HOME:-}"
  if [[ -z "${base// }" ]]; then
    if [[ -n "${HOME:-}" ]]; then
      base="$HOME/.local/state"
    else
      base="/tmp"
    fi
  fi
  printf '%s' "$base/amux/openclaw-dx-context.json"
}

context_read_json() {
  local path raw
  path="$(context_file_path)"
  if [[ ! -f "$path" ]]; then
    printf '{}'
    return 0
  fi
  raw="$(cat "$path" 2>/dev/null || true)"
  if jq -e . >/dev/null 2>&1 <<<"$raw"; then
    jq -c . <<<"$raw"
  else
    printf '{}'
  fi
}

context_write_json() {
  local payload="$1"
  local path dir tmp
  path="$(context_file_path)"
  dir="$(dirname "$path")"
  if ! mkdir -p "$dir" >/dev/null 2>&1; then
    return 0
  fi
  tmp="${path}.tmp.$$"
  if ! printf '%s\n' "$payload" >"$tmp" 2>/dev/null; then
    rm -f "$tmp" >/dev/null 2>&1 || true
    return 0
  fi
  mv "$tmp" "$path" >/dev/null 2>&1 || {
    rm -f "$tmp" >/dev/null 2>&1 || true
    return 0
  }
}

context_timestamp_utc() {
  date -u +"%Y-%m-%dT%H:%M:%SZ"
}

context_project_path() {
  jq -r '.project.path // ""' <<<"$(context_read_json)"
}

context_workspace_id() {
  jq -r '.workspace.id // ""' <<<"$(context_read_json)"
}

context_agent_id() {
  jq -r '.agent.id // ""' <<<"$(context_read_json)"
}

context_assistant_hint() {
  local workspace_id="${1:-}"
  jq -r --arg ws "$workspace_id" '
    if ($ws | length) > 0 and ((.workspace.id // "") == $ws) and ((.workspace.assistant // "") | length) > 0 then
      .workspace.assistant
    elif ($ws | length) > 0 and ((.agent.workspace_id // "") == $ws) and ((.agent.assistant // "") | length) > 0 then
      .agent.assistant
    elif ((.agent.assistant // "") | length) > 0 then
      .agent.assistant
    else
      .workspace.assistant // ""
    end
  ' <<<"$(context_read_json)"
}

context_resolve_project() {
  local explicit="${1:-}"
  if [[ -n "${explicit// }" ]]; then
    printf '%s' "$explicit"
    return 0
  fi
  context_project_path
}

context_resolve_workspace() {
  local explicit="${1:-}"
  if [[ -n "${explicit// }" ]]; then
    printf '%s' "$explicit"
    return 0
  fi
  context_workspace_id
}

context_resolve_agent() {
  local explicit="${1:-}"
  if [[ -n "${explicit// }" ]]; then
    printf '%s' "$explicit"
    return 0
  fi
  context_agent_id
}

context_set_project() {
  local project_path="$1"
  local project_name="${2:-}"
  local canonical ctx ts updated
  canonical="$(canonicalize_path "$project_path")"
  if [[ -z "${canonical// }" ]]; then
    canonical="$project_path"
  fi
  if [[ -z "${canonical// }" ]]; then
    return 0
  fi

  ctx="$(context_read_json)"
  ts="$(context_timestamp_utc)"
  updated="$(jq -c --arg path "$canonical" --arg name "$project_name" --arg ts "$ts" '
    (.project.path // "") as $prev_path
    | .project = {
        path: $path,
        name: (if ($name | length) > 0 then $name elif $prev_path == $path then (.project.name // "") else "" end)
      }
    | if $prev_path != $path then
        .workspace = null
        | .agent = null
      else
        .
      end
    | .updated_at = $ts
  ' <<<"$ctx")"
  context_write_json "$updated"
}

context_set_workspace() {
  local workspace_id="$1"
  local workspace_name="${2:-}"
  local repo_path="${3:-}"
  local assistant="${4:-}"
  if [[ -z "${workspace_id// }" ]]; then
    return 0
  fi

  local canonical_repo ctx ts updated
  canonical_repo="$(canonicalize_path "$repo_path")"
  if [[ -z "${canonical_repo// }" ]]; then
    canonical_repo="$repo_path"
  fi
  ctx="$(context_read_json)"
  ts="$(context_timestamp_utc)"
  updated="$(jq -c \
    --arg id "$workspace_id" \
    --arg name "$workspace_name" \
    --arg repo "$canonical_repo" \
    --arg assistant "$assistant" \
    --arg ts "$ts" '
      (.workspace // {}) as $prev
      | ($prev.id // "") as $prev_id
      | .workspace = {
          id: $id,
          name: (
            if ($name | length) > 0 then
              $name
            elif $prev_id == $id then
              ($prev.name // "")
            else
              ""
            end
          ),
          repo: (
            if ($repo | length) > 0 then
              $repo
            elif $prev_id == $id then
              ($prev.repo // "")
            else
              ""
            end
          ),
          assistant: (
            if ($assistant | length) > 0 then
              $assistant
            elif $prev_id == $id then
              ($prev.assistant // "")
            else
              ""
            end
          )
        }
      | if ((.workspace.repo // "") | length) > 0 then
          .project = ((.project // {}) + {path: .workspace.repo})
        else
          .
        end
      | if $prev_id != $id then
          .agent = null
        else
          .
        end
      | .updated_at = $ts
    ' <<<"$ctx")"
  context_write_json "$updated"
}

context_set_agent() {
  local agent_id="$1"
  local workspace_id="${2:-}"
  local assistant="${3:-}"
  if [[ -z "${agent_id// }" ]]; then
    return 0
  fi
  local ctx ts updated
  ctx="$(context_read_json)"
  ts="$(context_timestamp_utc)"
  updated="$(jq -c --arg id "$agent_id" --arg workspace_id "$workspace_id" --arg assistant "$assistant" --arg ts "$ts" '
    .agent = {id: $id, workspace_id: $workspace_id, assistant: $assistant}
    | .updated_at = $ts
  ' <<<"$ctx")"
  context_write_json "$updated"
}

context_set_workspace_with_lookup() {
  local workspace_id="$1"
  local assistant_override="${2:-}"
  if [[ -z "${workspace_id// }" ]]; then
    return 0
  fi
  local ws_row ws_name ws_repo ws_assistant
  if ws_row="$(workspace_row_by_id "$workspace_id")" && [[ -n "${ws_row// }" ]]; then
    ws_name="$(jq -r '.name // ""' <<<"$ws_row")"
    ws_repo="$(jq -r '.repo // ""' <<<"$ws_row")"
    ws_assistant="$(jq -r '.assistant // ""' <<<"$ws_row")"
    if [[ -n "${assistant_override// }" ]]; then
      ws_assistant="$assistant_override"
    fi
    context_set_workspace "$workspace_id" "$ws_name" "$ws_repo" "$ws_assistant"
    return 0
  fi
  context_set_workspace "$workspace_id" "" "" "$assistant_override"
}

project_row_by_path() {
  local project_path="$1"
  local canonical out
  canonical="$(canonicalize_path "$project_path")"
  if ! out="$(amux_ok_json project list)"; then
    return 1
  fi
  jq -c --arg raw "$project_path" --arg canonical "$canonical" '
    .data // []
    | map(select((.path // "") == $raw or (.path // "") == $canonical))
    | .[0] // empty
  ' <<<"$out"
}

ensure_project_registered() {
  local project_path="$1"
  local existing add_out
  if existing="$(project_row_by_path "$project_path")" && [[ -n "${existing// }" ]]; then
    printf '%s' "$existing"
    return 0
  fi
  if ! add_out="$(amux_ok_json project add "$project_path")"; then
    return 1
  fi
  jq -c '.data // {}' <<<"$add_out"
}

completion_signal_present() {
  local summary="${1:-}"
  if [[ -z "${summary// }" ]]; then
    return 1
  fi
  local lower
  lower="$(printf '%s' "$summary" | tr '[:upper:]' '[:lower:]')"
  if [[ "$lower" == *"not done"* || "$lower" == *"not complete"* || "$lower" == *"still working"* ]]; then
    return 1
  fi
  case "$lower" in
    *"done"*|*"completed"*|*"finished"*|*"tests passed"*|*"ready for review"*|*"ready to review"*|*"ready to ship"*|*"implemented"*|*"fixed "*)
      return 0
      ;;
    *)
      return 1
      ;;
  esac
}

workspace_scope_hint_from_task() {
  local task="${1:-}"
  if [[ -z "${task// }" ]]; then
    printf ''
    return 0
  fi
  local lower
  lower="$(printf '%s' "$task" | tr '[:upper:]' '[:lower:]')"
  case "$lower" in
    *"refactor"*|*"review"*|*"audit"*|*"spike"*|*"experiment"*|*"parallel"*|*"hotfix"*|*"bugfix"*|*"cleanup"*|*"tech debt"*|*"debt"*|*"migration"*)
      printf 'nested'
      ;;
    *"greenfield"*|*"from scratch"*|*"bootstrap"*|*"scaffold"*|*"new project"*|*"initial setup"*|*"init repo"*)
      printf 'project'
      ;;
    *)
      printf ''
      ;;
  esac
}

run_self_json() {
  local out
  if [[ ! -x "$SELF_SCRIPT" ]]; then
    return 1
  fi
  out="$("$SELF_SCRIPT" "$@" 2>/dev/null || true)"
  if ! jq -e . >/dev/null 2>&1 <<<"$out"; then
    return 1
  fi
  printf '%s' "$out"
}

turn_needs_timeout_recovery() {
  local turn_json="$1"
  jq -e '
    (
      ((.overall_status // .status // "") == "timed_out")
      or ((.status // "") == "timed_out")
    )
    and ((.agent_id // "") | length > 0)
  ' >/dev/null 2>&1 <<<"$turn_json"
}

recover_timeout_turn_once() {
  local turn_json="$1"
  local wait_timeout="$2"
  local idle_threshold="$3"
  local step_script="${OPENCLAW_DX_STEP_SCRIPT:-$SCRIPT_DIR/openclaw-step.sh}"

  if [[ "${OPENCLAW_DX_TIMEOUT_RECOVERY:-true}" == "false" ]]; then
    printf '%s' "$turn_json"
    return
  fi
  if ! turn_needs_timeout_recovery "$turn_json"; then
    printf '%s' "$turn_json"
    return
  fi
  if [[ ! -x "$step_script" ]]; then
    printf '%s' "$turn_json"
    return
  fi

  local agent_id
  agent_id="$(jq -r '.agent_id // ""' <<<"$turn_json")"
  if [[ -z "${agent_id// }" ]]; then
    printf '%s' "$turn_json"
    return
  fi

  local recovery_text recovery_wait recovery_idle
  recovery_text="${OPENCLAW_DX_TIMEOUT_RECOVERY_TEXT:-Continue from current state and provide a one-line status update plus files changed.}"
  recovery_wait="${OPENCLAW_DX_TIMEOUT_RECOVERY_WAIT_TIMEOUT:-$wait_timeout}"
  recovery_idle="${OPENCLAW_DX_TIMEOUT_RECOVERY_IDLE_THRESHOLD:-$idle_threshold}"

  local follow_json
  follow_json="$(OPENCLAW_STEP_SKIP_PRESENT=true "$step_script" send \
    --agent "$agent_id" \
    --text "$recovery_text" \
    --enter \
    --wait-timeout "$recovery_wait" \
    --idle-threshold "$recovery_idle" 2>&1 || true)"
  if ! jq -e . >/dev/null 2>&1 <<<"$follow_json"; then
    printf '%s' "$turn_json"
    return
  fi

  local recovered
  recovered="$(jq -r '
    (
      (.ok // false)
      and
      (
        (.response.substantive_output // false)
        or (.response.changed // false)
        or (
          (
            (.response.status // .status // .overall_status // "")
            | ascii_downcase
            | test("^(timed_out|command_error|error)$")
            | not
          )
          and ((.summary // "") | length > 0)
        )
      )
    )
  ' <<<"$follow_json")"
  if [[ "$recovered" != "true" ]]; then
    printf '%s' "$turn_json"
    return
  fi

  printf '%s' "$follow_json"
}

wait_timeout_to_seconds_or_zero() {
  local raw="$1"
  if [[ "$raw" =~ ^[0-9]+$ ]]; then
    printf '%s' "$raw"
    return
  fi
  if [[ "$raw" =~ ^([0-9]+)s$ ]]; then
    printf '%s' "${BASH_REMATCH[1]}"
    return
  fi
  if [[ "$raw" =~ ^([0-9]+)m$ ]]; then
    printf '%s' "$((BASH_REMATCH[1] * 60))"
    return
  fi
  if [[ "$raw" =~ ^([0-9]+)h$ ]]; then
    printf '%s' "$((BASH_REMATCH[1] * 3600))"
    return
  fi
  printf '0'
}

normalize_turn_wait_timeout() {
  local wait_timeout="$1"
  local min_seconds="${OPENCLAW_DX_MIN_WAIT_TIMEOUT_SECONDS:-45}"
  if ! [[ "$min_seconds" =~ ^[0-9]+$ ]]; then
    min_seconds=45
  fi
  if [[ "$min_seconds" -le 0 ]]; then
    printf '%s' "$wait_timeout"
    return
  fi
  local resolved_seconds
  resolved_seconds="$(wait_timeout_to_seconds_or_zero "$wait_timeout")"
  if [[ "$resolved_seconds" -eq 0 ]]; then
    printf '%s' "$wait_timeout"
    return
  fi
  if [[ "$resolved_seconds" -lt "$min_seconds" ]]; then
    printf '%ss' "$min_seconds"
    return
  fi
  printf '%s' "$wait_timeout"
}

append_action() {
  local actions_json="$1"
  local id="$2"
  local label="$3"
  local command="$4"
  local style="$5"
  local prompt="$6"
  jq -cn \
    --argjson actions "$actions_json" \
    --arg id "$id" \
    --arg lbl "$label" \
    --arg command "$command" \
    --arg style "$style" \
    --arg prompt "$prompt" \
    '$actions + [{id: $id, label: $lbl, command: $command, style: $style, prompt: $prompt}]'
}

emit_turn_passthrough() {
  local command_name="$1"
  local workflow_name="$2"
  local turn_json="$3"

  if ! jq -e . >/dev/null 2>&1 <<<"$turn_json"; then
    local recovered_json
    recovered_json="$(printf '%s\n' "$turn_json" | sed -n '/^[[:space:]]*{/,$p')"
    if [[ -n "${recovered_json// }" ]] && jq -e . >/dev/null 2>&1 <<<"$recovered_json"; then
      turn_json="$recovered_json"
    else
      recovered_json="$(printf '%s\n' "$turn_json" | awk '/^[[:space:]]*\\{/{line=$0} END{print line}')"
      if [[ -n "${recovered_json// }" ]] && jq -e . >/dev/null 2>&1 <<<"$recovered_json"; then
        turn_json="$recovered_json"
      else
        emit_error "$command_name" "command_error" "turn script returned non-JSON output" "$turn_json"
        return
      fi
    fi
  fi

  local normalized_json
  normalized_json="$(jq -c \
    --arg command "$command_name" \
    --arg workflow "$workflow_name" \
    --arg dx_ref "$DX_CMD_REF" \
    --arg step_ref "$STEP_CMD_REF" \
    '
      def scrub_text:
        if type == "string" then
          gsub("\r"; "")
          | sub("[[:space:]]*█+[[:space:]]*$"; "")
          | sub("[[:space:]]+$"; "")
        elif type == "array" then
          map(scrub_text)
        elif type == "object" then
          with_entries(.value |= scrub_text)
        else
          .
        end;
      def fallback_next_action:
        if ((.next_action // "") | length) > 0 then
          .next_action
        elif ((.overall_status // .status // "") == "needs_input") then
          "Reply to the pending prompt, then continue the turn."
        elif ((.overall_status // .status // "") == "completed" or (.status // "") == "idle") then
          "Continue with a follow-up task or run status/review."
        else
          "Check status and continue with the next focused step."
        end;
      def fallback_suggested_command:
        if ((.suggested_command // "") | length) > 0 then
          .suggested_command
        elif ((.overall_status // .status // "") == "needs_input") and ((.agent_id // "") | length) > 0 then
          ($step_ref + " send --agent " + .agent_id + " --text \"Continue using the safest option and report status plus next action.\" --enter --wait-timeout 60s --idle-threshold 10s")
        elif ((.quick_actions // []) | length) > 0 then
          (
            (.quick_actions | map(.command // "") | map(select(length > 0)) | .[0])
            // ""
          )
        elif ((.agent_id // "") | length) > 0 then
          ($step_ref + " send --agent " + .agent_id + " --text \"Provide a one-line progress status.\" --enter --wait-timeout 60s --idle-threshold 10s")
        elif ((.workspace_id // "") | length) > 0 then
          ($dx_ref + " status --workspace " + .workspace_id)
        else
          ""
        end;
      (scrub_text) as $clean
      | $clean
      | del(.openclaw, .quick_action_by_id, .quick_action_prompts_by_id)
      | .next_action = (fallback_next_action)
      | .suggested_command = (fallback_suggested_command)
      | . + {command: $command, workflow: $workflow}
    ' <<<"$turn_json")"
  normalized_json="$(jq -c --arg dx "$DX_CMD_REF" --arg turn "$TURN_CMD_REF" --arg step "$STEP_CMD_REF" '
    def rewrite:
      if type == "string" then
        gsub("skills/amux/scripts/openclaw-dx\\.sh"; $dx)
        | gsub("skills/amux/scripts/openclaw-turn\\.sh"; $turn)
        | gsub("skills/amux/scripts/openclaw-step\\.sh"; $step)
      elif type == "array" then
        map(rewrite)
      elif type == "object" then
        with_entries(.value |= rewrite)
      else
        .
      end;
    rewrite
  ' <<<"$normalized_json")"

  local workspace_id agent_id assistant
  workspace_id="$(jq -r '.workspace_id // ""' <<<"$normalized_json")"
  agent_id="$(jq -r '.agent_id // ""' <<<"$normalized_json")"
  assistant="$(jq -r '.assistant // ""' <<<"$normalized_json")"
  if [[ -n "$workspace_id" ]]; then
    context_set_workspace_with_lookup "$workspace_id" "$assistant"
  fi
  if [[ -n "$agent_id" ]]; then
    context_set_agent "$agent_id" "$workspace_id" "$assistant"
  fi

  if [[ -x "$OPENCLAW_PRESENT_SCRIPT" ]]; then
    "$OPENCLAW_PRESENT_SCRIPT" <<<"$normalized_json"
  else
    printf '%s\n' "$normalized_json"
  fi
}

cmd_project_add() {
  local path=""
  local use_cwd=false
  local workspace_name=""
  local assistant=""
  local base=""
  local inferred_from_cwd=false

  while [[ $# -gt 0 ]]; do
    case "$1" in
      --path)
        path="$2"; shift 2 ;;
      --cwd)
        use_cwd=true; shift ;;
      --workspace)
        workspace_name="$2"; shift 2 ;;
      --assistant)
        assistant="$2"; shift 2 ;;
      --base)
        base="$2"; shift 2 ;;
      *)
        emit_error "project.add" "command_error" "unknown flag" "$1"
        return
        ;;
    esac
  done

  if [[ -z "$path" ]]; then
    if [[ "$use_cwd" == "true" ]]; then
      path="$(default_project_path_hint)"
      inferred_from_cwd=true
    else
      path="$(current_git_root)"
      if [[ -n "$path" ]]; then
        inferred_from_cwd=true
      fi
    fi
  fi

  if [[ -z "$path" ]]; then
    local pwd_hint
    pwd_hint="$(canonicalize_path "$(pwd -P)")"
    emit_error "project.add" "command_error" "missing project path" "pass --path <repo> (or use --cwd in a git repo). current_dir=$pwd_hint"
    return
  fi

  local add_out
  if ! add_out="$(amux_ok_json project add "$path")"; then
    emit_amux_error "project.add"
    return
  fi

  local project_name project_path
  project_name="$(jq -r '.data.name // ""' <<<"$add_out")"
  project_path="$(jq -r '.data.path // ""' <<<"$add_out")"

  local workspace_data='null'
  local workspace_id=""

  if [[ -n "$workspace_name" ]]; then
    local ws_create_out
    local ws_args=(workspace create "$workspace_name" --project "$project_path")
    if [[ -n "$assistant" ]]; then
      ws_args+=(--assistant "$assistant")
    fi
    if [[ -n "$base" ]]; then
      ws_args+=(--base "$base")
    fi

    if ! ws_create_out="$(amux_ok_json "${ws_args[@]}")"; then
      local err_payload err_code err_message retry_cmd
      err_payload="$AMUX_ERROR_OUTPUT"
      if [[ -z "${err_payload// }" ]] && [[ -n "${AMUX_ERROR_CAPTURE_FILE:-}" ]] && [[ -f "$AMUX_ERROR_CAPTURE_FILE" ]]; then
        err_payload="$(cat "$AMUX_ERROR_CAPTURE_FILE" 2>/dev/null || true)"
      fi
      err_code=""
      err_message=""
      if jq -e . >/dev/null 2>&1 <<<"$err_payload"; then
        err_code="$(jq -r '.error.code // ""' <<<"$err_payload")"
        err_message="$(jq -r '.error.message // ""' <<<"$err_payload")"
      fi
      if workspace_create_needs_initial_commit "$err_code" "$err_message"; then
        retry_cmd="skills/amux/scripts/openclaw-dx.sh project add --path $(shell_quote "$project_path") --workspace $(shell_quote "$workspace_name") --assistant $(shell_quote "${assistant:-codex}")"
        if [[ -n "$base" ]]; then
          retry_cmd+=" --base $(shell_quote "$base")"
        fi
        emit_initial_commit_guidance "project.add" "$project_path" "$retry_cmd" "$err_message"
        return
      fi
      emit_amux_error "project.add" "$err_payload"
      return
    fi
    workspace_data="$(jq -c '.data' <<<"$ws_create_out")"
    workspace_id="$(jq -r '.data.id // ""' <<<"$ws_create_out")"
  fi

  context_set_project "$project_path" "$project_name"
  if [[ -n "$workspace_id" ]]; then
    local workspace_name_out workspace_assistant_out
    workspace_name_out="$(jq -r '.name // ""' <<<"$workspace_data")"
    workspace_assistant_out="$(jq -r '.assistant // ""' <<<"$workspace_data")"
    context_set_workspace "$workspace_id" "$workspace_name_out" "$project_path" "$workspace_assistant_out"
  fi

  RESULT_OK=true
  RESULT_COMMAND="project.add"
  RESULT_STATUS="ok"
  if [[ -n "$workspace_id" ]]; then
    RESULT_SUMMARY="Project ready and workspace created: $workspace_id"
  else
    RESULT_SUMMARY="Project registered: $project_name"
  fi

  RESULT_NEXT_ACTION="Create/select a workspace and start a focused coding turn."
  RESULT_SUGGESTED_COMMAND=""
  if [[ -n "$workspace_id" ]]; then
    RESULT_SUGGESTED_COMMAND="skills/amux/scripts/openclaw-dx.sh start --workspace $(shell_quote "$workspace_id") --assistant $(shell_quote "${assistant:-codex}") --prompt \"Analyze the biggest tech-debt items and fix the top one.\""
  else
    RESULT_SUGGESTED_COMMAND="skills/amux/scripts/openclaw-dx.sh workspace create --name mobile --project $(shell_quote "$project_path") --assistant codex"
  fi

  local actions='[]'
  actions="$(append_action "$actions" "ws_list" "Workspaces" "skills/amux/scripts/openclaw-dx.sh workspace list --project $(shell_quote "$project_path")" "primary" "List workspaces for this project")"
  if [[ -z "$workspace_id" ]]; then
    actions="$(append_action "$actions" "ws_create" "Create WS" "skills/amux/scripts/openclaw-dx.sh workspace create --name mobile --project $(shell_quote "$project_path") --assistant codex" "success" "Create a workspace for mobile coding")"
  else
    actions="$(append_action "$actions" "start" "Start" "skills/amux/scripts/openclaw-dx.sh start --workspace $(shell_quote "$workspace_id") --assistant $(shell_quote "${assistant:-codex}") --prompt \"Analyze technical debt and implement the highest-impact fix.\"" "success" "Start a coding turn in this workspace")"
  fi
  actions="$(append_action "$actions" "status" "Status" "skills/amux/scripts/openclaw-dx.sh status" "primary" "Show global coding status")"
  RESULT_QUICK_ACTIONS="$actions"

  RESULT_DATA="$(jq -cn --argjson project "$(jq -c '.data' <<<"$add_out")" --argjson workspace "$workspace_data" '{project: $project, workspace: $workspace}')"
  if [[ "$inferred_from_cwd" == "true" ]]; then
    RESULT_DATA="$(jq -c '. + {path_source: "cwd_or_git_root"}' <<<"$RESULT_DATA")"
  fi

  RESULT_MESSAGE="✅ Project registered: $project_name"$'\n'"Path: $project_path"
  if [[ "$inferred_from_cwd" == "true" ]]; then
    RESULT_MESSAGE+=$'\n'"Source: inferred from current working git repo"
  fi
  if [[ -n "$workspace_id" ]]; then
    local workspace_root
    workspace_root="$(jq -r '.root // ""' <<<"$workspace_data")"
    RESULT_MESSAGE+=$'\n'"Workspace: $workspace_id"
    if [[ -n "$workspace_root" ]]; then
      RESULT_MESSAGE+=$'\n'"Root: $workspace_root"
    fi
  fi
  RESULT_MESSAGE+=$'\n'"Next: $RESULT_NEXT_ACTION"

  RESULT_DELIVERY_ACTION="send"
  RESULT_DELIVERY_PRIORITY=1
  RESULT_DELIVERY_DROP_PENDING=true
  emit_result
}

cmd_project_list() {
  local limit=12
  local page=1
  local query=""
  while [[ $# -gt 0 ]]; do
    case "$1" in
      --limit)
        limit="$2"; shift 2 ;;
      --page)
        page="$2"; shift 2 ;;
      --query)
        query="$2"; shift 2 ;;
      *)
        emit_error "project.list" "command_error" "unknown flag" "$1"
        return
        ;;
    esac
  done

  if ! is_positive_int "$limit"; then
    limit=12
  fi
  if ! is_positive_int "$page"; then
    page=1
  fi

  local out
  if ! out="$(amux_ok_json project list)"; then
    emit_amux_error "project.list"
    return
  fi

  local sorted_all sorted count total_count preview lines
  sorted_all="$(jq -c '.data // [] | sort_by(.name)' <<<"$out")"
  total_count="$(jq -r 'length' <<<"$sorted_all")"
  sorted="$sorted_all"
  if [[ -n "${query// }" ]]; then
    sorted="$(jq -c --arg q "$query" '
      ($q | ascii_downcase) as $needle
      | map(
          select(
            ((.name // "" | ascii_downcase) | contains($needle))
            or ((.path // "" | ascii_downcase) | contains($needle))
          )
        )
    ' <<<"$sorted_all")"
  fi
  count="$(jq -r 'length' <<<"$sorted")"
  local total_pages=1
  if [[ "$count" -gt 0 ]]; then
    total_pages=$(( (count + limit - 1) / limit ))
  fi
  if [[ "$page" -gt "$total_pages" ]]; then
    page="$total_pages"
  fi
  local offset
  offset=$(( (page - 1) * limit ))
  preview="$(jq -c --argjson offset "$offset" --argjson limit "$limit" '.[ $offset : ($offset + $limit) ]' <<<"$sorted")"
  lines="$(jq -r --argjson offset "$offset" '. | to_entries | map("\(($offset + .key + 1)). \(.value.name) — \(.value.path)") | join("\n")' <<<"$preview")"
  local has_prev=false
  local has_next=false
  if [[ "$count" -gt 0 && "$page" -gt 1 ]]; then
    has_prev=true
  fi
  if [[ "$count" -gt 0 && "$page" -lt "$total_pages" ]]; then
    has_next=true
  fi

  RESULT_OK=true
  RESULT_COMMAND="project.list"
  RESULT_STATUS="ok"
  RESULT_SUMMARY="$count project(s) registered"
  if [[ -n "${query// }" ]]; then
    RESULT_SUMMARY="$count project(s) matched \"$query\""
  fi
  if [[ "$count" -gt 0 ]]; then
    RESULT_SUMMARY+=" (page $page/$total_pages)"
  fi
  RESULT_NEXT_ACTION="Pick a project and create/select a workspace."
  RESULT_SUGGESTED_COMMAND=""
  if [[ "$count" -gt 0 ]]; then
    local first_project_name
    first_project_name="$(jq -r '.[0].name // ""' <<<"$preview")"
    if [[ -n "$first_project_name" ]]; then
      RESULT_SUGGESTED_COMMAND="skills/amux/scripts/openclaw-dx.sh project pick --name $(shell_quote "$first_project_name")"
    else
      RESULT_SUGGESTED_COMMAND="skills/amux/scripts/openclaw-dx.sh project pick --index 1"
    fi
  elif [[ -n "${query// }" ]]; then
    RESULT_NEXT_ACTION="Try a broader query or register a new project."
  fi

  local first_project_path first_project_name
  first_project_path="$(jq -r '.[0].path // ""' <<<"$preview")"
  first_project_name="$(jq -r '.[0].name // ""' <<<"$preview")"
  local actions='[]'
  if [[ -n "$first_project_name" ]]; then
    actions="$(append_action "$actions" "pick1" "Pick #1" "skills/amux/scripts/openclaw-dx.sh project pick --name $(shell_quote "$first_project_name")" "primary" "Select the first project")"
  fi
  if [[ -n "$first_project_path" ]]; then
    actions="$(append_action "$actions" "ws1" "WS #1" "skills/amux/scripts/openclaw-dx.sh workspace list --project $(shell_quote "$first_project_path")" "primary" "List workspaces for project #1")"
  fi
  local page_cmd_base="skills/amux/scripts/openclaw-dx.sh project list --limit $limit"
  if [[ -n "${query// }" ]]; then
    page_cmd_base+=" --query $(shell_quote "$query")"
  fi
  if [[ "$has_prev" == "true" ]]; then
    actions="$(append_action "$actions" "prev_page" "Prev" "$page_cmd_base --page $((page - 1))" "primary" "Show previous projects page")"
  fi
  if [[ "$has_next" == "true" ]]; then
    actions="$(append_action "$actions" "next_page" "Next" "$page_cmd_base --page $((page + 1))" "primary" "Show next projects page")"
  fi
  if [[ -n "${query// }" ]]; then
    actions="$(append_action "$actions" "clear_query" "Clear Query" "skills/amux/scripts/openclaw-dx.sh project list" "primary" "Show all projects")"
  fi
  actions="$(append_action "$actions" "status" "Status" "skills/amux/scripts/openclaw-dx.sh status" "primary" "Show global coding status")"
  RESULT_QUICK_ACTIONS="$actions"

  RESULT_DATA="$(jq -cn --arg query "$query" --argjson count "$count" --argjson total_count "$total_count" --argjson page "$page" --argjson limit "$limit" --argjson total_pages "$total_pages" --argjson has_prev "$has_prev" --argjson has_next "$has_next" --argjson projects "$sorted" --argjson projects_page "$preview" '{query: $query, count: $count, total_count: $total_count, page: $page, limit: $limit, total_pages: $total_pages, has_prev: $has_prev, has_next: $has_next, projects: $projects, projects_page: $projects_page}')"

  RESULT_MESSAGE="✅ $count project(s) registered"
  if [[ -n "${query// }" ]]; then
    RESULT_MESSAGE="✅ $count project(s) matched \"$query\" (from $total_count total)"
  fi
  if [[ "$count" -gt 0 ]]; then
    RESULT_MESSAGE+=$'\n'"Page: $page/$total_pages"
  fi
  if [[ -n "${lines// }" ]]; then
    RESULT_MESSAGE+=$'\n'"$lines"
  fi
  RESULT_MESSAGE+=$'\n'"Next: $RESULT_NEXT_ACTION"

  RESULT_DELIVERY_ACTION="send"
  RESULT_DELIVERY_PRIORITY=1
  RESULT_DELIVERY_DROP_PENDING=true
  emit_result
}

cmd_project_pick() {
  local index=""
  local name_query=""
  local path_query=""
  local workspace_name=""
  local assistant=""
  local base=""

  while [[ $# -gt 0 ]]; do
    case "$1" in
      --index)
        index="$2"; shift 2 ;;
      --name)
        name_query="$2"; shift 2 ;;
      --path)
        path_query="$2"; shift 2 ;;
      --workspace)
        workspace_name="$2"; shift 2 ;;
      --assistant)
        assistant="$2"; shift 2 ;;
      --base)
        base="$2"; shift 2 ;;
      *)
        emit_error "project.pick" "command_error" "unknown flag" "$1"
        return
        ;;
    esac
  done

  local selector_count=0
  [[ -n "$index" ]] && selector_count=$((selector_count + 1))
  [[ -n "${name_query// }" ]] && selector_count=$((selector_count + 1))
  [[ -n "${path_query// }" ]] && selector_count=$((selector_count + 1))
  if [[ "$selector_count" -gt 1 ]]; then
    emit_error "project.pick" "command_error" "provide only one selector" "use --index, --name, or --path"
    return
  fi
  if [[ -z "$index" && -z "${name_query// }" && -z "${path_query// }" ]]; then
    emit_error "project.pick" "command_error" "missing selector" "provide --index <n>, --name <query>, or --path <repo>"
    return
  fi
  local canonical_query=""
  if [[ -n "${path_query// }" ]]; then
    canonical_query="$(canonicalize_path "$path_query")"
    name_query="$path_query"
  fi

  local out sorted count selected selected_name selected_path selected_index resolved_by resolved_input
  if ! out="$(amux_ok_json project list)"; then
    emit_amux_error "project.pick"
    return
  fi
  sorted="$(jq -c '.data // [] | sort_by(.name)' <<<"$out")"
  count="$(jq -r 'length' <<<"$sorted")"

  resolved_by="index"
  resolved_input="$index"
  selected='null'
  selected_index=""

  if [[ -n "${name_query// }" ]]; then
    resolved_by="name"
    resolved_input="$name_query"
    local matches match_count match_lines
    matches="$(jq -c --arg q "$name_query" --arg c "$canonical_query" '
      ($q | ascii_downcase) as $needle
      | ($c | ascii_downcase) as $canonical_needle
      | (
          to_entries
          | map(
              select(
                ((.value.name // "") == $q)
                or ((.value.path // "") == $q)
                or ($c != "" and (.value.path // "") == $c)
              )
              | (.value + {index: (.key + 1)})
            )
        ) as $exact
      | if ($exact | length) > 0 then
          $exact
        else
          (
            to_entries
            | map(
                select(
                  ((.value.name // "" | ascii_downcase) | contains($needle))
                  or ((.value.path // "" | ascii_downcase) | contains($needle))
                  or ($canonical_needle != "" and ((.value.path // "" | ascii_downcase) | contains($canonical_needle)))
                )
                | (.value + {index: (.key + 1)})
              )
          )
        end
    ' <<<"$sorted")"
    match_count="$(jq -r 'length' <<<"$matches")"
    if [[ "$match_count" -eq 0 ]]; then
      emit_error "project.pick" "command_error" "no project matched query" "$name_query"
      return
    fi
    if [[ "$match_count" -gt 1 ]]; then
      match_lines="$(jq -r '. | map("\(.index). \(.name) — \(.path)") | join("\n")' <<<"$matches")"

      RESULT_OK=false
      RESULT_COMMAND="project.pick"
      RESULT_STATUS="attention"
      RESULT_SUMMARY="Multiple projects matched \"$name_query\""
      RESULT_NEXT_ACTION="Pick one match by index (or use the exact project path)."
      RESULT_SUGGESTED_COMMAND=""

      local actions='[]'
      while IFS= read -r row; do
        [[ -z "${row// }" ]] && continue
        local row_name
        row_name="$(jq -r '.name // ""' <<<"$row")"
        local row_index
        row_index="$(jq -r '.index // 0' <<<"$row")"
        if ! is_positive_int "$row_index"; then
          continue
        fi
        actions="$(append_action "$actions" "pick_${row_index}" "Pick #$row_index" "skills/amux/scripts/openclaw-dx.sh project pick --index $row_index" "primary" "Select $row_name")"
      done < <(jq -c '.[0:6][]' <<<"$matches")
      actions="$(append_action "$actions" "list" "List" "skills/amux/scripts/openclaw-dx.sh project list --query $(shell_quote "$name_query")" "primary" "Show filtered projects again")"
      RESULT_QUICK_ACTIONS="$actions"

      RESULT_DATA="$(jq -cn --arg query "$name_query" --argjson matches "$matches" '{query: $query, matches: $matches}')"
      RESULT_MESSAGE="⚠️ Multiple projects matched \"$name_query\""$'\n'"$match_lines"$'\n'"Next: $RESULT_NEXT_ACTION"
      emit_result
      return
    fi
    selected="$(jq -c '.[0]' <<<"$matches")"
    selected_index="$(jq -r '.index // ""' <<<"$selected")"
  else
    if ! is_positive_int "$index"; then
      emit_error "project.pick" "command_error" "--index must be a positive integer"
      return
    fi
    if (( index > count )); then
      emit_error "project.pick" "command_error" "project index out of range" "index=$index total=$count"
      return
    fi
    selected="$(jq -c --argjson idx "$index" '.[($idx - 1)]' <<<"$sorted")"
    selected_index="$index"
  fi

  selected_name="$(jq -r '.name // ""' <<<"$selected")"
  selected_path="$(jq -r '.path // ""' <<<"$selected")"
  if [[ -z "${selected_path// }" ]]; then
    emit_error "project.pick" "command_error" "selected project has no path" "$selected"
    return
  fi

  if [[ -z "$workspace_name" ]]; then
    context_set_project "$selected_path" "$selected_name"

    RESULT_OK=true
    RESULT_COMMAND="project.pick"
    RESULT_STATUS="ok"
    RESULT_SUMMARY="Selected project: $selected_name"
    RESULT_NEXT_ACTION="Create a workspace on this project, or choose an existing workspace."
    RESULT_SUGGESTED_COMMAND="skills/amux/scripts/openclaw-dx.sh workspace create --name mobile --project $(shell_quote "$selected_path") --assistant codex"

    local actions='[]'
    actions="$(append_action "$actions" "ws_create" "Create WS" "$RESULT_SUGGESTED_COMMAND" "success" "Create a workspace on the selected project")"
    actions="$(append_action "$actions" "ws_list" "Workspaces" "skills/amux/scripts/openclaw-dx.sh workspace list --project $(shell_quote "$selected_path")" "primary" "List project workspaces")"
    RESULT_QUICK_ACTIONS="$actions"

    RESULT_DATA="$(jq -cn --arg resolved_by "$resolved_by" --arg resolved_input "$resolved_input" --argjson index "$selected_index" --argjson project "$selected" '{resolved_by: $resolved_by, resolved_input: $resolved_input, index: $index, project: $project}')"
    RESULT_MESSAGE="✅ Selected project: $selected_name"$'\n'"Path: $selected_path"
    if [[ -n "${selected_index// }" ]]; then
      RESULT_MESSAGE+=$'\n'"Index: $selected_index"
    fi
    RESULT_MESSAGE+=$'\n'"Next: $RESULT_NEXT_ACTION"
    emit_result
    return
  fi

  local ws_out ws_args
  ws_args=(workspace create "$workspace_name" --project "$selected_path")
  if [[ -n "$assistant" ]]; then
    ws_args+=(--assistant "$assistant")
  fi
  if [[ -n "$base" ]]; then
    ws_args+=(--base "$base")
  fi
  if ! ws_out="$(amux_ok_json "${ws_args[@]}")"; then
    emit_amux_error "project.pick"
    return
  fi

  local ws_id ws_root
  ws_id="$(jq -r '.data.id // ""' <<<"$ws_out")"
  ws_root="$(jq -r '.data.root // ""' <<<"$ws_out")"
  local ws_assistant
  ws_assistant="$(jq -r '.data.assistant // ""' <<<"$ws_out")"
  context_set_project "$selected_path" "$selected_name"
  context_set_workspace "$ws_id" "$workspace_name" "$selected_path" "$ws_assistant"

  RESULT_OK=true
  RESULT_COMMAND="project.pick"
  RESULT_STATUS="ok"
  RESULT_SUMMARY="Selected project and created workspace: $ws_id"
  RESULT_NEXT_ACTION="Start a coding turn in the new workspace."
  RESULT_SUGGESTED_COMMAND="skills/amux/scripts/openclaw-dx.sh start --workspace $(shell_quote "$ws_id") --assistant $(shell_quote "${assistant:-codex}") --prompt \"Analyze the biggest debt items and fix one high-impact issue.\""

  local actions='[]'
  actions="$(append_action "$actions" "start" "Start" "$RESULT_SUGGESTED_COMMAND" "success" "Start a coding turn")"
  actions="$(append_action "$actions" "status" "Status" "skills/amux/scripts/openclaw-dx.sh status --workspace $(shell_quote "$ws_id")" "primary" "Show workspace status")"
  RESULT_QUICK_ACTIONS="$actions"

  RESULT_DATA="$(jq -cn --arg resolved_by "$resolved_by" --arg resolved_input "$resolved_input" --argjson index "$selected_index" --argjson project "$selected" --argjson workspace "$(jq -c '.data' <<<"$ws_out")" '{resolved_by: $resolved_by, resolved_input: $resolved_input, index: $index, project: $project, workspace: $workspace}')"
  RESULT_MESSAGE="✅ Project selected: $selected_name"$'\n'"Workspace: $ws_id"$'\n'"Root: $ws_root"
  if [[ -n "${selected_index// }" ]]; then
    RESULT_MESSAGE+=$'\n'"Index: $selected_index"
  fi
  RESULT_MESSAGE+=$'\n'"Next: $RESULT_NEXT_ACTION"
  emit_result
}

cmd_guide() {
  local project=""
  local workspace=""
  local task=""
  local assistant="${OPENCLAW_DX_GUIDE_ASSISTANT:-codex}"

  while [[ $# -gt 0 ]]; do
    case "$1" in
      --project)
        project="$2"; shift 2 ;;
      --workspace)
        workspace="$2"; shift 2 ;;
      --task)
        shift
        if [[ $# -eq 0 ]]; then
          emit_error "guide" "command_error" "missing value for --task"
          return
        fi
        task="$1"; shift
        while [[ $# -gt 0 && "$1" != --* ]]; do
          task+=" $1"
          shift
        done
        ;;
      --assistant)
        assistant="$2"; shift 2 ;;
      *)
        emit_error "guide" "command_error" "unknown flag" "$1"
        return
        ;;
    esac
  done

  if [[ -z "${assistant// }" ]]; then
    assistant="codex"
  fi

  local projects_out projects_json project_count
  if ! projects_out="$(amux_ok_json project list)"; then
    emit_amux_error "guide"
    return
  fi
  projects_json="$(jq -c '.data // [] | sort_by(.name)' <<<"$projects_out")"
  project_count="$(jq -r 'length' <<<"$projects_json")"

  local selected_project='null'
  local selected_project_name=""
  local selected_project_path=""
  local project_query=""
  if [[ -n "${project// }" ]]; then
    project_query="$(canonicalize_path "$project")"
    selected_project="$(jq -c --arg q "$project" --arg c "$project_query" '
      (map(select((.path // "") == $q or (.path // "") == $c or (.name // "") == $q)) | .[0]) //
      (map(select(
        ((.name // "" | ascii_downcase) | contains(($q | ascii_downcase)))
        or ((.path // "" | ascii_downcase) | contains(($q | ascii_downcase)))
      )) | .[0]) //
      null
    ' <<<"$projects_json")"
  fi
  if [[ "$selected_project" == "null" && "$project_count" -eq 1 ]]; then
    selected_project="$(jq -c '.[0]' <<<"$projects_json")"
  fi
  if [[ "$selected_project" != "null" ]]; then
    selected_project_name="$(jq -r '.name // ""' <<<"$selected_project")"
    selected_project_path="$(jq -r '.path // ""' <<<"$selected_project")"
  fi

  local ws_out ws_json
  if ! ws_out="$(amux_ok_json workspace list)"; then
    emit_amux_error "guide"
    return
  fi
  ws_json="$(jq -c '.data // []' <<<"$ws_out")"

  local selected_workspace='null'
  local selected_workspace_id=""
  local selected_workspace_name=""
  local selected_workspace_repo=""

  if [[ -n "$workspace" ]]; then
    selected_workspace="$(jq -c --arg id "$workspace" 'map(select(.id == $id)) | .[0] // null' <<<"$ws_json")"
    if [[ "$selected_workspace" == "null" ]]; then
      emit_error "guide" "command_error" "workspace not found" "$workspace"
      return
    fi
  fi

  local agents_out agents_json
  if ! agents_out="$(amux_ok_json agent list)"; then
    emit_amux_error "guide"
    return
  fi
  agents_json="$(jq -c '.data // []' <<<"$agents_out")"

  if [[ "$selected_workspace" == "null" && -n "$selected_project_path" ]]; then
    local project_workspaces active_workspace_id
    project_workspaces="$(jq -c --arg repo "$selected_project_path" 'map(select((.repo // "") == $repo))' <<<"$ws_json")"
    active_workspace_id="$(jq -nr --argjson workspaces "$project_workspaces" --argjson agents "$agents_json" '
      ($workspaces | map(.id // "")) as $ids
      | ($agents | map(select((.workspace_id // "") as $wid | ($ids | index($wid)) != null)))
      | .[0].workspace_id // ""
    ')"
    if [[ -n "$active_workspace_id" ]]; then
      selected_workspace="$(jq -c --arg id "$active_workspace_id" 'map(select(.id == $id)) | .[0] // null' <<<"$project_workspaces")"
    elif [[ "$(jq -r 'length' <<<"$project_workspaces")" -gt 0 ]]; then
      selected_workspace="$(jq -c '.[0]' <<<"$project_workspaces")"
    fi
  fi

  if [[ "$selected_workspace" != "null" ]]; then
    selected_workspace_id="$(jq -r '.id // ""' <<<"$selected_workspace")"
    selected_workspace_name="$(jq -r '.name // ""' <<<"$selected_workspace")"
    selected_workspace_repo="$(jq -r '.repo // ""' <<<"$selected_workspace")"
  fi

  if [[ "$selected_project" == "null" && -n "$selected_workspace_repo" ]]; then
    selected_project="$(jq -c --arg repo "$selected_workspace_repo" '
      (map(select((.path // "") == $repo)) | .[0]) //
      null
    ' <<<"$projects_json")"
    if [[ "$selected_project" != "null" ]]; then
      selected_project_name="$(jq -r '.name // ""' <<<"$selected_project")"
      selected_project_path="$(jq -r '.path // ""' <<<"$selected_project")"
    else
      selected_project_name="$(basename "$selected_workspace_repo")"
      selected_project_path="$selected_workspace_repo"
      selected_project="$(jq -cn --arg name "$selected_project_name" --arg path "$selected_project_path" '{name: $name, path: $path, inferred: true}')"
    fi
  fi

  local context_repo="$selected_project_path"
  if [[ -z "$context_repo" && -n "$selected_workspace_repo" ]]; then
    context_repo="$selected_workspace_repo"
  fi

  local project_workspaces='[]'
  if [[ -n "$context_repo" ]]; then
    project_workspaces="$(jq -c --arg repo "$context_repo" 'map(select((.repo // "") == $repo))' <<<"$ws_json")"
  fi
  local project_workspace_count
  project_workspace_count="$(jq -r 'length' <<<"$project_workspaces")"

  local workspace_agents='[]'
  if [[ -n "$selected_workspace_id" ]]; then
    workspace_agents="$(jq -c --arg id "$selected_workspace_id" 'map(select((.workspace_id // "") == $id))' <<<"$agents_json")"
  fi
  local workspace_agent_count primary_agent primary_session
  workspace_agent_count="$(jq -r 'length' <<<"$workspace_agents")"
  primary_agent="$(jq -r '.[0].agent_id // ""' <<<"$workspace_agents")"
  primary_session="$(jq -r '.[0].session_name // ""' <<<"$workspace_agents")"

  local terms_out terms_json
  if ! terms_out="$(amux_ok_json terminal list)"; then
    terms_json='[]'
  else
    terms_json="$(jq -c '.data // []' <<<"$terms_out")"
  fi
  local workspace_terminal_count=0
  if [[ -n "$selected_workspace_id" ]]; then
    workspace_terminal_count="$(jq -r --arg id "$selected_workspace_id" '[.[] | select((.workspace_id // "") == $id)] | length' <<<"$terms_json")"
  fi

  local capture_lines="${OPENCLAW_DX_GUIDE_CAPTURE_LINES:-120}"
  if ! is_positive_int "$capture_lines"; then
    capture_lines=120
  fi
  local capture_status=""
  local capture_summary=""
  local capture_needs_input="false"
  local capture_hint=""
  local capture_has_completion="false"
  if [[ -n "$primary_session" ]]; then
    local capture_out
    if capture_out="$(amux_ok_json agent capture "$primary_session" --lines "$capture_lines")"; then
      capture_status="$(jq -r '.data.status // ""' <<<"$capture_out")"
      capture_summary="$(jq -r '.data.summary // .data.latest_line // ""' <<<"$capture_out")"
      capture_needs_input="$(jq -r '.data.needs_input // false' <<<"$capture_out")"
      capture_hint="$(jq -r '.data.input_hint // ""' <<<"$capture_out")"
      if completion_signal_present "$capture_summary"; then
        capture_has_completion="true"
      fi
    fi
  fi

  local kickoff_prompt="$task"
  if [[ -z "${kickoff_prompt// }" ]]; then
    kickoff_prompt="Analyze current workspace, identify highest-impact work, implement it, and summarize validation plus next action."
  fi

  local stage="unknown"
  local reason=""
  RESULT_OK=true
  RESULT_COMMAND="guide"
  RESULT_STATUS="ok"
  RESULT_SUMMARY=""
  RESULT_NEXT_ACTION=""
  RESULT_SUGGESTED_COMMAND=""

  if [[ -z "$selected_project_path" ]]; then
    if [[ "$project_count" -eq 0 ]]; then
      stage="add_project"
      reason="No project is registered yet."
      RESULT_SUMMARY="Guide: register your first project"
      RESULT_NEXT_ACTION="Register the current repo as a project, then create a workspace."
      RESULT_SUGGESTED_COMMAND="skills/amux/scripts/openclaw-dx.sh project add --cwd --workspace mobile --assistant $(shell_quote "$assistant")"
    else
      stage="select_project"
      reason="Project context is not selected."
      RESULT_SUMMARY="Guide: choose a project"
      RESULT_NEXT_ACTION="Pick one project to continue."
      RESULT_SUGGESTED_COMMAND="skills/amux/scripts/openclaw-dx.sh project list"
    fi
  elif [[ "$project_workspace_count" -eq 0 ]]; then
    stage="create_workspace"
    reason="This project has no workspace yet."
    RESULT_SUMMARY="Guide: create a workspace"
    RESULT_NEXT_ACTION="Create a workspace, then start coding."
    RESULT_SUGGESTED_COMMAND="skills/amux/scripts/openclaw-dx.sh workspace decide --project $(shell_quote "$selected_project_path") --task $(shell_quote "$kickoff_prompt") --assistant $(shell_quote "$assistant")"
  elif [[ -z "$selected_workspace_id" ]]; then
    stage="select_workspace"
    reason="A workspace is required before starting or continuing agents."
    RESULT_SUMMARY="Guide: select a workspace"
    RESULT_NEXT_ACTION="Pick a workspace for this project."
    RESULT_SUGGESTED_COMMAND="skills/amux/scripts/openclaw-dx.sh workspace list --project $(shell_quote "$selected_project_path")"
  elif [[ "$workspace_agent_count" -eq 0 ]]; then
    stage="start_agent"
    reason="No active coding agent is running in this workspace."
    RESULT_SUMMARY="Guide: start a coding turn"
    RESULT_NEXT_ACTION="Start an agent turn in this workspace."
    RESULT_SUGGESTED_COMMAND="skills/amux/scripts/openclaw-dx.sh start --workspace $(shell_quote "$selected_workspace_id") --assistant $(shell_quote "$assistant") --prompt $(shell_quote "$kickoff_prompt")"
  elif [[ "$capture_needs_input" == "true" ]]; then
    stage="reply_agent"
    reason="Active agent is waiting for user input."
    RESULT_STATUS="needs_input"
    RESULT_SUMMARY="Guide: reply to blocked agent"
    RESULT_NEXT_ACTION="Reply to the active prompt so work can continue."
    if [[ -n "$primary_agent" ]]; then
      RESULT_SUGGESTED_COMMAND="skills/amux/scripts/openclaw-dx.sh continue --agent $(shell_quote "$primary_agent") --text $(shell_quote "${capture_hint:-Continue with the safest option and report status plus next action.}") --enter"
    else
      RESULT_SUGGESTED_COMMAND="skills/amux/scripts/openclaw-dx.sh continue --workspace $(shell_quote "$selected_workspace_id") --text $(shell_quote "${capture_hint:-Continue with the safest option and report status plus next action.}") --enter"
    fi
  elif [[ "$capture_status" == "session_exited" ]]; then
    stage="restart_agent"
    reason="Agent session exited."
    RESULT_STATUS="attention"
    RESULT_SUMMARY="Guide: restart the coding agent"
    RESULT_NEXT_ACTION="Restart an agent turn in this workspace."
    RESULT_SUGGESTED_COMMAND="skills/amux/scripts/openclaw-dx.sh start --workspace $(shell_quote "$selected_workspace_id") --assistant $(shell_quote "$assistant") --prompt $(shell_quote "$kickoff_prompt")"
  elif [[ "$capture_has_completion" == "true" ]]; then
    stage="review_and_ship"
    reason="Agent output indicates a completed change."
    RESULT_STATUS="attention"
    RESULT_SUMMARY="Guide: review and ship"
    RESULT_NEXT_ACTION="Run review, then commit/push if clean."
    RESULT_SUGGESTED_COMMAND="skills/amux/scripts/openclaw-dx.sh review --workspace $(shell_quote "$selected_workspace_id") --assistant codex"
  else
    stage="continue_agent"
    reason="Agent is active and can continue with the next task."
    RESULT_SUMMARY="Guide: continue current turn"
    RESULT_NEXT_ACTION="Continue the agent or monitor status/alerts."
    if [[ -n "$primary_agent" ]]; then
      RESULT_SUGGESTED_COMMAND="skills/amux/scripts/openclaw-dx.sh continue --agent $(shell_quote "$primary_agent") --text \"Continue from current state and report status plus next action.\" --enter"
    else
      RESULT_SUGGESTED_COMMAND="skills/amux/scripts/openclaw-dx.sh continue --workspace $(shell_quote "$selected_workspace_id") --text \"Continue from current state and report status plus next action.\" --enter"
    fi
  fi

  local actions='[]'
  local first_project_name first_project_path first_workspace_id
  first_project_name="$(jq -r '.[0].name // ""' <<<"$projects_json")"
  first_project_path="$(jq -r '.[0].path // ""' <<<"$projects_json")"
  first_workspace_id="$(jq -r '.[0].id // ""' <<<"$project_workspaces")"

  case "$stage" in
    add_project)
      actions="$(append_action "$actions" "add_cwd" "Add Project" "skills/amux/scripts/openclaw-dx.sh project add --cwd --workspace mobile --assistant $(shell_quote "$assistant")" "success" "Register current directory and create a workspace")"
      actions="$(append_action "$actions" "project_list" "Projects" "skills/amux/scripts/openclaw-dx.sh project list" "primary" "List registered projects")"
      ;;
    select_project)
      actions="$(append_action "$actions" "project_list" "Projects" "skills/amux/scripts/openclaw-dx.sh project list" "primary" "List registered projects")"
      if [[ -n "$first_project_name" ]]; then
        actions="$(append_action "$actions" "pick_first" "Pick #1" "skills/amux/scripts/openclaw-dx.sh project pick --name $(shell_quote "$first_project_name")" "success" "Select the first listed project")"
      fi
      ;;
    create_workspace)
      actions="$(append_action "$actions" "decide_ws" "Decide WS" "skills/amux/scripts/openclaw-dx.sh workspace decide --project $(shell_quote "$selected_project_path") --task $(shell_quote "$kickoff_prompt") --assistant $(shell_quote "$assistant")" "success" "Get project vs nested workspace recommendation")"
      actions="$(append_action "$actions" "create_ws" "Create WS" "skills/amux/scripts/openclaw-dx.sh workspace create --name mobile --project $(shell_quote "$selected_project_path") --assistant $(shell_quote "$assistant")" "primary" "Create a project workspace directly")"
      ;;
    select_workspace)
      actions="$(append_action "$actions" "list_ws" "Workspaces" "skills/amux/scripts/openclaw-dx.sh workspace list --project $(shell_quote "$selected_project_path")" "primary" "List project workspaces")"
      if [[ -n "$first_workspace_id" ]]; then
        actions="$(append_action "$actions" "start_ws" "Start #1" "skills/amux/scripts/openclaw-dx.sh start --workspace $(shell_quote "$first_workspace_id") --assistant $(shell_quote "$assistant") --prompt $(shell_quote "$kickoff_prompt")" "success" "Start coding in the first workspace")"
      fi
      ;;
    start_agent)
      actions="$(append_action "$actions" "start" "Start" "skills/amux/scripts/openclaw-dx.sh start --workspace $(shell_quote "$selected_workspace_id") --assistant $(shell_quote "$assistant") --prompt $(shell_quote "$kickoff_prompt")" "success" "Start coding turn")"
      actions="$(append_action "$actions" "dual" "Dual Pass" "skills/amux/scripts/openclaw-dx.sh workflow dual --workspace $(shell_quote "$selected_workspace_id") --implement-assistant claude --review-assistant codex" "primary" "Implement then review with separate assistants")"
      actions="$(append_action "$actions" "terminal" "Next.js Dev" "skills/amux/scripts/openclaw-dx.sh terminal preset --workspace $(shell_quote "$selected_workspace_id") --kind nextjs" "primary" "Start Next.js dev server in this workspace")"
      ;;
    reply_agent)
      actions="$(append_action "$actions" "reply" "Reply" "$RESULT_SUGGESTED_COMMAND" "danger" "Reply to blocked agent prompt")"
      actions="$(append_action "$actions" "status_ws" "Status" "skills/amux/scripts/openclaw-dx.sh status --workspace $(shell_quote "$selected_workspace_id")" "primary" "Check workspace state")"
      actions="$(append_action "$actions" "alerts_ws" "Alerts" "skills/amux/scripts/openclaw-dx.sh alerts --workspace $(shell_quote "$selected_workspace_id")" "primary" "Show blocking alerts only")"
      ;;
    restart_agent)
      actions="$(append_action "$actions" "restart" "Restart" "$RESULT_SUGGESTED_COMMAND" "danger" "Restart agent in this workspace")"
      actions="$(append_action "$actions" "status_ws" "Status" "skills/amux/scripts/openclaw-dx.sh status --workspace $(shell_quote "$selected_workspace_id")" "primary" "Check workspace state")"
      ;;
    review_and_ship)
      actions="$(append_action "$actions" "review" "Review" "skills/amux/scripts/openclaw-dx.sh review --workspace $(shell_quote "$selected_workspace_id") --assistant codex" "success" "Review uncommitted changes")"
      actions="$(append_action "$actions" "ship" "Ship" "skills/amux/scripts/openclaw-dx.sh git ship --workspace $(shell_quote "$selected_workspace_id")" "primary" "Commit current changes")"
      actions="$(append_action "$actions" "dual" "Dual Pass" "skills/amux/scripts/openclaw-dx.sh workflow dual --workspace $(shell_quote "$selected_workspace_id") --implement-assistant claude --review-assistant codex" "primary" "Run implementation+review pass")"
      ;;
    continue_agent)
      actions="$(append_action "$actions" "continue" "Continue" "$RESULT_SUGGESTED_COMMAND" "success" "Continue active coding turn")"
      actions="$(append_action "$actions" "status_ws" "Status" "skills/amux/scripts/openclaw-dx.sh status --workspace $(shell_quote "$selected_workspace_id")" "primary" "Check workspace state")"
      actions="$(append_action "$actions" "logs" "Terminal Logs" "skills/amux/scripts/openclaw-dx.sh terminal logs --workspace $(shell_quote "$selected_workspace_id") --lines 120" "primary" "Inspect terminal output")"
      ;;
  esac

  if [[ -n "$selected_workspace_id" ]]; then
    actions="$(append_action "$actions" "cleanup" "Cleanup" "skills/amux/scripts/openclaw-dx.sh cleanup --older-than 24h --yes" "primary" "Prune stale sessions")"
  elif [[ -n "$first_project_path" ]]; then
    actions="$(append_action "$actions" "workspace_list_first" "WS #1" "skills/amux/scripts/openclaw-dx.sh workspace list --project $(shell_quote "$first_project_path")" "primary" "Inspect first project's workspaces")"
  fi
  actions="$(append_action "$actions" "status_global" "Global Status" "skills/amux/scripts/openclaw-dx.sh status" "primary" "Show global status across projects")"
  RESULT_QUICK_ACTIONS="$actions"

  local workspace_count_total
  workspace_count_total="$(jq -r 'length' <<<"$ws_json")"
  RESULT_DATA="$(jq -cn \
    --arg stage "$stage" \
    --arg reason "$reason" \
    --arg project_query "$project" \
    --arg workspace_query "$workspace" \
    --arg task "$task" \
    --arg assistant "$assistant" \
    --arg selected_workspace_id "$selected_workspace_id" \
    --arg selected_workspace_name "$selected_workspace_name" \
    --arg selected_workspace_repo "$selected_workspace_repo" \
    --arg primary_agent "$primary_agent" \
    --arg primary_session "$primary_session" \
    --arg capture_status "$capture_status" \
    --arg capture_summary "$capture_summary" \
    --arg capture_hint "$capture_hint" \
    --argjson capture_needs_input "$capture_needs_input" \
    --argjson capture_has_completion "$capture_has_completion" \
    --argjson project_count "$project_count" \
    --argjson workspace_count_total "$workspace_count_total" \
    --argjson project_workspace_count "$project_workspace_count" \
    --argjson workspace_agent_count "$workspace_agent_count" \
    --argjson workspace_terminal_count "$workspace_terminal_count" \
    --argjson selected_project "$selected_project" \
    --argjson selected_workspace "$selected_workspace" \
    --argjson project_workspaces "$project_workspaces" \
    --argjson workspace_agents "$workspace_agents" \
    '{
      stage: $stage,
      reason: $reason,
      project_query: $project_query,
      workspace_query: $workspace_query,
      task: $task,
      assistant: $assistant,
      project_count: $project_count,
      workspace_count_total: $workspace_count_total,
      project_workspace_count: $project_workspace_count,
      selected_project: $selected_project,
      selected_workspace: $selected_workspace,
      selected_workspace_id: $selected_workspace_id,
      selected_workspace_name: $selected_workspace_name,
      selected_workspace_repo: $selected_workspace_repo,
      workspace_agent_count: $workspace_agent_count,
      workspace_terminal_count: $workspace_terminal_count,
      primary_agent: $primary_agent,
      primary_session: $primary_session,
      capture_status: $capture_status,
      capture_summary: $capture_summary,
      capture_needs_input: $capture_needs_input,
      capture_hint: $capture_hint,
      capture_has_completion: $capture_has_completion,
      project_workspaces: $project_workspaces,
      workspace_agents: $workspace_agents
    }')"

  RESULT_MESSAGE="✅ Guide stage: $stage"$'\n'"Reason: $reason"
  if [[ -n "$selected_project_path" ]]; then
    RESULT_MESSAGE+=$'\n'"Project: ${selected_project_name:-$selected_project_path}"$'\n'"Path: $selected_project_path"
  fi
  if [[ -n "$selected_workspace_id" ]]; then
    RESULT_MESSAGE+=$'\n'"Workspace: $selected_workspace_id ($selected_workspace_name)"$'\n'"Agents: $workspace_agent_count, Terminals: $workspace_terminal_count"
  fi
  if [[ -n "${capture_summary// }" ]]; then
    RESULT_MESSAGE+=$'\n'"Latest: $capture_summary"
  fi
  RESULT_MESSAGE+=$'\n'"Next: $RESULT_NEXT_ACTION"

  emit_result
}

workspace_create_emit_existing_recovery() {
  local project="$1"
  local requested_name="$2"
  local requested_scope="$3"
  local requested_assistant="$4"
  local conflict_message="$5"

  local ws_out ws_rows existing
  if ! ws_out="$(amux_ok_json workspace list --repo "$project")"; then
    return 1
  fi
  ws_rows="$(jq -c '.data // []' <<<"$ws_out")"

  existing="$(jq -c --arg name "$requested_name" --arg project "$project" '
    (
      map(select((.name // "") == $name))
      + map(select((.root // "") == $project))
      + map(select((.repo // "") == $project))
      + .
    )
    | map(select((.id // "") | length > 0))
    | unique_by(.id)
    | .[0] // empty
  ' <<<"$ws_rows")"
  if [[ -z "${existing// }" ]]; then
    return 1
  fi

  local existing_id existing_name existing_root existing_assistant
  existing_id="$(jq -r '.id // ""' <<<"$existing")"
  existing_name="$(jq -r '.name // ""' <<<"$existing")"
  existing_root="$(jq -r '.root // ""' <<<"$existing")"
  existing_assistant="$(jq -r '.assistant // ""' <<<"$existing")"
  if [[ -z "$existing_id" ]]; then
    return 1
  fi

  context_set_workspace "$existing_id" "$existing_name" "$project" "$existing_assistant"

  local suggested_assistant
  suggested_assistant="$requested_assistant"
  if [[ -z "$suggested_assistant" ]]; then
    suggested_assistant="$existing_assistant"
  fi
  if [[ -z "$suggested_assistant" ]]; then
    suggested_assistant="codex"
  fi

  local alt_name
  alt_name="$(sanitize_workspace_name "${requested_name}-2")"
  if [[ "$alt_name" == "$requested_name" ]]; then
    alt_name="$(sanitize_workspace_name "${requested_name}-$(date +%H%M%S)")"
  fi

  RESULT_OK=false
  RESULT_COMMAND="workspace.create"
  RESULT_STATUS="attention"
  RESULT_SUMMARY="Workspace already exists: $existing_id"
  RESULT_NEXT_ACTION="Reuse the existing workspace, or retry with a different workspace name."
  RESULT_SUGGESTED_COMMAND="skills/amux/scripts/openclaw-dx.sh start --workspace $(shell_quote "$existing_id") --assistant $(shell_quote "$suggested_assistant") --prompt \"Summarize current status and continue with next high-impact task.\""

  local actions='[]'
  actions="$(append_action "$actions" "start_existing" "Use Existing" "$RESULT_SUGGESTED_COMMAND" "success" "Start in the existing workspace")"
  actions="$(append_action "$actions" "list_ws" "Workspaces" "skills/amux/scripts/openclaw-dx.sh workspace list --project $(shell_quote "$project")" "primary" "List all workspaces for this project")"
  actions="$(append_action "$actions" "retry_new_name" "New Name" "skills/amux/scripts/openclaw-dx.sh workspace create --name $(shell_quote "$alt_name") --project $(shell_quote "$project") --scope $(shell_quote "$requested_scope") --assistant $(shell_quote "$suggested_assistant")" "primary" "Retry workspace creation with a new name")"
  RESULT_QUICK_ACTIONS="$actions"

  RESULT_DATA="$(jq -cn \
    --arg project "$project" \
    --arg requested_name "$requested_name" \
    --arg requested_scope "$requested_scope" \
    --arg requested_assistant "$requested_assistant" \
    --arg conflict_message "$conflict_message" \
    --arg alt_name "$alt_name" \
    --argjson existing "$existing" \
    '{project: $project, requested_name: $requested_name, requested_scope: $requested_scope, requested_assistant: $requested_assistant, conflict_message: $conflict_message, alt_name: $alt_name, existing_workspace: $existing}')"

  RESULT_MESSAGE="⚠️ Workspace name/branch conflict while creating \"$requested_name\""$'\n'"Reusing existing workspace: $existing_id ($existing_name)"
  if [[ -n "$existing_root" ]]; then
    RESULT_MESSAGE+=$'\n'"Root: $existing_root"
  fi
  if [[ -n "${conflict_message// }" ]]; then
    RESULT_MESSAGE+=$'\n'"Conflict: $conflict_message"
  fi
  RESULT_MESSAGE+=$'\n'"Next: $RESULT_NEXT_ACTION"

  emit_result
  return 0
}

workspace_create_needs_initial_commit() {
  local err_code="${1:-}"
  local err_message="${2:-}"
  if [[ "$err_code" != "create_failed" ]]; then
    return 1
  fi
  local lower
  lower="$(printf '%s' "$err_message" | tr '[:upper:]' '[:lower:]')"
  if [[ "$lower" == *"invalid reference: head"* || "$lower" == *"not a valid object name head"* || "$lower" == *"ambiguous argument 'head'"* ]]; then
    return 0
  fi
  return 1
}

emit_initial_commit_guidance() {
  local command_name="$1"
  local project="$2"
  local retry_command="$3"
  local raw_error="$4"
  local commit_cmd
  commit_cmd="git -C $(shell_quote "$project") add -A && git -C $(shell_quote "$project") commit -m \"chore: initial commit\""

  RESULT_OK=false
  RESULT_COMMAND="$command_name"
  RESULT_STATUS="attention"
  RESULT_SUMMARY="Workspace creation blocked: repository has no initial commit"
  RESULT_NEXT_ACTION="Create the first commit in this repository, then retry workspace creation."
  RESULT_SUGGESTED_COMMAND="$commit_cmd"

  local actions='[]'
  actions="$(append_action "$actions" "retry" "Retry" "$retry_command" "primary" "Retry workspace creation after initial commit")"
  actions="$(append_action "$actions" "project_only" "Project Only" "skills/amux/scripts/openclaw-dx.sh project add --path $(shell_quote "$project")" "primary" "Register project without creating a workspace")"
  RESULT_QUICK_ACTIONS="$actions"

  RESULT_DATA="$(jq -cn --arg project "$project" --arg retry_command "$retry_command" --arg commit_command "$commit_cmd" --arg error "$raw_error" '{project: $project, retry_command: $retry_command, commit_command: $commit_command, error: $error, reason: "initial_commit_required"}')"
  RESULT_MESSAGE="⚠️ Workspace creation requires an initial commit"$'\n'"Project: $project"$'\n'"Error: $raw_error"$'\n'"Next: $RESULT_NEXT_ACTION"
  emit_result
}

cmd_workspace_create() {
  local name=""
  local project=""
  local from_workspace=""
  local scope=""
  local assistant=""
  local base=""

  while [[ $# -gt 0 ]]; do
    case "$1" in
      --name)
        name="$2"; shift 2 ;;
      --project)
        project="$2"; shift 2 ;;
      --from-workspace)
        from_workspace="$2"; shift 2 ;;
      --scope)
        scope="$2"; shift 2 ;;
      --assistant)
        assistant="$2"; shift 2 ;;
      --base)
        base="$2"; shift 2 ;;
      *)
        emit_error "workspace.create" "command_error" "unknown flag" "$1"
        return
        ;;
    esac
  done

  if [[ -z "$name" ]]; then
    emit_error "workspace.create" "command_error" "missing required flag: --name"
    return
  fi

  local parent_row=''
  local parent_name=""
  local parent_repo=""

  if [[ -n "$from_workspace" ]]; then
    if ! parent_row="$(workspace_row_by_id "$from_workspace")"; then
      emit_amux_error "workspace.create"
      return
    fi
    if [[ -z "${parent_row// }" ]]; then
      emit_error "workspace.create" "command_error" "--from-workspace not found" "$from_workspace"
      return
    fi
    parent_name="$(jq -r '.name // ""' <<<"$parent_row")"
    parent_repo="$(jq -r '.repo // ""' <<<"$parent_row")"
  fi

  if [[ -z "$scope" ]]; then
    if [[ -n "$from_workspace" ]]; then
      scope="nested"
    else
      scope="project"
    fi
  fi

  case "$scope" in
    project|nested) ;;
    *)
      emit_error "workspace.create" "command_error" "--scope must be project or nested"
      return
      ;;
  esac

  if [[ "$scope" == "nested" && -z "$from_workspace" ]]; then
    emit_error "workspace.create" "command_error" "nested scope requires --from-workspace"
    return
  fi

  if [[ -z "$project" && -n "$parent_repo" ]]; then
    project="$parent_repo"
  fi
  if [[ -z "$project" ]]; then
    project="$(context_resolve_project "")"
  fi

  if [[ -z "$project" ]]; then
    emit_error "workspace.create" "command_error" "missing project context" "provide --project or --from-workspace"
    return
  fi

  local final_name="$name"
  if [[ "$scope" == "nested" ]]; then
    final_name="$(compose_nested_workspace_name "$parent_name" "$name")"
  fi

  local out
  local args=(workspace create "$final_name" --project "$project")
  if [[ -n "$assistant" ]]; then
    args+=(--assistant "$assistant")
  fi
  if [[ -n "$base" ]]; then
    args+=(--base "$base")
  fi
  if ! out="$(amux_ok_json "${args[@]}")"; then
    local err_payload err_code err_message
    err_payload="$AMUX_ERROR_OUTPUT"
    if [[ -z "${err_payload// }" ]] && [[ -n "${AMUX_ERROR_CAPTURE_FILE:-}" ]] && [[ -f "$AMUX_ERROR_CAPTURE_FILE" ]]; then
      err_payload="$(cat "$AMUX_ERROR_CAPTURE_FILE" 2>/dev/null || true)"
    fi
    err_code=""
    err_message=""
    if jq -e . >/dev/null 2>&1 <<<"$err_payload"; then
      err_code="$(jq -r '.error.code // ""' <<<"$err_payload")"
      err_message="$(jq -r '.error.message // ""' <<<"$err_payload")"
    fi
    if workspace_create_needs_initial_commit "$err_code" "$err_message"; then
      local retry_cmd
      retry_cmd="skills/amux/scripts/openclaw-dx.sh workspace create --name $(shell_quote "$name") --project $(shell_quote "$project") --scope $(shell_quote "$scope") --assistant $(shell_quote "${assistant:-codex}")"
      if [[ -n "$from_workspace" ]]; then
        retry_cmd="skills/amux/scripts/openclaw-dx.sh workspace create --name $(shell_quote "$name") --from-workspace $(shell_quote "$from_workspace") --scope $(shell_quote "$scope") --assistant $(shell_quote "${assistant:-codex}")"
      fi
      if [[ -n "$base" ]]; then
        retry_cmd+=" --base $(shell_quote "$base")"
      fi
      emit_initial_commit_guidance "workspace.create" "$project" "$retry_cmd" "$err_message"
      return
    fi
    if [[ "$err_code" == "create_failed" ]] && [[ "$err_message" == *"already exists"* || "$err_message" == *"already used by worktree"* ]]; then
      if workspace_create_emit_existing_recovery "$project" "$final_name" "$scope" "$assistant" "$err_message"; then
        return
      fi
    fi
    emit_amux_error "workspace.create" "$err_payload"
    return
  fi

  local ws_id ws_root assistant_out
  ws_id="$(jq -r '.data.id // ""' <<<"$out")"
  ws_root="$(jq -r '.data.root // ""' <<<"$out")"
  assistant_out="$(jq -r '.data.assistant // ""' <<<"$out")"
  context_set_workspace "$ws_id" "$final_name" "$project" "$assistant_out"

  RESULT_OK=true
  RESULT_COMMAND="workspace.create"
  RESULT_STATUS="ok"
  RESULT_SUMMARY="Workspace ready: $ws_id"
  RESULT_NEXT_ACTION="Start coding in this workspace, or run terminal setup commands."
  RESULT_SUGGESTED_COMMAND="skills/amux/scripts/openclaw-dx.sh start --workspace $(shell_quote "$ws_id") --assistant $(shell_quote "${assistant_out:-codex}") --prompt \"Analyze the biggest debt item and implement the fix.\""

  local actions='[]'
  actions="$(append_action "$actions" "start" "Start" "$RESULT_SUGGESTED_COMMAND" "success" "Start a coding turn in this workspace")"
  actions="$(append_action "$actions" "term" "Terminal" "skills/amux/scripts/openclaw-dx.sh terminal run --workspace $(shell_quote "$ws_id") --text \"pwd\" --enter" "primary" "Run a terminal command in this workspace")"
  actions="$(append_action "$actions" "status" "Status" "skills/amux/scripts/openclaw-dx.sh status --workspace $(shell_quote "$ws_id")" "primary" "Show workspace status")"
  RESULT_QUICK_ACTIONS="$actions"

  RESULT_DATA="$(jq -cn --arg scope "$scope" --arg requested_name "$name" --arg final_name "$final_name" --arg parent_workspace "$from_workspace" --argjson workspace "$(jq -c '.data' <<<"$out")" '{scope: $scope, requested_name: $requested_name, final_name: $final_name, parent_workspace: $parent_workspace, workspace: $workspace}')"

  RESULT_MESSAGE="✅ Workspace ready: $ws_id"$'\n'"Name: $final_name"$'\n'"Scope: $scope"$'\n'"Project: $project"$'\n'"Root: $ws_root"$'\n'"Next: $RESULT_NEXT_ACTION"
  emit_result
}

cmd_workspace_list() {
  local project=""
  local workspace_id=""
  local limit=20
  local page=1

  while [[ $# -gt 0 ]]; do
    case "$1" in
      --project)
        project="$2"; shift 2 ;;
      --workspace)
        workspace_id="$2"; shift 2 ;;
      --limit)
        limit="$2"; shift 2 ;;
      --page)
        page="$2"; shift 2 ;;
      *)
        emit_error "workspace.list" "command_error" "unknown flag" "$1"
        return
        ;;
    esac
  done

  if ! is_positive_int "$limit"; then
    limit=20
  fi
  if ! is_positive_int "$page"; then
    page=1
  fi

  local project_from_context=false
  if [[ -z "$project" ]]; then
    project="$(context_resolve_project "")"
    if [[ -n "$project" ]]; then
      project_from_context=true
    fi
  fi

  local ws_out ws_args
  ws_args=(workspace list)
  if [[ -n "$project" ]]; then
    ws_args+=(--repo "$project")
  fi
  if ! ws_out="$(amux_ok_json "${ws_args[@]}")"; then
    emit_amux_error "workspace.list"
    return
  fi

  local ws_json
  ws_json="$(jq -c '.data // []' <<<"$ws_out")"
  if [[ -n "$workspace_id" ]]; then
    ws_json="$(jq -c --arg id "$workspace_id" 'map(select(.id == $id))' <<<"$ws_json")"
  fi

  local agents_out terminals_out agents_json terminals_json
  if ! agents_out="$(amux_ok_json agent list)"; then
    agents_json='[]'
  else
    agents_json="$(jq -c '.data // []' <<<"$agents_out")"
  fi
  if ! terminals_out="$(amux_ok_json terminal list)"; then
    terminals_json='[]'
  else
    terminals_json="$(jq -c '.data // []' <<<"$terminals_out")"
  fi

  local enriched sorted preview count lines
  enriched="$(jq -cn --argjson ws "$ws_json" --argjson agents "$agents_json" --argjson terms "$terminals_json" '
    $ws
    | map(
        . as $w
        | $w + {
            agent_count: ($agents | map(select(.workspace_id == $w.id)) | length),
            terminal_count: ($terms | map(select(.workspace_id == $w.id)) | length)
          }
      )
  ')"
  sorted="$(jq -c 'sort_by(.created) | reverse' <<<"$enriched")"
  count="$(jq -r 'length' <<<"$sorted")"
  local total_pages=1
  if [[ "$count" -gt 0 ]]; then
    total_pages=$(( (count + limit - 1) / limit ))
  fi
  if [[ "$page" -gt "$total_pages" ]]; then
    page="$total_pages"
  fi
  local offset
  offset=$(( (page - 1) * limit ))
  preview="$(jq -c --argjson offset "$offset" --argjson limit "$limit" '.[ $offset : ($offset + $limit) ]' <<<"$sorted")"

  lines="$(jq -r --argjson offset "$offset" '. | to_entries | map("\(($offset + .key + 1)). \(.value.id)  \(.value.name)  (a:\(.value.agent_count), t:\(.value.terminal_count))") | join("\n")' <<<"$preview")"
  local has_prev=false
  local has_next=false
  if [[ "$count" -gt 0 && "$page" -gt 1 ]]; then
    has_prev=true
  fi
  if [[ "$count" -gt 0 && "$page" -lt "$total_pages" ]]; then
    has_next=true
  fi

  RESULT_OK=true
  RESULT_COMMAND="workspace.list"
  RESULT_STATUS="ok"
  RESULT_SUMMARY="$count workspace(s)"
  if [[ "$count" -gt 0 ]]; then
    RESULT_SUMMARY+=" (page $page/$total_pages)"
  fi
  RESULT_NEXT_ACTION="Choose a workspace and start/continue a coding turn."
  RESULT_SUGGESTED_COMMAND=""

  local first_ws
  first_ws="$(jq -r '.[0].id // ""' <<<"$preview")"
  if [[ -n "$first_ws" ]]; then
    RESULT_SUGGESTED_COMMAND="skills/amux/scripts/openclaw-dx.sh start --workspace $(shell_quote "$first_ws") --assistant codex --prompt \"Summarize current objectives and pick the next coding task.\""
  fi

  if [[ -n "$project" ]]; then
    context_set_project "$project" ""
  fi
  if [[ -n "$workspace_id" ]]; then
    local selected_ws
    selected_ws="$(jq -c '.[0] // null' <<<"$preview")"
    if [[ "$selected_ws" != "null" ]]; then
      local selected_name selected_repo selected_assistant
      selected_name="$(jq -r '.name // ""' <<<"$selected_ws")"
      selected_repo="$(jq -r '.repo // ""' <<<"$selected_ws")"
      selected_assistant="$(jq -r '.assistant // ""' <<<"$selected_ws")"
      context_set_workspace "$workspace_id" "$selected_name" "$selected_repo" "$selected_assistant"
    fi
  elif [[ "$count" -eq 1 ]]; then
    local only_ws
    only_ws="$(jq -c '.[0] // null' <<<"$preview")"
    if [[ "$only_ws" != "null" ]]; then
      local only_id only_name only_repo only_assistant
      only_id="$(jq -r '.id // ""' <<<"$only_ws")"
      only_name="$(jq -r '.name // ""' <<<"$only_ws")"
      only_repo="$(jq -r '.repo // ""' <<<"$only_ws")"
      only_assistant="$(jq -r '.assistant // ""' <<<"$only_ws")"
      context_set_workspace "$only_id" "$only_name" "$only_repo" "$only_assistant"
    fi
  fi

  local actions='[]'
  if [[ -n "$first_ws" ]]; then
    actions="$(append_action "$actions" "start" "Start #1" "$RESULT_SUGGESTED_COMMAND" "success" "Start coding in the first listed workspace")"
    actions="$(append_action "$actions" "status" "Status #1" "skills/amux/scripts/openclaw-dx.sh status --workspace $(shell_quote "$first_ws")" "primary" "Check status for the first listed workspace")"
  fi
  local list_cmd_base="skills/amux/scripts/openclaw-dx.sh workspace list --limit $limit"
  if [[ -n "$project" ]]; then
    list_cmd_base+=" --project $(shell_quote "$project")"
  fi
  if [[ -n "$workspace_id" ]]; then
    list_cmd_base+=" --workspace $(shell_quote "$workspace_id")"
  fi
  if [[ "$has_prev" == "true" ]]; then
    actions="$(append_action "$actions" "prev_page" "Prev" "$list_cmd_base --page $((page - 1))" "primary" "Show previous workspaces page")"
  fi
  if [[ "$has_next" == "true" ]]; then
    actions="$(append_action "$actions" "next_page" "Next" "$list_cmd_base --page $((page + 1))" "primary" "Show next workspaces page")"
  fi
  actions="$(append_action "$actions" "global" "Global" "skills/amux/scripts/openclaw-dx.sh status" "primary" "Show global coding status")"
  RESULT_QUICK_ACTIONS="$actions"

  RESULT_DATA="$(jq -cn --arg project "$project" --argjson project_from_context "$project_from_context" --argjson count "$count" --argjson page "$page" --argjson limit "$limit" --argjson total_pages "$total_pages" --argjson has_prev "$has_prev" --argjson has_next "$has_next" --argjson workspaces "$sorted" --argjson workspaces_page "$preview" '{project: $project, project_from_context: $project_from_context, count: $count, page: $page, limit: $limit, total_pages: $total_pages, has_prev: $has_prev, has_next: $has_next, workspaces: $workspaces, workspaces_page: $workspaces_page}')"

  RESULT_MESSAGE="✅ $count workspace(s)"
  if [[ "$count" -gt 0 ]]; then
    RESULT_MESSAGE+=$'\n'"Page: $page/$total_pages"
  fi
  if [[ "$project_from_context" == "true" ]]; then
    RESULT_MESSAGE+=$'\n'"Project: $project (from active context)"
  elif [[ -n "$project" ]]; then
    RESULT_MESSAGE+=$'\n'"Project: $project"
  fi
  if [[ -n "${lines// }" ]]; then
    RESULT_MESSAGE+=$'\n'"$lines"
  fi
  RESULT_MESSAGE+=$'\n'"Next: $RESULT_NEXT_ACTION"
  emit_result
}

cmd_workspace_decide() {
  local project=""
  local from_workspace=""
  local task=""
  local assistant="${OPENCLAW_DX_DECIDE_ASSISTANT:-codex}"
  local name=""

  while [[ $# -gt 0 ]]; do
    case "$1" in
      --project)
        project="$2"; shift 2 ;;
      --from-workspace)
        from_workspace="$2"; shift 2 ;;
      --task)
        shift
        if [[ $# -eq 0 ]]; then
          emit_error "workspace.decide" "command_error" "missing value for --task"
          return
        fi
        task="$1"; shift
        while [[ $# -gt 0 && "$1" != --* ]]; do
          task+=" $1"
          shift
        done
        ;;
      --assistant)
        assistant="$2"; shift 2 ;;
      --name|--workspace-name)
        name="$2"; shift 2 ;;
      *)
        emit_error "workspace.decide" "command_error" "unknown flag" "$1"
        return
        ;;
    esac
  done

  if [[ -z "$project" && -z "$from_workspace" ]]; then
    project="$(context_resolve_project "")"
  fi
  if [[ -z "$project" && -z "$from_workspace" ]]; then
    emit_error "workspace.decide" "command_error" "missing context" "provide --project or --from-workspace"
    return
  fi

  local parent_row=""
  local parent_repo=""
  local parent_name=""
  local parent_id=""
  if [[ -n "$from_workspace" ]]; then
    if ! parent_row="$(workspace_row_by_id "$from_workspace")"; then
      emit_amux_error "workspace.decide"
      return
    fi
    if [[ -z "${parent_row// }" ]]; then
      emit_error "workspace.decide" "command_error" "--from-workspace not found" "$from_workspace"
      return
    fi
    parent_id="$(jq -r '.id // ""' <<<"$parent_row")"
    parent_repo="$(jq -r '.repo // ""' <<<"$parent_row")"
    parent_name="$(jq -r '.name // ""' <<<"$parent_row")"
    if [[ -z "$project" ]]; then
      project="$parent_repo"
    fi
  fi

  if [[ -z "$project" ]]; then
    emit_error "workspace.decide" "command_error" "missing project context" "project could not be inferred"
    return
  fi
  context_set_project "$project" ""

  local ws_out
  if ! ws_out="$(amux_ok_json workspace list --repo "$project")"; then
    emit_amux_error "workspace.decide"
    return
  fi
  local ws_json
  ws_json="$(jq -c '.data // []' <<<"$ws_out")"

  local agents_out
  if ! agents_out="$(amux_ok_json agent list)"; then
    emit_amux_error "workspace.decide"
    return
  fi
  local agents_json
  agents_json="$(jq -c '.data // []' <<<"$agents_out")"

  local existing_count active_project_agents
  existing_count="$(jq -r 'length' <<<"$ws_json")"
  active_project_agents="$(jq -nr --argjson ws "$ws_json" --argjson agents "$agents_json" '
    ($ws | map(.id // "")) as $ids
    | $agents
    | map(select((.workspace_id // "") as $wid | ($ids | index($wid)) != null))
    | length
  ')"

  local scope_hint recommendation reason
  scope_hint="$(workspace_scope_hint_from_task "$task")"
  recommendation="project"
  reason="Default to a project workspace."

  if [[ -n "$parent_id" ]]; then
    recommendation="nested"
    reason="Parent workspace specified; nested workspace keeps context isolated."
  elif [[ "$scope_hint" == "nested" ]]; then
    recommendation="nested"
    reason="Task wording suggests an isolated or parallel change."
  elif [[ "$scope_hint" == "project" ]]; then
    recommendation="project"
    reason="Task wording suggests primary project work."
  elif [[ "$existing_count" -eq 0 ]]; then
    recommendation="project"
    reason="No workspace exists for this project yet."
  elif [[ "$active_project_agents" -gt 0 ]]; then
    recommendation="nested"
    reason="There are active agents on this project; nested workspace reduces interference."
  fi

  if [[ -z "$parent_id" && "$recommendation" == "nested" ]]; then
    parent_id="$(jq -r '.[0].id // ""' <<<"$ws_json")"
    parent_name="$(jq -r '.[0].name // ""' <<<"$ws_json")"
    if [[ -z "$parent_id" ]]; then
      recommendation="project"
      reason="Nested workspace requested but no parent workspace exists yet."
    fi
  fi

  if [[ -z "$name" ]]; then
    if [[ "$recommendation" == "nested" ]]; then
      name="refactor"
    else
      name="mainline"
    fi
  fi

  local final_project_name final_nested_name
  final_project_name="$(sanitize_workspace_name "$name")"
  final_nested_name="$final_project_name"
  if [[ "$recommendation" == "nested" && -n "$parent_name" ]]; then
    final_nested_name="$(compose_nested_workspace_name "$parent_name" "$name")"
  fi

  local project_create_cmd="" nested_create_cmd="" kickoff_prompt="" recommended_command="" alternate_command=""
  project_create_cmd="skills/amux/scripts/openclaw-dx.sh workspace create --name $(shell_quote "$final_project_name") --project $(shell_quote "$project") --assistant $(shell_quote "$assistant")"
  if [[ -n "$parent_id" ]]; then
    nested_create_cmd="skills/amux/scripts/openclaw-dx.sh workspace create --name $(shell_quote "$name") --from-workspace $(shell_quote "$parent_id") --scope nested --assistant $(shell_quote "$assistant")"
  else
    nested_create_cmd=""
  fi

  kickoff_prompt="$task"
  if [[ -z "${kickoff_prompt// }" ]]; then
    kickoff_prompt="Summarize objectives and implement the next highest-impact task."
  fi

  if [[ "$recommendation" == "nested" && -n "$parent_id" ]]; then
    recommended_command="skills/amux/scripts/openclaw-dx.sh workflow kickoff --from-workspace $(shell_quote "$parent_id") --scope nested --name $(shell_quote "$name") --assistant $(shell_quote "$assistant") --prompt $(shell_quote "$kickoff_prompt")"
    alternate_command="skills/amux/scripts/openclaw-dx.sh workflow kickoff --project $(shell_quote "$project") --name $(shell_quote "$final_project_name") --assistant $(shell_quote "$assistant") --prompt $(shell_quote "$kickoff_prompt")"
  else
    recommended_command="skills/amux/scripts/openclaw-dx.sh workflow kickoff --project $(shell_quote "$project") --name $(shell_quote "$final_project_name") --assistant $(shell_quote "$assistant") --prompt $(shell_quote "$kickoff_prompt")"
    if [[ -n "$parent_id" ]]; then
      alternate_command="skills/amux/scripts/openclaw-dx.sh workflow kickoff --from-workspace $(shell_quote "$parent_id") --scope nested --name $(shell_quote "$name") --assistant $(shell_quote "$assistant") --prompt $(shell_quote "$kickoff_prompt")"
    fi
  fi

  RESULT_OK=true
  RESULT_COMMAND="workspace.decide"
  RESULT_STATUS="ok"
  RESULT_SUMMARY="Recommended scope: $recommendation"
  RESULT_NEXT_ACTION="Create the recommended workspace and start coding."
  RESULT_SUGGESTED_COMMAND="$recommended_command"

  local actions='[]'
  actions="$(append_action "$actions" "recommended" "Recommended" "$recommended_command" "success" "Create recommended workspace and start coding")"
  actions="$(append_action "$actions" "project_ws" "Project WS" "$project_create_cmd" "primary" "Create a project-level workspace")"
  if [[ -n "$nested_create_cmd" ]]; then
    actions="$(append_action "$actions" "nested_ws" "Nested WS" "$nested_create_cmd" "primary" "Create a nested workspace")"
  fi
  if [[ -n "$alternate_command" ]]; then
    actions="$(append_action "$actions" "alternate" "Alternate" "$alternate_command" "primary" "Run the alternate kickoff flow")"
  fi
  RESULT_QUICK_ACTIONS="$actions"

  RESULT_DATA="$(jq -cn \
    --arg recommendation "$recommendation" \
    --arg reason "$reason" \
    --arg project "$project" \
    --arg parent_workspace "$parent_id" \
    --arg parent_name "$parent_name" \
    --arg suggested_name "$name" \
    --arg final_project_name "$final_project_name" \
    --arg final_nested_name "$final_nested_name" \
    --arg recommended_command "$recommended_command" \
    --arg alternate_command "$alternate_command" \
    --arg project_create_command "$project_create_cmd" \
    --arg nested_create_command "$nested_create_cmd" \
    --argjson existing_count "$existing_count" \
    --argjson active_project_agents "$active_project_agents" \
    --argjson workspaces "$ws_json" \
    '{
      recommendation: $recommendation,
      reason: $reason,
      project: $project,
      parent_workspace: $parent_workspace,
      parent_name: $parent_name,
      suggested_name: $suggested_name,
      final_project_name: $final_project_name,
      final_nested_name: $final_nested_name,
      recommended_command: $recommended_command,
      alternate_command: $alternate_command,
      project_create_command: $project_create_command,
      nested_create_command: $nested_create_command,
      existing_workspaces: $existing_count,
      active_project_agents: $active_project_agents,
      workspaces: $workspaces
    }')"

  RESULT_MESSAGE="✅ Workspace decision: $recommendation"$'\n'"Reason: $reason"$'\n'"Project: $project"
  if [[ -n "$parent_id" ]]; then
    RESULT_MESSAGE+=$'\n'"Parent workspace: $parent_id"
  fi
  RESULT_MESSAGE+=$'\n'"Project option: $project_create_cmd"
  if [[ -n "$nested_create_cmd" ]]; then
    RESULT_MESSAGE+=$'\n'"Nested option: $nested_create_cmd"
  fi
  RESULT_MESSAGE+=$'\n'"Next: $RESULT_NEXT_ACTION"

  emit_result
}

cmd_start() {
  local workspace=""
  local assistant=""
  local prompt=""
  local max_steps="${OPENCLAW_DX_MAX_STEPS:-3}"
  local turn_budget="${OPENCLAW_DX_TURN_BUDGET:-180}"
  local wait_timeout="${OPENCLAW_DX_WAIT_TIMEOUT:-60s}"
  local idle_threshold="${OPENCLAW_DX_IDLE_THRESHOLD:-10s}"

  while [[ $# -gt 0 ]]; do
    case "$1" in
      --workspace)
        workspace="$2"; shift 2 ;;
      --assistant)
        assistant="$2"; shift 2 ;;
      --prompt)
        shift
        if [[ $# -eq 0 ]]; then
          emit_error "start" "command_error" "missing value for --prompt"
          return
        fi
        prompt="$1"; shift
        while [[ $# -gt 0 && "$1" != --* ]]; do
          prompt+=" $1"
          shift
        done
        ;;
      --max-steps)
        max_steps="$2"; shift 2 ;;
      --turn-budget)
        turn_budget="$2"; shift 2 ;;
      --wait-timeout)
        wait_timeout="$2"; shift 2 ;;
      --idle-threshold)
        idle_threshold="$2"; shift 2 ;;
      *)
        emit_error "start" "command_error" "unknown flag" "$1"
        return
        ;;
    esac
  done

  workspace="$(context_resolve_workspace "$workspace")"
  wait_timeout="$(normalize_turn_wait_timeout "$wait_timeout")"
  if [[ -z "$workspace" || -z "$prompt" ]]; then
    emit_error "start" "command_error" "missing required flags" "start requires --prompt and a workspace (pass --workspace or set active context)"
    return
  fi
  if ! workspace_require_exists "start" "$workspace"; then
    return
  fi

  if [[ -z "$assistant" ]]; then
    assistant="$(default_assistant_for_workspace "$workspace")"
  fi
  if [[ -z "$assistant" ]]; then
    assistant="$(context_assistant_hint "$workspace")"
  fi
  if [[ -z "$assistant" ]]; then
    assistant="codex"
  fi
  if ! assistant_require_known "start" "$assistant"; then
    return
  fi
  context_set_workspace_with_lookup "$workspace" "$assistant"

  if [[ ! -x "$TURN_SCRIPT" ]]; then
    emit_error "start" "command_error" "turn script is not executable" "$TURN_SCRIPT"
    return
  fi

  local turn_json
  turn_json="$(OPENCLAW_TURN_SKIP_PRESENT=true "$TURN_SCRIPT" run \
    --workspace "$workspace" \
    --assistant "$assistant" \
    --prompt "$prompt" \
    --max-steps "$max_steps" \
    --turn-budget "$turn_budget" \
    --wait-timeout "$wait_timeout" \
    --idle-threshold "$idle_threshold" 2>&1 || true)"

  local permission_retry_enabled permission_fallback_assistant
  permission_retry_enabled="${OPENCLAW_DX_PERMISSION_RETRY:-true}"
  permission_fallback_assistant="${OPENCLAW_DX_PERMISSION_FALLBACK_ASSISTANT:-gemini}"
  if [[ "$permission_retry_enabled" != "false" ]] && turn_reports_permission_mode_gate "$turn_json"; then
    if [[ -n "${permission_fallback_assistant// }" && "$permission_fallback_assistant" != "$assistant" ]]; then
      local retry_turn_json
      retry_turn_json="$(OPENCLAW_TURN_SKIP_PRESENT=true "$TURN_SCRIPT" run \
        --workspace "$workspace" \
        --assistant "$permission_fallback_assistant" \
        --prompt "$prompt" \
        --max-steps "$max_steps" \
        --turn-budget "$turn_budget" \
        --wait-timeout "$wait_timeout" \
        --idle-threshold "$idle_threshold" 2>&1 || true)"
      if jq -e . >/dev/null 2>&1 <<<"$retry_turn_json"; then
        turn_json="$retry_turn_json"
      fi
    fi
  fi

  local nochange_retry_enabled nochange_fallback_assistant
  nochange_retry_enabled="${OPENCLAW_DX_NOCHANGE_RETRY:-true}"
  nochange_fallback_assistant="${OPENCLAW_DX_NOCHANGE_FALLBACK_ASSISTANT:-codex}"
  if [[ "$nochange_retry_enabled" != "false" ]] && turn_reports_no_workspace_change_claim "$turn_json"; then
    if [[ -n "${nochange_fallback_assistant// }" && "$nochange_fallback_assistant" != "$assistant" ]]; then
      local nochange_retry_json
      nochange_retry_json="$(OPENCLAW_TURN_SKIP_PRESENT=true "$TURN_SCRIPT" run \
        --workspace "$workspace" \
        --assistant "$nochange_fallback_assistant" \
        --prompt "$prompt" \
        --max-steps "$max_steps" \
        --turn-budget "$turn_budget" \
        --wait-timeout "$wait_timeout" \
        --idle-threshold "$idle_threshold" 2>&1 || true)"
      if jq -e . >/dev/null 2>&1 <<<"$nochange_retry_json"; then
        turn_json="$nochange_retry_json"
      fi
    fi
  fi

  turn_json="$(recover_timeout_turn_once "$turn_json" "$wait_timeout" "$idle_threshold")"

  emit_turn_passthrough "start" "coding_turn" "$turn_json"
}

cmd_continue() {
  local agent=""
  local workspace=""
  local text="${OPENCLAW_DX_CONTINUE_TEXT:-Continue from current state and provide concise status and next action.}"
  local enter=false
  local auto_start=false
  local start_assistant=""
  local max_steps="${OPENCLAW_DX_MAX_STEPS:-3}"
  local turn_budget="${OPENCLAW_DX_TURN_BUDGET:-180}"
  local wait_timeout="${OPENCLAW_DX_WAIT_TIMEOUT:-60s}"
  local idle_threshold="${OPENCLAW_DX_IDLE_THRESHOLD:-10s}"

  while [[ $# -gt 0 ]]; do
    case "$1" in
      --agent)
        agent="$2"; shift 2 ;;
      --workspace)
        workspace="$2"; shift 2 ;;
      --text)
        shift
        if [[ $# -eq 0 ]]; then
          emit_error "continue" "command_error" "missing value for --text"
          return
        fi
        text="$1"; shift
        while [[ $# -gt 0 && "$1" != --* ]]; do
          text+=" $1"
          shift
        done
        ;;
      --enter)
        enter=true; shift ;;
      --auto-start)
        auto_start=true; shift ;;
      --assistant)
        start_assistant="$2"; shift 2 ;;
      --max-steps)
        max_steps="$2"; shift 2 ;;
      --turn-budget)
        turn_budget="$2"; shift 2 ;;
      --wait-timeout)
        wait_timeout="$2"; shift 2 ;;
      --idle-threshold)
        idle_threshold="$2"; shift 2 ;;
      *)
        emit_error "continue" "command_error" "unknown flag" "$1"
        return
        ;;
    esac
  done

  if [[ -n "$start_assistant" ]]; then
    if ! assistant_require_known "continue" "$start_assistant"; then
      return
    fi
    if [[ "$auto_start" != "true" ]]; then
      emit_error "continue" "command_error" "--assistant requires --auto-start" "pass --auto-start when selecting an assistant for fallback start"
      return
    fi
  fi

  workspace="$(context_resolve_workspace "$workspace")"
  wait_timeout="$(normalize_turn_wait_timeout "$wait_timeout")"
  if [[ -z "$agent" && -z "$workspace" ]]; then
    agent="$(context_resolve_agent "")"
  fi
  if [[ -z "$agent" && -z "$workspace" ]]; then
    local active_agents_out active_agents_json active_count
    if active_agents_out="$(amux_ok_json agent list)"; then
      active_agents_json="$(jq -c '.data // []' <<<"$active_agents_out")"
      active_count="$(jq -r 'length' <<<"$active_agents_json")"
      if [[ "$active_count" == "1" ]]; then
        agent="$(jq -r '.[0].agent_id // ""' <<<"$active_agents_json")"
        workspace="$(jq -r '.[0].workspace_id // ""' <<<"$active_agents_json")"
      elif [[ "$active_count" =~ ^[0-9]+$ ]] && [[ "$active_count" -gt 1 ]]; then
        local first_agent first_workspace lines
        first_agent="$(jq -r '.[0].agent_id // ""' <<<"$active_agents_json")"
        first_workspace="$(jq -r '.[0].workspace_id // ""' <<<"$active_agents_json")"

        RESULT_OK=false
        RESULT_COMMAND="continue"
        RESULT_STATUS="attention"
        RESULT_SUMMARY="Multiple active agents found ($active_count)"
        RESULT_NEXT_ACTION="Choose one active agent to continue."
        RESULT_SUGGESTED_COMMAND=""
        if [[ -n "$first_agent" ]]; then
          RESULT_SUGGESTED_COMMAND="skills/amux/scripts/openclaw-dx.sh continue --agent $(shell_quote "$first_agent") --text \"Continue from current state and report status plus next action.\" --enter"
        elif [[ -n "$first_workspace" ]]; then
          RESULT_SUGGESTED_COMMAND="skills/amux/scripts/openclaw-dx.sh continue --workspace $(shell_quote "$first_workspace") --text \"Continue from current state and report status plus next action.\" --enter"
        fi

        local actions='[]'
        while IFS= read -r row; do
          [[ -z "${row// }" ]] && continue
          local row_index row_agent row_workspace row_session continue_cmd label
          row_index="$(jq -r '.index // ""' <<<"$row")"
          row_agent="$(jq -r '.agent_id // ""' <<<"$row")"
          row_workspace="$(jq -r '.workspace_id // ""' <<<"$row")"
          row_session="$(jq -r '.session_name // ""' <<<"$row")"
          if [[ -z "$row_agent" ]]; then
            continue
          fi
          continue_cmd="skills/amux/scripts/openclaw-dx.sh continue --agent $(shell_quote "$row_agent") --text \"Continue from current state and report status plus next action.\" --enter"
          label="Continue #$row_index"
          actions="$(append_action "$actions" "continue_${row_index}" "$label" "$continue_cmd" "primary" "Continue $row_agent in $row_workspace $row_session")"
        done < <(jq -c 'to_entries | map({index: (.key + 1), agent_id: (.value.agent_id // ""), workspace_id: (.value.workspace_id // ""), session_name: (.value.session_name // "")}) | .[0:6][]' <<<"$active_agents_json")
        actions="$(append_action "$actions" "status" "Status" "skills/amux/scripts/openclaw-dx.sh status" "primary" "See all active agents and alerts")"
        RESULT_QUICK_ACTIONS="$actions"

        RESULT_DATA="$(jq -cn --argjson active_count "$active_count" --argjson agents "$active_agents_json" '{reason: "multiple_active_agents", active_count: $active_count, agents: $agents}')"
        lines="$(jq -r '. | to_entries | map("\(.key + 1). \(.value.agent_id // "") (\(.value.workspace_id // "unknown"))") | join("\n")' <<<"$(jq -c '.[0:6]' <<<"$active_agents_json")")"
        RESULT_MESSAGE="⚠️ Multiple active agents found"$'\n'"$lines"$'\n'"Next: $RESULT_NEXT_ACTION"
        emit_result
        return
      fi
    fi
  fi
  if [[ -z "$agent" && -z "$workspace" ]]; then
    emit_error "continue" "command_error" "missing target" "provide --agent/--workspace or set active context"
    return
  fi

  if [[ -z "$agent" && -n "$workspace" ]]; then
    if ! workspace_require_exists "continue" "$workspace"; then
      return
    fi
    context_set_workspace_with_lookup "$workspace" ""
    agent="$(agent_for_workspace "$workspace")"
    if [[ -z "$agent" ]]; then
      if [[ "$auto_start" == "true" ]]; then
        local resolved_assistant
        resolved_assistant="$start_assistant"
        if [[ -z "$resolved_assistant" ]]; then
          resolved_assistant="$(default_assistant_for_workspace "$workspace")"
        fi
        if [[ -z "$resolved_assistant" ]]; then
          resolved_assistant="$(context_assistant_hint "$workspace")"
        fi
        if [[ -z "$resolved_assistant" ]]; then
          resolved_assistant="codex"
        fi
        if ! assistant_require_known "continue" "$resolved_assistant"; then
          return
        fi

        local start_json
        if ! start_json="$(run_self_json start --workspace "$workspace" --assistant "$resolved_assistant" --prompt "$text" --max-steps "$max_steps" --turn-budget "$turn_budget" --wait-timeout "$wait_timeout" --idle-threshold "$idle_threshold")"; then
          emit_error "continue" "command_error" "failed auto-start continuation" "unable to launch start fallback"
          return
        fi
        jq -c --arg command "continue" --arg workflow "auto_start_turn" '. + {command: $command, workflow: $workflow, auto_started: true}' <<<"$start_json"
        return
      fi

      RESULT_OK=false
      RESULT_COMMAND="continue"
      RESULT_STATUS="attention"
      RESULT_SUMMARY="No active agent found for workspace $workspace"
      RESULT_NEXT_ACTION="Start a new agent turn in this workspace, then continue. You can also use --auto-start."
      RESULT_SUGGESTED_COMMAND="skills/amux/scripts/openclaw-dx.sh continue --workspace $(shell_quote "$workspace") --auto-start --assistant codex --text \"Resume work and provide status plus next action.\""
      RESULT_DATA="$(jq -cn --arg workspace "$workspace" '{workspace: $workspace, reason: "no_active_agent"}')"
      local start_cmd
      start_cmd="skills/amux/scripts/openclaw-dx.sh start --workspace $(shell_quote "$workspace") --assistant codex --prompt \"Resume work and provide status plus next action.\""
      RESULT_QUICK_ACTIONS="$(jq -cn --arg cmd "$RESULT_SUGGESTED_COMMAND" --arg start_cmd "$start_cmd" '
        [
          {id:"auto_start", label:"Auto Start", command:$cmd, style:"success", prompt:"Auto-start and continue in one command"},
          {id:"start", label:"Start", command:$start_cmd, style:"primary", prompt:"Start a new coding turn"}
        ]')"
      RESULT_MESSAGE="⚠️ No active agent in workspace $workspace"$'\n'"Next: $RESULT_NEXT_ACTION"
      emit_result
      return
    fi
  fi

  if [[ -n "$agent" ]]; then
    context_set_agent "$agent" "$workspace" ""
  fi

  if [[ ! -x "$TURN_SCRIPT" ]]; then
    emit_error "continue" "command_error" "turn script is not executable" "$TURN_SCRIPT"
    return
  fi

  local turn_args=(
    "$TURN_SCRIPT" send
    --agent "$agent"
    --text "$text"
    --max-steps "$max_steps"
    --turn-budget "$turn_budget"
    --wait-timeout "$wait_timeout"
    --idle-threshold "$idle_threshold"
  )
  if [[ "$enter" == "true" ]]; then
    turn_args+=(--enter)
  fi

  local turn_json
  turn_json="$(OPENCLAW_TURN_SKIP_PRESENT=true "${turn_args[@]}" 2>&1 || true)"
  turn_json="$(recover_timeout_turn_once "$turn_json" "$wait_timeout" "$idle_threshold")"

  emit_turn_passthrough "continue" "followup_turn" "$turn_json"
}

cmd_status() {
  local result_command="${OPENCLAW_DX_STATUS_RESULT_COMMAND:-status}"
  case "$result_command" in
    status|alerts) ;;
    *) result_command="status" ;;
  esac
  local project=""
  local workspace=""
  local limit=12
  local capture_lines="${OPENCLAW_DX_STATUS_CAPTURE_LINES:-120}"
  local capture_agents_default="${OPENCLAW_DX_STATUS_CAPTURE_AGENTS:-6}"
  local capture_agents="$capture_agents_default"
  local capture_agents_explicit=false
  local older_than="${OPENCLAW_DX_STATUS_ALERT_OLDER_THAN:-24h}"
  local recent_workspaces="${OPENCLAW_DX_STATUS_RECENT_WORKSPACES:-4}"
  local alerts_only=false
  local include_stale_alerts=false

  while [[ $# -gt 0 ]]; do
    case "$1" in
      --project)
        project="$2"; shift 2 ;;
      --workspace)
        workspace="$2"; shift 2 ;;
      --limit)
        limit="$2"; shift 2 ;;
      --capture-lines)
        capture_lines="$2"; shift 2 ;;
      --capture-agents)
        capture_agents="$2"
        capture_agents_explicit=true
        shift 2 ;;
      --older-than)
        older_than="$2"; shift 2 ;;
      --recent-workspaces)
        recent_workspaces="$2"; shift 2 ;;
      --alerts-only)
        alerts_only=true; shift ;;
      --include-stale)
        include_stale_alerts=true; shift ;;
      *)
        emit_error "$result_command" "command_error" "unknown flag" "$1"
        return
        ;;
    esac
  done

  if [[ "${OPENCLAW_DX_FORCE_ALERTS_ONLY:-false}" == "true" ]]; then
    alerts_only=true
  fi
  if [[ "${OPENCLAW_DX_STATUS_INCLUDE_STALE_ALERTS:-false}" == "true" ]]; then
    include_stale_alerts=true
  fi

  if ! is_positive_int "$limit"; then
    limit=12
  fi
  if ! is_positive_int "$capture_lines"; then
    capture_lines=120
  fi
  if [[ "$capture_agents_explicit" != "true" ]]; then
    if [[ -n "$workspace" ]]; then
      capture_agents="${OPENCLAW_DX_STATUS_CAPTURE_AGENTS_WORKSPACE:-1}"
    elif [[ -n "$project" ]]; then
      capture_agents="${OPENCLAW_DX_STATUS_CAPTURE_AGENTS_PROJECT:-2}"
    fi
  fi
  if ! is_positive_int "$capture_agents"; then
    capture_agents="$capture_agents_default"
    if ! is_positive_int "$capture_agents"; then
      capture_agents=6
    fi
  fi
  if [[ ! "$recent_workspaces" =~ ^[0-9]+$ ]]; then
    recent_workspaces=4
  fi

  local projects_out ws_out agents_out terms_out sessions_out prune_out
  if ! projects_out="$(amux_ok_json project list)"; then
    emit_amux_error "$result_command"
    return
  fi

  local ws_args=(workspace list)
  if [[ -n "$project" ]]; then
    ws_args+=(--repo "$project")
  fi
  if ! ws_out="$(amux_ok_json "${ws_args[@]}")"; then
    emit_amux_error "$result_command"
    return
  fi

  local agents_args=(agent list)
  if [[ -n "$workspace" ]]; then
    agents_args+=(--workspace "$workspace")
  fi
  if ! agents_out="$(amux_ok_json "${agents_args[@]}")"; then
    emit_amux_error "$result_command"
    return
  fi

  local term_args=(terminal list)
  if [[ -n "$workspace" ]]; then
    term_args+=(--workspace "$workspace")
  fi
  if ! terms_out="$(amux_ok_json "${term_args[@]}")"; then
    emit_amux_error "$result_command"
    return
  fi

  if ! sessions_out="$(amux_ok_json session list)"; then
    emit_amux_error "$result_command"
    return
  fi

  if ! prune_out="$(amux_ok_json session prune --older-than "$older_than")"; then
    prune_out='{"ok":true,"data":{"dry_run":true,"pruned":[],"total":0,"errors":[]}}'
  fi

  local ws_json agents_json terms_json workspace_total_count recent_workspaces_applied=false
  ws_json="$(jq -c '.data // []' <<<"$ws_out")"
  if [[ -n "$workspace" ]]; then
    ws_json="$(jq -c --arg id "$workspace" 'map(select(.id == $id))' <<<"$ws_json")"
    if [[ "$(jq -r 'length' <<<"$ws_json")" -eq 0 ]]; then
      emit_error "$result_command" "command_error" "workspace not found" "$workspace"
      return
    fi
    context_set_workspace_with_lookup "$workspace" ""
  fi
  workspace_total_count="$(jq -r 'length' <<<"$ws_json")"
  if [[ -n "$project" && -z "$workspace" && "$recent_workspaces" -gt 0 ]]; then
    ws_json="$(jq -c --argjson n "$recent_workspaces" 'sort_by(.created // "") | reverse | .[:$n]' <<<"$ws_json")"
    recent_workspaces_applied=true
  fi
  agents_json="$(jq -c '.data // []' <<<"$agents_out")"
  terms_json="$(jq -c '.data // []' <<<"$terms_out")"
  if [[ -n "$project" && -z "$workspace" ]]; then
    local scoped_workspace_ids
    scoped_workspace_ids="$(jq -c 'map(.id)' <<<"$ws_json")"
    agents_json="$(jq -c --argjson ids "$scoped_workspace_ids" 'map(select((.workspace_id // "") as $wid | ($ids | index($wid)) != null))' <<<"$agents_json")"
    terms_json="$(jq -c --argjson ids "$scoped_workspace_ids" 'map(select((.workspace_id // "") as $wid | ($ids | index($wid)) != null))' <<<"$terms_json")"
  fi
  local workspace_order
  workspace_order="$(jq -c 'sort_by(.created // "") | reverse | map(.id)' <<<"$ws_json")"
  agents_json="$(jq -c --argjson order "$workspace_order" 'sort_by(. as $a | (($order | index($a.workspace_id // "")) // 999999), ($a.session_name // ""))' <<<"$agents_json")"

  local project_count workspace_count agent_count terminal_count session_count prune_total
  project_count="$(jq -r '.data // [] | length' <<<"$projects_out")"
  workspace_count="$(jq -r 'length' <<<"$ws_json")"
  agent_count="$(jq -r 'length' <<<"$agents_json")"
  terminal_count="$(jq -r 'length' <<<"$terms_json")"
  session_count="$(jq -r '.data // [] | length' <<<"$sessions_out")"
  if [[ -n "$project" && -z "$workspace" ]]; then
    session_count="$agent_count"
  fi
  prune_total="$(jq -r '.data.total // 0' <<<"$prune_out")"

  local alerts='[]'
  local captures='[]'

  while IFS= read -r session_name; do
    [[ -z "$session_name" ]] && continue

    local capture_out
    if ! capture_out="$(amux_ok_json agent capture "$session_name" --lines "$capture_lines")"; then
      continue
    fi

    local capture_status capture_summary capture_needs_input capture_hint
    capture_status="$(jq -r '.data.status // "captured"' <<<"$capture_out")"
    capture_summary="$(jq -r '.data.summary // .data.latest_line // ""' <<<"$capture_out")"
    capture_needs_input="$(jq -r '.data.needs_input // false' <<<"$capture_out")"
    capture_hint="$(jq -r '.data.input_hint // ""' <<<"$capture_out")"
    if [[ "$capture_needs_input" == "true" ]]; then
      local capture_hint_lc capture_summary_lc
      capture_hint_lc="$(printf '%s' "$capture_hint" | tr '[:upper:]' '[:lower:]')"
      capture_summary_lc="$(printf '%s' "$capture_summary" | tr '[:upper:]' '[:lower:]')"
      if [[ "$capture_hint_lc" == "what can i do for you?"* || "$capture_summary_lc" == *"needs input: what can i do for you?"* ]]; then
        capture_needs_input=false
      fi
    fi

    local agent_row agent_row_json agent_id workspace_id
    agent_row="$(jq -c --arg s "$session_name" '.[] | select(.session_name == $s)' <<<"$agents_json" | head -n 1)"
    agent_row_json='{}'
    if [[ -n "${agent_row// }" ]]; then
      agent_row_json="$agent_row"
    fi
    agent_id="$(jq -r '.agent_id // ""' <<<"$agent_row_json")"
    workspace_id="$(jq -r '.workspace_id // ""' <<<"$agent_row_json")"

    captures="$(jq -cn --argjson captures "$captures" --arg session "$session_name" --arg agent_id "$agent_id" --arg workspace_id "$workspace_id" --arg status "$capture_status" --arg summary "$capture_summary" --arg hint "$capture_hint" --argjson needs_input "$capture_needs_input" '$captures + [{session_name: $session, agent_id: $agent_id, workspace_id: $workspace_id, status: $status, summary: $summary, needs_input: $needs_input, input_hint: $hint}]')"

    if [[ "$capture_needs_input" == "true" ]]; then
      alerts="$(jq -cn --argjson alerts "$alerts" --arg type "needs_input" --arg session "$session_name" --arg agent_id "$agent_id" --arg workspace_id "$workspace_id" --arg summary "$capture_summary" --arg input_hint "$capture_hint" '$alerts + [{type: $type, session_name: $session, agent_id: $agent_id, workspace_id: $workspace_id, summary: $summary, input_hint: $input_hint}]')"
      continue
    fi

    if [[ "$capture_status" == "session_exited" ]]; then
      alerts="$(jq -cn --argjson alerts "$alerts" --arg type "session_exited" --arg session "$session_name" --arg agent_id "$agent_id" --arg workspace_id "$workspace_id" --arg summary "$capture_summary" '$alerts + [{type: $type, session_name: $session, agent_id: $agent_id, workspace_id: $workspace_id, summary: $summary}]')"
      continue
    fi

    if completion_signal_present "$capture_summary"; then
      alerts="$(jq -cn --argjson alerts "$alerts" --arg type "completed" --arg session "$session_name" --arg agent_id "$agent_id" --arg workspace_id "$workspace_id" --arg summary "$capture_summary" '$alerts + [{type: $type, session_name: $session, agent_id: $agent_id, workspace_id: $workspace_id, summary: $summary}]')"
    fi
  done < <(jq -r --argjson cap "$capture_agents" '.[:$cap][]?.session_name' <<<"$agents_json")

  if [[ "$include_stale_alerts" == "true" ]] && [[ -z "$workspace" ]] && [[ -z "$project" ]] && [[ "$prune_total" =~ ^[0-9]+$ ]] && [[ "$prune_total" -gt 0 ]]; then
    alerts="$(jq -cn --argjson alerts "$alerts" --arg older_than "$older_than" --argjson total "$prune_total" '$alerts + [{type: "stale_sessions", total: $total, older_than: $older_than}]')"
  fi

  local needs_input_count completed_count stale_alert_count alert_count
  needs_input_count="$(jq -r '[.[] | select(.type == "needs_input")] | length' <<<"$alerts")"
  completed_count="$(jq -r '[.[] | select(.type == "completed")] | length' <<<"$alerts")"
  stale_alert_count="$(jq -r '[.[] | select(.type == "stale_sessions")] | length' <<<"$alerts")"
  alert_count="$(jq -r 'length' <<<"$alerts")"

  local status="ok"
  if [[ "$needs_input_count" -gt 0 ]]; then
    status="needs_input"
  elif [[ "$alert_count" -gt 0 ]]; then
    status="attention"
  fi

  local summary
  if [[ "$status" == "ok" ]]; then
    summary="All clear: $agent_count agent(s), $terminal_count terminal(s), $workspace_count workspace(s)."
  else
    summary="$alert_count alert(s): $needs_input_count need input, $completed_count completed, $stale_alert_count stale session alert(s)."
  fi

  local next_action suggested_command
  next_action="Review active agents and continue where needed."
  local refresh_cmd
  refresh_cmd="skills/amux/scripts/openclaw-dx.sh $result_command"
  if [[ -n "$project" ]]; then
    refresh_cmd+=" --project $(shell_quote "$project")"
  fi
  if [[ -n "$workspace" ]]; then
    refresh_cmd+=" --workspace $(shell_quote "$workspace")"
  fi
  if [[ "$include_stale_alerts" == "true" ]]; then
    refresh_cmd+=" --include-stale"
  fi
  if [[ -n "$project" && -z "$workspace" && "$recent_workspaces" -gt 0 ]]; then
    refresh_cmd+=" --recent-workspaces $(shell_quote "$recent_workspaces")"
  fi
  if [[ "$alerts_only" == "true" && "$result_command" != "alerts" ]]; then
    refresh_cmd+=" --alerts-only"
  fi
  suggested_command="$refresh_cmd"

  local first_needs_input_agent first_completed_workspace first_completed_agent
  first_needs_input_agent="$(jq -r '.[] | select(.type == "needs_input") | .agent_id // empty' <<<"$alerts" | head -n 1)"
  first_completed_workspace="$(jq -r '.[] | select(.type == "completed") | .workspace_id // empty' <<<"$alerts" | head -n 1)"
  first_completed_agent="$(jq -r '.[] | select(.type == "completed") | .agent_id // empty' <<<"$alerts" | head -n 1)"
  if [[ -n "$first_needs_input_agent" ]]; then
    next_action="Reply to the blocked agent prompt first."
    suggested_command="skills/amux/scripts/openclaw-dx.sh continue --agent $(shell_quote "$first_needs_input_agent") --text \"Continue using the safest option and then report status plus next action.\" --enter"
  elif [[ "$completed_count" -gt 0 ]]; then
    next_action="Review recently completed agent work and ship if clean."
    if [[ -n "$first_completed_workspace" ]]; then
      suggested_command="skills/amux/scripts/openclaw-dx.sh review --workspace $(shell_quote "$first_completed_workspace") --assistant codex"
    elif [[ -n "$first_completed_agent" ]]; then
      suggested_command="skills/amux/scripts/openclaw-dx.sh continue --agent $(shell_quote "$first_completed_agent") --text \"Summarize final changes, tests, and remaining risks in 5 bullets.\" --enter"
    fi
  elif [[ "$stale_alert_count" -gt 0 ]]; then
    next_action="Clean stale sessions to reduce noise."
    suggested_command="skills/amux/scripts/openclaw-dx.sh cleanup --older-than $(shell_quote "$older_than") --yes"
  fi

  local ws_enriched ws_preview ws_lines
  ws_enriched="$(jq -cn --argjson ws "$ws_json" --argjson agents "$agents_json" --argjson terms "$terms_json" '
    $ws
    | map(
        . as $w
        | $w + {
            agent_count: ($agents | map(select(.workspace_id == $w.id)) | length),
            terminal_count: ($terms | map(select(.workspace_id == $w.id)) | length)
          }
      )
    | sort_by(.created)
    | reverse
  ')"
  ws_preview="$(jq -c --argjson limit "$limit" '.[0:$limit]' <<<"$ws_enriched")"
  ws_lines="$(jq -r '. | map("- \(.id) \(.name) (a:\(.agent_count), t:\(.terminal_count))") | join("\n")' <<<"$ws_preview")"

  local alert_lines
  alert_lines="$(jq -r --argjson limit "$limit" '.[:$limit] | map(
      if .type == "needs_input" then
        "- ❓ " + (.workspace_id // "") + " " + (.agent_id // "") + ": " + (.summary // "needs input")
      elif .type == "session_exited" then
        "- 🛑 " + (.workspace_id // "") + " " + (.agent_id // "") + ": session exited"
      elif .type == "completed" then
        "- ✅ " + (.workspace_id // "") + " " + (.agent_id // "") + ": " + (.summary // "completed")
      elif .type == "stale_sessions" then
        "- 🧹 stale sessions: " + ((.total // 0) | tostring) + " older than " + (.older_than // "")
      else
        "- ⚠️ " + (.type // "alert")
      end
    ) | join("\n")' <<<"$alerts")"

  local actions='[]'
  actions="$(append_action "$actions" "refresh" "Refresh" "$refresh_cmd" "primary" "Refresh agent/workspace status")"
  if [[ -n "$first_needs_input_agent" ]]; then
    actions="$(append_action "$actions" "reply" "Reply" "skills/amux/scripts/openclaw-dx.sh continue --agent $(shell_quote "$first_needs_input_agent") --text \"Continue using the safest option and report status and blockers.\" --enter" "danger" "Reply to blocked agent")"
  fi
  if [[ "$completed_count" -gt 0 ]]; then
    if [[ -n "$first_completed_workspace" ]]; then
      actions="$(append_action "$actions" "review_done" "Review Done" "skills/amux/scripts/openclaw-dx.sh review --workspace $(shell_quote "$first_completed_workspace") --assistant codex" "success" "Review completed workspace changes")"
      actions="$(append_action "$actions" "ship_done" "Ship Done" "skills/amux/scripts/openclaw-dx.sh git ship --workspace $(shell_quote "$first_completed_workspace") --push" "primary" "Commit and push completed workspace changes")"
    elif [[ -n "$first_completed_agent" ]]; then
      actions="$(append_action "$actions" "summary_done" "Summarize" "skills/amux/scripts/openclaw-dx.sh continue --agent $(shell_quote "$first_completed_agent") --text \"Summarize final changes, tests, and risks.\" --enter" "primary" "Capture final summary for completed agent")"
    fi
  fi
  if [[ "$stale_alert_count" -gt 0 ]]; then
    actions="$(append_action "$actions" "cleanup" "Cleanup" "skills/amux/scripts/openclaw-dx.sh cleanup --older-than $(shell_quote "$older_than") --yes" "danger" "Prune stale sessions")"
  fi
  local first_ws
  first_ws="$(jq -r '.[0].id // ""' <<<"$ws_enriched")"
  if [[ -n "$first_ws" ]]; then
    actions="$(append_action "$actions" "continue_ws" "Continue WS" "skills/amux/scripts/openclaw-dx.sh continue --workspace $(shell_quote "$first_ws") --text \"Status update and next action.\" --enter" "success" "Continue active work in top workspace")"
  fi

  RESULT_OK=true
  RESULT_COMMAND="$result_command"
  RESULT_STATUS="$status"
  RESULT_SUMMARY="$summary"
  RESULT_NEXT_ACTION="$next_action"
  RESULT_SUGGESTED_COMMAND="$suggested_command"
  RESULT_QUICK_ACTIONS="$actions"

  RESULT_DATA="$(jq -cn \
    --argjson counts "$(jq -cn --argjson project_count "$project_count" --argjson workspace_count "$workspace_count" --argjson workspace_total_count "$workspace_total_count" --argjson agent_count "$agent_count" --argjson terminal_count "$terminal_count" --argjson session_count "$session_count" --argjson prune_total "$prune_total" --argjson completed_count "$completed_count" --argjson include_stale_alerts "$include_stale_alerts" --argjson recent_workspaces "$recent_workspaces" --argjson recent_workspaces_applied "$recent_workspaces_applied" '{projects: $project_count, workspaces: $workspace_count, workspace_total: $workspace_total_count, agents: $agent_count, terminals: $terminal_count, sessions: $session_count, prune_candidates: $prune_total, completed_alerts: $completed_count, include_stale_alerts: $include_stale_alerts, recent_workspaces: $recent_workspaces, recent_workspaces_applied: $recent_workspaces_applied}')" \
    --argjson workspaces "$ws_enriched" \
    --argjson alerts "$alerts" \
    --argjson captures "$captures" \
    '{counts: $counts, workspaces: $workspaces, alerts: $alerts, captures: $captures}')"

  RESULT_MESSAGE="$(printf '%s %s' "$(if [[ "$status" == "ok" ]]; then printf '✅'; elif [[ "$status" == "needs_input" ]]; then printf '❓'; else printf '⚠️'; fi)" "$summary")"
  RESULT_MESSAGE+=$'\n'"Counts: projects=$project_count workspaces=$workspace_count agents=$agent_count terminals=$terminal_count sessions=$session_count"
  if [[ "$recent_workspaces_applied" == "true" ]]; then
    RESULT_MESSAGE+=$'\n'"Scope: showing $workspace_count of $workspace_total_count most recent project workspace(s)"
  fi
  if [[ "$alert_count" -gt 0 ]] && [[ -n "${alert_lines// }" ]]; then
    RESULT_MESSAGE+=$'\n'"Alerts:"$'\n'"$alert_lines"
  fi
  if [[ "$alerts_only" != "true" ]] && [[ -n "${ws_lines// }" ]]; then
    RESULT_MESSAGE+=$'\n'"Workspaces:"$'\n'"$ws_lines"
  fi
  RESULT_MESSAGE+=$'\n'"Next: $next_action"

  if [[ "$status" == "ok" ]]; then
    RESULT_DELIVERY_ACTION="edit"
    RESULT_DELIVERY_PRIORITY=2
    RESULT_DELIVERY_RETRY_AFTER_SECONDS=20
    RESULT_DELIVERY_REPLACE_PREVIOUS=true
    RESULT_DELIVERY_DROP_PENDING=false
  else
    RESULT_DELIVERY_ACTION="send"
    RESULT_DELIVERY_PRIORITY=0
    RESULT_DELIVERY_RETRY_AFTER_SECONDS=0
    RESULT_DELIVERY_REPLACE_PREVIOUS=false
    RESULT_DELIVERY_DROP_PENDING=true
  fi

  emit_result
}

cmd_alerts() {
  OPENCLAW_DX_FORCE_ALERTS_ONLY=true OPENCLAW_DX_STATUS_RESULT_COMMAND=alerts cmd_status "$@"
}

cmd_terminal_run() {
  local workspace=""
  local text=""
  local enter=false

  while [[ $# -gt 0 ]]; do
    case "$1" in
      --workspace)
        workspace="$2"; shift 2 ;;
      --text)
        shift
        if [[ $# -eq 0 ]]; then
          emit_error "terminal.run" "command_error" "missing value for --text"
          return
        fi
        text="$1"; shift
        while [[ $# -gt 0 && "$1" != --* ]]; do
          text+=" $1"
          shift
        done
        ;;
      --enter)
        enter=true; shift ;;
      *)
        emit_error "terminal.run" "command_error" "unknown flag" "$1"
        return
        ;;
    esac
  done

  workspace="$(context_resolve_workspace "$workspace")"
  if [[ -z "$workspace" || -z "$text" ]]; then
    emit_error "terminal.run" "command_error" "missing required flags" "terminal run requires --text and a workspace (pass --workspace or set active context)"
    return
  fi
  context_set_workspace_with_lookup "$workspace" ""

  local args=(terminal run --workspace "$workspace" --text "$text")
  if [[ "$enter" == "true" ]]; then
    args+=(--enter=true)
  fi

  local out
  if ! out="$(amux_ok_json "${args[@]}")"; then
    emit_amux_error "terminal.run"
    return
  fi

  local session_name created
  session_name="$(jq -r '.data.session_name // ""' <<<"$out")"
  created="$(jq -r '.data.created // false' <<<"$out")"

  RESULT_OK=true
  RESULT_COMMAND="terminal.run"
  RESULT_STATUS="ok"
  RESULT_SUMMARY="Terminal command sent to workspace $workspace"
  RESULT_NEXT_ACTION="Check terminal logs for command output."
  RESULT_SUGGESTED_COMMAND="skills/amux/scripts/openclaw-dx.sh terminal logs --workspace $(shell_quote "$workspace") --lines 120"
  RESULT_DATA="$(jq -cn --argjson result "$(jq -c '.data' <<<"$out")" '{terminal: $result}')"

  local actions='[]'
  actions="$(append_action "$actions" "logs" "Logs" "$RESULT_SUGGESTED_COMMAND" "primary" "Capture terminal output")"
  actions="$(append_action "$actions" "status" "Status" "skills/amux/scripts/openclaw-dx.sh status --workspace $(shell_quote "$workspace")" "primary" "Check workspace status")"
  RESULT_QUICK_ACTIONS="$actions"

  RESULT_MESSAGE="✅ Terminal command sent"$'\n'"Workspace: $workspace"$'\n'"Session: $session_name"$'\n'"Created: $created"$'\n'"Command: $text"$'\n'"Next: $RESULT_NEXT_ACTION"
  emit_result
}

cmd_terminal_preset() {
  local workspace=""
  local kind="nextjs"
  local port="${OPENCLAW_DX_TERMINAL_PORT:-3000}"
  local host="${OPENCLAW_DX_TERMINAL_HOST:-0.0.0.0}"
  local manager="auto"

  while [[ $# -gt 0 ]]; do
    case "$1" in
      --workspace)
        workspace="$2"; shift 2 ;;
      --kind|--preset)
        kind="$2"; shift 2 ;;
      --port)
        port="$2"; shift 2 ;;
      --host)
        host="$2"; shift 2 ;;
      --manager)
        manager="$2"; shift 2 ;;
      *)
        emit_error "terminal.preset" "command_error" "unknown flag" "$1"
        return
        ;;
    esac
  done

  workspace="$(context_resolve_workspace "$workspace")"
  if [[ -z "$workspace" ]]; then
    emit_error "terminal.preset" "command_error" "missing required flag: --workspace (or set active context workspace)"
    return
  fi
  context_set_workspace_with_lookup "$workspace" ""
  if ! is_positive_int "$port"; then
    port=3000
  fi
  if ! is_valid_hostname "$host"; then
    host="0.0.0.0"
  fi

  local launch_cmd=""
  case "$kind" in
    nextjs)
      case "$manager" in
        auto)
          launch_cmd="export NEXT_TELEMETRY_DISABLED=1; if [ -f pnpm-lock.yaml ] && command -v pnpm >/dev/null 2>&1; then pnpm dev -- --port \"$port\" --hostname \"$host\"; elif [ -f yarn.lock ] && command -v yarn >/dev/null 2>&1; then yarn dev --port \"$port\" --hostname \"$host\"; elif { [ -f bun.lockb ] || [ -f bun.lock ]; } && command -v bun >/dev/null 2>&1; then bun run dev -- --port \"$port\" --hostname \"$host\"; else npm run dev -- --port \"$port\" --hostname \"$host\"; fi"
          ;;
        pnpm)
          launch_cmd="export NEXT_TELEMETRY_DISABLED=1; pnpm dev -- --port \"$port\" --hostname \"$host\""
          ;;
        yarn)
          launch_cmd="export NEXT_TELEMETRY_DISABLED=1; yarn dev --port \"$port\" --hostname \"$host\""
          ;;
        bun)
          launch_cmd="export NEXT_TELEMETRY_DISABLED=1; bun run dev -- --port \"$port\" --hostname \"$host\""
          ;;
        npm)
          launch_cmd="export NEXT_TELEMETRY_DISABLED=1; npm run dev -- --port \"$port\" --hostname \"$host\""
          ;;
        *)
          emit_error "terminal.preset" "command_error" "--manager must be auto|npm|pnpm|yarn|bun"
          return
          ;;
      esac
      ;;
    *)
      emit_error "terminal.preset" "command_error" "--kind must be nextjs"
      return
      ;;
  esac

  local out
  if ! out="$(amux_ok_json terminal run --workspace "$workspace" --text "$launch_cmd" --enter=true)"; then
    emit_amux_error "terminal.preset"
    return
  fi

  local session_name created
  session_name="$(jq -r '.data.session_name // ""' <<<"$out")"
  created="$(jq -r '.data.created // false' <<<"$out")"

  RESULT_OK=true
  RESULT_COMMAND="terminal.preset"
  RESULT_STATUS="ok"
  RESULT_SUMMARY="Started $kind preset in workspace $workspace"
  RESULT_NEXT_ACTION="Watch logs for server readiness and continue coding."
  RESULT_SUGGESTED_COMMAND="skills/amux/scripts/openclaw-dx.sh terminal logs --workspace $(shell_quote "$workspace") --lines 120"
  RESULT_DATA="$(jq -cn --arg workspace "$workspace" --arg kind "$kind" --arg manager "$manager" --arg host "$host" --argjson port "$port" --arg command "$launch_cmd" --arg session_name "$session_name" --argjson created "$created" --argjson terminal "$(jq -c '.data' <<<"$out")" '{workspace: $workspace, kind: $kind, manager: $manager, host: $host, port: $port, command: $command, session_name: $session_name, created: $created, terminal: $terminal}')"

  local actions='[]'
  actions="$(append_action "$actions" "logs" "Logs" "$RESULT_SUGGESTED_COMMAND" "primary" "Tail terminal logs for startup")"
  actions="$(append_action "$actions" "status" "Status" "skills/amux/scripts/openclaw-dx.sh status --workspace $(shell_quote "$workspace")" "primary" "Check workspace status")"
  actions="$(append_action "$actions" "alerts" "Alerts" "skills/amux/scripts/openclaw-dx.sh alerts --workspace $(shell_quote "$workspace")" "primary" "Check blockers requiring attention")"
  RESULT_QUICK_ACTIONS="$actions"

  RESULT_MESSAGE="✅ Terminal preset started: $kind"$'\n'"Workspace: $workspace"$'\n'"Session: $session_name"$'\n'"Created: $created"$'\n'"Host/Port: $host:$port"$'\n'"Next: $RESULT_NEXT_ACTION"
  emit_result
}

cmd_terminal_logs() {
  local workspace=""
  local lines=200
  local retry_attempts="${OPENCLAW_DX_TERMINAL_LOGS_RETRIES:-4}"
  local retry_delay_seconds="${OPENCLAW_DX_TERMINAL_LOGS_RETRY_DELAY:-1}"

  while [[ $# -gt 0 ]]; do
    case "$1" in
      --workspace)
        workspace="$2"; shift 2 ;;
      --lines)
        lines="$2"; shift 2 ;;
      *)
        emit_error "terminal.logs" "command_error" "unknown flag" "$1"
        return
        ;;
    esac
  done

  workspace="$(context_resolve_workspace "$workspace")"
  if [[ -z "$workspace" ]]; then
    emit_error "terminal.logs" "command_error" "missing required flag: --workspace (or set active context workspace)"
    return
  fi
  context_set_workspace_with_lookup "$workspace" ""
  if ! is_positive_int "$lines"; then
    lines=200
  fi
  if ! is_positive_int "$retry_attempts"; then
    retry_attempts=4
  fi
  if ! is_positive_int "$retry_delay_seconds"; then
    retry_delay_seconds=1
  fi

  local out
  local attempt=1
  while true; do
    if out="$(amux_ok_json terminal logs --workspace "$workspace" --lines "$lines")"; then
      break
    fi
    local err_out err_code err_message
    err_out="$AMUX_ERROR_OUTPUT"
    if [[ -z "${err_out// }" ]] && [[ -n "${AMUX_ERROR_CAPTURE_FILE:-}" ]] && [[ -f "$AMUX_ERROR_CAPTURE_FILE" ]]; then
      err_out="$(cat "$AMUX_ERROR_CAPTURE_FILE" 2>/dev/null || true)"
    fi
    err_code=""
    err_message=""
    if jq -e . >/dev/null 2>&1 <<<"$err_out"; then
      err_code="$(jq -r '.error.code // ""' <<<"$err_out")"
      err_message="$(jq -r '.error.message // ""' <<<"$err_out")"
    fi
    if [[ "$err_code" == "capture_failed" && "$attempt" -lt "$retry_attempts" ]]; then
      sleep "$retry_delay_seconds"
      attempt=$((attempt + 1))
      continue
    fi
    if { [[ "$err_code" == "not_found" && "$err_message" == *"no terminal session found for workspace"* ]]; } || [[ "$err_out" == *"no terminal session found for workspace"* ]]; then
      RESULT_OK=false
      RESULT_COMMAND="terminal.logs"
      RESULT_STATUS="attention"
      RESULT_SUMMARY="No terminal session found for workspace $workspace"
      RESULT_NEXT_ACTION="Start a terminal command or preset first, then fetch logs."
      RESULT_SUGGESTED_COMMAND="skills/amux/scripts/openclaw-dx.sh terminal run --workspace $(shell_quote "$workspace") --text \"pwd\" --enter"
      RESULT_DATA="$(jq -cn --arg workspace "$workspace" --argjson error "$(normalize_json_or_default "$err_out" '{}')" '{workspace: $workspace, error: $error, reason: "no_terminal_session"}')"

      local actions='[]'
      actions="$(append_action "$actions" "term_run" "Run Cmd" "$RESULT_SUGGESTED_COMMAND" "primary" "Start a terminal session with a quick command")"
      actions="$(append_action "$actions" "preset" "Preset" "skills/amux/scripts/openclaw-dx.sh terminal preset --workspace $(shell_quote "$workspace") --kind nextjs" "success" "Start a Next.js dev terminal preset")"
      actions="$(append_action "$actions" "status" "Status" "skills/amux/scripts/openclaw-dx.sh status --workspace $(shell_quote "$workspace")" "primary" "Check workspace status")"
      RESULT_QUICK_ACTIONS="$actions"
      RESULT_MESSAGE="⚠️ No terminal session found for workspace $workspace"$'\n'"Next: $RESULT_NEXT_ACTION"
      emit_result
      return
    fi
    emit_amux_error "terminal.logs"
    return
  done

  local content excerpt
  content="$(jq -r '.data.content // ""' <<<"$out")"
  excerpt="$(printf '%s\n' "$content" | tail -n 20)"

  RESULT_OK=true
  RESULT_COMMAND="terminal.logs"
  RESULT_STATUS="ok"
  RESULT_SUMMARY="Captured terminal logs for workspace $workspace"
  RESULT_NEXT_ACTION="Continue coding or run another terminal command."
  RESULT_SUGGESTED_COMMAND="skills/amux/scripts/openclaw-dx.sh terminal run --workspace $(shell_quote "$workspace") --text \"npm test\" --enter"
  RESULT_DATA="$(jq -cn --argjson result "$(jq -c '.data' <<<"$out")" '{terminal: $result}')"

  local actions='[]'
  actions="$(append_action "$actions" "term_run" "Run Cmd" "$RESULT_SUGGESTED_COMMAND" "primary" "Run a follow-up terminal command")"
  actions="$(append_action "$actions" "status" "Status" "skills/amux/scripts/openclaw-dx.sh status --workspace $(shell_quote "$workspace")" "primary" "Check workspace status")"
  RESULT_QUICK_ACTIONS="$actions"

  RESULT_MESSAGE="✅ Terminal logs captured"$'\n'"Workspace: $workspace"
  if [[ -n "${excerpt// }" ]]; then
    RESULT_MESSAGE+=$'\n'"Logs:"$'\n'"$excerpt"
  fi
  RESULT_MESSAGE+=$'\n'"Next: $RESULT_NEXT_ACTION"
  emit_result
}

cmd_cleanup() {
  local older_than="${OPENCLAW_DX_CLEANUP_OLDER_THAN:-24h}"
  local yes=false

  while [[ $# -gt 0 ]]; do
    case "$1" in
      --older-than)
        older_than="$2"; shift 2 ;;
      --yes)
        yes=true; shift ;;
      *)
        emit_error "cleanup" "command_error" "unknown flag" "$1"
        return
        ;;
    esac
  done

  local args=(session prune --older-than "$older_than")
  if [[ "$yes" == "true" ]]; then
    args+=(--yes)
  fi

  local out
  if ! out="$(amux_ok_json "${args[@]}")"; then
    emit_amux_error "cleanup"
    return
  fi

  local total dry_run
  total="$(jq -r '.data.total // 0' <<<"$out")"
  dry_run="$(jq -r '.data.dry_run // false' <<<"$out")"

  RESULT_OK=true
  RESULT_COMMAND="cleanup"
  RESULT_STATUS="ok"
  if [[ "$dry_run" == "true" ]]; then
    RESULT_SUMMARY="Session cleanup dry-run result: $total"
  else
    RESULT_SUMMARY="Session cleanup result: $total"
  fi
  RESULT_NEXT_ACTION="Refresh status to verify active sessions and agents."
  RESULT_SUGGESTED_COMMAND="skills/amux/scripts/openclaw-dx.sh status"
  RESULT_DATA="$(jq -cn --argjson prune "$(jq -c '.data' <<<"$out")" '{prune: $prune}')"

  local actions='[]'
  if [[ "$dry_run" == "true" && "$total" -gt 0 ]]; then
    actions="$(append_action "$actions" "confirm" "Confirm" "skills/amux/scripts/openclaw-dx.sh cleanup --older-than $(shell_quote "$older_than") --yes" "danger" "Prune stale sessions now")"
  fi
  actions="$(append_action "$actions" "status" "Status" "skills/amux/scripts/openclaw-dx.sh status" "primary" "Refresh global status")"
  RESULT_QUICK_ACTIONS="$actions"

  RESULT_MESSAGE="✅ Cleanup $(if [[ "$dry_run" == "true" ]]; then printf '(dry run)'; else printf 'completed'; fi)"$'\n'"Older than: $older_than"$'\n'"Total: $total"$'\n'"Next: $RESULT_NEXT_ACTION"

  emit_result
}

cmd_review() {
  local workspace=""
  local assistant="${OPENCLAW_DX_REVIEW_ASSISTANT:-codex}"
  local prompt="${OPENCLAW_DX_REVIEW_PROMPT:-Review current uncommitted changes. Return findings first ordered by severity with file references, then residual risks and test gaps.}"
  local max_steps="${OPENCLAW_DX_REVIEW_MAX_STEPS:-2}"
  local turn_budget="${OPENCLAW_DX_REVIEW_TURN_BUDGET:-180}"
  local wait_timeout="${OPENCLAW_DX_WAIT_TIMEOUT:-60s}"
  local idle_threshold="${OPENCLAW_DX_IDLE_THRESHOLD:-10s}"

  while [[ $# -gt 0 ]]; do
    case "$1" in
      --workspace)
        workspace="$2"; shift 2 ;;
      --assistant)
        assistant="$2"; shift 2 ;;
      --prompt)
        shift
        if [[ $# -eq 0 ]]; then
          emit_error "review" "command_error" "missing value for --prompt"
          return
        fi
        prompt="$1"; shift
        while [[ $# -gt 0 && "$1" != --* ]]; do
          prompt+=" $1"
          shift
        done
        ;;
      --max-steps)
        max_steps="$2"; shift 2 ;;
      --turn-budget)
        turn_budget="$2"; shift 2 ;;
      --wait-timeout)
        wait_timeout="$2"; shift 2 ;;
      --idle-threshold)
        idle_threshold="$2"; shift 2 ;;
      *)
        emit_error "review" "command_error" "unknown flag" "$1"
        return
        ;;
    esac
  done

  workspace="$(context_resolve_workspace "$workspace")"
  if [[ -z "$workspace" ]]; then
    emit_error "review" "command_error" "missing required flag: --workspace (or set active context workspace)"
    return
  fi
  if ! workspace_require_exists "review" "$workspace"; then
    return
  fi
  if ! assistant_require_known "review" "$assistant"; then
    return
  fi
  context_set_workspace_with_lookup "$workspace" "$assistant"

  if [[ ! -x "$TURN_SCRIPT" ]]; then
    emit_error "review" "command_error" "turn script is not executable" "$TURN_SCRIPT"
    return
  fi

  local turn_json
  turn_json="$(OPENCLAW_TURN_SKIP_PRESENT=true "$TURN_SCRIPT" run \
    --workspace "$workspace" \
    --assistant "$assistant" \
    --prompt "$prompt" \
    --max-steps "$max_steps" \
    --turn-budget "$turn_budget" \
    --wait-timeout "$wait_timeout" \
    --idle-threshold "$idle_threshold" 2>&1 || true)"

  emit_turn_passthrough "review" "review_turn" "$turn_json"
}

cmd_git_ship() {
  local workspace=""
  local message=""
  local push=false

  while [[ $# -gt 0 ]]; do
    case "$1" in
      --workspace)
        workspace="$2"; shift 2 ;;
      --message)
        message="$2"; shift 2 ;;
      --push)
        push=true; shift ;;
      *)
        emit_error "git.ship" "command_error" "unknown flag" "$1"
        return
        ;;
    esac
  done

  workspace="$(context_resolve_workspace "$workspace")"
  if [[ -z "$workspace" ]]; then
    emit_error "git.ship" "command_error" "missing required flag: --workspace (or set active context workspace)"
    return
  fi

  local ws_row
  if ! ws_row="$(workspace_row_by_id "$workspace")"; then
    emit_amux_error "git.ship"
    return
  fi
  if [[ -z "${ws_row// }" ]]; then
    emit_error "git.ship" "command_error" "workspace not found" "$workspace"
    return
  fi
  local ws_name ws_repo ws_assistant
  ws_name="$(jq -r '.name // ""' <<<"$ws_row")"
  ws_repo="$(jq -r '.repo // ""' <<<"$ws_row")"
  ws_assistant="$(jq -r '.assistant // ""' <<<"$ws_row")"
  context_set_workspace "$workspace" "$ws_name" "$ws_repo" "$ws_assistant"

  local ws_root
  ws_root="$(jq -r '.root // ""' <<<"$ws_row")"
  if [[ -z "$ws_root" || ! -d "$ws_root" ]]; then
    emit_error "git.ship" "command_error" "workspace root is unavailable" "$ws_root"
    return
  fi

  local porcelain
  porcelain="$(git -C "$ws_root" status --porcelain --untracked-files=all 2>/dev/null || true)"
  if [[ -z "${porcelain// }" ]]; then
    local branch upstream_ref has_upstream=false has_origin=false ahead_count=0 pushed=false push_error=""
    branch="$(git -C "$ws_root" rev-parse --abbrev-ref HEAD 2>/dev/null || true)"
    upstream_ref=""
    if upstream_ref="$(git -C "$ws_root" rev-parse --abbrev-ref --symbolic-full-name '@{u}' 2>/dev/null)"; then
      has_upstream=true
      ahead_count="$(git -C "$ws_root" rev-list --count '@{u}..HEAD' 2>/dev/null || true)"
      if ! [[ "$ahead_count" =~ ^[0-9]+$ ]]; then
        ahead_count=0
      fi
    fi
    if git -C "$ws_root" remote get-url origin >/dev/null 2>&1; then
      has_origin=true
    fi

    if [[ "$push" == "true" ]]; then
      local push_cmd=()
      if [[ "$has_upstream" == "true" ]]; then
        if [[ "$ahead_count" -gt 0 ]]; then
          push_cmd=(git -C "$ws_root" push)
        fi
      elif [[ "$has_origin" == "true" ]]; then
        push_cmd=(git -C "$ws_root" push -u origin HEAD)
      else
        push_error="origin remote is not configured"
      fi
      if [[ "${#push_cmd[@]}" -gt 0 ]]; then
        if ! "${push_cmd[@]}" >/dev/null 2>&1; then
          push_error="git push failed"
        else
          pushed=true
        fi
      fi
    fi

    local suggest_push=false
    if [[ "$push" != "true" ]] && { [[ "$ahead_count" -gt 0 ]] || { [[ "$has_upstream" != "true" ]] && [[ "$has_origin" == "true" ]]; }; }; then
      suggest_push=true
    fi

    RESULT_OK=true
    RESULT_COMMAND="git.ship"
    RESULT_STATUS="ok"
    RESULT_SUMMARY="No changes to commit in workspace $workspace"
    RESULT_NEXT_ACTION="Continue coding or run a review workflow."
    RESULT_SUGGESTED_COMMAND="skills/amux/scripts/openclaw-dx.sh review --workspace $(shell_quote "$workspace")"

    if [[ "$push" == "true" && "$pushed" == "true" ]]; then
      RESULT_SUMMARY="No new changes to commit; pushed existing commits for $workspace"
      RESULT_NEXT_ACTION="Run review or continue implementation."
      RESULT_SUGGESTED_COMMAND="skills/amux/scripts/openclaw-dx.sh review --workspace $(shell_quote "$workspace")"
    elif [[ "$push" == "true" && -n "$push_error" ]]; then
      RESULT_STATUS="attention"
      RESULT_SUMMARY="No new changes to commit; push failed for $workspace"
      RESULT_NEXT_ACTION="Fix push issues, then retry push."
      RESULT_SUGGESTED_COMMAND="skills/amux/scripts/openclaw-dx.sh git ship --workspace $(shell_quote "$workspace") --push"
    elif [[ "$push" == "true" && "$has_upstream" == "true" && "$ahead_count" -eq 0 ]]; then
      RESULT_SUMMARY="No new changes to commit; branch is already pushed"
      RESULT_NEXT_ACTION="Continue coding or run review."
    elif [[ "$suggest_push" == "true" ]]; then
      RESULT_STATUS="attention"
      if [[ "$has_upstream" == "true" ]]; then
        RESULT_SUMMARY="No new changes to commit; $ahead_count commit(s) are ready to push"
      else
        RESULT_SUMMARY="No new changes to commit; branch has no upstream push target"
      fi
      RESULT_NEXT_ACTION="Push current commits to remote."
      RESULT_SUGGESTED_COMMAND="skills/amux/scripts/openclaw-dx.sh git ship --workspace $(shell_quote "$workspace") --push"
    fi

    local actions='[]'
    if [[ "$push" != "true" && "$suggest_push" == "true" ]]; then
      actions="$(append_action "$actions" "push" "Push" "skills/amux/scripts/openclaw-dx.sh git ship --workspace $(shell_quote "$workspace") --push" "success" "Push existing commits to remote")"
    fi
    actions="$(append_action "$actions" "review" "Review" "skills/amux/scripts/openclaw-dx.sh review --workspace $(shell_quote "$workspace")" "primary" "Run review workflow")"
    actions="$(append_action "$actions" "status" "Status" "skills/amux/scripts/openclaw-dx.sh status --workspace $(shell_quote "$workspace")" "primary" "Check workspace status")"
    RESULT_QUICK_ACTIONS="$actions"

    RESULT_DATA="$(jq -cn \
      --arg workspace "$workspace" \
      --arg root "$ws_root" \
      --arg branch "$branch" \
      --arg upstream "$upstream_ref" \
      --argjson has_upstream "$has_upstream" \
      --argjson has_origin "$has_origin" \
      --argjson ahead_count "$ahead_count" \
      --argjson committed false \
      --argjson pushed "$pushed" \
      --arg push_error "$push_error" \
      --argjson push_requested "$push" \
      '{workspace: $workspace, root: $root, branch: $branch, upstream: $upstream, has_upstream: $has_upstream, has_origin: $has_origin, ahead_count: $ahead_count, committed: $committed, pushed: $pushed, push_requested: $push_requested, push_error: $push_error, reason: "no_changes"}')"

    local message_prefix="✅"
    if [[ "$RESULT_STATUS" != "ok" ]]; then
      message_prefix="⚠️"
    fi
    RESULT_MESSAGE="$message_prefix No new changes to commit in workspace $workspace"
    if [[ "$push" == "true" && "$pushed" == "true" ]]; then
      RESULT_MESSAGE+=$'\n'"Push: success"
    elif [[ "$push" == "true" && -n "$push_error" ]]; then
      RESULT_MESSAGE+=$'\n'"Push: failed ($push_error)"
    elif [[ "$suggest_push" == "true" ]]; then
      if [[ "$has_upstream" == "true" ]]; then
        RESULT_MESSAGE+=$'\n'"Unpushed commits: $ahead_count"
      else
        RESULT_MESSAGE+=$'\n'"Unpushed branch: no upstream configured"
      fi
    fi
    RESULT_MESSAGE+=$'\n'"Next: $RESULT_NEXT_ACTION"
    emit_result
    return
  fi

  local file_count
  file_count="$(printf '%s\n' "$porcelain" | sed '/^$/d' | wc -l | tr -d ' ')"
  if [[ -z "$message" ]]; then
    message="chore(amux): update $workspace ($file_count files)"
  fi

  if ! git -C "$ws_root" add -A >/dev/null 2>&1; then
    emit_error "git.ship" "command_error" "git add failed" "$ws_root"
    return
  fi

  local commit_output
  if ! commit_output="$(git -C "$ws_root" commit -m "$message" 2>&1)"; then
    emit_error "git.ship" "command_error" "git commit failed" "$commit_output"
    return
  fi

  local commit_hash branch
  commit_hash="$(git -C "$ws_root" rev-parse --short HEAD 2>/dev/null || true)"
  branch="$(git -C "$ws_root" rev-parse --abbrev-ref HEAD 2>/dev/null || true)"

  local pushed=false
  local push_error=""
  if [[ "$push" == "true" ]]; then
    local push_cmd
    if git -C "$ws_root" rev-parse --abbrev-ref --symbolic-full-name '@{u}' >/dev/null 2>&1; then
      push_cmd=(git -C "$ws_root" push)
    else
      push_cmd=(git -C "$ws_root" push -u origin HEAD)
    fi
    if ! "${push_cmd[@]}" >/dev/null 2>&1; then
      push_error="git push failed"
    else
      pushed=true
    fi
  fi

  RESULT_OK=true
  RESULT_COMMAND="git.ship"
  RESULT_STATUS="ok"
  if [[ -n "$push_error" ]]; then
    RESULT_STATUS="attention"
  fi

  RESULT_SUMMARY="Committed $file_count file(s) in $workspace"
  if [[ "$pushed" == "true" ]]; then
    RESULT_SUMMARY+=" and pushed"
  fi

  RESULT_NEXT_ACTION="Run a review pass or continue implementation."
  RESULT_SUGGESTED_COMMAND="skills/amux/scripts/openclaw-dx.sh review --workspace $(shell_quote "$workspace")"
  if [[ "$pushed" != "true" ]]; then
    RESULT_SUGGESTED_COMMAND="skills/amux/scripts/openclaw-dx.sh git ship --workspace $(shell_quote "$workspace") --push"
  fi

  local actions='[]'
  if [[ "$pushed" != "true" ]]; then
    actions="$(append_action "$actions" "push" "Push" "skills/amux/scripts/openclaw-dx.sh git ship --workspace $(shell_quote "$workspace") --push" "success" "Push latest commit")"
  fi
  actions="$(append_action "$actions" "review" "Review" "skills/amux/scripts/openclaw-dx.sh review --workspace $(shell_quote "$workspace")" "primary" "Run review workflow")"
  actions="$(append_action "$actions" "status" "Status" "skills/amux/scripts/openclaw-dx.sh status --workspace $(shell_quote "$workspace")" "primary" "Check workspace status")"
  RESULT_QUICK_ACTIONS="$actions"

  RESULT_DATA="$(jq -cn --arg workspace "$workspace" --arg root "$ws_root" --arg commit_hash "$commit_hash" --arg branch "$branch" --arg message "$message" --argjson file_count "$file_count" --argjson pushed "$pushed" --arg push_error "$push_error" '{workspace: $workspace, root: $root, commit_hash: $commit_hash, branch: $branch, message: $message, file_count: $file_count, pushed: $pushed, push_error: $push_error}')"

  RESULT_MESSAGE="✅ Commit created"$'\n'"Workspace: $workspace"$'\n'"Branch: $branch"$'\n'"Commit: $commit_hash"$'\n'"Files: $file_count"
  if [[ "$pushed" == "true" ]]; then
    RESULT_MESSAGE+=$'\n'"Push: success"
  elif [[ -n "$push_error" ]]; then
    RESULT_MESSAGE+=$'\n'"Push: failed"
  else
    RESULT_MESSAGE+=$'\n'"Push: skipped"
  fi
  RESULT_MESSAGE+=$'\n'"Next: $RESULT_NEXT_ACTION"

  emit_result
}

cmd_workflow_kickoff() {
  local name=""
  local project=""
  local from_workspace=""
  local scope=""
  local assistant=""
  local prompt=""
  local base=""
  local max_steps="${OPENCLAW_DX_MAX_STEPS:-3}"
  local turn_budget="${OPENCLAW_DX_TURN_BUDGET:-180}"
  local wait_timeout="${OPENCLAW_DX_WAIT_TIMEOUT:-60s}"
  local idle_threshold="${OPENCLAW_DX_IDLE_THRESHOLD:-10s}"

  while [[ $# -gt 0 ]]; do
    case "$1" in
      --name|--workspace-name)
        name="$2"; shift 2 ;;
      --project)
        project="$2"; shift 2 ;;
      --from-workspace)
        from_workspace="$2"; shift 2 ;;
      --scope)
        scope="$2"; shift 2 ;;
      --assistant)
        assistant="$2"; shift 2 ;;
      --prompt)
        shift
        if [[ $# -eq 0 ]]; then
          emit_error "workflow.kickoff" "command_error" "missing value for --prompt"
          return
        fi
        prompt="$1"; shift
        while [[ $# -gt 0 && "$1" != --* ]]; do
          prompt+=" $1"
          shift
        done
        ;;
      --base)
        base="$2"; shift 2 ;;
      --max-steps)
        max_steps="$2"; shift 2 ;;
      --turn-budget)
        turn_budget="$2"; shift 2 ;;
      --wait-timeout)
        wait_timeout="$2"; shift 2 ;;
      --idle-threshold)
        idle_threshold="$2"; shift 2 ;;
      *)
        emit_error "workflow.kickoff" "command_error" "unknown flag" "$1"
        return
        ;;
    esac
  done

  if [[ -z "$name" || -z "$prompt" ]]; then
    emit_error "workflow.kickoff" "command_error" "missing required flags" "workflow kickoff requires --name and --prompt"
    return
  fi
  if [[ -n "$assistant" ]]; then
    if ! assistant_require_known "workflow.kickoff" "$assistant"; then
      return
    fi
  fi
  if [[ -z "$project" && -z "$from_workspace" ]]; then
    project="$(context_resolve_project "")"
  fi
  if [[ -z "$project" && -z "$from_workspace" ]]; then
    emit_error "workflow.kickoff" "command_error" "missing project context" "provide --project or --from-workspace"
    return
  fi

  local project_data='null'
  if [[ -n "$project" ]]; then
    local ensured_project
    if ! ensured_project="$(ensure_project_registered "$project")"; then
      emit_amux_error "workflow.kickoff"
      return
    fi
    project_data="$(normalize_json_or_default "$ensured_project" 'null')"
    local ensured_path
    ensured_path="$(jq -r '.path // ""' <<<"$project_data")"
    if [[ -n "$ensured_path" ]]; then
      project="$ensured_path"
      context_set_project "$project" "$(jq -r '.name // ""' <<<"$project_data")"
    fi
  fi

  local ws_args=(workspace create --name "$name")
  if [[ -n "$project" ]]; then
    ws_args+=(--project "$project")
  fi
  if [[ -n "$from_workspace" ]]; then
    ws_args+=(--from-workspace "$from_workspace")
  fi
  if [[ -n "$scope" ]]; then
    ws_args+=(--scope "$scope")
  fi
  if [[ -n "$assistant" ]]; then
    ws_args+=(--assistant "$assistant")
  fi
  if [[ -n "$base" ]]; then
    ws_args+=(--base "$base")
  fi

  local ws_json
  if ! ws_json="$(run_self_json "${ws_args[@]}")"; then
    emit_error "workflow.kickoff" "command_error" "failed to run workspace.create subcommand" "${ws_args[*]}"
    return
  fi

  local ws_ok
  ws_ok="$(jq -r '.ok // false' <<<"$ws_json")"
  if [[ "$ws_ok" != "true" ]]; then
    jq -c --arg command "workflow.kickoff" --arg workflow "kickoff" --arg phase "workspace" '. + {command: $command, workflow: $workflow, phase: $phase}' <<<"$ws_json"
    return
  fi

  local workspace_id
  workspace_id="$(jq -r '.data.workspace.id // .data.id // .workspace_id // ""' <<<"$ws_json")"
  if [[ -z "$workspace_id" ]]; then
    emit_error "workflow.kickoff" "command_error" "workspace id missing from workspace.create result" "$ws_json"
    return
  fi

  local start_args=(
    start
    --workspace "$workspace_id"
    --prompt "$prompt"
    --max-steps "$max_steps"
    --turn-budget "$turn_budget"
    --wait-timeout "$wait_timeout"
    --idle-threshold "$idle_threshold"
  )
  if [[ -n "$assistant" ]]; then
    start_args+=(--assistant "$assistant")
  fi

  local start_json
  if ! start_json="$(run_self_json "${start_args[@]}")"; then
    emit_error "workflow.kickoff" "command_error" "failed to run start subcommand" "${start_args[*]}"
    return
  fi

  local kickoff_payload
  kickoff_payload="$(jq -cn \
    --argjson project "$project_data" \
    --argjson workspace "$(jq -c '.data.workspace // .data // {}' <<<"$ws_json")" \
    --arg workspace_id "$workspace_id" \
    '{project: $project, workspace: $workspace, workspace_id: $workspace_id}')"

  local kickoff_json
  kickoff_json="$(jq -c \
    --arg command "workflow.kickoff" \
    --arg workflow "kickoff" \
    --argjson kickoff "$kickoff_payload" \
    --arg workspace_id "$workspace_id" \
    '
      def turn_snapshot:
        {
          mode: (.mode // ""),
          turn_id: (.turn_id // ""),
          status: (.status // ""),
          overall_status: (.overall_status // ""),
          summary: (.summary // ""),
          next_action: (.next_action // ""),
          suggested_command: (.suggested_command // ""),
          agent_id: (.agent_id // ""),
          workspace_id: (.workspace_id // ""),
          assistant: (.assistant // ""),
          steps_used: (.steps_used // null),
          max_steps: (.max_steps // null),
          elapsed_seconds: (.elapsed_seconds // null),
          milestones: (.milestones // [])
        };
      ((.quick_actions // []) + [
        {
          id: "status_ws",
          label: "Status",
          command: ("skills/amux/scripts/openclaw-dx.sh status --workspace " + $workspace_id),
          style: "primary",
          prompt: "Check workspace status"
        },
        {
          id: "review_ws",
          label: "Review",
          command: ("skills/amux/scripts/openclaw-dx.sh review --workspace " + $workspace_id + " --assistant codex"),
          style: "primary",
          prompt: "Run review on uncommitted changes"
        }
      ]) as $actions
      | .quick_actions = ($actions | unique_by(.id))
      | .data = ((.data // {}) + {
          kickoff: $kickoff,
          project: ($kickoff.project // null),
          workspace: ($kickoff.workspace // null),
          workspace_id: $workspace_id,
          turn: (turn_snapshot)
        })
      | . + {
          command: $command,
          workflow: $workflow,
          kickoff: $kickoff,
          phase: "start"
        }
      | del(.openclaw, .quick_action_by_id, .quick_action_prompts_by_id)
    ' <<<"$start_json")"

  if [[ -x "$OPENCLAW_PRESENT_SCRIPT" ]]; then
    "$OPENCLAW_PRESENT_SCRIPT" <<<"$kickoff_json"
  else
    printf '%s\n' "$kickoff_json"
  fi
}

cmd_workflow_dual() {
  local workspace=""
  local implement_assistant=""
  local implement_prompt="${OPENCLAW_DX_IMPLEMENT_PROMPT:-Identify the highest-impact technical-debt item in this workspace, implement the fix, run targeted validation, and summarize changed files plus remaining risks.}"
  local review_assistant="${OPENCLAW_DX_REVIEW_ASSISTANT:-codex}"
  local review_prompt="${OPENCLAW_DX_REVIEW_PROMPT:-Review current uncommitted changes. Return findings first ordered by severity with file references, then residual risks and test gaps.}"
  local auto_continue_impl="${OPENCLAW_DX_DUAL_AUTO_CONTINUE_IMPL:-true}"
  local auto_continue_impl_prompt="${OPENCLAW_DX_DUAL_AUTO_CONTINUE_PROMPT:-Continue using the safest option and report status plus next action.}"
  local implement_needs_input_retry="${OPENCLAW_DX_IMPLEMENT_NEEDS_INPUT_RETRY:-true}"
  local implement_needs_input_fallback_assistant="${OPENCLAW_DX_IMPLEMENT_NEEDS_INPUT_FALLBACK_ASSISTANT:-codex}"
  local review_needs_input_retry="${OPENCLAW_DX_REVIEW_NEEDS_INPUT_RETRY:-true}"
  local review_needs_input_fallback_assistant="${OPENCLAW_DX_REVIEW_NEEDS_INPUT_FALLBACK_ASSISTANT:-codex}"
  local max_steps="${OPENCLAW_DX_MAX_STEPS:-3}"
  local turn_budget="${OPENCLAW_DX_TURN_BUDGET:-180}"
  local wait_timeout="${OPENCLAW_DX_WAIT_TIMEOUT:-60s}"
  local idle_threshold="${OPENCLAW_DX_IDLE_THRESHOLD:-10s}"
  local progress_stderr="${OPENCLAW_DX_PROGRESS_STDERR:-true}"
  local dual_started_at
  dual_started_at="$(date +%s)"

  dx_dual_progress() {
    local message="$1"
    if [[ "$progress_stderr" == "false" ]]; then
      return
    fi
    local now elapsed
    now="$(date +%s)"
    elapsed="$((now - dual_started_at))"
    printf '[openclaw-dx][workflow dual][%ss] %s\n' "$elapsed" "$message" >&2
  }

  while [[ $# -gt 0 ]]; do
    case "$1" in
      --workspace)
        workspace="$2"; shift 2 ;;
      --implement-assistant)
        implement_assistant="$2"; shift 2 ;;
      --implement-prompt)
        shift
        if [[ $# -eq 0 ]]; then
          emit_error "workflow.dual" "command_error" "missing value for --implement-prompt"
          return
        fi
        implement_prompt="$1"; shift
        while [[ $# -gt 0 && "$1" != --* ]]; do
          implement_prompt+=" $1"
          shift
        done
        ;;
      --review-assistant)
        review_assistant="$2"; shift 2 ;;
      --review-prompt)
        shift
        if [[ $# -eq 0 ]]; then
          emit_error "workflow.dual" "command_error" "missing value for --review-prompt"
          return
        fi
        review_prompt="$1"; shift
        while [[ $# -gt 0 && "$1" != --* ]]; do
          review_prompt+=" $1"
          shift
        done
        ;;
      --max-steps)
        max_steps="$2"; shift 2 ;;
      --turn-budget)
        turn_budget="$2"; shift 2 ;;
      --wait-timeout)
        wait_timeout="$2"; shift 2 ;;
      --idle-threshold)
        idle_threshold="$2"; shift 2 ;;
      --auto-continue-impl)
        auto_continue_impl="$2"; shift 2 ;;
      --no-auto-continue-impl)
        auto_continue_impl="false"; shift ;;
      --auto-continue-impl-prompt)
        shift
        if [[ $# -eq 0 ]]; then
          emit_error "workflow.dual" "command_error" "missing value for --auto-continue-impl-prompt"
          return
        fi
        auto_continue_impl_prompt="$1"; shift
        while [[ $# -gt 0 && "$1" != --* ]]; do
          auto_continue_impl_prompt+=" $1"
          shift
        done
        ;;
      *)
        emit_error "workflow.dual" "command_error" "unknown flag" "$1"
        return
        ;;
    esac
  done

  local auto_continue_impl_lc
  auto_continue_impl_lc="$(printf '%s' "$auto_continue_impl" | tr '[:upper:]' '[:lower:]')"
  case "$auto_continue_impl_lc" in
    true|1|yes|on)
      auto_continue_impl="true"
      ;;
    false|0|no|off)
      auto_continue_impl="false"
      ;;
    *)
      auto_continue_impl="true"
      ;;
  esac

  workspace="$(context_resolve_workspace "$workspace")"
  if [[ -z "$workspace" ]]; then
    emit_error "workflow.dual" "command_error" "missing required flag: --workspace (or set active context workspace)"
    return
  fi
  if ! workspace_require_exists "workflow.dual" "$workspace"; then
    return
  fi

  if [[ -z "$implement_assistant" ]]; then
    implement_assistant="$(default_assistant_for_workspace "$workspace")"
  fi
  if [[ -z "$implement_assistant" ]]; then
    implement_assistant="$(context_assistant_hint "$workspace")"
  fi
  if [[ -z "$implement_assistant" ]]; then
    implement_assistant="codex"
  fi
  if ! assistant_require_known "workflow.dual" "$implement_assistant"; then
    return
  fi
  if [[ -z "$review_assistant" ]]; then
    review_assistant="codex"
  fi
  if ! assistant_require_known "workflow.dual" "$review_assistant"; then
    return
  fi

  local implement_args=(
    start
    --workspace "$workspace"
    --assistant "$implement_assistant"
    --prompt "$implement_prompt"
    --max-steps "$max_steps"
    --turn-budget "$turn_budget"
    --wait-timeout "$wait_timeout"
    --idle-threshold "$idle_threshold"
  )

  local implementation_json
  dx_dual_progress "implementation phase starting (assistant=$implement_assistant workspace=$workspace)"
  if ! implementation_json="$(run_self_json "${implement_args[@]}")"; then
    dx_dual_progress "implementation phase failed to execute"
    emit_error "workflow.dual" "command_error" "failed to run implementation phase" "${implement_args[*]}"
    return
  fi

  local impl_ok impl_status impl_summary impl_next impl_cmd
  local effective_implement_assistant
  effective_implement_assistant="$implement_assistant"
  impl_ok="$(jq -r '.ok // false' <<<"$implementation_json")"
  impl_status="$(jq -r '.overall_status // .status // "unknown"' <<<"$implementation_json")"
  impl_summary="$(jq -r '.summary // ""' <<<"$implementation_json")"
  impl_next="$(jq -r '.next_action // ""' <<<"$implementation_json")"
  impl_cmd="$(jq -r '.suggested_command // ""' <<<"$implementation_json")"
  dx_dual_progress "implementation phase finished (status=$impl_status)"

  if [[ "$implement_needs_input_retry" != "false" ]] \
    && [[ "$impl_status" == "needs_input" ]] \
    && [[ -n "${implement_needs_input_fallback_assistant// }" ]] \
    && [[ "$implement_needs_input_fallback_assistant" != "$effective_implement_assistant" ]]; then
    dx_dual_progress "implementation needs input; retrying with fallback assistant=$implement_needs_input_fallback_assistant"
    local impl_retry_args impl_retry_json
    impl_retry_args=(
      start
      --workspace "$workspace"
      --assistant "$implement_needs_input_fallback_assistant"
      --prompt "$implement_prompt"
      --max-steps "$max_steps"
      --turn-budget "$turn_budget"
      --wait-timeout "$wait_timeout"
      --idle-threshold "$idle_threshold"
    )
    if impl_retry_json="$(run_self_json "${impl_retry_args[@]}")"; then
      implementation_json="$impl_retry_json"
      impl_ok="$(jq -r '.ok // false' <<<"$implementation_json")"
      impl_status="$(jq -r '.overall_status // .status // "unknown"' <<<"$implementation_json")"
      impl_summary="$(jq -r '.summary // ""' <<<"$implementation_json")"
      impl_next="$(jq -r '.next_action // ""' <<<"$implementation_json")"
      impl_cmd="$(jq -r '.suggested_command // ""' <<<"$implementation_json")"
      effective_implement_assistant="$implement_needs_input_fallback_assistant"
      dx_dual_progress "fallback implementation finished (status=$impl_status assistant=$effective_implement_assistant)"
    fi
  fi

  if [[ "$auto_continue_impl" == "true" ]] \
    && [[ "$impl_ok" == "true" ]] \
    && [[ "$impl_status" == "needs_input" ]]; then
    local impl_agent_id
    impl_agent_id="$(jq -r '.agent_id // ""' <<<"$implementation_json")"
    if [[ -n "${impl_agent_id// }" ]] && [[ -x "$STEP_SCRIPT_PATH" ]]; then
      dx_dual_progress "implementation needs input; auto-continuing once"
      local impl_auto_json
      impl_auto_json="$("$STEP_SCRIPT_PATH" send \
        --agent "$impl_agent_id" \
        --text "$auto_continue_impl_prompt" \
        --enter \
        --wait-timeout "$wait_timeout" \
        --idle-threshold "$idle_threshold" 2>&1 || true)"
      if jq -e . >/dev/null 2>&1 <<<"$impl_auto_json"; then
        implementation_json="$impl_auto_json"
        impl_ok="$(jq -r '.ok // false' <<<"$implementation_json")"
        impl_status="$(jq -r '.overall_status // .status // "unknown"' <<<"$implementation_json")"
        impl_summary="$(jq -r '.summary // ""' <<<"$implementation_json")"
        impl_next="$(jq -r '.next_action // ""' <<<"$implementation_json")"
        impl_cmd="$(jq -r '.suggested_command // ""' <<<"$implementation_json")"
        dx_dual_progress "auto-continue implementation finished (status=$impl_status)"
      else
        dx_dual_progress "auto-continue implementation returned non-json output"
      fi
    fi
  fi

  local review_json='null'
  local review_skipped_reason=""
  local effective_review_assistant
  effective_review_assistant="$review_assistant"
  if [[ "$impl_ok" == "true" ]] && [[ "$impl_status" != "needs_input" ]] && [[ "$impl_status" != "session_exited" ]] && [[ "$impl_status" != "command_error" ]] && [[ "$impl_status" != "agent_error" ]]; then
    local review_args=(
      review
      --workspace "$workspace"
      --assistant "$review_assistant"
      --prompt "$review_prompt"
      --max-steps "$max_steps"
      --turn-budget "$turn_budget"
      --wait-timeout "$wait_timeout"
      --idle-threshold "$idle_threshold"
    )
    dx_dual_progress "review phase starting (assistant=$review_assistant workspace=$workspace)"
    if ! review_json="$(run_self_json "${review_args[@]}")"; then
      review_json='null'
      review_skipped_reason="review_phase_failed"
      dx_dual_progress "review phase failed to execute"
    fi
  else
    review_skipped_reason="implementation_not_ready"
    dx_dual_progress "review phase skipped (reason=$review_skipped_reason)"
  fi

  local review_ok="false"
  local review_status="skipped"
  local review_summary="Review phase was skipped."
  local review_next=""
  local review_cmd=""
  if [[ "$review_json" != "null" ]]; then
    review_ok="$(jq -r '.ok // false' <<<"$review_json")"
    review_status="$(jq -r '.overall_status // .status // "unknown"' <<<"$review_json")"
    review_summary="$(jq -r '.summary // ""' <<<"$review_json")"
    review_next="$(jq -r '.next_action // ""' <<<"$review_json")"
    review_cmd="$(jq -r '.suggested_command // ""' <<<"$review_json")"
    dx_dual_progress "review phase finished (status=$review_status)"

    if [[ "$review_needs_input_retry" != "false" ]] \
      && [[ "$review_status" == "needs_input" || "$review_status" == "timed_out" || "$review_status" == "partial" || "$review_status" == "partial_budget" ]] \
      && [[ -n "${review_needs_input_fallback_assistant// }" ]] \
      && [[ "$review_needs_input_fallback_assistant" != "$effective_review_assistant" ]]; then
      dx_dual_progress "review returned status=$review_status; retrying with fallback assistant=$review_needs_input_fallback_assistant"
      local review_retry_args review_retry_json
      review_retry_args=(
        review
        --workspace "$workspace"
        --assistant "$review_needs_input_fallback_assistant"
        --prompt "$review_prompt"
        --max-steps "$max_steps"
        --turn-budget "$turn_budget"
        --wait-timeout "$wait_timeout"
        --idle-threshold "$idle_threshold"
      )
      if review_retry_json="$(run_self_json "${review_retry_args[@]}")"; then
        review_json="$review_retry_json"
        review_ok="$(jq -r '.ok // false' <<<"$review_json")"
        review_status="$(jq -r '.overall_status // .status // "unknown"' <<<"$review_json")"
        review_summary="$(jq -r '.summary // ""' <<<"$review_json")"
        review_next="$(jq -r '.next_action // ""' <<<"$review_json")"
        review_cmd="$(jq -r '.suggested_command // ""' <<<"$review_json")"
        effective_review_assistant="$review_needs_input_fallback_assistant"
        dx_dual_progress "fallback review finished (status=$review_status assistant=$effective_review_assistant)"
      fi
    fi
  fi

  RESULT_OK=true
  RESULT_COMMAND="workflow.dual"
  RESULT_STATUS="ok"
  RESULT_SUMMARY="Dual-pass finished: implement=$impl_status review=$review_status"
  RESULT_NEXT_ACTION="Ship or continue implementation based on review findings."
  RESULT_SUGGESTED_COMMAND="skills/amux/scripts/openclaw-dx.sh git ship --workspace $(shell_quote "$workspace")"
  local codex_continue_cmd
  codex_continue_cmd="skills/amux/scripts/openclaw-dx.sh start --workspace $(shell_quote "$workspace") --assistant codex --prompt \"Continue from current state and provide concise status plus next action.\""
  local impl_needs_input_prefers_codex=false

  if [[ "$impl_ok" != "true" ]]; then
    RESULT_STATUS="attention"
    RESULT_SUMMARY="Implementation phase failed."
    RESULT_NEXT_ACTION="${impl_next:-Fix implementation blockers and rerun dual workflow.}"
    if [[ -n "$impl_cmd" ]]; then
      RESULT_SUGGESTED_COMMAND="$impl_cmd"
    else
      RESULT_SUGGESTED_COMMAND="skills/amux/scripts/openclaw-dx.sh start --workspace $(shell_quote "$workspace") --assistant $(shell_quote "$implement_assistant") --prompt $(shell_quote "$implement_prompt")"
    fi
  elif [[ "$impl_status" == "needs_input" ]]; then
    RESULT_STATUS="needs_input"
    RESULT_SUMMARY="Implementation needs input before review can run."
    RESULT_NEXT_ACTION="${impl_next:-Reply to implementation prompt first.}"
    if [[ "$implement_assistant" != "codex" ]] && { [[ -z "$impl_cmd" ]] || [[ "$impl_cmd" == *"openclaw-step.sh send --agent"* ]] || [[ "$impl_cmd" == *"openclaw-step.sh send --agent"* ]]; }; then
      impl_needs_input_prefers_codex=true
      RESULT_SUGGESTED_COMMAND="$codex_continue_cmd"
    elif [[ -n "$impl_cmd" ]]; then
      RESULT_SUGGESTED_COMMAND="$impl_cmd"
    else
      RESULT_SUGGESTED_COMMAND="$codex_continue_cmd"
    fi
  elif [[ "$impl_status" == "session_exited" || "$impl_status" == "command_error" || "$impl_status" == "agent_error" ]]; then
    RESULT_STATUS="attention"
    RESULT_SUMMARY="Implementation ended early with status: $impl_status"
    RESULT_NEXT_ACTION="${impl_next:-Restart implementation and continue.}"
    if [[ -n "$impl_cmd" ]]; then
      RESULT_SUGGESTED_COMMAND="$impl_cmd"
    fi
  elif [[ "$impl_status" == "timed_out" || "$impl_status" == "partial" || "$impl_status" == "partial_budget" ]]; then
    RESULT_STATUS="attention"
    RESULT_SUMMARY="Implementation returned partial progress (status: $impl_status)."
    RESULT_NEXT_ACTION="${impl_next:-Continue implementation to completion, then rerun review if needed.}"
    if [[ -n "$impl_cmd" ]]; then
      RESULT_SUGGESTED_COMMAND="$impl_cmd"
    fi
  elif [[ "$review_json" == "null" ]]; then
    RESULT_STATUS="attention"
    RESULT_SUMMARY="Implementation finished, but review phase did not run."
    RESULT_NEXT_ACTION="Run review to validate uncommitted changes."
    RESULT_SUGGESTED_COMMAND="skills/amux/scripts/openclaw-dx.sh review --workspace $(shell_quote "$workspace") --assistant $(shell_quote "$effective_review_assistant")"
  elif [[ "$review_ok" != "true" ]]; then
    RESULT_STATUS="attention"
    RESULT_SUMMARY="Review phase failed."
    RESULT_NEXT_ACTION="${review_next:-Rerun review and inspect failures.}"
    if [[ -n "$review_cmd" ]]; then
      RESULT_SUGGESTED_COMMAND="$review_cmd"
    fi
  elif [[ "$review_status" == "needs_input" ]]; then
    RESULT_STATUS="needs_input"
    RESULT_SUMMARY="Review needs input."
    RESULT_NEXT_ACTION="${review_next:-Reply to review prompt first.}"
    if [[ -n "$review_cmd" ]]; then
      RESULT_SUGGESTED_COMMAND="$review_cmd"
    fi
  elif [[ "$review_status" == "session_exited" || "$review_status" == "command_error" || "$review_status" == "agent_error" ]]; then
    RESULT_STATUS="attention"
    RESULT_SUMMARY="Review ended early with status: $review_status"
    RESULT_NEXT_ACTION="${review_next:-Rerun review or continue implementation.}"
    if [[ -n "$review_cmd" ]]; then
      RESULT_SUGGESTED_COMMAND="$review_cmd"
    fi
  elif [[ "$review_status" == "timed_out" || "$review_status" == "partial" || "$review_status" == "partial_budget" ]]; then
    RESULT_STATUS="attention"
    RESULT_SUMMARY="Review returned partial progress."
    RESULT_NEXT_ACTION="${review_next:-Continue review for a full pass.}"
    if [[ -n "$review_cmd" ]]; then
      RESULT_SUGGESTED_COMMAND="$review_cmd"
    fi
  fi

  local actions='[]'
  if [[ "$impl_status" == "needs_input" && "$impl_needs_input_prefers_codex" == "true" ]]; then
    actions="$(append_action "$actions" "switch_codex" "Switch Codex" "$codex_continue_cmd" "danger" "Switch to a non-interactive implementation assistant")"
  elif [[ "$impl_status" == "needs_input" && -n "$impl_cmd" ]]; then
    actions="$(append_action "$actions" "continue_impl" "Continue Impl" "$impl_cmd" "danger" "Reply to implementation prompt")"
  elif [[ "$impl_status" == "needs_input" ]]; then
    actions="$(append_action "$actions" "switch_codex" "Switch Codex" "$codex_continue_cmd" "danger" "Switch to a non-interactive implementation assistant")"
  fi
  if [[ "$review_json" == "null" && "$review_skipped_reason" != "implementation_not_ready" ]]; then
    actions="$(append_action "$actions" "run_review" "Run Review" "skills/amux/scripts/openclaw-dx.sh review --workspace $(shell_quote "$workspace") --assistant $(shell_quote "$effective_review_assistant")" "primary" "Run review phase now")"
  elif [[ "$review_status" == "needs_input" && -n "$review_cmd" ]]; then
    actions="$(append_action "$actions" "continue_review" "Continue Review" "$review_cmd" "danger" "Reply to review prompt")"
  elif [[ ("$review_status" == "timed_out" || "$review_status" == "partial" || "$review_status" == "partial_budget") && -n "$review_cmd" ]]; then
    actions="$(append_action "$actions" "finish_review" "Finish Review" "$review_cmd" "primary" "Continue review to completion")"
  fi
  actions="$(append_action "$actions" "status" "Status" "skills/amux/scripts/openclaw-dx.sh status --workspace $(shell_quote "$workspace")" "primary" "Check workspace status")"
  actions="$(append_action "$actions" "alerts" "Alerts" "skills/amux/scripts/openclaw-dx.sh alerts --workspace $(shell_quote "$workspace")" "primary" "Check blocking alerts")"
  if [[ "$RESULT_STATUS" == "ok" ]]; then
    actions="$(append_action "$actions" "ship" "Ship" "skills/amux/scripts/openclaw-dx.sh git ship --workspace $(shell_quote "$workspace")" "success" "Commit current changes")"
  fi
  RESULT_QUICK_ACTIONS="$actions"

  local implementation_compact review_compact
  implementation_compact="$(jq -c '{
      ok, command, workflow, status, overall_status, summary, next_action, suggested_command,
      agent_id, workspace_id, assistant, steps_used, max_steps, elapsed_seconds, progress_percent,
      quick_actions
    }' <<<"$(normalize_json_or_default "$implementation_json" '{}')" 2>/dev/null || printf '{}')"
  review_compact="$(jq -c '
      if . == null then
        null
      else
        {
          ok, command, workflow, status, overall_status, summary, next_action, suggested_command,
          agent_id, workspace_id, assistant, steps_used, max_steps, elapsed_seconds, progress_percent,
          quick_actions
        }
      end
    ' <<<"$(normalize_json_or_default "$review_json" 'null')" 2>/dev/null || printf 'null')"

  RESULT_DATA="$(jq -cn \
    --arg workspace "$workspace" \
    --arg implement_assistant "$effective_implement_assistant" \
    --arg review_assistant "$effective_review_assistant" \
    --arg review_skipped_reason "$review_skipped_reason" \
    --argjson implementation "$implementation_compact" \
    --argjson review "$review_compact" \
    '{
      workspace: $workspace,
      implement_assistant: $implement_assistant,
      review_assistant: $review_assistant,
      review_skipped_reason: $review_skipped_reason,
      implementation: $implementation,
      review: $review
    }')"

  RESULT_MESSAGE="✅ Dual-pass workflow completed"$'\n'"Workspace: $workspace"
  RESULT_MESSAGE+=$'\n'"Implement ($effective_implement_assistant): $impl_status"
  if [[ -n "${impl_summary// }" ]]; then
    RESULT_MESSAGE+=$'\n'"  $impl_summary"
  fi
  if [[ "$review_json" == "null" ]]; then
    RESULT_MESSAGE+=$'\n'"Review ($effective_review_assistant): skipped"
  else
    RESULT_MESSAGE+=$'\n'"Review ($effective_review_assistant): $review_status"
    if [[ -n "${review_summary// }" ]]; then
      RESULT_MESSAGE+=$'\n'"  $review_summary"
    fi
  fi
  RESULT_MESSAGE+=$'\n'"Next: $RESULT_NEXT_ACTION"

  emit_result
}

cmd_workflow() {
  if [[ $# -lt 1 ]]; then
    emit_error "workflow" "command_error" "missing workflow subcommand"
    return
  fi

  local sub="$1"
  shift
  case "$sub" in
    kickoff)
      cmd_workflow_kickoff "$@"
      ;;
    dual)
      cmd_workflow_dual "$@"
      ;;
    *)
      emit_error "workflow" "command_error" "unknown workflow subcommand" "$sub"
      ;;
  esac
}

cmd_assistants() {
  local config_path
  config_path="${AMUX_HOME:-$HOME/.amux}/config.json"
  local workspace=""
  local probe=false
  local limit="${OPENCLAW_DX_ASSISTANTS_LIMIT:-6}"
  local probe_prompt="${OPENCLAW_DX_ASSISTANTS_PROBE_PROMPT:-Reply in one line with READY and the top current objective for this workspace.}"
  local max_steps="${OPENCLAW_DX_MAX_STEPS:-2}"
  local turn_budget="${OPENCLAW_DX_TURN_BUDGET:-150}"
  local wait_timeout="${OPENCLAW_DX_WAIT_TIMEOUT:-45s}"
  local idle_threshold="${OPENCLAW_DX_IDLE_THRESHOLD:-8s}"

  while [[ $# -gt 0 ]]; do
    case "$1" in
      --workspace)
        workspace="$2"; shift 2 ;;
      --probe)
        probe=true; shift ;;
      --limit)
        limit="$2"; shift 2 ;;
      --prompt)
        shift
        if [[ $# -eq 0 ]]; then
          emit_error "assistants" "command_error" "missing value for --prompt"
          return
        fi
        probe_prompt="$1"; shift
        while [[ $# -gt 0 && "$1" != --* ]]; do
          probe_prompt+=" $1"
          shift
        done
        ;;
      --max-steps)
        max_steps="$2"; shift 2 ;;
      --turn-budget)
        turn_budget="$2"; shift 2 ;;
      --wait-timeout)
        wait_timeout="$2"; shift 2 ;;
      --idle-threshold)
        idle_threshold="$2"; shift 2 ;;
      *)
        emit_error "assistants" "command_error" "unknown flag" "$1"
        return
        ;;
    esac
  done

  if ! is_positive_int "$limit"; then
    limit=6
  fi
  workspace="$(context_resolve_workspace "$workspace")"
  if [[ "$probe" == "true" && -z "$workspace" ]]; then
    emit_error "assistants" "command_error" "--probe requires --workspace (or active context workspace)"
    return
  fi
  if [[ "$probe" == "true" ]]; then
    if ! workspace_require_exists "assistants" "$workspace"; then
      return
    fi
  fi

  local assistant_cmds
  assistant_cmds='{"claude":"claude","codex":"codex","gemini":"gemini","amp":"amp","opencode":"opencode","droid":"droid","cline":"cline","cursor":"agent","pi":"pi"}'

  if [[ -f "$config_path" ]] && jq -e . >/dev/null 2>&1 <"$config_path"; then
    while IFS=$'\t' read -r id cmd; do
      [[ -z "$id" ]] && continue
      if [[ -n "${cmd// }" ]]; then
        assistant_cmds="$(jq -cn --argjson cmds "$assistant_cmds" --arg id "$id" --arg command "$cmd" '$cmds + {($id): $command}')"
      fi
    done < <(jq -r '.assistants // {} | to_entries[] | "\(.key)\t\(.value.command // "")"' "$config_path")
  fi

  local names
  names="$(jq -r 'keys[]' <<<"$assistant_cmds" | sort)"

  local assistants='[]'
  local ready_count=0
  local missing_count=0

  while IFS= read -r name; do
    [[ -z "$name" ]] && continue
    local cmd bin_path binary status
    cmd="$(jq -r --arg name "$name" '.[$name] // ""' <<<"$assistant_cmds")"
    binary="$(printf '%s\n' "$cmd" | awk '{print $1}')"
    bin_path=""
    status="missing"
    if [[ -n "$binary" ]]; then
      bin_path="$(command -v "$binary" 2>/dev/null || true)"
      if [[ -n "$bin_path" ]]; then
        status="ready"
      fi
    fi

    if [[ "$status" == "ready" ]]; then
      ready_count=$((ready_count + 1))
    else
      missing_count=$((missing_count + 1))
    fi

    assistants="$(jq -cn --argjson list "$assistants" --arg name "$name" --arg command "$cmd" --arg binary "$binary" --arg path "$bin_path" --arg status "$status" '$list + [{name: $name, command: $command, binary: $binary, path: $path, status: $status}]')"
  done <<<"$names"

  local probe_results='[]'
  local probe_passed=0
  local probe_needs_input=0
  local probe_failed=0
  local probe_count=0

  if [[ "$probe" == "true" ]]; then
    if [[ ! -x "$TURN_SCRIPT" ]]; then
      emit_error "assistants" "command_error" "turn script is not executable" "$TURN_SCRIPT"
      return
    fi
    while IFS= read -r ready_name; do
      [[ -z "$ready_name" ]] && continue
      if [[ "$probe_count" -ge "$limit" ]]; then
        break
      fi

      local turn_json turn_ok turn_status turn_overall turn_summary normalized_result
      turn_json="$(OPENCLAW_TURN_SKIP_PRESENT=true "$TURN_SCRIPT" run \
        --workspace "$workspace" \
        --assistant "$ready_name" \
        --prompt "$probe_prompt" \
        --max-steps "$max_steps" \
        --turn-budget "$turn_budget" \
        --wait-timeout "$wait_timeout" \
        --idle-threshold "$idle_threshold" 2>&1 || true)"

      turn_ok="false"
      turn_status="command_error"
      turn_overall="command_error"
      turn_summary="assistant probe failed"

      if jq -e . >/dev/null 2>&1 <<<"$turn_json"; then
        turn_ok="$(jq -r '.ok // false' <<<"$turn_json")"
        turn_status="$(jq -r '.status // "unknown"' <<<"$turn_json")"
        turn_overall="$(jq -r '.overall_status // .status // "unknown"' <<<"$turn_json")"
        turn_summary="$(jq -r '.summary // ""' <<<"$turn_json")"
      else
        turn_summary="$turn_json"
      fi

      normalized_result="failed"
      if [[ "$turn_ok" == "true" && ( "$turn_overall" == "completed" || "$turn_status" == "idle" ) ]]; then
        normalized_result="passed"
        probe_passed=$((probe_passed + 1))
      elif [[ "$turn_overall" == "needs_input" || "$turn_status" == "needs_input" ]]; then
        normalized_result="needs_input"
        probe_needs_input=$((probe_needs_input + 1))
      else
        probe_failed=$((probe_failed + 1))
      fi

      probe_results="$(jq -cn --argjson probes "$probe_results" --arg assistant "$ready_name" --arg result "$normalized_result" --arg status "$turn_status" --arg overall_status "$turn_overall" --arg summary "$turn_summary" '$probes + [{assistant: $assistant, result: $result, status: $status, overall_status: $overall_status, summary: $summary}]')"
      probe_count=$((probe_count + 1))
    done < <(jq -r '.[] | select(.status == "ready") | .name' <<<"$assistants")
  fi

  local overall_status="ok"
  if [[ "$missing_count" -gt 0 ]]; then
    overall_status="attention"
  fi
  if [[ "$probe_failed" -gt 0 ]]; then
    overall_status="attention"
  fi
  if [[ "$probe_needs_input" -gt 0 ]]; then
    overall_status="needs_input"
  fi

  local lines
  lines="$(jq -r '. | map((if .status == "ready" then "- ✅ " else "- ⚠️ " end) + .name + " → " + .command) | join("\n")' <<<"$assistants")"
  local probe_lines
  probe_lines="$(jq -r '. | map("- " + (if .result == "passed" then "✅ " elif .result == "needs_input" then "❓ " else "⚠️ " end) + .assistant + ": " + (.summary // .overall_status // .status)) | join("\n")' <<<"$probe_results")"

  local first_ready claude_ready codex_ready first_probe_passed claude_probe_passed codex_probe_passed
  first_ready="$(jq -r '.[] | select(.status == "ready") | .name' <<<"$assistants" | head -n 1)"
  claude_ready="$(jq -r '[.[] | select(.name == "claude" and .status == "ready")] | length' <<<"$assistants")"
  codex_ready="$(jq -r '[.[] | select(.name == "codex" and .status == "ready")] | length' <<<"$assistants")"
  first_probe_passed="$(jq -r '.[] | select(.result == "passed") | .assistant' <<<"$probe_results" | head -n 1)"
  claude_probe_passed="$(jq -r '[.[] | select(.assistant == "claude" and .result == "passed")] | length' <<<"$probe_results")"
  codex_probe_passed="$(jq -r '[.[] | select(.assistant == "codex" and .result == "passed")] | length' <<<"$probe_results")"

  RESULT_OK=true
  RESULT_COMMAND="assistants"
  RESULT_STATUS="$overall_status"
  RESULT_SUMMARY="$ready_count ready, $missing_count missing"
  if [[ "$probe" == "true" ]]; then
    RESULT_SUMMARY+=", probe: $probe_passed passed, $probe_needs_input needs input, $probe_failed failed"
  fi
  RESULT_NEXT_ACTION="Use ready assistants for implementation/review handoffs."
  if [[ "$missing_count" -gt 0 ]]; then
    RESULT_NEXT_ACTION="Install or remap missing assistant binaries in ~/.amux/config.json."
  fi
  if [[ "$probe_needs_input" -gt 0 ]]; then
    RESULT_NEXT_ACTION="Some assistants need interactive permission input. Use codex for non-interactive mobile flows."
  elif [[ "$probe_failed" -gt 0 ]]; then
    RESULT_NEXT_ACTION="Investigate failing assistant probes before relying on those assistants."
  fi

  local dual_ready=false
  if [[ "$probe" == "true" ]]; then
    if [[ "$claude_probe_passed" -gt 0 && "$codex_probe_passed" -gt 0 ]]; then
      dual_ready=true
    fi
  elif [[ "$claude_ready" -gt 0 && "$codex_ready" -gt 0 ]]; then
    dual_ready=true
  fi

  local preferred_assistant=""
  if [[ "$probe" == "true" ]]; then
    if [[ "$codex_probe_passed" -gt 0 ]]; then
      preferred_assistant="codex"
    elif [[ -n "$first_probe_passed" ]]; then
      preferred_assistant="$first_probe_passed"
    fi
  elif [[ -n "$first_ready" ]]; then
    preferred_assistant="$first_ready"
  fi

  RESULT_SUGGESTED_COMMAND="skills/amux/scripts/openclaw-dx.sh status"
  if [[ -n "$workspace" && "$dual_ready" == "true" ]]; then
    RESULT_SUGGESTED_COMMAND="skills/amux/scripts/openclaw-dx.sh workflow dual --workspace $(shell_quote "$workspace") --implement-assistant claude --review-assistant codex"
  elif [[ -n "$workspace" && -n "$preferred_assistant" ]]; then
    RESULT_SUGGESTED_COMMAND="skills/amux/scripts/openclaw-dx.sh start --workspace $(shell_quote "$workspace") --assistant $(shell_quote "$preferred_assistant") --prompt \"Summarize current status and next action in one line.\""
  elif [[ -n "$workspace" && -n "$first_ready" ]]; then
    RESULT_SUGGESTED_COMMAND="skills/amux/scripts/openclaw-dx.sh start --workspace $(shell_quote "$workspace") --assistant $(shell_quote "$first_ready") --prompt \"Summarize current status and next action in one line.\""
  fi

  local actions='[]'
  actions="$(append_action "$actions" "status" "Status" "skills/amux/scripts/openclaw-dx.sh status" "primary" "Show current work/agent status")"
  actions="$(append_action "$actions" "review" "Review" "skills/amux/scripts/openclaw-dx.sh review --workspace <workspace_id> --assistant codex" "primary" "Run a review workflow")"
  if [[ "$probe" != "true" && -n "$workspace" ]]; then
    actions="$(append_action "$actions" "probe" "Probe" "skills/amux/scripts/openclaw-dx.sh assistants --workspace $(shell_quote "$workspace") --probe --limit $(shell_quote "$limit")" "primary" "Run readiness probes for ready assistants")"
  fi
  if [[ -n "$workspace" && "$dual_ready" == "true" ]]; then
    actions="$(append_action "$actions" "dual" "Dual Pass" "skills/amux/scripts/openclaw-dx.sh workflow dual --workspace $(shell_quote "$workspace") --implement-assistant claude --review-assistant codex" "success" "Implement with claude and review with codex")"
  elif [[ -n "$workspace" && -n "$preferred_assistant" ]]; then
    actions="$(append_action "$actions" "start_ready" "Start Ready" "skills/amux/scripts/openclaw-dx.sh start --workspace $(shell_quote "$workspace") --assistant $(shell_quote "$preferred_assistant") --prompt \"Summarize current status and next action in one line.\"" "primary" "Start with best probe-passed assistant")"
  elif [[ -n "$workspace" && -n "$first_ready" ]]; then
    actions="$(append_action "$actions" "start_ready" "Start Ready" "skills/amux/scripts/openclaw-dx.sh start --workspace $(shell_quote "$workspace") --assistant $(shell_quote "$first_ready") --prompt \"Summarize current status and next action in one line.\"" "primary" "Start with first ready assistant")"
  fi
  RESULT_QUICK_ACTIONS="$actions"

  RESULT_DATA="$(jq -cn \
    --arg config_path "$config_path" \
    --arg workspace "$workspace" \
    --argjson probe "$probe" \
    --argjson limit "$limit" \
    --argjson ready_count "$ready_count" \
    --argjson missing_count "$missing_count" \
    --argjson probe_count "$probe_count" \
    --argjson probe_passed "$probe_passed" \
    --argjson probe_needs_input "$probe_needs_input" \
    --argjson probe_failed "$probe_failed" \
    --argjson assistants "$assistants" \
    --argjson probes "$probe_results" \
    '{
      config_path: $config_path,
      workspace: $workspace,
      probe: $probe,
      limit: $limit,
      ready_count: $ready_count,
      missing_count: $missing_count,
      probe_count: $probe_count,
      probe_passed: $probe_passed,
      probe_needs_input: $probe_needs_input,
      probe_failed: $probe_failed,
      assistants: $assistants,
      probes: $probes
    }')"

  RESULT_MESSAGE="$(if [[ "$overall_status" == "ok" ]]; then printf '✅'; else printf '⚠️'; fi) Assistant readiness: $ready_count ready, $missing_count missing"
  if [[ "$probe" == "true" ]]; then
    RESULT_MESSAGE+=$'\n'"Probe: passed=$probe_passed needs_input=$probe_needs_input failed=$probe_failed"
  fi
  if [[ -n "${lines// }" ]]; then
    RESULT_MESSAGE+=$'\n'"$lines"
  fi
  if [[ "$probe" == "true" ]] && [[ -n "${probe_lines// }" ]]; then
    RESULT_MESSAGE+=$'\n'"Probes:"$'\n'"$probe_lines"
  fi
  RESULT_MESSAGE+=$'\n'"Next: $RESULT_NEXT_ACTION"
  emit_result
}

require_prereqs() {
  if ! command -v jq >/dev/null 2>&1; then
    printf '{"ok":false,"command":"unknown","status":"command_error","summary":"jq is required","error":"missing binary: jq"}\n'
    exit 0
  fi
  if ! command -v amux >/dev/null 2>&1; then
    printf '{"ok":false,"command":"unknown","status":"command_error","summary":"amux is required","error":"missing binary: amux"}\n'
    exit 0
  fi
}

if [[ $# -lt 1 ]]; then
  usage
  emit_error "help" "command_error" "missing command"
  exit 0
fi

require_prereqs

SCRIPT_SOURCE="${BASH_SOURCE[0]:-$0}"
SCRIPT_DIR="$(cd "$(dirname "$SCRIPT_SOURCE")" >/dev/null 2>&1 && pwd -P)"
SCRIPT_PATH="$SCRIPT_DIR/$(basename "$SCRIPT_SOURCE")"

TURN_SCRIPT="${OPENCLAW_DX_TURN_SCRIPT:-$SCRIPT_DIR/openclaw-turn.sh}"
if [[ ! -x "$TURN_SCRIPT" ]]; then
  TURN_SCRIPT="$SCRIPT_DIR/openclaw-turn.sh"
fi
SELF_SCRIPT="${OPENCLAW_DX_SELF_SCRIPT:-$SCRIPT_DIR/openclaw-dx.sh}"
if [[ ! -x "$SELF_SCRIPT" ]]; then
  SELF_SCRIPT="$SCRIPT_PATH"
fi
STEP_SCRIPT_PATH="${OPENCLAW_DX_STEP_SCRIPT:-$SCRIPT_DIR/openclaw-step.sh}"
if [[ ! -x "$STEP_SCRIPT_PATH" ]]; then
  STEP_SCRIPT_PATH="$SCRIPT_DIR/openclaw-step.sh"
fi
OPENCLAW_PRESENT_SCRIPT="${OPENCLAW_PRESENT_SCRIPT:-$SCRIPT_DIR/openclaw-present.sh}"

DX_CMD_REF="${OPENCLAW_DX_CMD_REF:-skills/amux/scripts/openclaw-dx.sh}"
TURN_CMD_REF="${OPENCLAW_DX_TURN_CMD_REF:-skills/amux/scripts/openclaw-turn.sh}"
STEP_CMD_REF="${OPENCLAW_DX_STEP_CMD_REF:-skills/amux/scripts/openclaw-step.sh}"

top_cmd="$1"
shift

case "$top_cmd" in
  project)
    if [[ $# -lt 1 ]]; then
      emit_error "project" "command_error" "missing project subcommand"
      exit 0
    fi
    sub="$1"
    shift
    case "$sub" in
      add)
        cmd_project_add "$@"
        ;;
      list|ls)
        cmd_project_list "$@"
        ;;
      pick)
        cmd_project_pick "$@"
        ;;
      *)
        emit_error "project" "command_error" "unknown project subcommand" "$sub"
        ;;
    esac
    ;;
  workspace)
    if [[ $# -lt 1 ]]; then
      emit_error "workspace" "command_error" "missing workspace subcommand"
      exit 0
    fi
    sub="$1"
    shift
    case "$sub" in
      create)
        cmd_workspace_create "$@"
        ;;
      list|ls)
        cmd_workspace_list "$@"
        ;;
      decide)
        cmd_workspace_decide "$@"
        ;;
      *)
        emit_error "workspace" "command_error" "unknown workspace subcommand" "$sub"
        ;;
    esac
    ;;
  start)
    cmd_start "$@"
    ;;
  continue)
    cmd_continue "$@"
    ;;
  status)
    cmd_status "$@"
    ;;
  alerts)
    cmd_alerts "$@"
    ;;
  terminal)
    if [[ $# -lt 1 ]]; then
      emit_error "terminal" "command_error" "missing terminal subcommand"
      exit 0
    fi
    sub="$1"
    shift
    case "$sub" in
      run)
        cmd_terminal_run "$@"
        ;;
      preset)
        cmd_terminal_preset "$@"
        ;;
      logs)
        cmd_terminal_logs "$@"
        ;;
      *)
        emit_error "terminal" "command_error" "unknown terminal subcommand" "$sub"
        ;;
    esac
    ;;
  cleanup)
    cmd_cleanup "$@"
    ;;
  review)
    cmd_review "$@"
    ;;
  guide)
    cmd_guide "$@"
    ;;
  git)
    if [[ $# -lt 1 ]]; then
      emit_error "git" "command_error" "missing git subcommand"
      exit 0
    fi
    sub="$1"
    shift
    case "$sub" in
      ship)
        cmd_git_ship "$@"
        ;;
      *)
        emit_error "git" "command_error" "unknown git subcommand" "$sub"
        ;;
    esac
    ;;
  workflow)
    cmd_workflow "$@"
    ;;
  assistants)
    cmd_assistants "$@"
    ;;
  help|-h|--help)
    usage
    RESULT_OK=true
    RESULT_COMMAND="help"
    RESULT_STATUS="ok"
    RESULT_SUMMARY="openclaw-dx help"
    RESULT_MESSAGE="ℹ️ openclaw-dx help printed to stderr"
    RESULT_DATA='{}'
    RESULT_QUICK_ACTIONS='[]'
    emit_result
    ;;
  *)
    emit_error "unknown" "command_error" "unknown command" "$top_cmd"
    ;;
esac

exit 0
