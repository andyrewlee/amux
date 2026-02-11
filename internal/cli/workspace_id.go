package cli

import (
	"fmt"
	"strings"

	"github.com/andyrewlee/amux/internal/data"
)

func parseWorkspaceIDFlag(raw string) (data.WorkspaceID, error) {
	wsID := data.WorkspaceID(strings.TrimSpace(raw))
	if !data.IsValidWorkspaceID(wsID) {
		return "", fmt.Errorf("invalid workspace id: %s", raw)
	}
	return wsID, nil
}
