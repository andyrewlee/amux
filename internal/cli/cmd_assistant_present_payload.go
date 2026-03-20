package cli

import "strings"

func assistantPresentChunksMeta(channel map[string]any, message string) []any {
	if items, ok := channel["chunks_meta"].([]any); ok && len(items) > 0 {
		return items
	}
	return []any{map[string]any{"index": 1, "total": 1, "text": message}}
}

func assistantPresentChunks(chunksMeta []any) []string {
	chunks := make([]string, 0, len(chunksMeta))
	for _, item := range chunksMeta {
		meta, _ := item.(map[string]any)
		chunks = append(chunks, assistantPresentString(meta["text"]))
	}
	return chunks
}

func assistantPresentBasePayload(message string, chunks []string, chunksMeta []any, actions []assistantPresentAction) map[string]any {
	return map[string]any{
		"message":          message,
		"chunks":           chunks,
		"chunks_meta":      chunksMeta,
		"actions":          actions,
		"action_tokens":    assistantPresentActionIDs(actions),
		"actions_fallback": assistantPresentActionFallback(actions),
	}
}

func assistantPresentBuildPresentation(
	channelID string,
	channel map[string]any,
	base map[string]any,
	message string,
	chunks []string,
	chunksMeta []any,
	actions []assistantPresentAction,
) map[string]any {
	switch channelID {
	case "telegram":
		presentation := assistantPresentCloneMap(channel)
		presentation["message"] = assistantPresentFallback(channel["message"], message)
		presentation["chunks"] = assistantPresentFallback(channel["chunks"], chunks)
		presentation["chunks_meta"] = assistantPresentFallback(channel["chunks_meta"], chunksMeta)
		presentation["callback_data_max_bytes"] = assistantPresentFallback(channel["callback_data_max_bytes"], 64)
		switch {
		case channel["inline_buttons"] != nil:
			presentation["inline_buttons"] = channel["inline_buttons"]
		case assistantPresentChannelInlineButtonsEnabled(channel):
			presentation["inline_buttons"] = assistantPresentTelegramInlineButtons(actions)
		default:
			presentation["inline_buttons"] = []any{}
		}
		if channel["action_tokens"] != nil {
			presentation["action_tokens"] = channel["action_tokens"]
		} else {
			presentation["action_tokens"] = assistantPresentCallbackData(actions)
		}
		presentation["actions_fallback"] = assistantPresentFallback(channel["actions_fallback"], assistantPresentActionFallback(actions))
		return presentation
	case "slack":
		presentation := assistantPresentCloneMap(base)
		presentation["blocks"] = assistantPresentSlackBlocks(actions)
		return presentation
	case "discord":
		presentation := assistantPresentCloneMap(base)
		presentation["components"] = assistantPresentDiscordComponents(actions)
		return presentation
	case "msteams":
		presentation := assistantPresentCloneMap(base)
		presentation["suggested_actions"] = assistantPresentMSTeamsActions(actions)
		return presentation
	case "webchat":
		presentation := assistantPresentCloneMap(base)
		presentation["quick_replies"] = assistantPresentWebchatReplies(actions)
		return presentation
	default:
		return assistantPresentCloneMap(base)
	}
}

func assistantPresentChannelInlineButtonsEnabled(channel map[string]any) bool {
	value, ok := channel["inline_buttons_enabled"]
	if !ok {
		return true
	}
	enabled, ok := value.(bool)
	return !ok || enabled
}

func assistantPresentTelegramInlineButtons(actions []assistantPresentAction) []any {
	if len(actions) == 0 {
		return []any{}
	}
	rows := make([]any, 0, (len(actions)+1)/2)
	for i := 0; i < len(actions); i += 2 {
		end := i + 2
		if end > len(actions) {
			end = len(actions)
		}
		row := make([]any, 0, end-i)
		for _, action := range actions[i:end] {
			row = append(row, map[string]any{
				"text":          action.Label,
				"callback_data": action.CallbackData,
				"style":         action.Style,
			})
		}
		rows = append(rows, row)
	}
	return rows
}

func assistantPresentSlackBlocks(actions []assistantPresentAction) []any {
	if len(actions) == 0 {
		return []any{}
	}
	elements := make([]any, 0, min(len(actions), 5))
	for _, action := range actions[:min(len(actions), 5)] {
		element := map[string]any{
			"type":      "button",
			"text":      map[string]any{"type": "plain_text", "text": assistantPresentTruncate(action.Label, 75)},
			"value":     action.ActionID,
			"action_id": action.ActionID,
		}
		switch action.Style {
		case "danger":
			element["style"] = "danger"
		case "success":
			element["style"] = "primary"
		}
		elements = append(elements, element)
	}
	return []any{map[string]any{"type": "actions", "elements": elements}}
}

func assistantPresentDiscordComponents(actions []assistantPresentAction) []any {
	if len(actions) == 0 {
		return []any{}
	}
	components := make([]any, 0, min(len(actions), 5))
	for _, action := range actions[:min(len(actions), 5)] {
		components = append(components, map[string]any{
			"type":      2,
			"style":     assistantPresentDiscordStyle(action.Style),
			"label":     assistantPresentTruncate(action.Label, 80),
			"custom_id": action.ActionID,
		})
	}
	return []any{map[string]any{"type": 1, "components": components}}
}

func assistantPresentDiscordStyle(style string) int {
	switch style {
	case "primary":
		return 1
	case "success":
		return 3
	case "danger":
		return 4
	default:
		return 2
	}
}

func assistantPresentMSTeamsActions(actions []assistantPresentAction) []any {
	out := make([]any, 0, len(actions))
	for _, action := range actions {
		out = append(out, map[string]any{
			"type":  "imBack",
			"title": assistantPresentTruncate(action.Label, 80),
			"value": action.Command,
		})
	}
	return out
}

func assistantPresentWebchatReplies(actions []assistantPresentAction) []any {
	out := make([]any, 0, len(actions))
	for _, action := range actions {
		out = append(out, map[string]any{
			"id":    action.ActionID,
			"label": action.Label,
			"value": action.Command,
		})
	}
	return out
}

func assistantPresentActionFallback(actions []assistantPresentAction) string {
	if len(actions) == 0 {
		return ""
	}
	parts := make([]string, 0, len(actions))
	for _, action := range actions {
		parts = append(parts, action.ActionID+"="+action.Label)
	}
	return "Actions: " + strings.Join(parts, " | ")
}

func assistantPresentActionIDs(actions []assistantPresentAction) []string {
	out := make([]string, 0, len(actions))
	for _, action := range actions {
		out = append(out, action.ActionID)
	}
	return out
}

func assistantPresentCallbackData(actions []assistantPresentAction) []string {
	out := make([]string, 0, len(actions))
	for _, action := range actions {
		out = append(out, action.CallbackData)
	}
	return out
}

func assistantPresentActionMap(actions []assistantPresentAction) map[string]any {
	out := make(map[string]any, len(actions))
	for _, action := range actions {
		out[action.ActionID] = action.Command
	}
	return out
}

func assistantPresentActionPromptMap(actions []assistantPresentAction) map[string]any {
	out := make(map[string]any, len(actions))
	for _, action := range actions {
		out[action.ActionID] = action.Prompt
	}
	return out
}

func assistantPresentCloneMap(source map[string]any) map[string]any {
	out := make(map[string]any, len(source))
	for key, value := range source {
		out[key] = value
	}
	return out
}

func assistantPresentFallback(current, fallback any) any {
	if current == nil {
		return fallback
	}
	switch value := current.(type) {
	case string:
		if strings.TrimSpace(value) == "" {
			return fallback
		}
	}
	return current
}

func assistantPresentString(value any) string {
	text, _ := value.(string)
	return text
}

func assistantPresentTruncate(value string, limit int) string {
	if limit <= 0 || len(value) <= limit {
		return value
	}
	runes := []rune(value)
	if len(runes) <= limit {
		return value
	}
	return string(runes[:limit])
}
