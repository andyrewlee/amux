#!/usr/bin/env bash
# openclaw-dx.sh — OpenClaw-first control plane for amux coding workflows.
#
# Covers project/workspace/agent/terminal/session/git/review flows in one UX layer.
# Workspace scope terms used throughout this script:
# - project workspace: created directly from a project
# - nested workspace: created from a parent workspace; still starts from the project's default branch

set -euo pipefail

usage() {
  cat >&2 <<'USAGE'
Usage:
  openclaw-dx.sh project add [--path <repo> | --cwd] [--workspace <name>] [--assistant <name>] [--base <branch>]
  openclaw-dx.sh project list [--limit <n>] [--page <n>] [--query <text>]
  openclaw-dx.sh project pick [--index <n> | --name <query> | --path <repo>] [--workspace <name>] [--assistant <name>] [--base <branch>]

  openclaw-dx.sh workspace create --name <name> [--project <repo>] [--from-workspace <id>] [--scope project|nested] [--assistant <name>] [--base <branch>]
  openclaw-dx.sh workspace list [--project <repo> | --all] [--workspace <id>] [--limit <n>] [--page <n>]
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

Notes:
  - Scope terms: "project workspace" and "nested workspace".
  - --base is supported for project scope only; nested scope always starts from the project's default branch.
USAGE
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
_openclaw_dx_cleanup() {
  if [[ -n "${AMUX_ERROR_CAPTURE_FILE:-}" && -f "$AMUX_ERROR_CAPTURE_FILE" ]]; then
    rm -f "$AMUX_ERROR_CAPTURE_FILE" 2>/dev/null || true
  fi
}
trap _openclaw_dx_cleanup EXIT
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
  context_json="$(context_payload_json)"

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

  if [[ "${OPENCLAW_DX_SKIP_PRESENT:-false}" != "true" && -x "$OPENCLAW_PRESENT_SCRIPT" ]]; then
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
  local workspace_hint
  local actions
  RESULT_OK=false
  RESULT_COMMAND="$command_name"
  RESULT_STATUS="$status"
  RESULT_SUMMARY="$summary"
  RESULT_MESSAGE="🛑 $summary"
  if [[ -n "${detail// }" ]]; then
    RESULT_MESSAGE+=$'\n'"$detail"
  fi
  RESULT_NEXT_ACTION="Fix the error and retry this command. Use guide if you need command examples."
  workspace_hint="$(context_workspace_id)"
  RESULT_SUGGESTED_COMMAND="skills/amux/scripts/openclaw-dx.sh guide"
  RESULT_MESSAGE+=$'\n'"Next: $RESULT_NEXT_ACTION"
  RESULT_DATA="$(jq -cn --arg detail "$detail" '{error: $detail}')"
  actions='[]'
  actions="$(append_action "$actions" "guide" "Guide" "skills/amux/scripts/openclaw-dx.sh guide" "primary" "Show command guidance")"
  if [[ -n "${workspace_hint// }" ]]; then
    actions="$(append_action "$actions" "status_ws" "WS Status" "skills/amux/scripts/openclaw-dx.sh status --workspace $(shell_quote "$workspace_hint")" "primary" "Check active workspace status")"
  else
    actions="$(append_action "$actions" "status" "Status" "skills/amux/scripts/openclaw-dx.sh status" "primary" "Show global coding status")"
  fi
  RESULT_QUICK_ACTIONS="$actions"
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
  local workspace_hint fallback_suggested next_action actions
  local details_workspace=""
  local terminal_bootstrap_cmd=""
  local terminal_session_bootstrap=false
  if [[ "$err_code" == *"agent"* ]]; then
    status="agent_error"
  fi
  workspace_hint="$(context_workspace_id)"
  if [[ -z "${workspace_hint// }" ]] && jq -e . >/dev/null 2>&1 <<<"$err_details"; then
    details_workspace="$(jq -r '.workspace_id // .workspace // ""' <<<"$err_details")"
    if [[ -n "${details_workspace// }" ]]; then
      workspace_hint="$details_workspace"
    fi
  fi
  fallback_suggested="skills/amux/scripts/openclaw-dx.sh guide"
  if [[ -n "${workspace_hint// }" ]]; then
    fallback_suggested="skills/amux/scripts/openclaw-dx.sh status --workspace $(shell_quote "$workspace_hint")"
  fi
  if [[ "$command_name" == terminal.* && -n "${workspace_hint// }" ]]; then
    terminal_bootstrap_cmd="skills/amux/scripts/openclaw-dx.sh terminal run --workspace $(shell_quote "$workspace_hint") --text \"pwd\" --enter"
    fallback_suggested="skills/amux/scripts/openclaw-dx.sh terminal logs --workspace $(shell_quote "$workspace_hint") --lines 120"
  fi
  next_action="Fix the failing amux command input and retry."
  if [[ "$command_name" == terminal.* ]]; then
    next_action="Terminal command failed at the amux layer. Retry, then check terminal logs/status."
  fi
  if [[ "$command_name" == terminal.* && ("$err_code" == "session_create_failed" || "$err_code" == "session_attach_failed" || "$err_code" == "session_not_found") ]]; then
    terminal_session_bootstrap=true
    if [[ -n "${terminal_bootstrap_cmd// }" ]]; then
      fallback_suggested="$terminal_bootstrap_cmd"
    fi
    next_action="Terminal session could not be created. Start a simple terminal session, then retry the original command."
    if [[ "$err_msg" == "exit status 1" || "$err_msg" == "amux command failed" ]]; then
      local workspace_label_hint
      workspace_label_hint="$workspace_hint"
      if [[ -n "${workspace_hint// }" ]]; then
        workspace_label_hint="$(workspace_label_for_id "$workspace_hint")"
      fi
      err_msg="Terminal session start failed for workspace $workspace_label_hint"
    fi
  fi
  if [[ "$err_msg" == *"server exited unexpectedly"* ]]; then
    next_action="Terminal session backend exited unexpectedly. Retry terminal start, then inspect workspace status/logs."
  fi
  RESULT_OK=false
  RESULT_COMMAND="$command_name"
  RESULT_STATUS="$status"
  RESULT_SUMMARY="$err_msg"
  RESULT_MESSAGE="🛑 $err_msg"
  RESULT_NEXT_ACTION="$next_action"
  RESULT_SUGGESTED_COMMAND="$fallback_suggested"
  RESULT_MESSAGE+=$'\n'"Next: $RESULT_NEXT_ACTION"
  RESULT_DATA="$(jq -cn --arg code "$err_code" --arg message "$err_msg" --argjson details "$err_details" '{error: {code: $code, message: $message, details: $details}}')"
  actions='[]'
  actions="$(append_action "$actions" "guide" "Guide" "skills/amux/scripts/openclaw-dx.sh guide" "primary" "Show command guidance")"
  if [[ -n "${workspace_hint// }" ]]; then
    actions="$(append_action "$actions" "status_ws" "WS Status" "skills/amux/scripts/openclaw-dx.sh status --workspace $(shell_quote "$workspace_hint")" "primary" "Check active workspace status")"
    if [[ "$command_name" == terminal.* ]]; then
      if [[ "$terminal_session_bootstrap" == "true" && -n "${terminal_bootstrap_cmd// }" ]]; then
        actions="$(append_action "$actions" "terminal_init_ws" "Init Terminal" "$terminal_bootstrap_cmd" "primary" "Start a minimal terminal session first")"
        actions="$(append_action "$actions" "logs_ws" "WS Logs" "skills/amux/scripts/openclaw-dx.sh terminal logs --workspace $(shell_quote "$workspace_hint") --lines 120" "secondary" "Inspect terminal logs for this workspace")"
      else
        actions="$(append_action "$actions" "logs_ws" "WS Logs" "skills/amux/scripts/openclaw-dx.sh terminal logs --workspace $(shell_quote "$workspace_hint") --lines 120" "primary" "Inspect terminal logs for this workspace")"
      fi
    fi
  else
    actions="$(append_action "$actions" "status" "Status" "skills/amux/scripts/openclaw-dx.sh status" "primary" "Show global coding status")"
  fi
  RESULT_QUICK_ACTIONS="$actions"
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
  if [[ "$normalized" =~ ^[a-z0-9][a-z0-9._-]*$ ]]; then
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

normalize_path_for_compare() {
  local path="$1"
  if [[ -z "$path" ]]; then
    printf ''
    return 0
  fi

  path="$(canonicalize_path "$path")"
  if [[ "$path" != /* ]]; then
    path="$(canonicalize_path "$(pwd -P)/$path")"
  fi
  while [[ "$path" != "/" && "$path" == */ ]]; do
    path="${path%/}"
  done
  printf '%s' "$path"
}

project_pick_disambiguate_matches() {
  local matches_json="$1"
  local context_repo
  context_repo="$(normalize_path_for_compare "$(context_project_path)")"

  local row row_path row_repo
  local context_match=""
  local context_match_count=0

  if [[ -n "${context_repo// }" ]]; then
    while IFS= read -r row; do
      [[ -z "${row// }" ]] && continue
      row_path="$(jq -r '.path // ""' <<<"$row")"
      row_repo="$(normalize_path_for_compare "$row_path")"
      if [[ -n "${row_repo// }" && "$row_repo" == "$context_repo" ]]; then
        context_match="$row"
        context_match_count=$((context_match_count + 1))
      fi
    done < <(jq -c '.[]' <<<"$matches_json")
  fi

  if [[ "$context_match_count" -gt 0 && -n "${context_match// }" ]]; then
    printf '%s' "$context_match"
    return 0
  fi

  local seen_repos=""
  local unique_repo_count=0
  local first_row=""
  while IFS= read -r row; do
    [[ -z "${row// }" ]] && continue
    [[ -z "$first_row" ]] && first_row="$row"
    row_path="$(jq -r '.path // ""' <<<"$row")"
    row_repo="$(normalize_path_for_compare "$row_path")"
    if [[ -z "${row_repo// }" ]]; then
      continue
    fi
    if ! printf '%s\n' "$seen_repos" | grep -Fqx -- "$row_repo"; then
      seen_repos+=$row_repo$'\n'
      unique_repo_count=$((unique_repo_count + 1))
    fi
  done < <(jq -c '.[]' <<<"$matches_json")

  if [[ "$unique_repo_count" -eq 1 && -n "${first_row// }" ]]; then
    printf '%s' "$first_row"
    return 0
  fi

  printf ''
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

context_payload_json() {
  local context_json
  context_json="$(context_read_json)"
  if [[ "${OPENCLAW_DX_EMBED_FULL_CONTEXT:-false}" == "true" ]]; then
    printf '%s' "$context_json"
    return 0
  fi
  jq -c '
    (.project // {}) as $project
    | (.workspace // {}) as $workspace
    | (.agent // {}) as $agent
    | (.updated_at // "") as $updated_at
    | (.workspace_lineage // {}) as $lineage
    | ($workspace.id // "") as $workspace_id
    | ($lineage[$workspace_id] // null) as $self_lineage
    | (($self_lineage.parent_workspace // "") | tostring) as $parent_id
    | {
        project: $project,
        workspace: $workspace,
        agent: $agent,
        updated_at: $updated_at,
        workspace_lineage: (
          if ($workspace_id | length) == 0 or $self_lineage == null then
            {}
          else
            (
              {($workspace_id): $self_lineage}
              + (
                  if ($parent_id | length) > 0 and ($lineage[$parent_id] != null) then
                    {($parent_id): $lineage[$parent_id]}
                  else
                    {}
                  end
                )
            )
          end
        )
      }
  ' <<<"$context_json" 2>/dev/null || printf '{}'
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
  local workspace_scope="${5:-}"
  local parent_workspace="${6:-}"
  local parent_name="${7:-}"
  if [[ -z "${workspace_id// }" ]]; then
    return 0
  fi

  local canonical_repo ctx ts updated
  canonical_repo="$(canonicalize_path "$repo_path")"
  if [[ -z "${canonical_repo// }" ]]; then
    canonical_repo="$repo_path"
  fi
  workspace_scope="$(normalize_workspace_scope "$workspace_scope")"
  ctx="$(context_read_json)"
  ts="$(context_timestamp_utc)"
  updated="$(jq -c \
    --arg id "$workspace_id" \
    --arg name "$workspace_name" \
    --arg repo "$canonical_repo" \
    --arg assistant "$assistant" \
    --arg scope "$workspace_scope" \
    --arg parent_workspace "$parent_workspace" \
    --arg parent_name "$parent_name" \
    --arg ts "$ts" '
      (.workspace // {}) as $prev
      | ($prev.id // "") as $prev_id
      | (
          if ($scope | length) > 0 then
            $scope
          elif $prev_id == $id then
            ($prev.scope // "")
          else
            ""
          end
        ) as $resolved_scope
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
          ),
          scope: $resolved_scope,
          scope_label: (
            if $resolved_scope == "nested" then
              "nested workspace"
            elif $resolved_scope == "project" then
              "project workspace"
            else
              ""
            end
          ),
          parent_workspace: (
            if ($parent_workspace | length) > 0 then
              $parent_workspace
            elif $prev_id == $id then
              ($prev.parent_workspace // "")
            else
              ""
            end
          ),
          parent_name: (
            if ($parent_name | length) > 0 then
              $parent_name
            elif $prev_id == $id then
              ($prev.parent_name // "")
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
  local ws_row ws_name ws_repo ws_assistant ws_scope ws_scope_source ws_parent ws_parent_name
  if ws_row="$(workspace_row_with_scope_by_id "$workspace_id")" && [[ -n "${ws_row// }" ]]; then
    ws_name="$(jq -r '.name // ""' <<<"$ws_row")"
    ws_repo="$(jq -r '.repo // ""' <<<"$ws_row")"
    ws_assistant="$(jq -r '.assistant // ""' <<<"$ws_row")"
    ws_scope="$(jq -r '.scope // ""' <<<"$ws_row")"
    ws_scope_source="$(jq -r '.scope_source // ""' <<<"$ws_row")"
    ws_parent="$(jq -r '.parent_workspace // ""' <<<"$ws_row")"
    ws_parent_name="$(jq -r '.parent_name // ""' <<<"$ws_row")"
    if [[ -n "${assistant_override// }" ]]; then
      ws_assistant="$assistant_override"
    fi
    context_set_workspace "$workspace_id" "$ws_name" "$ws_repo" "$ws_assistant" "$ws_scope" "$ws_parent" "$ws_parent_name"
    context_set_workspace_lineage_if_authoritative "$workspace_id" "$ws_scope" "$ws_parent" "$ws_parent_name" "$ws_scope_source"
    return 0
  fi
  context_set_workspace "$workspace_id" "" "" "$assistant_override" "" "" ""
}

normalize_workspace_scope() {
  local scope="${1:-}"
  case "$scope" in
    project|nested)
      printf '%s' "$scope"
      ;;
    *)
      printf ''
      ;;
  esac
}

workspace_scope_label() {
  local scope
  scope="$(normalize_workspace_scope "${1:-}")"
  case "$scope" in
    nested)
      printf 'nested workspace'
      ;;
    project)
      printf 'project workspace'
      ;;
    *)
      printf 'workspace'
      ;;
  esac
}

workspace_scope_title() {
  local scope
  scope="$(normalize_workspace_scope "${1:-}")"
  case "$scope" in
    nested)
      printf 'Nested workspace'
      ;;
    project)
      printf 'Project workspace'
      ;;
    *)
      printf 'Workspace'
      ;;
  esac
}

workspace_brief_label() {
  local workspace_id="${1:-}"
  local workspace_name="${2:-}"
  local scope_label="${3:-}"
  local parent_workspace="${4:-}"

  local label="$workspace_id"
  if [[ -n "${workspace_name// }" ]]; then
    label+=" ($workspace_name)"
  fi
  if [[ -n "${scope_label// }" ]]; then
    label+=" [$scope_label"
    if [[ -n "${parent_workspace// }" ]]; then
      label+=" <- $parent_workspace"
    fi
    label+="]"
  fi
  printf '%s' "$label"
}

workspace_label_for_id() {
  local workspace_id="${1:-}"
  if [[ -z "${workspace_id// }" ]]; then
    printf ''
    return 0
  fi

  local ctx_json ws_name ws_scope_label ws_parent
  ctx_json="$(workspace_context_payload_by_id "$workspace_id")"
  if [[ -z "${ctx_json// }" || "$ctx_json" == "null" ]]; then
    printf '%s' "$workspace_id"
    return 0
  fi

  ws_name="$(jq -r '.name // ""' <<<"$ctx_json")"
  ws_scope_label="$(jq -r '.scope_label // ""' <<<"$ctx_json")"
  ws_parent="$(jq -r '.parent_workspace // ""' <<<"$ctx_json")"
  workspace_brief_label "$workspace_id" "$ws_name" "$ws_scope_label" "$ws_parent"
}

context_workspace_lineage_map() {
  jq -c '.workspace_lineage // {}' <<<"$(context_read_json)"
}

workspace_scope_source_is_authoritative() {
  case "${1:-}" in
    lineage|workspace)
      return 0
      ;;
    *)
      return 1
      ;;
  esac
}

context_set_workspace_lineage() {
  local workspace_id="$1"
  local scope="${2:-}"
  local parent_workspace="${3:-}"
  local parent_name="${4:-}"
  if [[ -z "${workspace_id// }" ]]; then
    return 0
  fi
  scope="$(normalize_workspace_scope "$scope")"
  if [[ -z "$scope" ]]; then
    return 0
  fi

  local ctx ts updated
  ctx="$(context_read_json)"
  ts="$(context_timestamp_utc)"
  updated="$(jq -c \
    --arg id "$workspace_id" \
    --arg scope "$scope" \
    --arg parent_workspace "$parent_workspace" \
    --arg parent_name "$parent_name" \
    --arg ts "$ts" '
      .workspace_lineage = (.workspace_lineage // {})
      | .workspace_lineage[$id] = {
          scope: $scope,
          parent_workspace: $parent_workspace,
          parent_name: $parent_name,
          updated_at: $ts
        }
      | .updated_at = $ts
    ' <<<"$ctx")"
  context_write_json "$updated"
}

context_set_workspace_lineage_if_authoritative() {
  local workspace_id="$1"
  local scope="${2:-}"
  local parent_workspace="${3:-}"
  local parent_name="${4:-}"
  local scope_source="${5:-}"
  if ! workspace_scope_source_is_authoritative "$scope_source"; then
    return 0
  fi
  context_set_workspace_lineage "$workspace_id" "$scope" "$parent_workspace" "$parent_name"
}

workspace_enrich_scope_json() {
  local ws_json="$1"
  local lineage_json
  lineage_json="$(context_workspace_lineage_map)"
  jq -cn --argjson ws "$ws_json" --argjson lineage "$lineage_json" '
    def valid_scope($scope):
      ($scope == "project" or $scope == "nested");
    def infer_scope_from_name($name; $name_parent_candidate):
      if (($name // "") | contains(".")) and (($name_parent_candidate // "") | length) > 0 then
        "nested"
      else
        "project"
      end;
    def scope_label($scope):
      if $scope == "nested" then "nested workspace" else "project workspace" end;
    $ws as $all
    | $all
    | map(
        . as $w
        | ($lineage[($w.id // "")] // {}) as $line
        | (($line.scope // "") | ascii_downcase) as $line_scope
        | (($w.scope // "") | ascii_downcase) as $row_scope
        | (
            (($w.name // "") | split(".") | .[0] // "") as $prefix
            | ($all | map(select((.repo // "") == ($w.repo // "") and (.name // "") == $prefix)) | .[0].id // "")
          ) as $name_parent_candidate
        | (
            if valid_scope($line_scope) then
              $line_scope
            elif valid_scope($row_scope) then
              $row_scope
            else
              infer_scope_from_name($w.name; $name_parent_candidate)
            end
          ) as $scope
        | (
            if $scope == "nested" then
              if (($line.parent_workspace // "") | length) > 0 then
                ($line.parent_workspace // "")
              elif ((($w.parent_workspace // $w.parent_workspace_id // "") | length) > 0) then
                ($w.parent_workspace // $w.parent_workspace_id // "")
              else
                $name_parent_candidate
              end
            else
              ""
            end
          ) as $parent_workspace
        | (
            if $scope == "nested" then
              if (($line.parent_name // "") | length) > 0 then
                ($line.parent_name // "")
              elif ((($w.parent_name // "") | length) > 0) then
                ($w.parent_name // "")
              elif (($name_parent_candidate // "") | length) > 0 then
                ((($w.name // "") | split(".") | .[0]) // "")
              else
                ""
              end
            else
              ""
            end
          ) as $parent_name
        | $w + {
            scope: $scope,
            scope_label: scope_label($scope),
            scope_source: (
              if valid_scope($line_scope) then
                "lineage"
              elif valid_scope($row_scope) then
                "workspace"
              else
                "name_inference"
              end
            ),
            parent_workspace: $parent_workspace,
            parent_name: $parent_name
          }
      )
  '
}

workspace_row_with_scope_by_id() {
  local workspace_id="$1"
  if [[ -z "${workspace_id// }" ]]; then
    printf ''
    return 0
  fi
  local ws_out ws_json ws_enriched
  if ! ws_out="$(amux_ok_json workspace list --archived)"; then
    return 1
  fi
  ws_json="$(jq -c '.data // []' <<<"$ws_out")"
  ws_enriched="$(workspace_enrich_scope_json "$ws_json")"
  jq -c --arg id "$workspace_id" '
    map(select(.id == $id))
    | .[0] // empty
  ' <<<"$ws_enriched"
}

workspace_context_payload_by_id() {
  local workspace_id="$1"
  if [[ -z "${workspace_id// }" ]]; then
    printf 'null'
    return 0
  fi
  local ws_row
  if ! ws_row="$(workspace_row_with_scope_by_id "$workspace_id")"; then
    printf 'null'
    return 0
  fi
  if [[ -z "${ws_row// }" ]]; then
    printf 'null'
    return 0
  fi
  jq -c '
    {
      id: (.id // ""),
      name: (.name // ""),
      repo: (.repo // ""),
      root: (.root // ""),
      assistant: (.assistant // ""),
      scope: (.scope // ""),
      scope_label: (.scope_label // ""),
      scope_source: (.scope_source // ""),
      parent_workspace: (.parent_workspace // ""),
      parent_name: (.parent_name // "")
    }
  ' <<<"$ws_row"
}

workspace_rows_with_repo_compare() {
  local rows_json="${1:-[]}"
  local enriched_rows='[]'
  local row repo repo_compare row_with_compare

  while IFS= read -r row; do
    [[ -z "${row// }" ]] && continue
    repo="$(jq -r '.repo // ""' <<<"$row")"
    repo_compare="$(normalize_path_for_compare "$repo")"
    row_with_compare="$(jq -c --arg repo_compare "$repo_compare" '. + {repo_compare: $repo_compare}' <<<"$row")"
    enriched_rows="$(jq -cn --argjson rows "$enriched_rows" --argjson row "$row_with_compare" '$rows + [$row]')"
  done < <(jq -c '.[]?' <<<"$rows_json")

  printf '%s' "$enriched_rows"
}

workspace_scope_metadata_map() {
  local ws_meta_out ws_meta_rows ws_meta_with_compare
  if ! ws_meta_out="$(amux_ok_json workspace list --archived)"; then
    printf '{}'
    return 0
  fi
  ws_meta_rows="$(jq -c '.data // []' <<<"$ws_meta_out")"
  ws_meta_rows="$(workspace_enrich_scope_json "$ws_meta_rows")"
  ws_meta_with_compare="$(workspace_rows_with_repo_compare "$ws_meta_rows")"
  jq -c '
    map({
      key: (.id // ""),
      value: {
        id: (.id // ""),
        name: (.name // ""),
        repo: (.repo // ""),
        repo_compare: (.repo_compare // ""),
        scope_label: (.scope_label // ""),
        parent_workspace: (.parent_workspace // "")
      }
    })
    | from_entries
  ' <<<"$ws_meta_with_compare"
}

project_row_by_path() {
  local project_path="$1"
  local canonical normalized_target out rows exact_match
  canonical="$(canonicalize_path "$project_path")"
  normalized_target="$(normalize_path_for_compare "$project_path")"
  if ! out="$(amux_ok_json project list)"; then
    return 1
  fi
  rows="$(jq -c '.data // []' <<<"$out")"
  exact_match="$(jq -c --arg raw "$project_path" --arg canonical "$canonical" '
    .data // []
    | map(select((.path // "") == $raw or (.path // "") == $canonical))
    | .[0] // empty
  ' <<<"$out")"
  if [[ -n "${exact_match// }" ]]; then
    printf '%s' "$exact_match"
    return 0
  fi

  if [[ -n "${normalized_target// }" ]]; then
    local row row_path row_normalized
    while IFS= read -r row; do
      [[ -z "${row// }" ]] && continue
      row_path="$(jq -r '.path // ""' <<<"$row")"
      row_normalized="$(normalize_path_for_compare "$row_path")"
      if [[ -n "${row_normalized// }" && "$row_normalized" == "$normalized_target" ]]; then
        printf '%s' "$row"
        return 0
      fi
    done < <(jq -c '.[]' <<<"$rows")
  fi

  printf ''
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
  out="$(OPENCLAW_DX_SKIP_PRESENT=true "$SELF_SCRIPT" "$@" 2>/dev/null || true)"
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
  local workspace_hint="${4:-}"
  local assistant_hint="${5:-}"

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
    --arg command_name "$command_name" \
    --arg workflow "$workflow_name" \
    --arg dx_ref "$DX_CMD_REF" \
    --arg step_ref "$STEP_CMD_REF" \
    --arg workspace_hint "$workspace_hint" \
    '
      def hint_workspace_id:
        if ((.workspace_id // "") | length) > 0 then
          (.workspace_id // "")
        elif ($workspace_hint | length) > 0 then
          $workspace_hint
        else
          ""
        end;
      def is_turn_command:
        ($command_name == "start" or $command_name == "continue" or $command_name == "review");
      def is_workspace_error:
        ((.overall_status // .status // "") == "command_error" or (.status // "") == "command_error" or (.overall_status // .status // "") == "partial");
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
        elif (is_workspace_error) and ((hint_workspace_id | length) > 0) and (is_turn_command) then
          "Check workspace status and assistant readiness, then retry with a focused follow-up."
        elif (is_workspace_error) and ((hint_workspace_id | length) > 0) then
          "Check workspace status, then retry with a focused follow-up."
        elif ((.overall_status // .status // "") == "completed" or (.status // "") == "idle") then
          "Continue with a follow-up task or run status/review."
        else
          "Check status and continue with the next focused step."
        end;
      def fallback_suggested_command:
        if ((.suggested_command // "") | length) > 0 then
          .suggested_command
        elif ((.overall_status // .status // "") == "needs_input") and ((.agent_id // "") | length) > 0 then
          ($dx_ref + " continue --agent " + .agent_id + " --text \"If a choice is required, pick the safest high-impact default, continue, and report status plus next action.\" --enter")
        elif ((.quick_actions // []) | length) > 0 then
          (
            (.quick_actions | map(.command // "") | map(select(length > 0)) | .[0])
            // ""
          )
        elif ((hint_workspace_id | length) > 0) then
          ($dx_ref + " status --workspace " + (hint_workspace_id))
        elif ((.agent_id // "") | length) > 0 then
          ($dx_ref + " continue --agent " + .agent_id + " --text \"Provide a one-line progress status.\" --enter")
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
      | .quick_actions = (
          (.quick_actions // []) as $actions
          | if ($actions | length) > 0 then
              $actions
            elif ((hint_workspace_id | length) > 0) then
              (
                [
                  {
                    id: "status_ws",
                    label: "WS Status",
                    command: ($dx_ref + " status --workspace " + (hint_workspace_id)),
                    style: "primary",
                    prompt: "Check workspace status before retrying"
                  }
                ]
                + (
                    if (is_workspace_error and is_turn_command) then
                      [
                        {
                          id: "assistants_ws",
                          label: "Assistants",
                          command: ($dx_ref + " assistants --workspace " + (hint_workspace_id) + " --probe --limit 3"),
                          style: "secondary",
                          prompt: "Check assistant readiness before retrying"
                        }
                      ]
                    else
                      []
                    end
                  )
              )
            else
              $actions
            end
        )
      | . + {command: $command, workflow: $workflow}
    ' <<<"$turn_json")"
  normalized_json="$(jq -c --arg dx "$DX_CMD_REF" --arg turn "$TURN_CMD_REF" --arg step "$STEP_CMD_REF" '
    def rewrite_followup_command:
      if test("openclaw-(step|turn)\\.sh send --agent [^ ]+") then
        (capture("openclaw-(?:step|turn)\\.sh send --agent (?<agent>[^ ]+)").agent) as $agent
        | (
            if test("--text \"[^\"]*\"") then
              capture(".*--text \"(?<text>[^\"]*)\"").text
            elif test("--text [^ ]+") then
              capture(".*--text (?<text>[^ ]+)").text
            else
              "Continue from current state and report status plus next action."
            end
          ) as $text
        | ("skills/amux/scripts/openclaw-dx.sh continue --agent " + $agent + " --text " + ($text | @sh)
          + (if test("(^| )--enter( |$)") then " --enter" else "" end))
      else
        .
      end;
    def rewrite:
      if type == "string" then
        gsub("skills/amux/scripts/openclaw-dx\\.sh"; $dx)
        | gsub("skills/amux/scripts/openclaw-turn\\.sh"; $turn)
        | gsub("skills/amux/scripts/openclaw-step\\.sh"; $step)
        | rewrite_followup_command
      elif type == "array" then
        map(rewrite)
      elif type == "object" then
        with_entries(.value |= rewrite)
      else
        .
      end;
    rewrite
  ' <<<"$normalized_json")"

  local workspace_id agent_id assistant workspace_context_json
  workspace_id="$(jq -r '.workspace_id // ""' <<<"$normalized_json")"
  if [[ -z "${workspace_id// }" && -n "${workspace_hint// }" ]]; then
    workspace_id="$workspace_hint"
  fi
  agent_id="$(jq -r '.agent_id // ""' <<<"$normalized_json")"
  assistant="$(jq -r '.assistant // ""' <<<"$normalized_json")"
  if [[ -z "${assistant// }" && -n "${assistant_hint// }" ]]; then
    assistant="$assistant_hint"
  fi
  workspace_context_json='null'
  if [[ -n "$workspace_id" ]]; then
    context_set_workspace_with_lookup "$workspace_id" "$assistant"
    workspace_context_json="$(workspace_context_payload_by_id "$workspace_id")"
  fi
  if [[ -n "$agent_id" ]]; then
    context_set_agent "$agent_id" "$workspace_id" "$assistant"
  fi
  normalized_json="$(jq -c --argjson workspace_context "$workspace_context_json" '
    if $workspace_context == null then
      .
    else
      .workspace_context = $workspace_context
      | .data = ((.data // {}) + {workspace_context: $workspace_context})
    end
  ' <<<"$normalized_json")"
  normalized_json="$(jq -c '
    def workspace_line($wc):
      ("Workspace: " + ($wc.id // ""))
      + (if (($wc.name // "") | length) > 0 then " (" + ($wc.name // "") + ")" else "" end)
      + (
          if (($wc.scope_label // "") | length) > 0 then
            " [" + ($wc.scope_label // "")
            + (if (($wc.parent_workspace // "") | length) > 0 then " <- " + ($wc.parent_workspace // "") else "" end)
            + "]"
          else
            ""
          end
        );
    def workspace_label($wc):
      (($wc.id // ""))
      + (if (($wc.name // "") | length) > 0 then " (" + ($wc.name // "") + ")" else "" end)
      + (
          if (($wc.scope_label // "") | length) > 0 then
            " [" + ($wc.scope_label // "")
            + (if (($wc.parent_workspace // "") | length) > 0 then " <- " + ($wc.parent_workspace // "") else "" end)
            + "]"
          else
            ""
          end
        );
    if .workspace_context == null then
      .
    else
      (.workspace_context) as $wc
      | (workspace_line($wc)) as $line
      | (workspace_label($wc)) as $label
      | .channel = (.channel // {})
      | (.summary // "") as $summary
      | .summary = (
          if ($label | length) == 0 then
            $summary
          elif ($summary | length) == 0 then
            $label
          elif ($summary | contains($label)) then
            $summary
          else
            $summary + " [" + $label + "]"
          end
        )
      | (.channel.message // "") as $msg
      | .channel.message = (
          if ($msg | length) == 0 then
            $line
          elif ($msg | contains($line)) then
            $msg
          else
            $msg + "\n" + $line
          end
        )
    end
  ' <<<"$normalized_json")"

  if [[ "${OPENCLAW_DX_SKIP_PRESENT:-false}" != "true" && -x "$OPENCLAW_PRESENT_SCRIPT" ]]; then
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
  local workspace_label=""
  local workspace_autorenamed_from=""

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
      if [[ "$err_code" == "create_failed" ]] && [[ "$err_message" == *"already exists"* || "$err_message" == *"already used by worktree"* ]]; then
        if workspace_create_emit_existing_recovery "$project_path" "$workspace_name" "project" "$assistant" "$err_message" "project.add"; then
          return
        fi
        local alt_workspace_name retry_create_out retry_args retry_succeeded=false
        local retry_suffixes=(2 3 4 5)
        for retry_suffix in "${retry_suffixes[@]}"; do
          alt_workspace_name="$(sanitize_workspace_name "${workspace_name}-${retry_suffix}")"
          if [[ -z "${alt_workspace_name// }" || "$alt_workspace_name" == "$workspace_name" ]]; then
            continue
          fi
          retry_args=(workspace create "$alt_workspace_name" --project "$project_path")
          if [[ -n "$assistant" ]]; then
            retry_args+=(--assistant "$assistant")
          fi
          if [[ -n "$base" ]]; then
            retry_args+=(--base "$base")
          fi
          if retry_create_out="$(amux_ok_json "${retry_args[@]}")"; then
            ws_create_out="$retry_create_out"
            workspace_autorenamed_from="$workspace_name"
            workspace_name="$alt_workspace_name"
            retry_succeeded=true
            break
          fi
          err_payload="$AMUX_ERROR_OUTPUT"
          if [[ -z "${err_payload// }" ]] && [[ -n "${AMUX_ERROR_CAPTURE_FILE:-}" ]] && [[ -f "$AMUX_ERROR_CAPTURE_FILE" ]]; then
            err_payload="$(cat "$AMUX_ERROR_CAPTURE_FILE" 2>/dev/null || true)"
          fi
        done
        if [[ "$retry_succeeded" != "true" ]]; then
          alt_workspace_name="$(sanitize_workspace_name "${workspace_name}-$(date +%H%M%S)")"
          if [[ -n "${alt_workspace_name// }" && "$alt_workspace_name" != "$workspace_name" ]]; then
            retry_args=(workspace create "$alt_workspace_name" --project "$project_path")
            if [[ -n "$assistant" ]]; then
              retry_args+=(--assistant "$assistant")
            fi
            if [[ -n "$base" ]]; then
              retry_args+=(--base "$base")
            fi
            if retry_create_out="$(amux_ok_json "${retry_args[@]}")"; then
              ws_create_out="$retry_create_out"
              workspace_autorenamed_from="$workspace_name"
              workspace_name="$alt_workspace_name"
            else
              err_payload="$AMUX_ERROR_OUTPUT"
              if [[ -z "${err_payload// }" ]] && [[ -n "${AMUX_ERROR_CAPTURE_FILE:-}" ]] && [[ -f "$AMUX_ERROR_CAPTURE_FILE" ]]; then
                err_payload="$(cat "$AMUX_ERROR_CAPTURE_FILE" 2>/dev/null || true)"
              fi
            fi
          fi
        fi
        if jq -e '.ok == true' >/dev/null 2>&1 <<<"$ws_create_out"; then
          :
        else
          emit_project_add_workspace_conflict_guidance "$project_path" "$workspace_name" "$assistant" "$base" "$err_message"
          return
        fi
      fi
      if ! jq -e '.ok == true' >/dev/null 2>&1 <<<"$ws_create_out"; then
        emit_amux_error "project.add" "$err_payload"
        return
      fi
    fi
    workspace_data="$(jq -c '.data' <<<"$ws_create_out")"
    workspace_id="$(jq -r '.data.id // ""' <<<"$ws_create_out")"
  fi

  context_set_project "$project_path" "$project_name"
  if [[ -n "$workspace_id" ]]; then
    local workspace_name_out workspace_assistant_out
    workspace_name_out="$(jq -r '.name // ""' <<<"$workspace_data")"
    workspace_assistant_out="$(jq -r '.assistant // ""' <<<"$workspace_data")"
    context_set_workspace "$workspace_id" "$workspace_name_out" "$project_path" "$workspace_assistant_out" "project" "" ""
    context_set_workspace_lineage "$workspace_id" "project" "" ""
    workspace_label="$(workspace_brief_label "$workspace_id" "$workspace_name_out" "$(workspace_scope_label "project")" "")"
  else
    workspace_label=""
  fi

  RESULT_OK=true
  RESULT_COMMAND="project.add"
  RESULT_STATUS="ok"
  if [[ -n "$workspace_label" ]]; then
    if [[ -n "$workspace_autorenamed_from" ]]; then
      RESULT_SUMMARY="Project ready and workspace created (fallback name): $workspace_label"
    else
      RESULT_SUMMARY="Project ready and workspace created: $workspace_label"
    fi
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

  RESULT_DATA="$(jq -cn --argjson project "$(jq -c '.data' <<<"$add_out")" --argjson workspace "$workspace_data" --arg workspace_label "$workspace_label" '{project: $project, workspace: $workspace, workspace_label: $workspace_label}')"
  if [[ "$inferred_from_cwd" == "true" ]]; then
    RESULT_DATA="$(jq -c '. + {path_source: "cwd_or_git_root"}' <<<"$RESULT_DATA")"
  fi
  if [[ -n "$workspace_autorenamed_from" ]]; then
    RESULT_DATA="$(jq -c --arg requested "$workspace_autorenamed_from" --arg used "$workspace_name" '. + {workspace_name_requested: $requested, workspace_name_used: $used, workspace_name_fallback: true}' <<<"$RESULT_DATA")"
  fi

  RESULT_MESSAGE="✅ Project registered: $project_name"$'\n'"Path: $project_path"
  if [[ "$inferred_from_cwd" == "true" ]]; then
    RESULT_MESSAGE+=$'\n'"Source: inferred from current working git repo"
  fi
  if [[ -n "$workspace_id" ]]; then
    local workspace_root workspace_name_final
    workspace_root="$(jq -r '.root // ""' <<<"$workspace_data")"
    workspace_name_final="$(jq -r '.name // ""' <<<"$workspace_data")"
    RESULT_MESSAGE+=$'\n'"Workspace: ${workspace_label:-$workspace_id}"
    if [[ -n "$workspace_autorenamed_from" ]]; then
      RESULT_MESSAGE+=$'\n'"Requested workspace name unavailable: $workspace_autorenamed_from"
      RESULT_MESSAGE+=$'\n'"Used workspace name: ${workspace_name_final:-$workspace_name}"
    fi
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
  local first_project_path first_project_name preferred_project_path preferred_project_name
  first_project_path="$(jq -r '.[0].path // ""' <<<"$preview")"
  first_project_name="$(jq -r '.[0].name // ""' <<<"$preview")"
  preferred_project_path="$first_project_path"
  preferred_project_name="$first_project_name"
  local context_project_for_match
  context_project_for_match="$(normalize_path_for_compare "$(context_project_path)")"
  if [[ "$count" -gt 0 && -n "${context_project_for_match// }" ]]; then
    local row row_path row_name row_path_for_match
    while IFS= read -r row; do
      [[ -z "${row// }" ]] && continue
      row_path="$(jq -r '.path // ""' <<<"$row")"
      row_name="$(jq -r '.name // ""' <<<"$row")"
      row_path_for_match="$(normalize_path_for_compare "$row_path")"
      if [[ -n "${row_path_for_match// }" && "$row_path_for_match" == "$context_project_for_match" ]]; then
        preferred_project_path="$row_path"
        preferred_project_name="$row_name"
        break
      fi
    done < <(jq -c '.[]?' <<<"$sorted")
  fi
  if [[ "$count" -gt 0 ]]; then
    if [[ -n "${preferred_project_path// }" ]]; then
      RESULT_SUGGESTED_COMMAND="skills/amux/scripts/openclaw-dx.sh project pick --path $(shell_quote "$preferred_project_path")"
    elif [[ -n "$preferred_project_name" ]]; then
      RESULT_SUGGESTED_COMMAND="skills/amux/scripts/openclaw-dx.sh project pick --name $(shell_quote "$preferred_project_name")"
    else
      RESULT_SUGGESTED_COMMAND="skills/amux/scripts/openclaw-dx.sh project pick --index 1"
    fi
  elif [[ -n "${query// }" ]]; then
    RESULT_NEXT_ACTION="Try a broader query or register a new project."
  fi

  local actions='[]'
  if [[ -n "${preferred_project_path// }" && "$preferred_project_path" != "$first_project_path" ]]; then
    actions="$(append_action "$actions" "pick_active" "Pick Active" "skills/amux/scripts/openclaw-dx.sh project pick --path $(shell_quote "$preferred_project_path")" "success" "Select the active context project")"
  fi
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
      local disambiguated
      disambiguated="$(project_pick_disambiguate_matches "$matches")"
      if [[ -n "${disambiguated// }" && "${disambiguated}" != "null" ]]; then
        selected="$disambiguated"
        selected_index="$(jq -r '.index // ""' <<<"$selected")"
        resolved_by="name"
      else
        match_lines="$(jq -r '. | map("\(.index). \(.name) — \(.path)") | join("\n")' <<<"$matches")"

        RESULT_OK=false
        RESULT_COMMAND="project.pick"
        RESULT_STATUS="attention"
        RESULT_SUMMARY="Multiple projects matched \"$name_query\""
        RESULT_NEXT_ACTION="Pick one match by index (or use the exact project path)."
        local suggested_index
        suggested_index="$(jq -r '.[0].index // 1' <<<"$matches")"
        if ! is_positive_int "$suggested_index"; then
          suggested_index="1"
        fi
        RESULT_SUGGESTED_COMMAND="skills/amux/scripts/openclaw-dx.sh project pick --index $suggested_index"

        local context_project_for_match="" context_project_candidate_path=""
        context_project_for_match="$(normalize_path_for_compare "$(context_project_path)")"
        if [[ -n "${context_project_for_match// }" ]]; then
          while IFS= read -r row; do
            [[ -z "${row// }" ]] && continue
            local row_index row_path row_path_for_match
            row_index="$(jq -r '.index // ""' <<<"$row")"
            row_path="$(jq -r '.path // ""' <<<"$row")"
            if [[ -z "${row_path// }" || -z "${row_index// }" ]]; then
              continue
            fi
            row_path_for_match="$(normalize_path_for_compare "$row_path")"
            if [[ -n "${row_path_for_match// }" && "$row_path_for_match" == "$context_project_for_match" ]]; then
              context_project_candidate_path="$row_path"
              break
            fi
          done < <(jq -c '.[]' <<<"$matches")
        fi
        if [[ -n "${context_project_candidate_path// }" ]]; then
          RESULT_SUGGESTED_COMMAND="skills/amux/scripts/openclaw-dx.sh project pick --path $(shell_quote "$context_project_candidate_path")"
        fi

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
    fi
    if [[ -z "${selected// }" || "$selected" == "null" ]]; then
      selected="$(jq -c '.[0]' <<<"$matches")"
      selected_index="$(jq -r '.index // ""' <<<"$selected")"
    fi
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
    actions="$(append_action "$actions" "ws_list" "Workspaces" "skills/amux/scripts/openclaw-dx.sh workspace list --project $(shell_quote "$selected_path")" "primary" "List workspaces in this project")"
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
  local ws_label
  ws_label="$(workspace_brief_label "$ws_id" "$workspace_name" "$(workspace_scope_label "project")" "")"
  context_set_project "$selected_path" "$selected_name"
  context_set_workspace "$ws_id" "$workspace_name" "$selected_path" "$ws_assistant" "project" "" ""
  context_set_workspace_lineage "$ws_id" "project" "" ""

  RESULT_OK=true
  RESULT_COMMAND="project.pick"
  RESULT_STATUS="ok"
  RESULT_SUMMARY="Selected project and created workspace: $ws_label"
  RESULT_NEXT_ACTION="Start a coding turn in the new workspace."
  RESULT_SUGGESTED_COMMAND="skills/amux/scripts/openclaw-dx.sh start --workspace $(shell_quote "$ws_id") --assistant $(shell_quote "${assistant:-codex}") --prompt \"Analyze the biggest debt items and fix one high-impact issue.\""

  local actions='[]'
  actions="$(append_action "$actions" "start" "Start" "$RESULT_SUGGESTED_COMMAND" "success" "Start a coding turn")"
  actions="$(append_action "$actions" "status" "Status" "skills/amux/scripts/openclaw-dx.sh status --workspace $(shell_quote "$ws_id")" "primary" "Show workspace status")"
  RESULT_QUICK_ACTIONS="$actions"

  RESULT_DATA="$(jq -cn --arg resolved_by "$resolved_by" --arg resolved_input "$resolved_input" --argjson index "$selected_index" --argjson project "$selected" --argjson workspace "$(jq -c '.data' <<<"$ws_out")" --arg workspace_label "$ws_label" '{resolved_by: $resolved_by, resolved_input: $resolved_input, index: $index, project: $project, workspace: $workspace, workspace_label: $workspace_label}')"
  RESULT_MESSAGE="✅ Project selected: $selected_name"$'\n'"Workspace: $ws_label"$'\n'"Root: $ws_root"
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
  ws_json="$(workspace_enrich_scope_json "$ws_json")"

  local selected_workspace='null'
  local selected_workspace_id=""
  local selected_workspace_name=""
  local selected_workspace_repo=""
  local selected_workspace_label=""

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
    local selected_workspace_scope selected_workspace_scope_label selected_workspace_parent
    selected_workspace_id="$(jq -r '.id // ""' <<<"$selected_workspace")"
    selected_workspace_name="$(jq -r '.name // ""' <<<"$selected_workspace")"
    selected_workspace_repo="$(jq -r '.repo // ""' <<<"$selected_workspace")"
    selected_workspace_scope="$(jq -r '.scope // ""' <<<"$selected_workspace")"
    selected_workspace_scope_label="$(workspace_scope_label "$selected_workspace_scope")"
    selected_workspace_parent="$(jq -r '.parent_workspace // ""' <<<"$selected_workspace")"
    selected_workspace_label="$(workspace_brief_label "$selected_workspace_id" "$selected_workspace_name" "$selected_workspace_scope_label" "$selected_workspace_parent")"
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
      actions="$(append_action "$actions" "decide_ws" "Decide WS" "skills/amux/scripts/openclaw-dx.sh workspace decide --project $(shell_quote "$selected_project_path") --task $(shell_quote "$kickoff_prompt") --assistant $(shell_quote "$assistant")" "success" "Get a project workspace vs nested workspace recommendation")"
      actions="$(append_action "$actions" "create_ws" "Create WS" "skills/amux/scripts/openclaw-dx.sh workspace create --name mobile --project $(shell_quote "$selected_project_path") --assistant $(shell_quote "$assistant")" "primary" "Create a project workspace")"
      ;;
    select_workspace)
      actions="$(append_action "$actions" "list_ws" "Workspaces" "skills/amux/scripts/openclaw-dx.sh workspace list --project $(shell_quote "$selected_project_path")" "primary" "List workspaces in this project")"
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
    --arg selected_workspace_label "$selected_workspace_label" \
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
      selected_workspace_label: $selected_workspace_label,
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
    RESULT_MESSAGE+=$'\n'"Workspace: ${selected_workspace_label:-$selected_workspace_id}"
    RESULT_MESSAGE+=$'\n'"Agents: $workspace_agent_count, Terminals: $workspace_terminal_count"
  fi
  if [[ -n "${capture_summary// }" ]]; then
    RESULT_MESSAGE+=$'\n'"Latest: $capture_summary"
  fi
  RESULT_MESSAGE+=$'\n'"Next: $RESULT_NEXT_ACTION"

  emit_result
}

workspace_conflict_alt_name() {
  local requested_name="$1"
  local alt_name
  alt_name="$(sanitize_workspace_name "${requested_name}-2")"
  if [[ "$alt_name" == "$requested_name" ]]; then
    alt_name="$(sanitize_workspace_name "${requested_name}-$(date +%H%M%S)")"
  fi
  printf '%s' "$alt_name"
}

workspace_create_emit_existing_recovery() {
  local project="$1"
  local requested_name="$2"
  local requested_scope="$3"
  local requested_assistant="$4"
  local conflict_message="$5"
  local command_name="${6:-workspace.create}"

  local ws_out ws_rows existing
  if ! ws_out="$(amux_ok_json workspace list --repo "$project")"; then
    return 1
  fi
  ws_rows="$(jq -c '.data // []' <<<"$ws_out")"

  existing="$(jq -c --arg name "$requested_name" --arg conflict_message "$conflict_message" '
    (
      map(select((.name // "") == $name))
      + map(
          . as $row
          | select(($row.root // "") != "" and ($conflict_message | contains(($row.root // ""))))
        )
    )
    | reduce .[] as $row (
        {seen: {}, out: []};
        ($row.id // "") as $id
        | if ($id | length) == 0 then
            .
          elif (.seen[$id] // false) then
            .
          else
            .seen[$id] = true
            | .out += [$row]
          end
      )
    | .out[0] // empty
  ' <<<"$ws_rows")"
  if [[ -z "${existing// }" ]]; then
    return 1
  fi

  local existing_id existing_name existing_root existing_assistant existing_scope existing_scope_source existing_parent existing_parent_name
  local existing_scoped_row
  existing_id="$(jq -r '.id // ""' <<<"$existing")"
  existing_name="$(jq -r '.name // ""' <<<"$existing")"
  existing_root="$(jq -r '.root // ""' <<<"$existing")"
  existing_assistant="$(jq -r '.assistant // ""' <<<"$existing")"
  if [[ -z "$existing_id" ]]; then
    return 1
  fi

  existing_scope="$(normalize_workspace_scope "$(jq -r '.scope // ""' <<<"$existing")")"
  existing_scope_source=""
  existing_parent="$(jq -r '.parent_workspace // ""' <<<"$existing")"
  existing_parent_name="$(jq -r '.parent_name // ""' <<<"$existing")"
  if existing_scoped_row="$(workspace_row_with_scope_by_id "$existing_id")" && [[ -n "${existing_scoped_row// }" ]]; then
    existing_scope="$(jq -r '.scope // ""' <<<"$existing_scoped_row")"
    existing_scope_source="$(jq -r '.scope_source // ""' <<<"$existing_scoped_row")"
    existing_parent="$(jq -r '.parent_workspace // ""' <<<"$existing_scoped_row")"
    existing_parent_name="$(jq -r '.parent_name // ""' <<<"$existing_scoped_row")"
  fi
  if [[ -z "$existing_scope" ]]; then
    existing_scope_source="name_inference"
    if [[ "$existing_name" == *.* ]]; then
      existing_scope="nested"
    else
      existing_scope="project"
    fi
  fi
  if [[ "$existing_scope" == "nested" ]]; then
    if [[ -z "$existing_parent_name" && "$existing_name" == *.* ]]; then
      existing_parent_name="${existing_name%%.*}"
    fi
    if [[ -z "$existing_parent" && -n "$existing_parent_name" ]]; then
      existing_parent="$(jq -r --arg parent_name "$existing_parent_name" --arg repo "$project" '
        map(select((.name // "") == $parent_name and (.repo // "") == $repo))
        | .[0].id // ""
      ' <<<"$ws_rows")"
    fi
  else
    existing_parent=""
    existing_parent_name=""
  fi

  local existing_scope_label existing_workspace_label existing_parent_label
  existing_scope_label="$(workspace_scope_label "$existing_scope")"
  existing_workspace_label="$(workspace_brief_label "$existing_id" "$existing_name" "$existing_scope_label" "$existing_parent")"
  existing_parent_label=""
  if [[ -n "$existing_parent" ]]; then
    existing_parent_label="$(workspace_label_for_id "$existing_parent")"
  fi

  context_set_workspace "$existing_id" "$existing_name" "$project" "$existing_assistant" "$existing_scope" "$existing_parent" "$existing_parent_name"
  context_set_workspace_lineage_if_authoritative "$existing_id" "$existing_scope" "$existing_parent" "$existing_parent_name" "$existing_scope_source"

  local suggested_assistant
  suggested_assistant="$requested_assistant"
  if [[ -z "$suggested_assistant" ]]; then
    suggested_assistant="$existing_assistant"
  fi
  if [[ -z "$suggested_assistant" ]]; then
    suggested_assistant="codex"
  fi

  local alt_name
  alt_name="$(workspace_conflict_alt_name "$requested_name")"

  RESULT_OK=false
  RESULT_COMMAND="$command_name"
  RESULT_STATUS="attention"
  RESULT_SUMMARY="Workspace already exists: $existing_workspace_label"
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
    --arg existing_scope "$existing_scope" \
    --arg existing_scope_label "$existing_scope_label" \
    --arg existing_workspace_label "$existing_workspace_label" \
    --arg requested_assistant "$requested_assistant" \
    --arg conflict_message "$conflict_message" \
    --arg alt_name "$alt_name" \
    --arg parent_workspace "$existing_parent" \
    --arg parent_workspace_label "$existing_parent_label" \
    --arg parent_name "$existing_parent_name" \
    --argjson existing "$existing" \
    '{project: $project, requested_name: $requested_name, requested_scope: $requested_scope, existing_scope: $existing_scope, existing_scope_label: $existing_scope_label, existing_workspace_label: $existing_workspace_label, requested_assistant: $requested_assistant, conflict_message: $conflict_message, alt_name: $alt_name, parent_workspace: $parent_workspace, parent_workspace_label: $parent_workspace_label, parent_name: $parent_name, existing_workspace: $existing}')"

  RESULT_MESSAGE="⚠️ Workspace name/branch conflict while creating \"$requested_name\""$'\n'"Reusing existing $existing_scope_label: $existing_workspace_label"
  if [[ -n "$existing_parent" ]]; then
    RESULT_MESSAGE+=$'\n'"Parent workspace: ${existing_parent_label:-$existing_parent}"
  fi
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

emit_project_add_workspace_conflict_guidance() {
  local project="$1"
  local requested_name="$2"
  local requested_assistant="$3"
  local requested_base="$4"
  local conflict_message="$5"

  local suggested_assistant alt_name retry_cmd list_cmd
  suggested_assistant="$requested_assistant"
  if [[ -z "$suggested_assistant" ]]; then
    suggested_assistant="codex"
  fi
  alt_name="$(workspace_conflict_alt_name "$requested_name")"

  retry_cmd="skills/amux/scripts/openclaw-dx.sh workspace create --name $(shell_quote "$alt_name") --project $(shell_quote "$project") --scope project --assistant $(shell_quote "$suggested_assistant")"
  if [[ -n "$requested_base" ]]; then
    retry_cmd+=" --base $(shell_quote "$requested_base")"
  fi
  list_cmd="skills/amux/scripts/openclaw-dx.sh workspace list --project $(shell_quote "$project")"

  RESULT_OK=false
  RESULT_COMMAND="project.add"
  RESULT_STATUS="attention"
  RESULT_SUMMARY="Project registered, but workspace name is unavailable: $requested_name"
  RESULT_NEXT_ACTION="Create a workspace with a different name, or reuse an existing workspace for this project."
  RESULT_SUGGESTED_COMMAND="$retry_cmd"
  RESULT_DATA="$(jq -cn --arg project "$project" --arg requested_name "$requested_name" --arg alt_name "$alt_name" --arg assistant "$suggested_assistant" --arg base "$requested_base" --arg conflict_message "$conflict_message" '{project: $project, requested_name: $requested_name, alt_name: $alt_name, assistant: $assistant, base: $base, conflict_message: $conflict_message, reason: "workspace_name_conflict"}')"

  local actions='[]'
  actions="$(append_action "$actions" "retry_new_name" "New Name" "$retry_cmd" "success" "Create workspace with a safe alternate name")"
  actions="$(append_action "$actions" "list_ws" "Workspaces" "$list_cmd" "primary" "List existing project workspaces")"
  actions="$(append_action "$actions" "status" "Status" "skills/amux/scripts/openclaw-dx.sh status --project $(shell_quote "$project")" "primary" "Check project status before retrying")"
  RESULT_QUICK_ACTIONS="$actions"

  RESULT_MESSAGE="⚠️ Project registered, but workspace name is unavailable: $requested_name"$'\n'"Project: $project"
  if [[ -n "${conflict_message// }" ]]; then
    RESULT_MESSAGE+=$'\n'"Conflict: $conflict_message"
  fi
  RESULT_MESSAGE+=$'\n'"Next: $RESULT_NEXT_ACTION"

  emit_result
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
  if [[ "$scope" == "project" && -n "$from_workspace" ]]; then
    emit_error "workspace.create" "command_error" "--from-workspace requires --scope nested" "remove --from-workspace or set --scope nested"
    return
  fi
  if [[ "$scope" == "nested" && -n "$base" ]]; then
    emit_error "workspace.create" "command_error" "--base is not supported for nested workspaces" "nested workspaces always start from the project default branch"
    return
  fi

  if [[ -z "$project" && -n "$parent_repo" ]]; then
    project="$parent_repo"
  fi
  if [[ -z "$project" ]]; then
    project="$(context_resolve_project "")"
  fi
  local project_for_match parent_repo_for_match
  project_for_match="$(normalize_path_for_compare "$project")"
  parent_repo_for_match="$(normalize_path_for_compare "$parent_repo")"
  if [[ -n "$from_workspace" && -n "$project_for_match" && -n "$parent_repo_for_match" && "$project_for_match" != "$parent_repo_for_match" ]]; then
    local retry_without_project retry_with_parent_repo
    retry_without_project="skills/amux/scripts/openclaw-dx.sh workspace create --name $(shell_quote "$name") --from-workspace $(shell_quote "$from_workspace") --scope $(shell_quote "$scope")"
    retry_with_parent_repo="skills/amux/scripts/openclaw-dx.sh workspace create --name $(shell_quote "$name") --from-workspace $(shell_quote "$from_workspace") --project $(shell_quote "$parent_repo") --scope $(shell_quote "$scope")"
    if [[ -n "$assistant" ]]; then
      retry_without_project+=" --assistant $(shell_quote "$assistant")"
      retry_with_parent_repo+=" --assistant $(shell_quote "$assistant")"
    fi

    RESULT_OK=false
    RESULT_COMMAND="workspace.create"
    RESULT_STATUS="command_error"
    RESULT_SUMMARY="--project does not match --from-workspace repository"
    RESULT_NEXT_ACTION="Use the same repository as --from-workspace, or remove --project."
    RESULT_SUGGESTED_COMMAND="$retry_without_project"
    RESULT_DATA="$(jq -cn --arg from_workspace "$from_workspace" --arg parent_repo "$parent_repo" --arg requested_project "$project" --arg retry_without_project "$retry_without_project" --arg retry_with_parent_repo "$retry_with_parent_repo" '{error: {from_workspace: $from_workspace, parent_repo: $parent_repo, requested_project: $requested_project}, retry_without_project: $retry_without_project, retry_with_parent_repo: $retry_with_parent_repo, reason: "project_workspace_mismatch"}')"
    local actions='[]'
    actions="$(append_action "$actions" "retry_without_project" "Retry (No Project)" "$retry_without_project" "success" "Use parent workspace repository implicitly")"
    actions="$(append_action "$actions" "retry_with_parent_repo" "Retry (Parent Repo)" "$retry_with_parent_repo" "primary" "Use the parent workspace repository explicitly")"
    RESULT_QUICK_ACTIONS="$actions"
    RESULT_MESSAGE="🛑 --project does not match --from-workspace repository"$'\n'"from-workspace $from_workspace belongs to $parent_repo"$'\n'"--project was $project"$'\n'"Next: $RESULT_NEXT_ACTION"
    emit_result
    return
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
    if [[ "$err_code" == "project_not_registered" ]]; then
      local missing_project register_cmd retry_cmd
      missing_project="$(jq -r '.error.details.project // ""' <<<"$err_payload")"
      if [[ -z "${missing_project// }" ]]; then
        missing_project="$project"
      fi

      local auto_registered_project retry_out retry_args
      if auto_registered_project="$(ensure_project_registered "$missing_project")"; then
        retry_args=(workspace create "$final_name" --project "$missing_project")
        if [[ -n "$assistant" ]]; then
          retry_args+=(--assistant "$assistant")
        fi
        if [[ -n "$base" ]]; then
          retry_args+=(--base "$base")
        fi
        if retry_out="$(amux_ok_json "${retry_args[@]}")"; then
          out="$retry_out"
          project="$missing_project"
          if jq -e . >/dev/null 2>&1 <<<"$auto_registered_project"; then
            context_set_project "$missing_project" "$(jq -r '.name // ""' <<<"$auto_registered_project")"
          fi
        else
          err_payload="$AMUX_ERROR_OUTPUT"
          err_code=""
          err_message=""
          if jq -e . >/dev/null 2>&1 <<<"$err_payload"; then
            err_code="$(jq -r '.error.code // ""' <<<"$err_payload")"
            err_message="$(jq -r '.error.message // ""' <<<"$err_payload")"
          fi
        fi
      fi

      if ! jq -e '.ok == true' >/dev/null 2>&1 <<<"$out"; then
        register_cmd="skills/amux/scripts/openclaw-dx.sh project add --path $(shell_quote "$missing_project")"
        retry_cmd="skills/amux/scripts/openclaw-dx.sh workspace create --name $(shell_quote "$name") --project $(shell_quote "$missing_project") --scope $(shell_quote "$scope") --assistant $(shell_quote "${assistant:-codex}")"
        if [[ -n "$from_workspace" ]]; then
          retry_cmd="skills/amux/scripts/openclaw-dx.sh workspace create --name $(shell_quote "$name") --from-workspace $(shell_quote "$from_workspace") --scope $(shell_quote "$scope") --assistant $(shell_quote "${assistant:-codex}")"
        fi
        if [[ -n "$base" ]]; then
          retry_cmd+=" --base $(shell_quote "$base")"
        fi

        RESULT_OK=false
        RESULT_COMMAND="workspace.create"
        RESULT_STATUS="attention"
        RESULT_SUMMARY="Project is not registered: $missing_project"
        RESULT_NEXT_ACTION="Register the project, then retry workspace creation."
        RESULT_SUGGESTED_COMMAND="$register_cmd"
        RESULT_DATA="$(jq -cn --arg project "$missing_project" --arg retry_command "$retry_cmd" --arg register_command "$register_cmd" --arg code "$err_code" --arg message "$err_message" '{project: $project, retry_command: $retry_command, register_command: $register_command, error: {code: $code, message: $message}, reason: "project_not_registered"}')"
        local actions='[]'
        actions="$(append_action "$actions" "add_project" "Add Project" "$register_cmd" "success" "Register this project in amux")"
        actions="$(append_action "$actions" "retry_create" "Retry Create" "$retry_cmd" "primary" "Retry workspace creation after project registration")"
        RESULT_QUICK_ACTIONS="$actions"
        RESULT_MESSAGE="⚠️ Project is not registered: $missing_project"$'\n'"Next: $RESULT_NEXT_ACTION"
        emit_result
        return
      fi
    fi
    if ! jq -e '.ok == true' >/dev/null 2>&1 <<<"$out"; then
      if [[ "$err_code" == "create_failed" ]] && [[ "$err_message" == *"already exists"* || "$err_message" == *"already used by worktree"* ]]; then
        if workspace_create_emit_existing_recovery "$project" "$final_name" "$scope" "$assistant" "$err_message"; then
          return
        fi

        local alt_name retry_cmd list_cmd
        alt_name="$(workspace_conflict_alt_name "$name")"
        retry_cmd="skills/amux/scripts/openclaw-dx.sh workspace create --name $(shell_quote "$alt_name") --project $(shell_quote "$project") --scope $(shell_quote "$scope") --assistant $(shell_quote "${assistant:-codex}")"
        if [[ -n "$from_workspace" ]]; then
          retry_cmd="skills/amux/scripts/openclaw-dx.sh workspace create --name $(shell_quote "$alt_name") --from-workspace $(shell_quote "$from_workspace") --scope $(shell_quote "$scope") --assistant $(shell_quote "${assistant:-codex}")"
        fi
        if [[ -n "$base" ]]; then
          retry_cmd+=" --base $(shell_quote "$base")"
        fi
        list_cmd="skills/amux/scripts/openclaw-dx.sh workspace list --project $(shell_quote "$project")"

        RESULT_OK=false
        RESULT_COMMAND="workspace.create"
        RESULT_STATUS="attention"
        RESULT_SUMMARY="Workspace name is unavailable: $final_name"
        RESULT_NEXT_ACTION="Retry workspace creation with a different name."
        RESULT_SUGGESTED_COMMAND="$retry_cmd"
        RESULT_DATA="$(jq -cn --arg project "$project" --arg requested_name "$name" --arg requested_final_name "$final_name" --arg alt_name "$alt_name" --arg scope "$scope" --arg parent_workspace "$from_workspace" --arg error_message "$err_message" '{project: $project, requested_name: $requested_name, requested_final_name: $requested_final_name, alt_name: $alt_name, scope: $scope, parent_workspace: $parent_workspace, error: {code: "create_failed", message: $error_message}, reason: "workspace_name_conflict"}')"
        local actions='[]'
        actions="$(append_action "$actions" "retry_new_name" "New Name" "$retry_cmd" "success" "Retry workspace creation with a new name")"
        actions="$(append_action "$actions" "list_ws" "Workspaces" "$list_cmd" "primary" "List existing workspaces for this project")"
        RESULT_QUICK_ACTIONS="$actions"
        RESULT_MESSAGE="⚠️ Workspace name is unavailable: $final_name"$'\n'"Project: $project"$'\n'"Conflict: $err_message"$'\n'"Next: $RESULT_NEXT_ACTION"
        emit_result
        return
      fi
      emit_amux_error "workspace.create" "$err_payload"
      return
    fi
  fi

  local ws_id ws_root assistant_out ws_base scope_label scope_title workspace_label parent_workspace_label
  ws_id="$(jq -r '.data.id // ""' <<<"$out")"
  ws_root="$(jq -r '.data.root // ""' <<<"$out")"
  assistant_out="$(jq -r '.data.assistant // ""' <<<"$out")"
  ws_base="$(jq -r '.data.base // ""' <<<"$out")"
  scope_label="$(workspace_scope_label "$scope")"
  scope_title="$(workspace_scope_title "$scope")"
  workspace_label="$(workspace_brief_label "$ws_id" "$final_name" "$scope_label" "$from_workspace")"
  parent_workspace_label=""
  if [[ -n "$from_workspace" ]]; then
    parent_workspace_label="$(workspace_label_for_id "$from_workspace")"
  fi
  context_set_workspace "$ws_id" "$final_name" "$project" "$assistant_out" "$scope" "$from_workspace" "$parent_name"
  context_set_workspace_lineage "$ws_id" "$scope" "$from_workspace" "$parent_name"

  RESULT_OK=true
  RESULT_COMMAND="workspace.create"
  RESULT_STATUS="ok"
  RESULT_SUMMARY="$scope_title ready: $workspace_label"
  RESULT_NEXT_ACTION="Start coding in this workspace, or run terminal setup commands."
  RESULT_SUGGESTED_COMMAND="skills/amux/scripts/openclaw-dx.sh start --workspace $(shell_quote "$ws_id") --assistant $(shell_quote "${assistant_out:-codex}") --prompt \"Analyze the biggest debt item and implement the fix.\""

  local actions='[]'
  actions="$(append_action "$actions" "start" "Start" "$RESULT_SUGGESTED_COMMAND" "success" "Start a coding turn in this workspace")"
  actions="$(append_action "$actions" "term" "Terminal" "skills/amux/scripts/openclaw-dx.sh terminal run --workspace $(shell_quote "$ws_id") --text \"pwd\" --enter" "primary" "Run a terminal command in this workspace")"
  actions="$(append_action "$actions" "status" "Status" "skills/amux/scripts/openclaw-dx.sh status --workspace $(shell_quote "$ws_id")" "primary" "Show workspace status")"
  RESULT_QUICK_ACTIONS="$actions"

  RESULT_DATA="$(jq -cn \
    --arg scope "$scope" \
    --arg scope_label "$scope_label" \
    --arg requested_name "$name" \
    --arg final_name "$final_name" \
    --arg parent_workspace "$from_workspace" \
    --arg parent_workspace_label "$parent_workspace_label" \
    --arg parent_name "$parent_name" \
    --arg base "$ws_base" \
    --arg workspace_label "$workspace_label" \
    --argjson workspace "$(jq -c '.data' <<<"$out")" \
    '{
      scope: $scope,
      scope_label: $scope_label,
      requested_name: $requested_name,
      final_name: $final_name,
      parent_workspace: $parent_workspace,
      parent_workspace_label: $parent_workspace_label,
      parent_name: $parent_name,
      base: $base,
      workspace_label: $workspace_label,
      base_behavior: "starts from project default branch unless --base is explicitly set on project scope",
      workspace: $workspace
    }')"

  RESULT_MESSAGE="✅ $scope_title ready: $workspace_label"$'\n'"Name: $final_name"$'\n'"Type: $scope_label"$'\n'"Project: $project"
  if [[ -n "$from_workspace" ]]; then
    RESULT_MESSAGE+=$'\n'"Parent workspace: ${parent_workspace_label:-$from_workspace}"
  fi
  if [[ -n "$ws_base" ]]; then
    RESULT_MESSAGE+=$'\n'"Base branch: $ws_base"
  fi
  RESULT_MESSAGE+=$'\n'"Root: $ws_root"$'\n'"Next: $RESULT_NEXT_ACTION"
  emit_result
}

cmd_workspace_list() {
  local project=""
  local workspace_id=""
  local limit=20
  local page=1
  local all_projects=false

  while [[ $# -gt 0 ]]; do
    case "$1" in
      --project)
        project="$2"; shift 2 ;;
      --all)
        all_projects=true; shift ;;
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
  if [[ "$all_projects" == "true" && -n "$project" ]]; then
    emit_error "workspace.list" "command_error" "--all conflicts with --project" "use either --all or --project <repo>"
    return
  fi

  local project_from_context=false
  if [[ "$all_projects" != "true" && -z "$project" ]]; then
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
    if [[ "$(jq -r 'length' <<<"$ws_json")" -eq 0 ]]; then
      emit_error "workspace.list" "command_error" "workspace not found" "$workspace_id"
      return
    fi
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

  local enriched sorted preview count lines project_scope_count nested_scope_count
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
  enriched="$(workspace_enrich_scope_json "$enriched")"
  sorted="$(jq -c 'sort_by(.created) | reverse' <<<"$enriched")"
  count="$(jq -r 'length' <<<"$sorted")"
  project_scope_count="$(jq -r '[.[] | select(.scope == "project")] | length' <<<"$sorted")"
  nested_scope_count="$(jq -r '[.[] | select(.scope == "nested")] | length' <<<"$sorted")"
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

  lines="$(jq -r --argjson offset "$offset" '
    . | to_entries
    | map(
      "\(($offset + .key + 1)). \(.value.id)  \(.value.name)  [\(.value.scope_label)\(if (.value.parent_workspace // "") != "" then " <- " + .value.parent_workspace else "" end)]  (a:\(.value.agent_count), t:\(.value.terminal_count))"
    )
    | join("\n")
  ' <<<"$preview")"
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
  RESULT_SUMMARY="$count workspace(s): $project_scope_count project, $nested_scope_count nested"
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
  local workspace_filter_label=""
  if [[ -n "$workspace_id" ]]; then
    local selected_ws
    selected_ws="$(jq -c '.[0] // null' <<<"$preview")"
    if [[ "$selected_ws" != "null" ]]; then
      local selected_name selected_repo selected_assistant
      local selected_scope selected_scope_source selected_parent selected_parent_name selected_scope_label
      selected_name="$(jq -r '.name // ""' <<<"$selected_ws")"
      selected_repo="$(jq -r '.repo // ""' <<<"$selected_ws")"
      selected_assistant="$(jq -r '.assistant // ""' <<<"$selected_ws")"
      selected_scope="$(jq -r '.scope // ""' <<<"$selected_ws")"
      selected_scope_source="$(jq -r '.scope_source // ""' <<<"$selected_ws")"
      selected_parent="$(jq -r '.parent_workspace // ""' <<<"$selected_ws")"
      selected_parent_name="$(jq -r '.parent_name // ""' <<<"$selected_ws")"
      selected_scope_label="$(workspace_scope_label "$selected_scope")"
      workspace_filter_label="$(workspace_brief_label "$workspace_id" "$selected_name" "$selected_scope_label" "$selected_parent")"
      context_set_workspace "$workspace_id" "$selected_name" "$selected_repo" "$selected_assistant" "$selected_scope" "$selected_parent" "$selected_parent_name"
      context_set_workspace_lineage_if_authoritative "$workspace_id" "$selected_scope" "$selected_parent" "$selected_parent_name" "$selected_scope_source"
    fi
  elif [[ "$count" -eq 1 ]]; then
    local only_ws
    only_ws="$(jq -c '.[0] // null' <<<"$preview")"
    if [[ "$only_ws" != "null" ]]; then
      local only_id only_name only_repo only_assistant only_scope only_scope_source only_parent only_parent_name
      only_id="$(jq -r '.id // ""' <<<"$only_ws")"
      only_name="$(jq -r '.name // ""' <<<"$only_ws")"
      only_repo="$(jq -r '.repo // ""' <<<"$only_ws")"
      only_assistant="$(jq -r '.assistant // ""' <<<"$only_ws")"
      only_scope="$(jq -r '.scope // ""' <<<"$only_ws")"
      only_scope_source="$(jq -r '.scope_source // ""' <<<"$only_ws")"
      only_parent="$(jq -r '.parent_workspace // ""' <<<"$only_ws")"
      only_parent_name="$(jq -r '.parent_name // ""' <<<"$only_ws")"
      context_set_workspace "$only_id" "$only_name" "$only_repo" "$only_assistant" "$only_scope" "$only_parent" "$only_parent_name"
      context_set_workspace_lineage_if_authoritative "$only_id" "$only_scope" "$only_parent" "$only_parent_name" "$only_scope_source"
    fi
  fi

  local actions='[]'
  if [[ -n "$first_ws" ]]; then
    actions="$(append_action "$actions" "start" "Start #1" "$RESULT_SUGGESTED_COMMAND" "success" "Start coding in the first listed workspace")"
    actions="$(append_action "$actions" "status" "Status #1" "skills/amux/scripts/openclaw-dx.sh status --workspace $(shell_quote "$first_ws")" "primary" "Check status for the first listed workspace")"
  fi
  local list_cmd_base="skills/amux/scripts/openclaw-dx.sh workspace list --limit $limit"
  if [[ "$all_projects" == "true" ]]; then
    list_cmd_base+=" --all"
  elif [[ -n "$project" ]]; then
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
  if [[ "$project_from_context" == "true" ]]; then
    actions="$(append_action "$actions" "all_projects" "All Projects" "skills/amux/scripts/openclaw-dx.sh workspace list --all --limit $(shell_quote "$limit")" "primary" "List workspaces across all projects")"
  fi
  actions="$(append_action "$actions" "global" "Global" "skills/amux/scripts/openclaw-dx.sh status" "primary" "Show global coding status")"
  RESULT_QUICK_ACTIONS="$actions"

  RESULT_DATA="$(jq -cn --arg project "$project" --arg workspace_filter_id "$workspace_id" --arg workspace_filter_label "$workspace_filter_label" --argjson project_from_context "$project_from_context" --argjson all_projects "$all_projects" --argjson count "$count" --argjson page "$page" --argjson limit "$limit" --argjson total_pages "$total_pages" --argjson has_prev "$has_prev" --argjson has_next "$has_next" --argjson project_scope_count "$project_scope_count" --argjson nested_scope_count "$nested_scope_count" --argjson workspaces "$sorted" --argjson workspaces_page "$preview" '{
    project: $project,
    project_from_context: $project_from_context,
    all_projects: $all_projects,
    count: $count,
    page: $page,
    limit: $limit,
    total_pages: $total_pages,
    has_prev: $has_prev,
    has_next: $has_next,
    project_scope_count: $project_scope_count,
    nested_scope_count: $nested_scope_count,
    workspace_filter: (
      if ($workspace_filter_id | length) > 0 then
        {id: $workspace_filter_id, label: (if ($workspace_filter_label | length) > 0 then $workspace_filter_label else $workspace_filter_id end)}
      else
        null
      end
    ),
    workspaces: $workspaces,
    workspaces_page: $workspaces_page
  }')"

  RESULT_MESSAGE="✅ $count workspace(s)"
  if [[ "$count" -gt 0 ]]; then
    RESULT_MESSAGE+=$'\n'"Page: $page/$total_pages"
  fi
  RESULT_MESSAGE+=$'\n'"Types: project=$project_scope_count nested=$nested_scope_count"
  if [[ "$all_projects" == "true" ]]; then
    RESULT_MESSAGE+=$'\n'"Project: all projects"
  elif [[ "$project_from_context" == "true" ]]; then
    RESULT_MESSAGE+=$'\n'"Project: $project (from active context)"
  elif [[ -n "$project" ]]; then
    RESULT_MESSAGE+=$'\n'"Project: $project"
  fi
  if [[ -n "$workspace_id" ]]; then
    RESULT_MESSAGE+=$'\n'"Workspace filter: ${workspace_filter_label:-$workspace_id}"
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
  local parent_selection_reason="none"
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
    parent_selection_reason="explicit_parent"
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
  local ws_scoped_json
  ws_scoped_json="$(workspace_enrich_scope_json "$ws_json")"

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
    local context_json context_workspace_id context_workspace_repo context_workspace_repo_for_match project_for_match
    context_json="$(context_read_json)"
    context_workspace_id="$(jq -r '.workspace.id // ""' <<<"$context_json")"
    context_workspace_repo="$(jq -r '.workspace.repo // ""' <<<"$context_json")"
    context_workspace_repo_for_match="$(normalize_path_for_compare "$context_workspace_repo")"
    project_for_match="$(normalize_path_for_compare "$project")"

    if [[ -n "${context_workspace_id// }" ]]; then
      local context_workspace_row
      context_workspace_row="$(jq -c --arg id "$context_workspace_id" '
        map(select((.id // "") == $id))
        | .[0] // empty
      ' <<<"$ws_json")"
      if [[ -n "${context_workspace_row// }" ]]; then
        if [[ -z "${project_for_match// }" || -z "${context_workspace_repo_for_match// }" || "$project_for_match" == "$context_workspace_repo_for_match" ]]; then
          parent_id="$(jq -r '.id // ""' <<<"$context_workspace_row")"
          parent_name="$(jq -r '.name // ""' <<<"$context_workspace_row")"
          parent_selection_reason="context_workspace"
        fi
      fi
    fi

    if [[ -z "$parent_id" ]]; then
      local project_parent_row
      project_parent_row="$(jq -c '
        map(select((.scope // "") == "project"))
        | sort_by(.created // .name // "")
        | reverse
        | .[0] // empty
      ' <<<"$ws_scoped_json")"
      if [[ -n "${project_parent_row// }" ]]; then
        parent_id="$(jq -r '.id // ""' <<<"$project_parent_row")"
        parent_name="$(jq -r '.name // ""' <<<"$project_parent_row")"
        parent_selection_reason="project_workspace"
      fi
    fi

    if [[ -z "$parent_id" ]]; then
      parent_id="$(jq -r '.[0].id // ""' <<<"$ws_scoped_json")"
      parent_name="$(jq -r '.[0].name // ""' <<<"$ws_scoped_json")"
      if [[ -n "$parent_id" ]]; then
        parent_selection_reason="first_available"
      fi
    fi

    if [[ -z "$parent_id" ]]; then
      recommendation="project"
      reason="Nested workspace requested but no parent workspace exists yet."
      parent_selection_reason="none"
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
  RESULT_SUMMARY="Recommended workspace type: $(workspace_scope_label "$recommendation")"
  RESULT_NEXT_ACTION="Create the recommended workspace and start coding."
  RESULT_SUGGESTED_COMMAND="$recommended_command"

  local actions='[]'
  actions="$(append_action "$actions" "recommended" "Recommended" "$recommended_command" "success" "Create recommended workspace and start coding")"
  actions="$(append_action "$actions" "project_ws" "Project WS" "$project_create_cmd" "primary" "Create a project workspace")"
  if [[ -n "$nested_create_cmd" ]]; then
    actions="$(append_action "$actions" "nested_ws" "Nested WS" "$nested_create_cmd" "primary" "Create a nested workspace")"
  fi
  if [[ -n "$alternate_command" ]]; then
    actions="$(append_action "$actions" "alternate" "Alternate" "$alternate_command" "primary" "Run the alternate kickoff flow")"
  fi
  RESULT_QUICK_ACTIONS="$actions"

  local parent_workspace_label=""
  if [[ -n "$parent_id" ]]; then
    parent_workspace_label="$(workspace_label_for_id "$parent_id")"
  fi

  RESULT_DATA="$(jq -cn \
    --arg recommendation "$recommendation" \
    --arg recommendation_label "$(workspace_scope_label "$recommendation")" \
    --arg reason "$reason" \
    --arg parent_selection_reason "$parent_selection_reason" \
    --arg project "$project" \
    --arg parent_workspace "$parent_id" \
    --arg parent_workspace_label "$parent_workspace_label" \
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
      recommendation_label: $recommendation_label,
      reason: $reason,
      parent_selection_reason: $parent_selection_reason,
      project: $project,
      parent_workspace: $parent_workspace,
      parent_workspace_label: $parent_workspace_label,
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

  RESULT_MESSAGE="✅ Workspace decision: $(workspace_scope_title "$recommendation")"$'\n'"Reason: $reason"$'\n'"Project: $project"
  if [[ -n "$parent_id" ]]; then
    RESULT_MESSAGE+=$'\n'"Parent workspace: ${parent_workspace_label:-$parent_id}"
    if [[ "$parent_selection_reason" == "context_workspace" ]]; then
      RESULT_MESSAGE+=$'\n'"Parent source: active workspace context"
    elif [[ "$parent_selection_reason" == "project_workspace" ]]; then
      RESULT_MESSAGE+=$'\n'"Parent source: latest project workspace"
    fi
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

  local start_command_error_retry
  start_command_error_retry="${OPENCLAW_DX_START_COMMAND_ERROR_RETRY:-true}"
  if [[ "$start_command_error_retry" != "false" ]] && jq -e . >/dev/null 2>&1 <<<"$turn_json"; then
    local start_retry_status start_retry_summary
    start_retry_status="$(jq -r '.overall_status // .status // ""' <<<"$turn_json")"
    start_retry_summary="$(jq -r '.summary // ""' <<<"$turn_json")"
    if [[ "$start_retry_status" == "command_error" || "$start_retry_status" == "partial" ]] \
      && [[ "$start_retry_summary" == *"amux command failed"* ]]; then
      local start_retry_json
      start_retry_json="$(OPENCLAW_TURN_SKIP_PRESENT=true "$TURN_SCRIPT" run \
        --workspace "$workspace" \
        --assistant "$assistant" \
        --prompt "$prompt" \
        --max-steps "$max_steps" \
        --turn-budget "$turn_budget" \
        --wait-timeout "$wait_timeout" \
        --idle-threshold "$idle_threshold" 2>&1 || true)"
      if jq -e . >/dev/null 2>&1 <<<"$start_retry_json"; then
        turn_json="$start_retry_json"
      fi
    fi
  fi

  local start_prune_retry start_prune_older_than
  start_prune_retry="${OPENCLAW_DX_START_PRUNE_RETRY:-true}"
  start_prune_older_than="${OPENCLAW_DX_START_PRUNE_OLDER_THAN:-2m}"
  if [[ "$start_prune_retry" != "false" ]] && jq -e . >/dev/null 2>&1 <<<"$turn_json"; then
    local start_prune_status start_prune_summary
    start_prune_status="$(jq -r '.overall_status // .status // ""' <<<"$turn_json")"
    start_prune_summary="$(jq -r '.summary // ""' <<<"$turn_json")"
    if [[ "$start_prune_status" == "command_error" || "$start_prune_status" == "partial" ]] \
      && [[ "$start_prune_summary" == *"amux command failed"* ]]; then
      amux_ok_json session prune --older-than "$start_prune_older_than" --yes >/dev/null 2>&1 || true
      local start_prune_retry_json
      start_prune_retry_json="$(OPENCLAW_TURN_SKIP_PRESENT=true "$TURN_SCRIPT" run \
        --workspace "$workspace" \
        --assistant "$assistant" \
        --prompt "$prompt" \
        --max-steps "$max_steps" \
        --turn-budget "$turn_budget" \
        --wait-timeout "$wait_timeout" \
        --idle-threshold "$idle_threshold" 2>&1 || true)"
      if jq -e . >/dev/null 2>&1 <<<"$start_prune_retry_json"; then
        turn_json="$start_prune_retry_json"
      fi
    fi
  fi

  local start_step_fallback
  start_step_fallback="${OPENCLAW_DX_START_STEP_FALLBACK:-true}"
  if [[ "$start_step_fallback" != "false" ]] \
    && [[ -x "$STEP_SCRIPT_PATH" ]] \
    && jq -e . >/dev/null 2>&1 <<<"$turn_json"; then
    local start_fallback_status start_fallback_summary
    start_fallback_status="$(jq -r '.overall_status // .status // ""' <<<"$turn_json")"
    start_fallback_summary="$(jq -r '.summary // ""' <<<"$turn_json")"
    if [[ "$start_fallback_status" == "command_error" || "$start_fallback_status" == "partial" ]] \
      && [[ "$start_fallback_summary" == *"amux command failed"* ]]; then
      local start_step_json start_step_ok start_step_status
      start_step_json="$("$STEP_SCRIPT_PATH" run \
        --workspace "$workspace" \
        --assistant "$assistant" \
        --prompt "$prompt" \
        --wait-timeout "$wait_timeout" \
        --idle-threshold "$idle_threshold" 2>&1 || true)"
      if jq -e . >/dev/null 2>&1 <<<"$start_step_json"; then
        start_step_ok="$(jq -r '.ok // false' <<<"$start_step_json")"
        start_step_status="$(jq -r '.overall_status // .status // ""' <<<"$start_step_json")"
        if [[ "$start_step_ok" == "true" && "$start_step_status" != "command_error" && "$start_step_status" != "agent_error" ]]; then
          turn_json="$start_step_json"
        fi
      fi
    fi
  fi

  local start_command_error_fallback_assistant
  start_command_error_fallback_assistant="${OPENCLAW_DX_START_COMMAND_ERROR_FALLBACK_ASSISTANT:-gemini}"
  if jq -e . >/dev/null 2>&1 <<<"$turn_json"; then
    local start_error_retry_status start_error_retry_summary
    start_error_retry_status="$(jq -r '.overall_status // .status // ""' <<<"$turn_json")"
    start_error_retry_summary="$(jq -r '.summary // ""' <<<"$turn_json")"
    if [[ ("$start_error_retry_status" == "command_error" || "$start_error_retry_status" == "partial") \
      && "$start_error_retry_summary" == *"amux command failed"* \
      && -n "${start_command_error_fallback_assistant// }" \
      && "$start_command_error_fallback_assistant" != "$assistant" ]]; then
      local start_fallback_json
      start_fallback_json="$(OPENCLAW_TURN_SKIP_PRESENT=true "$TURN_SCRIPT" run \
        --workspace "$workspace" \
        --assistant "$start_command_error_fallback_assistant" \
        --prompt "$prompt" \
        --max-steps "$max_steps" \
        --turn-budget "$turn_budget" \
        --wait-timeout "$wait_timeout" \
        --idle-threshold "$idle_threshold" 2>&1 || true)"
      if jq -e . >/dev/null 2>&1 <<<"$start_fallback_json"; then
        local start_fallback_ok start_fallback_status
        start_fallback_ok="$(jq -r '.ok // false' <<<"$start_fallback_json")"
        start_fallback_status="$(jq -r '.overall_status // .status // ""' <<<"$start_fallback_json")"
        if [[ "$start_fallback_ok" == "true" && "$start_fallback_status" != "command_error" && "$start_fallback_status" != "agent_error" ]]; then
          turn_json="$start_fallback_json"
        fi
      fi
    fi
  fi

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

  emit_turn_passthrough "start" "coding_turn" "$turn_json" "$workspace" "$assistant"
}

cmd_continue() {
  local agent=""
  local workspace=""
  local workspace_from_flag=false
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
        workspace="$2"
        workspace_from_flag=true
        shift 2 ;;
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

  if [[ -n "$start_assistant" && "$auto_start" != "true" ]]; then
    emit_error "continue" "command_error" "--assistant requires --auto-start" "pass --auto-start when selecting an assistant for fallback start"
    return
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
        local first_agent first_workspace lines lines_header
        local preferred_project preferred_project_for_match same_project_count
        local workspace_meta='{}'
        local prioritized_active_agents_json
        preferred_project="$(context_resolve_project "")"
        preferred_project_for_match="$(normalize_path_for_compare "$preferred_project")"
        workspace_meta="$(workspace_scope_metadata_map)"
        prioritized_active_agents_json="$(jq -c --arg preferred_repo "$preferred_project_for_match" --argjson meta "$workspace_meta" '
          if ($preferred_repo | length) == 0 then
            .
          else
            map(. + {
              __same_repo: (
                (($meta[(.workspace_id // "")].repo_compare // "") as $repo
                  | ($repo == $preferred_repo))
              )
            })
            | sort_by((if .__same_repo then 0 else 1 end))
            | map(del(.__same_repo))
          end
        ' <<<"$active_agents_json")"
        same_project_count="$(jq -r --arg preferred_repo "$preferred_project_for_match" --argjson meta "$workspace_meta" '
          if ($preferred_repo | length) == 0 then
            0
          else
            [ .[] | select(
                (($meta[(.workspace_id // "")].repo_compare // "") as $repo
                  | ($repo == $preferred_repo)
                )
              ) ] | length
          end
        ' <<<"$prioritized_active_agents_json")"
        first_agent="$(jq -r '.[0].agent_id // ""' <<<"$prioritized_active_agents_json")"
        first_workspace="$(jq -r '.[0].workspace_id // ""' <<<"$prioritized_active_agents_json")"

        RESULT_OK=false
        RESULT_COMMAND="continue"
        RESULT_STATUS="attention"
        RESULT_SUMMARY="Multiple active agents found ($active_count)"
        if [[ -n "$preferred_project" && "$same_project_count" =~ ^[0-9]+$ ]] && [[ "$same_project_count" -gt 0 ]]; then
          RESULT_SUMMARY+="; $same_project_count in active project"
        fi
        RESULT_NEXT_ACTION="Choose one active agent to continue."
        RESULT_SUGGESTED_COMMAND=""
        if [[ -n "$first_agent" ]]; then
          RESULT_SUGGESTED_COMMAND="skills/amux/scripts/openclaw-dx.sh continue --agent $(shell_quote "$first_agent") --text \"Continue from current state and report status plus next action.\" --enter"
        elif [[ -n "$first_workspace" ]]; then
          RESULT_SUGGESTED_COMMAND="skills/amux/scripts/openclaw-dx.sh continue --workspace $(shell_quote "$first_workspace") --text \"Continue from current state and report status plus next action.\" --enter"
        fi

        local actions='[]'
        lines=""
        lines_header="⚠️ Multiple active agents found"
        if [[ "$active_count" -gt 6 ]]; then
          lines_header+=" (showing 6 of $active_count)"
        fi
        while IFS= read -r row; do
          [[ -z "${row// }" ]] && continue
          local row_index row_agent row_workspace row_session continue_cmd label
          local row_workspace_name row_scope_label row_parent row_workspace_label
          row_index="$(jq -r '.index // ""' <<<"$row")"
          row_agent="$(jq -r '.agent_id // ""' <<<"$row")"
          row_workspace="$(jq -r '.workspace_id // ""' <<<"$row")"
          row_session="$(jq -r '.session_name // ""' <<<"$row")"
          if [[ -z "$row_agent" ]]; then
            continue
          fi
          row_workspace_name="$(jq -r --arg wid "$row_workspace" --argjson meta "$workspace_meta" '$meta[$wid].name // ""' <<<"{}")"
          row_scope_label="$(jq -r --arg wid "$row_workspace" --argjson meta "$workspace_meta" '$meta[$wid].scope_label // ""' <<<"{}")"
          row_parent="$(jq -r --arg wid "$row_workspace" --argjson meta "$workspace_meta" '$meta[$wid].parent_workspace // ""' <<<"{}")"
          row_workspace_label="$(workspace_brief_label "$row_workspace" "$row_workspace_name" "$row_scope_label" "$row_parent")"
          continue_cmd="skills/amux/scripts/openclaw-dx.sh continue --agent $(shell_quote "$row_agent") --text \"Continue from current state and report status plus next action.\" --enter"
          label="Continue #$row_index"
          actions="$(append_action "$actions" "continue_${row_index}" "$label" "$continue_cmd" "primary" "Continue $row_agent in $row_workspace_label $row_session")"
          if [[ -n "${lines// }" ]]; then
            lines+=$'\n'
          fi
          lines+="$row_index. $row_agent ($row_workspace_label)"
        done < <(jq -c 'to_entries | map({index: (.key + 1), agent_id: (.value.agent_id // ""), workspace_id: (.value.workspace_id // ""), session_name: (.value.session_name // "")}) | .[0:6][]' <<<"$prioritized_active_agents_json")
        actions="$(append_action "$actions" "status" "Status" "skills/amux/scripts/openclaw-dx.sh status" "primary" "See all active agents and alerts")"
        if [[ -n "$preferred_project" ]]; then
          actions="$(append_action "$actions" "status_project" "Project Status" "skills/amux/scripts/openclaw-dx.sh status --project $(shell_quote "$preferred_project")" "primary" "Show active agents in the current project")"
        fi
        RESULT_QUICK_ACTIONS="$actions"

        RESULT_DATA="$(jq -cn --argjson active_count "$active_count" --arg preferred_project "$preferred_project" --argjson same_project_count "$same_project_count" --argjson agents "$prioritized_active_agents_json" --argjson workspace_details "$workspace_meta" '{reason: "multiple_active_agents", active_count: $active_count, preferred_project: $preferred_project, same_project_count: $same_project_count, agents: $agents, workspace_details: $workspace_details}')"
        RESULT_MESSAGE="$lines_header"$'\n'"$lines"$'\n'"Next: $RESULT_NEXT_ACTION"
        if [[ "$same_project_count" =~ ^[0-9]+$ ]] && [[ "$same_project_count" -gt 0 ]]; then
          RESULT_MESSAGE+=" (context-project agents shown first)"
        fi
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

      local workspace_context_row workspace_context_label
      local workspace_name_hint workspace_scope_hint workspace_parent_hint workspace_repo_hint workspace_repo_hint_for_match
      workspace_context_label="$workspace"
      workspace_context_row="$(workspace_context_payload_by_id "$workspace")"
      if [[ "${workspace_context_row:-null}" != "null" ]]; then
        workspace_name_hint="$(jq -r '.name // ""' <<<"$workspace_context_row")"
        workspace_scope_hint="$(jq -r '.scope_label // ""' <<<"$workspace_context_row")"
        workspace_parent_hint="$(jq -r '.parent_workspace // ""' <<<"$workspace_context_row")"
        workspace_repo_hint="$(jq -r '.repo // ""' <<<"$workspace_context_row")"
        workspace_context_label="$(workspace_brief_label "$workspace" "$workspace_name_hint" "$workspace_scope_hint" "$workspace_parent_hint")"
      fi
      workspace_repo_hint_for_match="$(normalize_path_for_compare "${workspace_repo_hint:-}")"

      local suggested_assistant
      suggested_assistant="$start_assistant"
      if [[ -z "$suggested_assistant" ]]; then
        suggested_assistant="$(default_assistant_for_workspace "$workspace")"
      fi
      if [[ -z "$suggested_assistant" ]]; then
        suggested_assistant="$(context_assistant_hint "$workspace")"
      fi
      if [[ -z "$suggested_assistant" ]]; then
        suggested_assistant="codex"
      fi

      local other_agents_query_failed=false
      local other_agents_query_error=""
      if [[ "$workspace_from_flag" != "true" ]]; then
        local other_agents_out other_agents_json prioritized_other_agents_json other_active_count same_repo_count
        local preview_agents_json preview_workspace_ids_json workspace_details_preview
        if other_agents_out="$(amux_ok_json agent list)"; then
          other_agents_json="$(jq -c '.data // []' <<<"$other_agents_out")"
          other_agents_json="$(jq -c --arg wid "$workspace" 'map(select((.workspace_id // "") != $wid))' <<<"$other_agents_json")"
          other_active_count="$(jq -r 'length' <<<"$other_agents_json")"
          if [[ "$other_active_count" =~ ^[0-9]+$ ]] && [[ "$other_active_count" -gt 0 ]]; then
            local first_other_agent first_other_workspace other_lines other_actions other_header
            local other_workspace_meta_all='{}'
            other_workspace_meta_all="$(workspace_scope_metadata_map)"
            prioritized_other_agents_json="$(jq -c --arg selected_repo "${workspace_repo_hint_for_match:-}" --argjson meta "$other_workspace_meta_all" '
              if ($selected_repo | length) == 0 then
                .
              else
                map(. + {
                  __same_repo: ((($meta[(.workspace_id // "")].repo_compare // "") as $repo
                    | ($repo == $selected_repo))
                  )
                })
                | sort_by((if .__same_repo then 0 else 1 end))
                | map(del(.__same_repo))
              end
            ' <<<"$other_agents_json")"
            same_repo_count="$(jq -r --arg selected_repo "${workspace_repo_hint_for_match:-}" --argjson meta "$other_workspace_meta_all" '
              if ($selected_repo | length) == 0 then
                0
              else
                [ .[] | select(
                    (($meta[(.workspace_id // "")].repo_compare // "") as $repo
                      | ($repo == $selected_repo)
                    )
                  ) ] | length
              end
            ' <<<"$prioritized_other_agents_json")"
            first_other_agent="$(jq -r '.[0].agent_id // ""' <<<"$prioritized_other_agents_json")"
            first_other_workspace="$(jq -r '.[0].workspace_id // ""' <<<"$prioritized_other_agents_json")"
            preview_agents_json="$(jq -c '.[0:6]' <<<"$prioritized_other_agents_json")"
            preview_workspace_ids_json="$(jq -c --arg selected "$workspace" '[.[].workspace_id // empty] + [$selected] | map(select(length > 0)) | unique' <<<"$preview_agents_json")"
            workspace_details_preview="$(jq -cn --argjson meta "$other_workspace_meta_all" --argjson ids "$preview_workspace_ids_json" '
              reduce $ids[] as $id ({}; if $meta[$id] then . + {($id): $meta[$id]} else . end)
            ')"

            other_actions='[]'
            other_lines=""
            while IFS= read -r row; do
              [[ -z "${row// }" ]] && continue
              local row_index row_agent row_workspace row_workspace_name row_scope_label row_parent row_workspace_label row_continue_cmd
              row_index="$(jq -r '.index // ""' <<<"$row")"
              row_agent="$(jq -r '.agent_id // ""' <<<"$row")"
              row_workspace="$(jq -r '.workspace_id // ""' <<<"$row")"
              [[ -z "$row_agent" ]] && continue
              row_workspace_name="$(jq -r --arg wid "$row_workspace" --argjson meta "$other_workspace_meta_all" '$meta[$wid].name // ""' <<<"{}")"
              row_scope_label="$(jq -r --arg wid "$row_workspace" --argjson meta "$other_workspace_meta_all" '$meta[$wid].scope_label // ""' <<<"{}")"
              row_parent="$(jq -r --arg wid "$row_workspace" --argjson meta "$other_workspace_meta_all" '$meta[$wid].parent_workspace // ""' <<<"{}")"
              row_workspace_label="$(workspace_brief_label "$row_workspace" "$row_workspace_name" "$row_scope_label" "$row_parent")"
              row_continue_cmd="skills/amux/scripts/openclaw-dx.sh continue --agent $(shell_quote "$row_agent") --text \"Continue from current state and report status plus next action.\" --enter"
              other_actions="$(append_action "$other_actions" "continue_${row_index}" "Continue #$row_index" "$row_continue_cmd" "primary" "Continue $row_agent in $row_workspace_label")"
              if [[ -n "${other_lines// }" ]]; then
                other_lines+=$'\n'
              fi
              other_lines+="$row_index. $row_agent ($row_workspace_label)"
            done < <(jq -c 'to_entries | map({index: (.key + 1), agent_id: (.value.agent_id // ""), workspace_id: (.value.workspace_id // "")}) | .[0:6][]' <<<"$prioritized_other_agents_json")
            other_actions="$(append_action "$other_actions" "auto_start" "Auto Start Here" "skills/amux/scripts/openclaw-dx.sh continue --workspace $(shell_quote "$workspace") --auto-start --assistant $(shell_quote "$suggested_assistant") --text \"Resume work and provide status plus next action.\"" "success" "Auto-start current workspace and continue")"
            other_actions="$(append_action "$other_actions" "status" "Status" "skills/amux/scripts/openclaw-dx.sh status" "primary" "Show all active agents and alerts")"

            RESULT_OK=false
            RESULT_COMMAND="continue"
            RESULT_STATUS="attention"
            RESULT_SUMMARY="No active agent in selected workspace; $other_active_count active in other workspace(s)"
            RESULT_NEXT_ACTION="Continue an active agent, or auto-start the selected workspace."
            if [[ -n "$first_other_agent" ]]; then
              RESULT_SUGGESTED_COMMAND="skills/amux/scripts/openclaw-dx.sh continue --agent $(shell_quote "$first_other_agent") --text \"Continue from current state and report status plus next action.\" --enter"
            elif [[ -n "$first_other_workspace" ]]; then
              RESULT_SUGGESTED_COMMAND="skills/amux/scripts/openclaw-dx.sh continue --workspace $(shell_quote "$first_other_workspace") --text \"Continue from current state and report status plus next action.\" --enter"
            else
              RESULT_SUGGESTED_COMMAND="skills/amux/scripts/openclaw-dx.sh status"
            fi
            RESULT_DATA="$(jq -cn --arg workspace "$workspace" --arg selected_workspace_label "$workspace_context_label" --arg selected_repo "${workspace_repo_hint:-}" --argjson active_count "$other_active_count" --argjson same_repo_count "$same_repo_count" --argjson agents "$preview_agents_json" --argjson agents_shown "$(jq -r 'length' <<<"$preview_agents_json")" --argjson agents_truncated "$(jq -r --argjson total "$other_active_count" '((length) < $total)' <<<"$preview_agents_json")" --argjson workspace_details "$workspace_details_preview" '{workspace: $workspace, selected_workspace_label: $selected_workspace_label, selected_repo: $selected_repo, reason: "no_active_agent_selected_workspace_has_others", active_count: $active_count, same_repo_count: $same_repo_count, agents: $agents, agents_shown: $agents_shown, agents_truncated: $agents_truncated, workspace_details: $workspace_details}')"
            RESULT_QUICK_ACTIONS="$other_actions"
            other_header="Active agents in other workspaces:"
            if [[ "$other_active_count" =~ ^[0-9]+$ ]] && [[ "$other_active_count" -gt 6 ]]; then
              other_header="Active agents in other workspaces (showing 6 of $other_active_count):"
            fi
            RESULT_MESSAGE="⚠️ No active agent in selected workspace: $workspace_context_label"$'\n'"$other_header"$'\n'"$other_lines"$'\n'"Next: $RESULT_NEXT_ACTION"
            if [[ "$same_repo_count" =~ ^[0-9]+$ ]] && [[ "$same_repo_count" -gt 0 ]]; then
              RESULT_MESSAGE+=" (same-project agents shown first)"
            fi
            emit_result
            return
          fi
        else
          other_agents_query_failed=true
          other_agents_query_error="${AMUX_ERROR_OUTPUT:-}"
        fi
      fi

      RESULT_OK=false
      RESULT_COMMAND="continue"
      RESULT_STATUS="attention"
      local auto_start_cmd
      auto_start_cmd="skills/amux/scripts/openclaw-dx.sh continue --workspace $(shell_quote "$workspace") --auto-start --assistant $(shell_quote "$suggested_assistant") --text \"Resume work and provide status plus next action.\""
      if [[ "$workspace_from_flag" != "true" && "$other_agents_query_failed" == "true" ]]; then
        RESULT_SUMMARY="No active agent in selected workspace; unable to query other active agents"
        RESULT_NEXT_ACTION="Check global status to pick an active agent, or auto-start this workspace."
        RESULT_SUGGESTED_COMMAND="skills/amux/scripts/openclaw-dx.sh status"
        RESULT_DATA="$(jq -cn --arg workspace "$workspace" --arg error "$other_agents_query_error" '{workspace: $workspace, reason: "no_active_agent_other_query_failed", error: $error}')"
      else
        RESULT_SUMMARY="No active agent found for workspace $workspace_context_label"
        RESULT_NEXT_ACTION="Start a new agent turn in this workspace, then continue. You can also use --auto-start."
        RESULT_SUGGESTED_COMMAND="$auto_start_cmd"
        RESULT_DATA="$(jq -cn --arg workspace "$workspace" '{workspace: $workspace, reason: "no_active_agent"}')"
      fi
      local start_cmd
      start_cmd="skills/amux/scripts/openclaw-dx.sh start --workspace $(shell_quote "$workspace") --assistant $(shell_quote "$suggested_assistant") --prompt \"Resume work and provide status plus next action.\""
      RESULT_QUICK_ACTIONS="$(jq -cn --arg auto_start_cmd "$auto_start_cmd" --arg start_cmd "$start_cmd" '
        [
          {id:"auto_start", label:"Auto Start", command:$auto_start_cmd, style:"success", prompt:"Auto-start and continue in one command"},
          {id:"start", label:"Start", command:$start_cmd, style:"primary", prompt:"Start a new coding turn"},
          {id:"status", label:"Status", command:"skills/amux/scripts/openclaw-dx.sh status", style:"primary", prompt:"Show global active-agent status"}
        ]')"
      RESULT_MESSAGE="⚠️ No active agent in workspace $workspace_context_label"
      if [[ "$workspace_from_flag" != "true" && "$other_agents_query_failed" == "true" ]]; then
        RESULT_MESSAGE+=$'\n'"Could not query active agents in other workspaces right now."
      fi
      RESULT_MESSAGE+=$'\n'"Next: $RESULT_NEXT_ACTION"
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

  local continue_auto_followup continue_auto_text
  continue_auto_followup="${OPENCLAW_DX_CONTINUE_NEEDS_INPUT_AUTO_CONTINUE:-true}"
  continue_auto_text="${OPENCLAW_DX_CONTINUE_NEEDS_INPUT_TEXT:-If a choice is required, pick the safest high-impact default, continue, and report status plus next action.}"
  if [[ "$continue_auto_followup" != "false" ]]; then
    local continue_status continue_agent
    continue_status="$(jq -r '.overall_status // .status // ""' <<<"$turn_json")"
    continue_agent="$(jq -r '.agent_id // ""' <<<"$turn_json")"
    if [[ "$continue_status" == "needs_input" && -n "${continue_agent// }" && -n "${continue_auto_text// }" ]]; then
      local continue_retry_args continue_retry_json continue_retry_ok continue_retry_status
      continue_retry_args=(
        "$TURN_SCRIPT" send
        --agent "$continue_agent"
        --text "$continue_auto_text"
        --max-steps "$max_steps"
        --turn-budget "$turn_budget"
        --wait-timeout "$wait_timeout"
        --idle-threshold "$idle_threshold"
        --enter
      )
      continue_retry_json="$(OPENCLAW_TURN_SKIP_PRESENT=true "${continue_retry_args[@]}" 2>&1 || true)"
      continue_retry_json="$(recover_timeout_turn_once "$continue_retry_json" "$wait_timeout" "$idle_threshold")"
      if jq -e . >/dev/null 2>&1 <<<"$continue_retry_json"; then
        continue_retry_ok="$(jq -r '.ok // false' <<<"$continue_retry_json")"
        continue_retry_status="$(jq -r '.overall_status // .status // ""' <<<"$continue_retry_json")"
        if [[ "$continue_retry_ok" == "true" && "$continue_retry_status" != "command_error" && "$continue_retry_status" != "agent_error" ]]; then
          turn_json="$continue_retry_json"
        fi
      fi
    fi
  fi

  emit_turn_passthrough "continue" "followup_turn" "$turn_json" "$workspace" ""
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
      capture_agents="${OPENCLAW_DX_STATUS_CAPTURE_AGENTS_WORKSPACE:-3}"
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
  ws_json="$(workspace_enrich_scope_json "$ws_json")"
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
  local workspace_order workspace_details selected_workspace_label=""
  workspace_order="$(jq -c 'sort_by(.created // "") | reverse | map(.id)' <<<"$ws_json")"
  agents_json="$(jq -c --argjson order "$workspace_order" 'sort_by(. as $a | (($order | index($a.workspace_id // "")) // 999999), ($a.session_name // ""))' <<<"$agents_json")"
  workspace_details="$(jq -c '
    map({
      key: (.id // ""),
      value: {
        workspace_id: (.id // ""),
        workspace_name: (.name // ""),
        workspace_scope: (.scope // ""),
        workspace_scope_label: (.scope_label // ""),
        parent_workspace: (.parent_workspace // ""),
        parent_name: (.parent_name // ""),
        workspace_label: (
          (.id // "")
          + (if (.name // "") != "" then " (" + (.name // "") + ")" else "" end)
          + (if (.scope_label // "") != "" then
              " ["
              + (.scope_label // "")
              + (if (.parent_workspace // "") != "" then " <- " + (.parent_workspace // "") else "" end)
              + "]"
            else "" end)
        )
      }
    })
    | from_entries
  ' <<<"$ws_json")"
  if [[ -n "$workspace" ]]; then
    selected_workspace_label="$(jq -r --arg id "$workspace" --argjson details "$workspace_details" '$details[$id].workspace_label // ""' <<<"{}")"
    if [[ -z "${selected_workspace_label// }" ]]; then
      selected_workspace_label="$workspace"
    fi
  fi

  local project_count workspace_count agent_count terminal_count session_count prune_total project_scope_count nested_scope_count
  project_count="$(jq -r '.data // [] | length' <<<"$projects_out")"
  workspace_count="$(jq -r 'length' <<<"$ws_json")"
  project_scope_count="$(jq -r '[.[] | select(.scope == "project")] | length' <<<"$ws_json")"
  nested_scope_count="$(jq -r '[.[] | select(.scope == "nested")] | length' <<<"$ws_json")"
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

    local agent_row agent_row_json agent_id workspace_id workspace_label
    agent_row="$(jq -c --arg s "$session_name" '.[] | select(.session_name == $s)' <<<"$agents_json" | head -n 1)"
    agent_row_json='{}'
    if [[ -n "${agent_row// }" ]]; then
      agent_row_json="$agent_row"
    fi
    agent_id="$(jq -r '.agent_id // ""' <<<"$agent_row_json")"
    workspace_id="$(jq -r '.workspace_id // ""' <<<"$agent_row_json")"
    workspace_label="$(jq -r --arg id "$workspace_id" --argjson details "$workspace_details" '$details[$id].workspace_label // ""' <<<"{}")"
    if [[ -z "${workspace_label// }" ]]; then
      workspace_label="$workspace_id"
    fi

    captures="$(jq -cn --argjson captures "$captures" --arg session "$session_name" --arg agent_id "$agent_id" --arg workspace_id "$workspace_id" --arg workspace_label "$workspace_label" --arg status "$capture_status" --arg summary "$capture_summary" --arg hint "$capture_hint" --argjson needs_input "$capture_needs_input" '$captures + [{session_name: $session, agent_id: $agent_id, workspace_id: $workspace_id, workspace_label: $workspace_label, status: $status, summary: $summary, needs_input: $needs_input, input_hint: $hint}]')"

    if [[ "$capture_needs_input" == "true" ]]; then
      alerts="$(jq -cn --argjson alerts "$alerts" --arg type "needs_input" --arg session "$session_name" --arg agent_id "$agent_id" --arg workspace_id "$workspace_id" --arg workspace_label "$workspace_label" --arg summary "$capture_summary" --arg input_hint "$capture_hint" '$alerts + [{type: $type, session_name: $session, agent_id: $agent_id, workspace_id: $workspace_id, workspace_label: $workspace_label, summary: $summary, input_hint: $input_hint}]')"
      continue
    fi

    if [[ "$capture_status" == "session_exited" ]]; then
      alerts="$(jq -cn --argjson alerts "$alerts" --arg type "session_exited" --arg session "$session_name" --arg agent_id "$agent_id" --arg workspace_id "$workspace_id" --arg workspace_label "$workspace_label" --arg summary "$capture_summary" '$alerts + [{type: $type, session_name: $session, agent_id: $agent_id, workspace_id: $workspace_id, workspace_label: $workspace_label, summary: $summary}]')"
      continue
    fi

    if completion_signal_present "$capture_summary"; then
      alerts="$(jq -cn --argjson alerts "$alerts" --arg type "completed" --arg session "$session_name" --arg agent_id "$agent_id" --arg workspace_id "$workspace_id" --arg workspace_label "$workspace_label" --arg summary "$capture_summary" '$alerts + [{type: $type, session_name: $session, agent_id: $agent_id, workspace_id: $workspace_id, workspace_label: $workspace_label, summary: $summary}]')"
    fi
  done < <(jq -r --argjson cap "$capture_agents" '.[:$cap][]?.session_name' <<<"$agents_json")

  if [[ "$include_stale_alerts" == "true" ]] && [[ -z "$workspace" ]] && [[ -z "$project" ]] && [[ "$prune_total" =~ ^[0-9]+$ ]] && [[ "$prune_total" -gt 0 ]]; then
    alerts="$(jq -cn --argjson alerts "$alerts" --arg older_than "$older_than" --argjson total "$prune_total" '$alerts + [{type: "stale_sessions", total: $total, older_than: $older_than}]')"
  fi
  alerts="$(jq -c '
    def alert_priority:
      if .type == "needs_input" then 0
      elif .type == "session_exited" then 1
      elif .type == "completed" then 2
      elif .type == "stale_sessions" then 3
      else 4 end;
    sort_by(alert_priority)
  ' <<<"$alerts")"

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

  local first_needs_input_agent first_completed_workspace first_completed_agent first_active_agent
  first_needs_input_agent="$(jq -r '.[] | select(.type == "needs_input") | .agent_id // empty' <<<"$alerts" | head -n 1)"
  first_completed_workspace="$(jq -r '.[] | select(.type == "completed") | .workspace_id // empty' <<<"$alerts" | head -n 1)"
  first_completed_agent="$(jq -r '.[] | select(.type == "completed") | .agent_id // empty' <<<"$alerts" | head -n 1)"
  first_active_agent="$(jq -r '.[] | .agent_id // empty | select(length > 0)' <<<"$captures" | head -n 1)"
  if [[ -n "$first_needs_input_agent" ]]; then
    next_action="Reply to the blocked agent prompt first."
    suggested_command="skills/amux/scripts/openclaw-dx.sh continue --agent $(shell_quote "$first_needs_input_agent") --text \"If a choice is required, pick the safest high-impact default, continue, and then report status plus next action.\" --enter"
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
  elif [[ -n "$workspace" && -n "$first_active_agent" ]]; then
    next_action="Continue the active agent in this workspace."
    suggested_command="skills/amux/scripts/openclaw-dx.sh continue --agent $(shell_quote "$first_active_agent") --text \"Status update and next action.\" --enter"
  fi
  if [[ "$alerts_only" == "true" && "$status" == "ok" ]]; then
    local full_status_cmd
    full_status_cmd="skills/amux/scripts/openclaw-dx.sh status"
    if [[ -n "$project" ]]; then
      full_status_cmd+=" --project $(shell_quote "$project")"
    fi
    if [[ -n "$workspace" ]]; then
      full_status_cmd+=" --workspace $(shell_quote "$workspace")"
    fi
    if [[ "$include_stale_alerts" == "true" ]]; then
      full_status_cmd+=" --include-stale"
    fi
    if [[ -n "$project" && -z "$workspace" && "$recent_workspaces" -gt 0 ]]; then
      full_status_cmd+=" --recent-workspaces $(shell_quote "$recent_workspaces")"
    fi
    next_action="Run full status view for workspace and agent details."
    suggested_command="$full_status_cmd"
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
  ws_lines="$(jq -r '
    . | map(
      "- \(.id) \(.name) [\(.scope_label)\(if (.parent_workspace // "") != "" then " <- " + .parent_workspace else "" end)] (a:\(.agent_count), t:\(.terminal_count))"
    ) | join("\n")
  ' <<<"$ws_preview")"

  local alert_lines
  alert_lines="$(jq -r --argjson limit "$limit" '.[:$limit] | map(
      if .type == "needs_input" then
        "- ❓ " + (.workspace_label // .workspace_id // "") + " " + (.agent_id // "") + ": " + (.summary // "needs input")
      elif .type == "session_exited" then
        "- 🛑 " + (.workspace_label // .workspace_id // "") + " " + (.agent_id // "") + ": session exited"
      elif .type == "completed" then
        "- ✅ " + (.workspace_label // .workspace_id // "") + " " + (.agent_id // "") + ": " + (.summary // "completed")
      elif .type == "stale_sessions" then
        "- 🧹 stale sessions: " + ((.total // 0) | tostring) + " older than " + (.older_than // "")
      else
        "- ⚠️ " + (.type // "alert")
      end
    ) | join("\n")' <<<"$alerts")"
  local agent_lines
  agent_lines="$(jq -r --argjson limit "$capture_agents" '
      [ .[] | select((.agent_id // "") != "") ][:$limit]
      | map(
          "- " + (.workspace_label // .workspace_id // "")
          + " " + (.agent_id // "")
          + ": "
          + ((.summary // .status // "active")
            | gsub("[\r\n]+"; " ")
            | .[0:120])
        )
      | join("\n")
    ' <<<"$captures")"

  local actions='[]'
  actions="$(append_action "$actions" "refresh" "Refresh" "$refresh_cmd" "primary" "Refresh agent/workspace status")"
  if [[ "$alerts_only" == "true" ]]; then
    local full_status_action
    full_status_action="skills/amux/scripts/openclaw-dx.sh status"
    if [[ -n "$project" ]]; then
      full_status_action+=" --project $(shell_quote "$project")"
    fi
    if [[ -n "$workspace" ]]; then
      full_status_action+=" --workspace $(shell_quote "$workspace")"
    fi
    if [[ "$include_stale_alerts" == "true" ]]; then
      full_status_action+=" --include-stale"
    fi
    if [[ -n "$project" && -z "$workspace" && "$recent_workspaces" -gt 0 ]]; then
      full_status_action+=" --recent-workspaces $(shell_quote "$recent_workspaces")"
    fi
    actions="$(append_action "$actions" "full_status" "Full Status" "$full_status_action" "success" "Show workspace/agent details beyond alerts-only output")"
  fi
  if [[ -n "$first_needs_input_agent" ]]; then
    actions="$(append_action "$actions" "reply" "Reply" "skills/amux/scripts/openclaw-dx.sh continue --agent $(shell_quote "$first_needs_input_agent") --text \"If a choice is required, pick the safest high-impact default, continue, and report status and blockers.\" --enter" "danger" "Reply to blocked agent")"
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
  if [[ "$needs_input_count" -eq 0 && -n "$workspace" && -n "$first_active_agent" ]]; then
    actions="$(append_action "$actions" "continue_agent" "Continue Agent" "skills/amux/scripts/openclaw-dx.sh continue --agent $(shell_quote "$first_active_agent") --text \"Status update and next action.\" --enter" "success" "Continue active agent in this workspace")"
  elif [[ -n "$first_ws" ]]; then
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
    --argjson counts "$(jq -cn --argjson project_count "$project_count" --argjson workspace_count "$workspace_count" --argjson workspace_total_count "$workspace_total_count" --argjson project_scope_count "$project_scope_count" --argjson nested_scope_count "$nested_scope_count" --argjson agent_count "$agent_count" --argjson terminal_count "$terminal_count" --argjson session_count "$session_count" --argjson prune_total "$prune_total" --argjson completed_count "$completed_count" --argjson include_stale_alerts "$include_stale_alerts" --argjson recent_workspaces "$recent_workspaces" --argjson recent_workspaces_applied "$recent_workspaces_applied" '{projects: $project_count, workspaces: $workspace_count, workspace_total: $workspace_total_count, project_workspaces: $project_scope_count, nested_workspaces: $nested_scope_count, agents: $agent_count, terminals: $terminal_count, sessions: $session_count, prune_candidates: $prune_total, completed_alerts: $completed_count, include_stale_alerts: $include_stale_alerts, recent_workspaces: $recent_workspaces, recent_workspaces_applied: $recent_workspaces_applied}')" \
    --argjson workspaces "$ws_enriched" \
    --argjson workspace_details "$workspace_details" \
    --argjson alerts "$alerts" \
    --argjson captures "$captures" \
    '{counts: $counts, workspaces: $workspaces, workspace_details: $workspace_details, alerts: $alerts, captures: $captures}')"

  RESULT_MESSAGE="$(printf '%s %s' "$(if [[ "$status" == "ok" ]]; then printf '✅'; elif [[ "$status" == "needs_input" ]]; then printf '❓'; else printf '⚠️'; fi)" "$summary")"
  RESULT_MESSAGE+=$'\n'"Counts: projects=$project_count workspaces=$workspace_count agents=$agent_count terminals=$terminal_count sessions=$session_count"
  RESULT_MESSAGE+=$'\n'"Workspace types: project=$project_scope_count nested=$nested_scope_count"
  if [[ -n "$project" ]]; then
    RESULT_MESSAGE+=$'\n'"Project: $project"
  fi
  if [[ -n "$workspace" ]]; then
    RESULT_MESSAGE+=$'\n'"Workspace: $selected_workspace_label"
  fi
  if [[ "$recent_workspaces_applied" == "true" ]]; then
    RESULT_MESSAGE+=$'\n'"Scope: showing $workspace_count of $workspace_total_count most recent workspace(s) in this project"
  fi
  if [[ "$alert_count" -gt 0 ]] && [[ -n "${alert_lines// }" ]]; then
    RESULT_MESSAGE+=$'\n'"Alerts:"$'\n'"$alert_lines"
  fi
  if [[ "$alerts_only" != "true" ]] && ([[ -n "$workspace" ]] || [[ -n "$project" ]]) && [[ -n "${agent_lines// }" ]]; then
    RESULT_MESSAGE+=$'\n'"Agents:"$'\n'"$agent_lines"
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
  local workspace_label
  workspace_label="$(workspace_label_for_id "$workspace")"

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
  RESULT_SUMMARY="Terminal command sent to workspace $workspace_label"
  RESULT_NEXT_ACTION="Check terminal logs for command output."
  RESULT_SUGGESTED_COMMAND="skills/amux/scripts/openclaw-dx.sh terminal logs --workspace $(shell_quote "$workspace") --lines 120"
  RESULT_DATA="$(jq -cn --arg workspace "$workspace" --arg workspace_label "$workspace_label" --argjson result "$(jq -c '.data' <<<"$out")" '{workspace: $workspace, workspace_label: $workspace_label, terminal: $result}')"

  local actions='[]'
  actions="$(append_action "$actions" "logs" "Logs" "$RESULT_SUGGESTED_COMMAND" "primary" "Capture terminal output")"
  actions="$(append_action "$actions" "status" "Status" "skills/amux/scripts/openclaw-dx.sh status --workspace $(shell_quote "$workspace")" "primary" "Check workspace status")"
  RESULT_QUICK_ACTIONS="$actions"

  RESULT_MESSAGE="✅ Terminal command sent"$'\n'"Workspace: $workspace_label"$'\n'"Session: $session_name"$'\n'"Created: $created"$'\n'"Command: $text"$'\n'"Next: $RESULT_NEXT_ACTION"
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
  local workspace_label
  workspace_label="$(workspace_label_for_id "$workspace")"
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
  RESULT_SUMMARY="Started $kind preset in workspace $workspace_label"
  RESULT_NEXT_ACTION="Watch logs for server readiness and continue coding."
  RESULT_SUGGESTED_COMMAND="skills/amux/scripts/openclaw-dx.sh terminal logs --workspace $(shell_quote "$workspace") --lines 120"
  RESULT_DATA="$(jq -cn --arg workspace "$workspace" --arg workspace_label "$workspace_label" --arg kind "$kind" --arg manager "$manager" --arg host "$host" --argjson port "$port" --arg command "$launch_cmd" --arg session_name "$session_name" --argjson created "$created" --argjson terminal "$(jq -c '.data' <<<"$out")" '{workspace: $workspace, workspace_label: $workspace_label, kind: $kind, manager: $manager, host: $host, port: $port, command: $command, session_name: $session_name, created: $created, terminal: $terminal}')"

  local actions='[]'
  actions="$(append_action "$actions" "logs" "Logs" "$RESULT_SUGGESTED_COMMAND" "primary" "Tail terminal logs for startup")"
  actions="$(append_action "$actions" "status" "Status" "skills/amux/scripts/openclaw-dx.sh status --workspace $(shell_quote "$workspace")" "primary" "Check workspace status")"
  actions="$(append_action "$actions" "alerts" "Alerts" "skills/amux/scripts/openclaw-dx.sh alerts --workspace $(shell_quote "$workspace")" "primary" "Check blockers requiring attention")"
  RESULT_QUICK_ACTIONS="$actions"

  RESULT_MESSAGE="✅ Terminal preset started: $kind"$'\n'"Workspace: $workspace_label"$'\n'"Session: $session_name"$'\n'"Created: $created"$'\n'"Host/Port: $host:$port"$'\n'"Next: $RESULT_NEXT_ACTION"
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
  local workspace_label
  workspace_label="$(workspace_label_for_id "$workspace")"
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
      RESULT_SUMMARY="No terminal session found for workspace $workspace_label"
      RESULT_NEXT_ACTION="Start a terminal command or preset first, then fetch logs."
      RESULT_SUGGESTED_COMMAND="skills/amux/scripts/openclaw-dx.sh terminal run --workspace $(shell_quote "$workspace") --text \"pwd\" --enter"
      RESULT_DATA="$(jq -cn --arg workspace "$workspace" --arg workspace_label "$workspace_label" --argjson error "$(normalize_json_or_default "$err_out" '{}')" '{workspace: $workspace, workspace_label: $workspace_label, error: $error, reason: "no_terminal_session"}')"

      local actions='[]'
      actions="$(append_action "$actions" "term_run" "Run Cmd" "$RESULT_SUGGESTED_COMMAND" "primary" "Start a terminal session with a quick command")"
      actions="$(append_action "$actions" "preset" "Preset" "skills/amux/scripts/openclaw-dx.sh terminal preset --workspace $(shell_quote "$workspace") --kind nextjs" "success" "Start a Next.js dev terminal preset")"
      actions="$(append_action "$actions" "status" "Status" "skills/amux/scripts/openclaw-dx.sh status --workspace $(shell_quote "$workspace")" "primary" "Check workspace status")"
      RESULT_QUICK_ACTIONS="$actions"
      RESULT_MESSAGE="⚠️ No terminal session found for workspace $workspace_label"$'\n'"Next: $RESULT_NEXT_ACTION"
      emit_result
      return
    fi
    emit_amux_error "terminal.logs"
    return
  done

  local content excerpt
  content="$(jq -r '.data.content // ""' <<<"$out")"
  excerpt="$(printf '%s\n' "$content" | tail -n 20)"
  local content_lower
  content_lower="$(printf '%s' "$content" | tr '[:upper:]' '[:lower:]')"

  local diagnostic_reason=""
  local diagnostic_hint=""
  local diagnostic_details=""
  local diagnostic_json='null'
  local default_next_action default_suggested_command
  default_next_action="Continue coding or run another terminal command."
  default_suggested_command="skills/amux/scripts/openclaw-dx.sh terminal run --workspace $(shell_quote "$workspace") --text \"npm test\" --enter"

  RESULT_OK=true
  RESULT_COMMAND="terminal.logs"
  RESULT_STATUS="ok"
  RESULT_SUMMARY="Captured terminal logs for workspace $workspace_label"
  RESULT_NEXT_ACTION="$default_next_action"
  RESULT_SUGGESTED_COMMAND="$default_suggested_command"

  if [[ "$content_lower" == *"err_ngrok_108"* || ( "$content_lower" == *"ngrok"* && "$content_lower" == *"authentication failed"* ) ]]; then
    RESULT_STATUS="attention"
    RESULT_SUMMARY="Terminal logs show ngrok authentication/session issue in workspace $workspace_label"
    RESULT_NEXT_ACTION="Close extra ngrok agent sessions or fix ngrok authentication, then retry the tunnel command."
    RESULT_SUGGESTED_COMMAND="skills/amux/scripts/openclaw-dx.sh terminal run --workspace $(shell_quote "$workspace") --text \"ngrok http 3000\" --enter"
    diagnostic_reason="ngrok_auth_session_limit"
    diagnostic_hint="ngrok reported authentication/session limit (ERR_NGROK_108)."
    diagnostic_details="Log excerpt includes ngrok authentication failed / ERR_NGROK_108."
  elif [[ "$content" == *"ERR_PNPM_NO_IMPORTER_MANIFEST_FOUND"* || "$content" == *"No package.json"* ]]; then
    RESULT_STATUS="attention"
    RESULT_SUMMARY="Terminal logs show missing package manifest in workspace $workspace_label"
    RESULT_NEXT_ACTION="Run the right server command for this repo, or use a Node/Next workspace before running the Next.js preset."
    RESULT_SUGGESTED_COMMAND="skills/amux/scripts/openclaw-dx.sh terminal run --workspace $(shell_quote "$workspace") --text \"pwd && ls -la\" --enter"
    diagnostic_reason="missing_package_manifest"
    diagnostic_hint="Preset command expected a Node project but no package manifest was found."
    diagnostic_details="Log excerpt includes ERR_PNPM_NO_IMPORTER_MANIFEST_FOUND / No package.json."
  elif [[ "$content_lower" == *"address already in use"* || "$content_lower" == *"eaddrinuse"* ]]; then
    RESULT_STATUS="attention"
    RESULT_SUMMARY="Terminal logs show port already in use in workspace $workspace_label"
    RESULT_NEXT_ACTION="Pick a different port or stop the existing process using that port, then retry your server/tunnel command."
    RESULT_SUGGESTED_COMMAND="skills/amux/scripts/openclaw-dx.sh terminal preset --workspace $(shell_quote "$workspace") --kind nextjs --port 3001"
    diagnostic_reason="port_in_use"
    diagnostic_hint="A process is already bound to the requested port."
    diagnostic_details="Log excerpt includes address already in use / EADDRINUSE."
  elif [[ ( "$content_lower" == *"connection refused"* || "$content_lower" == *"failed to connect"* ) && ( "$content_lower" == *"localhost"* || "$content_lower" == *"127.0.0.1"* || "$content_lower" == *"0.0.0.0"* ) ]]; then
    RESULT_STATUS="attention"
    RESULT_SUMMARY="Terminal logs show tunnel/local-server connectivity issue in workspace $workspace_label"
    RESULT_NEXT_ACTION="Start your local server first, then retry the tunnel command."
    RESULT_SUGGESTED_COMMAND="skills/amux/scripts/openclaw-dx.sh terminal preset --workspace $(shell_quote "$workspace") --kind nextjs --port 3000"
    diagnostic_reason="local_server_unreachable"
    diagnostic_hint="Tunnel could not connect to the local server endpoint."
    diagnostic_details="Log excerpt includes connection refused / failed to connect to localhost."
  fi

  if [[ -n "$diagnostic_reason" ]]; then
    diagnostic_json="$(jq -cn --arg reason "$diagnostic_reason" --arg hint "$diagnostic_hint" --arg details "$diagnostic_details" '{reason: $reason, hint: $hint, details: $details}')"
  fi
  RESULT_DATA="$(jq -cn --arg workspace "$workspace" --arg workspace_label "$workspace_label" --argjson result "$(jq -c '.data' <<<"$out")" --argjson diagnostic "$diagnostic_json" '{workspace: $workspace, workspace_label: $workspace_label, terminal: $result, diagnostic: $diagnostic}')"

  local actions='[]'
  actions="$(append_action "$actions" "term_run" "Run Cmd" "$RESULT_SUGGESTED_COMMAND" "primary" "Run a follow-up terminal command")"
  if [[ "$diagnostic_reason" == "ngrok_auth_session_limit" ]]; then
    actions="$(append_action "$actions" "retry_ngrok" "Retry Ngrok" "skills/amux/scripts/openclaw-dx.sh terminal run --workspace $(shell_quote "$workspace") --text \"ngrok http 3000\" --enter" "primary" "Retry tunnel without terminating other local ngrok processes")"
    actions="$(append_action "$actions" "logs_refresh" "Refresh Logs" "skills/amux/scripts/openclaw-dx.sh terminal logs --workspace $(shell_quote "$workspace") --lines $(shell_quote "$lines")" "primary" "Re-check tunnel logs after fixing auth/session limits")"
  elif [[ "$diagnostic_reason" == "missing_package_manifest" ]]; then
    actions="$(append_action "$actions" "preset_nextjs" "Preset Next.js" "skills/amux/scripts/openclaw-dx.sh terminal preset --workspace $(shell_quote "$workspace") --kind nextjs" "primary" "Retry Next.js preset in the correct workspace")"
  elif [[ "$diagnostic_reason" == "port_in_use" ]]; then
    actions="$(append_action "$actions" "preset_port_3001" "Use Port 3001" "skills/amux/scripts/openclaw-dx.sh terminal preset --workspace $(shell_quote "$workspace") --kind nextjs --port 3001" "primary" "Retry server startup on an alternate port")"
  elif [[ "$diagnostic_reason" == "local_server_unreachable" ]]; then
    actions="$(append_action "$actions" "start_server" "Start Server" "skills/amux/scripts/openclaw-dx.sh terminal preset --workspace $(shell_quote "$workspace") --kind nextjs --port 3000" "primary" "Start a local server before tunneling")"
    actions="$(append_action "$actions" "retry_ngrok" "Retry Ngrok" "skills/amux/scripts/openclaw-dx.sh terminal run --workspace $(shell_quote "$workspace") --text \"ngrok http 3000\" --enter" "primary" "Retry tunnel after server is running")"
  fi
  actions="$(append_action "$actions" "status" "Status" "skills/amux/scripts/openclaw-dx.sh status --workspace $(shell_quote "$workspace")" "primary" "Check workspace status")"
  RESULT_QUICK_ACTIONS="$actions"

  local message_prefix
  message_prefix="✅"
  if [[ "$RESULT_STATUS" == "attention" ]]; then
    message_prefix="⚠️"
  fi
  RESULT_MESSAGE="$message_prefix Terminal logs captured"$'\n'"Workspace: $workspace_label"
  if [[ -n "$diagnostic_hint" ]]; then
    RESULT_MESSAGE+=$'\n'"Detected issue: $diagnostic_hint"
  fi
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

  emit_turn_passthrough "review" "review_turn" "$turn_json" "$workspace" "$assistant"
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
  if ! ws_row="$(workspace_row_with_scope_by_id "$workspace")"; then
    emit_amux_error "git.ship"
    return
  fi
  if [[ -z "${ws_row// }" ]]; then
    emit_error "git.ship" "command_error" "workspace not found" "$workspace"
    return
  fi
  local ws_name ws_repo ws_assistant ws_scope ws_scope_source ws_parent ws_parent_name
  ws_name="$(jq -r '.name // ""' <<<"$ws_row")"
  ws_repo="$(jq -r '.repo // ""' <<<"$ws_row")"
  ws_assistant="$(jq -r '.assistant // ""' <<<"$ws_row")"
  ws_scope="$(jq -r '.scope // ""' <<<"$ws_row")"
  ws_scope_source="$(jq -r '.scope_source // ""' <<<"$ws_row")"
  ws_parent="$(jq -r '.parent_workspace // ""' <<<"$ws_row")"
  ws_parent_name="$(jq -r '.parent_name // ""' <<<"$ws_row")"
  local ws_scope_label workspace_label
  ws_scope_label="$(workspace_scope_label "$ws_scope")"
  workspace_label="$(workspace_brief_label "$workspace" "$ws_name" "$ws_scope_label" "$ws_parent")"
  context_set_workspace "$workspace" "$ws_name" "$ws_repo" "$ws_assistant" "$ws_scope" "$ws_parent" "$ws_parent_name"
  context_set_workspace_lineage_if_authoritative "$workspace" "$ws_scope" "$ws_parent" "$ws_parent_name" "$ws_scope_source"

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
    RESULT_SUMMARY="No changes to commit in $workspace_label"
    RESULT_NEXT_ACTION="Continue coding or run a review workflow."
    RESULT_SUGGESTED_COMMAND="skills/amux/scripts/openclaw-dx.sh review --workspace $(shell_quote "$workspace")"

    if [[ "$push" == "true" && "$pushed" == "true" ]]; then
      RESULT_SUMMARY="No new changes to commit; pushed existing commits for $workspace_label"
      RESULT_NEXT_ACTION="Run review or continue implementation."
      RESULT_SUGGESTED_COMMAND="skills/amux/scripts/openclaw-dx.sh review --workspace $(shell_quote "$workspace")"
    elif [[ "$push" == "true" && -n "$push_error" ]]; then
      RESULT_STATUS="attention"
      RESULT_SUMMARY="No new changes to commit; push failed for $workspace_label"
      if [[ "$push_error" == "origin remote is not configured" ]]; then
        RESULT_NEXT_ACTION="Configure an origin remote in this workspace, then retry push."
        RESULT_SUGGESTED_COMMAND="skills/amux/scripts/openclaw-dx.sh terminal run --workspace $(shell_quote "$workspace") --text \"git remote -v\" --enter"
      else
        RESULT_NEXT_ACTION="Fix push issues, then retry push."
        RESULT_SUGGESTED_COMMAND="skills/amux/scripts/openclaw-dx.sh git ship --workspace $(shell_quote "$workspace") --push"
      fi
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
    if [[ "$push" == "true" && "$push_error" == "origin remote is not configured" ]]; then
      actions="$(append_action "$actions" "show_remote" "Remote" "skills/amux/scripts/openclaw-dx.sh terminal run --workspace $(shell_quote "$workspace") --text \"git remote -v\" --enter" "primary" "Inspect git remotes in this workspace")"
      actions="$(append_action "$actions" "retry_push" "Retry Push" "skills/amux/scripts/openclaw-dx.sh git ship --workspace $(shell_quote "$workspace") --push" "primary" "Retry push after configuring origin")"
    elif [[ "$push" == "true" && -n "$push_error" ]]; then
      actions="$(append_action "$actions" "retry_push" "Retry Push" "skills/amux/scripts/openclaw-dx.sh git ship --workspace $(shell_quote "$workspace") --push" "primary" "Retry push after fixing errors")"
    fi
    actions="$(append_action "$actions" "review" "Review" "skills/amux/scripts/openclaw-dx.sh review --workspace $(shell_quote "$workspace")" "primary" "Run review workflow")"
    actions="$(append_action "$actions" "status" "Status" "skills/amux/scripts/openclaw-dx.sh status --workspace $(shell_quote "$workspace")" "primary" "Check workspace status")"
    RESULT_QUICK_ACTIONS="$actions"

    RESULT_DATA="$(jq -cn \
      --arg workspace "$workspace" \
      --arg workspace_label "$workspace_label" \
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
      '{workspace: $workspace, workspace_label: $workspace_label, root: $root, branch: $branch, upstream: $upstream, has_upstream: $has_upstream, has_origin: $has_origin, ahead_count: $ahead_count, committed: $committed, pushed: $pushed, push_requested: $push_requested, push_error: $push_error, reason: "no_changes"}')"

    local message_prefix="✅"
    if [[ "$RESULT_STATUS" != "ok" ]]; then
      message_prefix="⚠️"
    fi
    RESULT_MESSAGE="$message_prefix No new changes to commit in $workspace_label"
    if [[ "$push" == "true" && "$pushed" == "true" ]]; then
      RESULT_MESSAGE+=$'\n'"Push: success"
    elif [[ "$push" == "true" && -n "$push_error" ]]; then
      RESULT_MESSAGE+=$'\n'"Push: failed ($push_error)"
      if [[ "$push_error" == "origin remote is not configured" ]]; then
        RESULT_MESSAGE+=$'\n'"Hint: set origin first (git remote add origin <repo-url>) and retry push."
      fi
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
    message="chore(amux): update ${ws_name:-$workspace} ($file_count files)"
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

  RESULT_SUMMARY="Committed $file_count file(s) in $workspace_label"
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

  RESULT_DATA="$(jq -cn --arg workspace "$workspace" --arg workspace_label "$workspace_label" --arg root "$ws_root" --arg commit_hash "$commit_hash" --arg branch "$branch" --arg message "$message" --argjson file_count "$file_count" --argjson pushed "$pushed" --arg push_error "$push_error" '{workspace: $workspace, workspace_label: $workspace_label, root: $root, commit_hash: $commit_hash, branch: $branch, message: $message, file_count: $file_count, pushed: $pushed, push_error: $push_error}')"

  RESULT_MESSAGE="✅ Commit created"$'\n'"Workspace: $workspace_label"$'\n'"Branch: $branch"$'\n'"Commit: $commit_hash"$'\n'"Files: $file_count"
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

  local kickoff_auto_continue kickoff_auto_text kickoff_initial_status kickoff_agent_id
  kickoff_auto_continue="${OPENCLAW_DX_KICKOFF_NEEDS_INPUT_AUTO_CONTINUE:-true}"
  kickoff_auto_text="${OPENCLAW_DX_KICKOFF_NEEDS_INPUT_TEXT:-If a choice is required, pick the safest high-impact default, continue, and report status plus next action.}"
  kickoff_initial_status="$(jq -r '.overall_status // .status // ""' <<<"$start_json")"
  kickoff_agent_id="$(jq -r '.agent_id // ""' <<<"$start_json")"
  if [[ "$kickoff_auto_continue" != "false" && "$kickoff_initial_status" == "needs_input" && -n "${kickoff_agent_id// }" ]]; then
    local kickoff_continue_json kickoff_continue_ok kickoff_continue_status
    if kickoff_continue_json="$(run_self_json continue --agent "$kickoff_agent_id" --text "$kickoff_auto_text" --enter --wait-timeout "$wait_timeout" --idle-threshold "$idle_threshold")"; then
      kickoff_continue_ok="$(jq -r '.ok // false' <<<"$kickoff_continue_json")"
      kickoff_continue_status="$(jq -r '.overall_status // .status // ""' <<<"$kickoff_continue_json")"
      if [[ "$kickoff_continue_ok" == "true" && "$kickoff_continue_status" != "command_error" && "$kickoff_continue_status" != "agent_error" ]]; then
        start_json="$kickoff_continue_json"
      fi
    fi
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
      (.suggested_command // "") as $suggested_command
      | (
          (.quick_actions // [])
          | map(
              if ((.command // "") == $suggested_command)
                and ((.overall_status // .status // "") == "needs_input")
                and ((.id // "") == "status" or (.label // "") == "Status")
              then
                . + {
                  label: "Reply + Continue",
                  style: "success",
                  prompt: "Send a safe follow-up and continue this turn"
                }
              else
                .
              end
            )
        ) as $base_actions
      | (($base_actions | map(.id // "") | index("continue")) != null) as $has_continue_id
      | (($base_actions | map(.command // "") | index($suggested_command)) != null) as $has_suggested_command
      | ($base_actions + [
        (
          if ($suggested_command | length) > 0 and ($has_continue_id | not) and ($has_suggested_command | not) then
            {
              id: "continue_turn",
              label: (if ((.overall_status // .status // "") == "needs_input") then "Reply + Continue" else "Continue" end),
              command: $suggested_command,
              style: (if ((.overall_status // .status // "") == "needs_input") then "success" else "primary" end),
              prompt: (if ((.overall_status // .status // "") == "needs_input") then "Send a safe follow-up and continue this turn" else "Continue this turn" end)
            }
          else
            empty
          end
        ),
        {
          id: "status_ws",
          label: "WS Status",
          command: ("skills/amux/scripts/openclaw-dx.sh status --workspace " + $workspace_id),
          style: "primary",
          prompt: "Check workspace status"
        },
        {
          id: "review_ws",
          label: "WS Review",
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

  printf '%s\n' "$kickoff_json"
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
  context_set_workspace_with_lookup "$workspace" ""
  local workspace_label
  workspace_label="$(workspace_label_for_id "$workspace")"

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
    && [[ "$impl_status" == "needs_input" || "$impl_status" == "timed_out" || "$impl_status" == "partial" || "$impl_status" == "partial_budget" ]] \
    && [[ -n "${implement_needs_input_fallback_assistant// }" ]] \
    && [[ "$implement_needs_input_fallback_assistant" != "$effective_implement_assistant" ]]; then
    dx_dual_progress "implementation returned status=$impl_status; retrying with fallback assistant=$implement_needs_input_fallback_assistant"
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

  local impl_auto_continue_eligible=false
  if [[ "$impl_ok" == "true" ]]; then
    case "$impl_status" in
      needs_input|timed_out|partial|partial_budget)
        impl_auto_continue_eligible=true
        ;;
      *)
        ;;
    esac
  fi

  if [[ "$auto_continue_impl" == "true" ]] && [[ "$impl_auto_continue_eligible" == "true" ]]; then
    local impl_agent_id
    impl_agent_id="$(jq -r '.agent_id // ""' <<<"$implementation_json")"
    if [[ -n "${impl_agent_id// }" ]] && [[ -x "$STEP_SCRIPT_PATH" ]]; then
      dx_dual_progress "implementation status=$impl_status; auto-continuing once"
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

  if [[ "$RESULT_STATUS" != "ok" ]]; then
    if [[ "$review_json" != "null" ]] && [[ "$review_status" != "completed" ]] && [[ "$review_status" != "idle" ]]; then
      if [[ -z "${RESULT_SUGGESTED_COMMAND// }" ]] || [[ "$RESULT_SUGGESTED_COMMAND" == "skills/amux/scripts/openclaw-dx.sh git ship --workspace $workspace" ]]; then
        RESULT_SUGGESTED_COMMAND="skills/amux/scripts/openclaw-dx.sh review --workspace $(shell_quote "$workspace") --assistant $(shell_quote "$effective_review_assistant")"
      fi
    elif [[ "$impl_status" == "needs_input" || "$impl_status" == "timed_out" || "$impl_status" == "partial" || "$impl_status" == "partial_budget" || "$impl_status" == "session_exited" || "$impl_status" == "command_error" || "$impl_status" == "agent_error" ]]; then
      if [[ -z "${RESULT_SUGGESTED_COMMAND// }" ]] || [[ "$RESULT_SUGGESTED_COMMAND" == "skills/amux/scripts/openclaw-dx.sh git ship --workspace $workspace" ]]; then
        RESULT_SUGGESTED_COMMAND="skills/amux/scripts/openclaw-dx.sh start --workspace $(shell_quote "$workspace") --assistant $(shell_quote "$effective_implement_assistant") --prompt $(shell_quote "$implement_prompt")"
      fi
    fi
    if [[ -z "${RESULT_SUGGESTED_COMMAND// }" ]]; then
      RESULT_SUGGESTED_COMMAND="skills/amux/scripts/openclaw-dx.sh status --workspace $(shell_quote "$workspace")"
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
    --arg workspace_label "$workspace_label" \
    --arg implement_assistant "$effective_implement_assistant" \
    --arg review_assistant "$effective_review_assistant" \
    --arg review_skipped_reason "$review_skipped_reason" \
    --argjson implementation "$implementation_compact" \
    --argjson review "$review_compact" \
    '{
      workspace: $workspace,
      workspace_label: $workspace_label,
      implement_assistant: $implement_assistant,
      review_assistant: $review_assistant,
      review_skipped_reason: $review_skipped_reason,
      implementation: $implementation,
      review: $review
    }')"

  local workflow_header
  case "$RESULT_STATUS" in
    ok)
      workflow_header="✅ Dual-pass workflow completed"
      ;;
    needs_input)
      workflow_header="❓ Dual-pass workflow needs input"
      ;;
    *)
      workflow_header="⚠️ Dual-pass workflow needs attention"
      ;;
  esac
  RESULT_MESSAGE="$workflow_header"$'\n'"Workspace: $workspace_label"
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
    context_set_workspace_with_lookup "$workspace" ""
  fi
  local workspace_label workspace_assistant_hint
  workspace_label=""
  workspace_assistant_hint=""
  if [[ -n "$workspace" ]]; then
    context_set_workspace_with_lookup "$workspace" ""
    workspace_label="$(workspace_label_for_id "$workspace")"
    workspace_assistant_hint="$(default_assistant_for_workspace "$workspace")"
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
    done < <(jq -r --arg preferred "$workspace_assistant_hint" '
      def probe_rank($name):
        if ($preferred | length) > 0 and $name == $preferred then
          -1
        elif $name == "codex" then
          0
        elif $name == "claude" then
          1
        elif $name == "gemini" then
          2
        elif $name == "cursor" then
          3
        elif $name == "opencode" then
          4
        elif $name == "amp" then
          5
        elif $name == "droid" then
          6
        elif $name == "cline" then
          7
        elif $name == "pi" then
          8
        else
          50
        end;
      [ .[] | select(.status == "ready") ]
      | sort_by(probe_rank(.name), .name)
      | .[].name
    ' <<<"$assistants")
  fi

  local overall_status="ok"
  if [[ "$missing_count" -gt 0 ]]; then
    overall_status="attention"
  fi
  if [[ "$probe_failed" -gt 0 ]]; then
    overall_status="attention"
  fi
  if [[ "$probe_needs_input" -gt 0 ]]; then
    if [[ "$probe_failed" -gt 0 ]]; then
      overall_status="attention"
    elif [[ "$probe_passed" -gt 0 ]] && [[ "$probe_failed" -eq 0 ]]; then
      if [[ "$missing_count" -gt 0 ]]; then
        overall_status="attention"
      else
        overall_status="ok"
      fi
    else
      overall_status="needs_input"
    fi
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
    if [[ "$probe_passed" -gt 0 ]] && [[ "$probe_failed" -eq 0 ]]; then
      RESULT_NEXT_ACTION="Use probe-passed assistants for non-interactive mobile flows; assistants needing local permission can be skipped."
    else
      RESULT_NEXT_ACTION="Some assistants need interactive permission input. Use codex for non-interactive mobile flows."
    fi
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
  local review_cmd
  review_cmd="skills/amux/scripts/openclaw-dx.sh review --workspace <workspace_id> --assistant codex"
  if [[ -n "$workspace" ]]; then
    review_cmd="skills/amux/scripts/openclaw-dx.sh review --workspace $(shell_quote "$workspace") --assistant codex"
  fi
  actions="$(append_action "$actions" "review" "Review" "$review_cmd" "primary" "Run a review workflow")"
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
    --arg workspace_label "$workspace_label" \
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
      workspace_label: $workspace_label,
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
  if [[ -n "$workspace_label" ]]; then
    RESULT_MESSAGE+=$'\n'"Workspace: $workspace_label"
  fi
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

flag_requires_value() {
  local flag="$1"
  case "$flag" in
    --path|--workspace|--assistant|--base|--limit|--page|--query|--index|--name|--project|--from-workspace|--scope|--task|--prompt|--agent|--text|--capture-lines|--capture-agents|--older-than|--recent-workspaces|--kind|--port|--host|--manager|--message|--max-steps|--turn-budget|--wait-timeout|--idle-threshold|--implement-assistant|--implement-prompt|--review-assistant|--review-prompt|--auto-continue-impl|--auto-continue-impl-prompt)
      return 0
      ;;
    *)
      return 1
      ;;
  esac
}

flag_allows_flag_like_value() {
  local flag="$1"
  case "$flag" in
    --prompt|--text|--message|--implement-prompt|--review-prompt|--auto-continue-impl-prompt)
      return 0
      ;;
    *)
      return 1
      ;;
  esac
}

validate_required_flag_values() {
  local command_name="$1"
  shift || true
  local expecting=""
  local token=""

  while [[ $# -gt 0 ]]; do
    token="$1"
    shift
    if [[ -n "$expecting" ]]; then
      if [[ "$token" == --* ]] && ! flag_allows_flag_like_value "$expecting"; then
        emit_error "$command_name" "command_error" "missing value for $expecting"
        return 1
      fi
      expecting=""
      continue
    fi
    if flag_requires_value "$token"; then
      expecting="$token"
    fi
  done

  if [[ -n "$expecting" ]]; then
    emit_error "$command_name" "command_error" "missing value for $expecting"
    return 1
  fi

  return 0
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

preflight_command="$top_cmd"
preflight_args=()
if [[ $# -gt 0 ]]; then
  preflight_args=("$@")
fi
case "$top_cmd" in
  project|workspace|terminal|git|workflow)
    if [[ ${#preflight_args[@]} -gt 0 ]]; then
      preflight_command="$top_cmd.${preflight_args[0]}"
      preflight_args=("${preflight_args[@]:1}")
    fi
    ;;
esac
if [[ ${#preflight_args[@]} -gt 0 ]]; then
  if ! validate_required_flag_values "$preflight_command" "${preflight_args[@]}"; then
    exit 0
  fi
else
  if ! validate_required_flag_values "$preflight_command"; then
    exit 0
  fi
fi

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
