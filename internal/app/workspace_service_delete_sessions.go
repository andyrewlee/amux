package app

import (
	"errors"
	"fmt"
	"strings"

	"github.com/andyrewlee/amux/internal/data"
)

// workspaceMetadataIDs delegates to the full identity set — see
// data.WorkspaceIdentitySet, which this delegates to.
func workspaceMetadataIDs(ws *data.Workspace) []data.WorkspaceID {
	return data.WorkspaceIdentitySet(ws)
}

// killWorkspaceSessionsForDeletedWorkspace cleans both current-ID sessions and
// exact persisted session names. The latter covers agents created before a
// workspace-ID normalization migration.
func (s *workspaceService) killWorkspaceSessionsForDeletedWorkspace(ws *data.Workspace) error {
	if ws == nil {
		return nil
	}
	var errs []error
	for _, id := range workspaceMetadataIDs(ws) {
		errs = append(errs, s.killWorkspaceSessionsForDelete(string(id)))
	}
	if s.killWorkspaceSessionNames == nil {
		return errors.Join(errs...)
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
		errs = append(errs, s.killWorkspaceSessionNames(sessionNames))
	}
	return errors.Join(errs...)
}

func (s *workspaceService) deleteWorkspaceMetadata(ws *data.Workspace) error {
	if s == nil || s.store == nil || ws == nil {
		return nil
	}
	var errs []error
	for _, id := range workspaceMetadataIDs(ws) {
		if err := s.store.Delete(id); err != nil {
			errs = append(errs, fmt.Errorf("delete workspace metadata %s: %w", id, err))
		}
	}
	return errors.Join(errs...)
}
