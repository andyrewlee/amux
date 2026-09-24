package workspacesvc

import (
	"errors"
	"fmt"
	"strings"

	"github.com/andyrewlee/amux/internal/data"
)

// WorkspaceMetadataIDs is the service-facing alias for the canonical identity
// set — see data.WorkspaceIdentitySet, which this delegates to.
func WorkspaceMetadataIDs(ws *data.Workspace) []data.WorkspaceID {
	return data.WorkspaceIdentitySet(ws)
}

// WorkspaceIDStrings is the string form of WorkspaceMetadataIDs, for stamping
// onto lifecycle result messages before worktree removal.
func WorkspaceIDStrings(ws *data.Workspace) []string {
	return data.WorkspaceIdentityStrings(ws)
}

// WorkspaceIDsOrComputed returns the stamped ID set when present, else falls
// back to the computed dual-form set — used by cleanup paths that must also
// handle messages produced before the stamping existed.
func WorkspaceIDsOrComputed(ws *data.Workspace, stamped []string) []string {
	if len(stamped) > 0 {
		return stamped
	}
	return WorkspaceIDStrings(ws)
}

// killWorkspaceSessionsForDeletedWorkspace cleans both current-ID sessions and
// exact persisted session names. The latter covers agents created before a
// workspace-ID normalization migration.
func (s *Service) killWorkspaceSessionsForDeletedWorkspace(ws *data.Workspace) error {
	if ws == nil {
		return nil
	}
	var errs []error
	for _, id := range WorkspaceMetadataIDs(ws) {
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

func (s *Service) deleteWorkspaceMetadata(ws *data.Workspace) error {
	if s == nil || s.store == nil || ws == nil {
		return nil
	}
	var errs []error
	for _, id := range WorkspaceMetadataIDs(ws) {
		if err := s.store.Delete(id); err != nil {
			errs = append(errs, fmt.Errorf("delete workspace metadata %s: %w", id, err))
		}
	}
	return errors.Join(errs...)
}
