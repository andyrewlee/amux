package cli

import (
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

func assistantDXWriteJSON(w io.Writer, payload any) {
	enc := json.NewEncoder(w)
	enc.SetEscapeHTML(false)
	_ = enc.Encode(payload)
}

func assistantDXQuote(value string) string {
	if strings.HasPrefix(strings.TrimSpace(value), "amux assistant ") {
		return strings.TrimSpace(value)
	}
	return shellQuoteCommandValue(value)
}

func assistantDXSelectedChannel() string {
	channel := strings.TrimSpace(os.Getenv("AMUX_ASSISTANT_CHANNEL"))
	if channel == "" {
		return "telegram"
	}
	return channel
}

func assistantDXSelfScriptRef() string {
	if value := strings.TrimSpace(os.Getenv("AMUX_ASSISTANT_DX_CMD_REF")); value != "" {
		return value
	}
	return assistantCompatDefaultScriptRef("assistant-dx.sh")
}

func assistantDXNewAction(id, label, command, style, prompt string) assistantDXQuickAction {
	if strings.TrimSpace(style) == "" {
		style = "primary"
	}
	return assistantDXQuickAction{
		ID:       strings.TrimSpace(id),
		ActionID: strings.TrimSpace(id),
		Label:    strings.TrimSpace(label),
		Command:  strings.TrimSpace(command),
		Style:    style,
		Prompt:   strings.TrimSpace(prompt),
	}
}

func assistantDXBuildPayload(
	ok bool,
	command string,
	status string,
	summary string,
	nextAction string,
	suggestedCommand string,
	data any,
	quickActions []assistantDXQuickAction,
	message string,
) assistantDXPayload {
	if data == nil {
		data = map[string]any{}
	}
	if quickActions == nil {
		quickActions = []assistantDXQuickAction{}
	}
	byID := make(map[string]string, len(quickActions))
	for i := range quickActions {
		if quickActions[i].ActionID == "" {
			quickActions[i].ActionID = quickActions[i].ID
		}
		if quickActions[i].ID == "" {
			quickActions[i].ID = quickActions[i].ActionID
		}
		if quickActions[i].Style == "" {
			quickActions[i].Style = "primary"
		}
		if quickActions[i].ActionID != "" {
			byID[quickActions[i].ActionID] = quickActions[i].Command
		}
	}
	if strings.TrimSpace(message) == "" {
		message = strings.TrimSpace(summary)
	}
	channel := assistantDXSelectedChannel()
	return assistantDXPayload{
		OK:               ok,
		Command:          strings.TrimSpace(command),
		Status:           strings.TrimSpace(status),
		Summary:          strings.TrimSpace(summary),
		NextAction:       strings.TrimSpace(nextAction),
		SuggestedCommand: strings.TrimSpace(suggestedCommand),
		Data:             data,
		QuickActions:     quickActions,
		QuickActionByID:  byID,
		Channel: assistantDXChannelPayload{
			Message:       message,
			Chunks:        []string{message},
			ChunksMeta:    []assistantDXChunkMeta{{Index: 1, Total: 1, Text: message}},
			InlineButtons: []any{},
		},
		AssistantUX: assistantDXAssistantUXPayload{
			SelectedChannel: channel,
		},
	}
}

func assistantDXErrorPayload(command, message, details string) assistantDXPayload {
	return assistantDXBuildPayload(
		false,
		command,
		"command_error",
		message,
		"Check command usage and retry.",
		"",
		map[string]any{"details": strings.TrimSpace(details)},
		[]assistantDXQuickAction{},
		"⚠️ "+strings.TrimSpace(message),
	)
}

func assistantDXObject(value any) map[string]any {
	if object, ok := value.(map[string]any); ok && object != nil {
		return object
	}
	return map[string]any{}
}

func assistantDXArray(value any) []map[string]any {
	raw, ok := value.([]any)
	if !ok {
		return []map[string]any{}
	}
	items := make([]map[string]any, 0, len(raw))
	for _, item := range raw {
		if object, ok := item.(map[string]any); ok && object != nil {
			items = append(items, object)
		}
	}
	return items
}

func assistantDXStringValue(value any) string {
	switch typed := value.(type) {
	case string:
		return strings.TrimSpace(typed)
	default:
		return ""
	}
}

func assistantDXBoolValue(value any) bool {
	switch typed := value.(type) {
	case bool:
		return typed
	case string:
		lower := strings.ToLower(strings.TrimSpace(typed))
		return lower == "true" || lower == "1" || lower == "yes"
	case float64:
		return typed != 0
	default:
		return false
	}
}

func assistantDXFieldString(object map[string]any, key string) string {
	return assistantDXStringValue(object[key])
}

func assistantDXFieldBool(object map[string]any, key string) bool {
	return assistantDXBoolValue(object[key])
}

func assistantDXWorkspaceID(row map[string]any) string {
	return assistantDXFieldString(row, "id")
}

func assistantDXSessionWorkspaceID(row map[string]any) string {
	return assistantDXFieldString(row, "workspace_id")
}

func assistantDXSessionType(row map[string]any) string {
	return assistantDXFieldString(row, "type")
}

func assistantDXWorkspaceArchived(row map[string]any) bool {
	return assistantDXFieldBool(row, "archived")
}

func assistantDXMergeWorkspaceRows(groups ...[]map[string]any) []map[string]any {
	seen := map[string]struct{}{}
	rows := []map[string]any{}
	for _, group := range groups {
		for _, row := range group {
			id := assistantDXWorkspaceID(row)
			if id == "" {
				continue
			}
			if _, ok := seen[id]; ok {
				continue
			}
			seen[id] = struct{}{}
			rows = append(rows, row)
		}
	}
	return rows
}

func assistantDXUniqueIDs(rows []map[string]any, getID func(map[string]any) string) []string {
	seen := map[string]struct{}{}
	ids := []string{}
	for _, row := range rows {
		id := strings.TrimSpace(getID(row))
		if id == "" {
			continue
		}
		if _, ok := seen[id]; ok {
			continue
		}
		seen[id] = struct{}{}
		ids = append(ids, id)
	}
	return ids
}

func assistantDXContainsID(ids []string, target string) bool {
	target = strings.TrimSpace(target)
	if target == "" {
		return false
	}
	for _, id := range ids {
		if id == target {
			return true
		}
	}
	return false
}

func assistantDXFirstWorkspaceID(rows []map[string]any) string {
	for _, row := range rows {
		if id := assistantDXWorkspaceID(row); id != "" {
			return id
		}
	}
	return ""
}

func assistantDXFirstAgentWorkspaceID(sessions []map[string]any, visibleIDs []string) string {
	if len(visibleIDs) == 0 {
		return ""
	}
	seen := map[string]struct{}{}
	for _, row := range sessions {
		wsID := assistantDXSessionWorkspaceID(row)
		if wsID == "" {
			continue
		}
		if !assistantDXContainsID(visibleIDs, wsID) {
			continue
		}
		if _, ok := seen[wsID]; ok {
			continue
		}
		seen[wsID] = struct{}{}
		return wsID
	}
	return ""
}

func assistantDXFirstOrphanedAgentWorkspaceID(sessions []map[string]any, visibleIDs []string) string {
	seen := map[string]struct{}{}
	for _, row := range sessions {
		wsID := assistantDXSessionWorkspaceID(row)
		if wsID == "" || assistantDXContainsID(visibleIDs, wsID) {
			continue
		}
		if _, ok := seen[wsID]; ok {
			continue
		}
		seen[wsID] = struct{}{}
		return wsID
	}
	return ""
}

func assistantDXUnionWorkspaceCount(workspaces, agentSessions []map[string]any) int {
	seen := map[string]struct{}{}
	for _, row := range workspaces {
		if id := assistantDXWorkspaceID(row); id != "" {
			seen[id] = struct{}{}
		}
	}
	for _, row := range agentSessions {
		if id := assistantDXSessionWorkspaceID(row); id != "" {
			seen[id] = struct{}{}
		}
	}
	return len(seen)
}

func assistantDXFilterAgentSessions(rows []map[string]any) []map[string]any {
	items := make([]map[string]any, 0, len(rows))
	for _, row := range rows {
		if assistantDXSessionType(row) == "agent" {
			items = append(items, row)
		}
	}
	return items
}

func assistantDXArchivedRowsWithDefault(rows []map[string]any) []map[string]any {
	items := make([]map[string]any, 0, len(rows))
	for _, row := range rows {
		copied := make(map[string]any, len(row)+1)
		for key, value := range row {
			copied[key] = value
		}
		if _, ok := copied["archived"]; !ok {
			copied["archived"] = true
		}
		items = append(items, copied)
	}
	return items
}

func assistantDXArchivedOnlyRows(rows []map[string]any) []map[string]any {
	items := make([]map[string]any, 0, len(rows))
	for _, row := range rows {
		if assistantDXWorkspaceArchived(row) {
			items = append(items, row)
		}
	}
	return items
}

func assistantDXSortedAssistantKeys(object map[string]any) []string {
	keys := make([]string, 0, len(object))
	for key := range object {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return keys
}

func assistantDXConfigAssistants(configPath string) []string {
	configPath = strings.TrimSpace(configPath)
	if configPath == "" {
		return []string{}
	}
	body, err := os.ReadFile(filepath.Clean(configPath))
	if err != nil {
		return []string{}
	}
	var decoded map[string]any
	if err := json.Unmarshal(body, &decoded); err != nil {
		return []string{}
	}
	assistants := assistantDXObject(decoded["assistants"])
	return assistantDXSortedAssistantKeys(assistants)
}
