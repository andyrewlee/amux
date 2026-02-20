#!/usr/bin/env bash
# openclaw-present.sh — augment amux chat payloads with channel-neutral render data.
#
# Reads one JSON object from stdin and emits the same object plus:
# - normalized quick action ids (`action_id`)
# - `quick_action_by_id` and `quick_action_prompts_by_id`
# - `.openclaw` with channel-specific presentation payloads

set -euo pipefail

if ! command -v jq >/dev/null 2>&1; then
  cat
  exit 0
fi

TMP_INPUT="$(mktemp "${TMPDIR:-/tmp}/openclaw-present.XXXXXX")"
cleanup_tmp() {
  rm -f "$TMP_INPUT" >/dev/null 2>&1 || true
}
trap cleanup_tmp EXIT

cat >"$TMP_INPUT"
if [[ ! -s "$TMP_INPUT" ]]; then
  cat "$TMP_INPUT"
  exit 0
fi

if ! grep -q '[^[:space:]]' "$TMP_INPUT"; then
  cat "$TMP_INPUT"
  exit 0
fi

if ! jq -e . >/dev/null 2>&1 <"$TMP_INPUT"; then
  cat "$TMP_INPUT"
  exit 0
fi

TARGET_CHANNEL="${OPENCLAW_CHANNEL:-}"

jq -c --arg target_channel "$TARGET_CHANNEL" '
  def normalize_style($style):
    if $style == "success" or $style == "danger" or $style == "primary" then
      $style
    else
      "primary"
    end;
  def sanitize_action_id($raw; $idx):
    (($raw // ("action_" + (($idx + 1) | tostring))) | tostring | ascii_downcase
      | gsub("[^a-z0-9:_-]"; "_")
      | gsub("_+"; "_")
      | .[0:64]
    ) as $id
    | if ($id | length) == 0 then
        ("action_" + (($idx + 1) | tostring))
      else
        $id
      end;
  def sanitize_callback_data($raw; $action_id):
    (($raw // ("qa:" + $action_id)) | tostring | .[0:64]);
  def normalize_actions($actions):
    ($actions // [])
    | to_entries
    | map(
        .key as $idx
        | (.value // {}) as $action
        | (sanitize_action_id(($action.action_id // $action.id // $action.callback_data); $idx)) as $action_id
        | {
            id: (($action.id // $action_id) | tostring),
            action_id: $action_id,
            label: (($action.label // "Action") | tostring),
            command: (($action.command // "") | tostring),
            style: normalize_style(($action.style // "primary")),
            prompt: (($action.prompt // "") | tostring),
            callback_data: sanitize_callback_data(($action.callback_data // null); $action_id)
          }
      );
  def action_rows($actions; $size):
    if ($actions | length) == 0 then
      []
    else
      [range(0; ($actions | length); $size) as $idx
        | ($actions[$idx:($idx + $size)])
      ]
    end;
  def action_fallback($actions):
    if ($actions | length) == 0 then
      ""
    else
      "Actions: "
      + ($actions | map((.action_id // .id) + "=" + (.label // "")) | join(" | "))
    end;
  def text_payload($message; $chunks; $chunks_meta; $actions):
    {
      message: $message,
      chunks: $chunks,
      chunks_meta: $chunks_meta,
      actions: $actions,
      action_tokens: ($actions | map(.action_id)),
      actions_fallback: action_fallback($actions)
    };
  def slack_style($style):
    if $style == "danger" then
      "danger"
    elif $style == "success" then
      "primary"
    else
      ""
    end;
  def discord_style($style):
    if $style == "primary" then
      1
    elif $style == "success" then
      3
    elif $style == "danger" then
      4
    else
      2
    end;
  def normalize_target_channel($raw):
    (($raw // "") | ascii_downcase | gsub("[^a-z0-9_-]"; "")) as $id
    | if $id == "teams" then "msteams" else $id end;
  (normalize_actions(.quick_actions // [])) as $quick_actions
  | ((.channel.message // .message // .summary // "") | tostring) as $message
  | ((.channel.chunks_meta // []) | if length > 0 then . else [{index: 1, total: 1, text: $message}] end) as $chunks_meta
  | ($chunks_meta | map(.text)) as $chunks
  | (text_payload($message; $chunks; $chunks_meta; $quick_actions)) as $base
  | ((.channel // {}) as $channel
    | def build_presentation($channel_id):
        if $channel_id == "telegram" then
          $channel + {
            message: ($channel.message // $message),
            chunks: ($channel.chunks // $chunks),
            chunks_meta: ($channel.chunks_meta // $chunks_meta),
            callback_data_max_bytes: ($channel.callback_data_max_bytes // 64),
            inline_buttons: (
              if $channel.inline_buttons != null then
                $channel.inline_buttons
              elif ($channel.inline_buttons_enabled // true) then
                (
                  action_rows($quick_actions; 2)
                  | map(map({text: .label, callback_data: .callback_data, style: .style}))
                )
              else
                []
              end
            ),
            action_tokens: ($channel.action_tokens // ($quick_actions | map(.callback_data))),
            actions_fallback: ($channel.actions_fallback // action_fallback($quick_actions))
          }
        elif $channel_id == "slack" then
          $base + {
            blocks: (
              if ($quick_actions | length) == 0 then
                []
              else
                [
                  {
                    type: "actions",
                    elements: (
                      $quick_actions[0:5]
                      | map(
                          {
                            type: "button",
                            text: {type: "plain_text", text: (.label[0:75])},
                            value: .action_id,
                            action_id: .action_id,
                            style: slack_style(.style)
                          }
                          | if .style == "" then del(.style) else . end
                        )
                    )
                  }
                ]
              end
            )
          }
        elif $channel_id == "discord" then
          $base + {
            components: (
              if ($quick_actions | length) == 0 then
                []
              else
                [
                  {
                    type: 1,
                    components: (
                      $quick_actions[0:5]
                      | map({
                          type: 2,
                          style: discord_style(.style),
                          label: (.label[0:80]),
                          custom_id: .action_id
                        })
                    )
                  }
                ]
              end
            )
          }
        elif $channel_id == "msteams" then
          $base + {
            suggested_actions: (
              $quick_actions
              | map({type: "imBack", title: (.label[0:80]), value: (.command // "")})
            )
          }
        elif $channel_id == "webchat" then
          $base + {
            quick_replies: (
              $quick_actions
              | map({id: .action_id, label: .label, value: .command})
            )
          }
        else
          $base
        end;
    [
      "generic",
      "telegram",
      "slack",
      "discord",
      "msteams",
      "webchat",
      "whatsapp",
      "signal",
      "line",
      "googlechat",
      "mattermost",
      "matrix",
      "irc",
      "feishu",
      "nextcloud_talk",
      "nostr",
      "tlon",
      "twitch",
      "zalo",
      "zalouser",
      "bluebubbles",
      "imessage"
    ] as $supported_channels
    | (normalize_target_channel($target_channel)) as $preferred
    | (
        if ($preferred | length) > 0 and (($supported_channels | index($preferred)) != null) then
          $preferred
        else
          "generic"
        end
      ) as $selected_channel
    | (build_presentation($selected_channel)) as $selected_presentation
    | (
        if $selected_channel == "generic" then
          {generic: $base}
        else
          {generic: $base, ($selected_channel): $selected_presentation}
        end
      ) as $channels
    | .quick_actions = $quick_actions
    | .quick_action_by_id = ($quick_actions | map({key: .action_id, value: .command}) | from_entries)
    | .quick_action_prompts_by_id = (
        $quick_actions
        | map({key: .action_id, value: .prompt})
        | from_entries
      )
    | .openclaw = {
        schema_version: "amux.openclaw.channel-ux.v1",
        supported_channels: $supported_channels,
        target_channel: $preferred,
        selected_channel: $selected_channel,
        channels: $channels,
        presentation: $selected_presentation,
        actions: {
          list: $quick_actions,
          map: ($quick_actions | map({key: .action_id, value: .command}) | from_entries),
          prompts: ($quick_actions | map({key: .action_id, value: .prompt}) | from_entries),
          fallback: action_fallback($quick_actions)
        }
      }
  )
' "$TMP_INPUT"
