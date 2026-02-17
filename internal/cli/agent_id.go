package cli

import (
	"errors"
	"strings"

	"github.com/andyrewlee/amux/internal/tmux"
)

var (
	errInvalidAgentID              = errors.New("agent_id must be in workspace_id:tab_id format")
	errAgentNotFound               = errors.New("agent not found")
	tmuxSessionsWithTagsForAgentID = tmux.SessionsWithTags
)

func formatAgentID(workspaceID, tabID string) string {
	workspaceID = strings.TrimSpace(workspaceID)
	tabID = strings.TrimSpace(tabID)
	if workspaceID == "" || tabID == "" {
		return ""
	}
	return workspaceID + ":" + tabID
}

func parseAgentID(agentID string) (string, string, error) {
	parts := strings.SplitN(strings.TrimSpace(agentID), ":", 2)
	if len(parts) != 2 {
		return "", "", errInvalidAgentID
	}
	workspaceID := strings.TrimSpace(parts[0])
	tabID := strings.TrimSpace(parts[1])
	if workspaceID == "" || tabID == "" {
		return "", "", errInvalidAgentID
	}
	return workspaceID, tabID, nil
}

func resolveSessionNameForAgentID(agentID string, opts tmux.Options) (string, error) {
	workspaceID, tabID, err := parseAgentID(agentID)
	if err != nil {
		return "", err
	}
	rows, err := tmuxSessionsWithTagsForAgentID(
		map[string]string{
			"@amux":           "1",
			"@amux_workspace": workspaceID,
			"@amux_tab":       tabID,
		},
		nil,
		opts,
	)
	if err != nil {
		return "", err
	}
	if len(rows) == 0 {
		return "", errAgentNotFound
	}
	return rows[0].Name, nil
}
