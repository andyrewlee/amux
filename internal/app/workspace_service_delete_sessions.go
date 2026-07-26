package app

import (
	"errors"
	"strings"

	"github.com/andyrewlee/amux/internal/data"
)

// killWorkspaceSessionsForDeletedWorkspace cleans both current-ID sessions and
// exact persisted session names. The latter covers agents created before a
// workspace-ID normalization migration.
func (s *workspaceService) killWorkspaceSessionsForDeletedWorkspace(ws *data.Workspace) error {
	if ws == nil {
		return nil
	}
	idErr := s.killWorkspaceSessionsForDelete(string(ws.ID()))
	if s.killWorkspaceSessionNames == nil {
		return idErr
	}
	sessionNames := make([]string, 0, len(ws.OpenTabs))
	seen := make(map[string]struct{}, len(ws.OpenTabs))
	for _, tab := range ws.OpenTabs {
		name := strings.TrimSpace(tab.SessionName)
		if name == "" {
			continue
		}
		if _, ok := seen[name]; ok {
			continue
		}
		seen[name] = struct{}{}
		sessionNames = append(sessionNames, name)
	}
	if len(sessionNames) > 0 {
		return errors.Join(idErr, s.killWorkspaceSessionNames(sessionNames))
	}
	return idErr
}
