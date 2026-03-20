package cli

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"regexp"
	"strings"
)

const assistantPresentUsage = "Usage: amux assistant present"

var (
	assistantPresentActionIDRegex           = regexp.MustCompile(`[^a-z0-9:_-]`)
	assistantPresentActionIDUnderscoreRegex = regexp.MustCompile(`_+`)
)

var assistantPresentSupportedChannels = []string{
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
	"imessage",
}

type assistantPresentAction struct {
	ID           string `json:"id"`
	ActionID     string `json:"action_id"`
	Label        string `json:"label"`
	Command      string `json:"command"`
	Style        string `json:"style"`
	Prompt       string `json:"prompt"`
	CallbackData string `json:"callback_data"`
}

func cmdAssistantPresent(w, wErr io.Writer, gf GlobalFlags, args []string, version string) int {
	_ = version

	fs := newFlagSet("assistant present")
	if err := fs.Parse(args); err != nil {
		return returnUsageError(w, wErr, gf, assistantPresentUsage, version, err)
	}
	if len(fs.Args()) > 0 {
		return returnUsageError(
			w, wErr, gf, assistantPresentUsage, version,
			fmt.Errorf("unexpected arguments: %s", strings.Join(fs.Args(), " ")),
		)
	}

	input, err := io.ReadAll(os.Stdin)
	if err != nil {
		if gf.JSON {
			ReturnError(w, "read_failed", err.Error(), nil, version)
		} else {
			Errorf(wErr, "failed to read stdin: %v", err)
		}
		return ExitInternalError
	}

	output := assistantPresentTransform(input, os.Getenv("AMUX_ASSISTANT_CHANNEL"))
	if _, err := w.Write(output); err != nil {
		if gf.JSON {
			ReturnError(wErr, "write_failed", err.Error(), nil, version)
		} else {
			Errorf(wErr, "failed to write stdout: %v", err)
		}
		return ExitInternalError
	}
	return ExitOK
}

func assistantPresentTransform(input []byte, targetChannel string) []byte {
	if len(input) == 0 || !assistantPresentHasNonSpace(input) {
		return append([]byte(nil), input...)
	}

	var payload any
	if err := json.Unmarshal(input, &payload); err != nil {
		return append([]byte(nil), input...)
	}
	root, ok := payload.(map[string]any)
	if !ok {
		return append([]byte(nil), input...)
	}

	quickActions := assistantPresentNormalizeActions(root["quick_actions"])
	message, channelMap := assistantPresentMessage(root)
	chunksMeta := assistantPresentChunksMeta(channelMap, message)
	chunks := assistantPresentChunks(chunksMeta)
	base := assistantPresentBasePayload(message, chunks, chunksMeta, quickActions)
	preferred := assistantPresentNormalizeTargetChannel(targetChannel)
	selected := "generic"
	if assistantPresentChannelSupported(preferred) {
		selected = preferred
	}
	selectedPresentation := assistantPresentBuildPresentation(selected, channelMap, base, message, chunks, chunksMeta, quickActions)

	channels := map[string]any{"generic": base}
	if selected != "generic" {
		channels[selected] = selectedPresentation
	}

	actionMap := assistantPresentActionMap(quickActions)
	promptMap := assistantPresentActionPromptMap(quickActions)

	root["quick_actions"] = quickActions
	root["quick_action_by_id"] = actionMap
	root["quick_action_prompts_by_id"] = promptMap
	root["assistant_ux"] = map[string]any{
		"schema_version":     "amux.assistant.channel-ux.v1",
		"supported_channels": assistantPresentSupportedChannels,
		"target_channel":     preferred,
		"selected_channel":   selected,
		"channels":           channels,
		"presentation":       selectedPresentation,
		"actions": map[string]any{
			"list":     quickActions,
			"map":      actionMap,
			"prompts":  promptMap,
			"fallback": assistantPresentActionFallback(quickActions),
		},
	}

	out, err := assistantCompatMarshalJSON(root)
	if err != nil {
		return append([]byte(nil), input...)
	}
	return append(out, '\n')
}

func assistantPresentHasNonSpace(input []byte) bool {
	for _, b := range input {
		if !strings.ContainsRune(" \t\r\n", rune(b)) {
			return true
		}
	}
	return false
}

func assistantPresentMessage(root map[string]any) (string, map[string]any) {
	channelMap := map[string]any{}
	if value, ok := root["channel"].(map[string]any); ok {
		channelMap = value
	}
	message := assistantPresentString(channelMap["message"])
	if strings.TrimSpace(message) == "" {
		message = assistantPresentString(root["message"])
	}
	if strings.TrimSpace(message) == "" {
		message = assistantPresentString(root["summary"])
	}
	return message, channelMap
}

func assistantPresentNormalizeActions(value any) []assistantPresentAction {
	items, ok := value.([]any)
	if !ok {
		return []assistantPresentAction{}
	}

	actions := make([]assistantPresentAction, 0, len(items))
	for i, item := range items {
		action, _ := item.(map[string]any)
		actionID := assistantPresentSanitizeActionID(
			firstNonEmpty(
				assistantPresentString(action["action_id"]),
				assistantPresentString(action["id"]),
				assistantPresentString(action["callback_data"]),
			),
			i,
		)
		actions = append(actions, assistantPresentAction{
			ID:           firstNonEmpty(assistantPresentString(action["id"]), actionID),
			ActionID:     actionID,
			Label:        firstNonEmpty(assistantPresentString(action["label"]), "Action"),
			Command:      assistantPresentString(action["command"]),
			Style:        assistantPresentNormalizeStyle(assistantPresentString(action["style"])),
			Prompt:       assistantPresentString(action["prompt"]),
			CallbackData: assistantPresentSanitizeCallbackData(assistantPresentString(action["callback_data"]), actionID),
		})
	}
	return actions
}

func assistantPresentSanitizeActionID(raw string, idx int) string {
	value := strings.ToLower(strings.TrimSpace(raw))
	if value == "" {
		value = fmt.Sprintf("action_%d", idx+1)
	}
	value = assistantPresentActionIDRegex.ReplaceAllString(value, "_")
	value = assistantPresentActionIDUnderscoreRegex.ReplaceAllString(value, "_")
	if len(value) > 64 {
		value = value[:64]
	}
	if value == "" {
		return fmt.Sprintf("action_%d", idx+1)
	}
	return value
}

func assistantPresentSanitizeCallbackData(raw, actionID string) string {
	value := strings.TrimSpace(raw)
	if value == "" {
		value = "qa:" + actionID
	}
	if len(value) > 64 {
		value = assistantPresentTruncate(value, 64)
	}
	return value
}

func assistantPresentNormalizeStyle(style string) string {
	switch strings.TrimSpace(style) {
	case "primary", "success", "danger":
		return strings.TrimSpace(style)
	default:
		return "primary"
	}
}

func assistantPresentNormalizeTargetChannel(raw string) string {
	value := strings.ToLower(strings.TrimSpace(raw))
	var b strings.Builder
	for _, r := range value {
		if (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') || r == '_' || r == '-' {
			b.WriteRune(r)
		}
	}
	value = b.String()
	if value == "teams" {
		return "msteams"
	}
	return value
}

func assistantPresentChannelSupported(channel string) bool {
	for _, candidate := range assistantPresentSupportedChannels {
		if candidate == channel {
			return true
		}
	}
	return false
}
