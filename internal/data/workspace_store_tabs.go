package data

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
)

// AppendOpenTab atomically appends a tab to workspace metadata.
// It acquires the workspace lock, reloads current metadata, and writes once to
// avoid lost-tab updates when multiple agent runs complete concurrently.
func (s *WorkspaceStore) AppendOpenTab(id WorkspaceID, tab TabInfo) error {
	if err := validateWorkspaceID(id); err != nil {
		return err
	}

	lockFiles, err := s.lockWorkspaceIDs(id)
	if err != nil {
		return err
	}
	defer unlockRegistryFiles(lockFiles)

	ws, err := s.load(id, true)
	if err != nil {
		return err
	}
	ws.storeID = id

	sessionName := strings.TrimSpace(tab.SessionName)
	if sessionName != "" {
		for _, existing := range ws.OpenTabs {
			if strings.TrimSpace(existing.SessionName) == sessionName {
				return nil
			}
		}
	}
	ws.OpenTabs = append(ws.OpenTabs, tab)

	return s.saveWorkspaceLocked(id, ws)
}

func (s *WorkspaceStore) saveWorkspaceLocked(id WorkspaceID, ws *Workspace) error {
	if ws == nil {
		return errors.New("workspace is required")
	}
	path := s.workspacePath(id)
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	data, err := json.MarshalIndent(ws, "", "  ")
	if err != nil {
		return err
	}
	tempPath := path + ".tmp"
	if err := os.WriteFile(tempPath, data, 0o644); err != nil {
		return err
	}
	if err := os.Rename(tempPath, path); err != nil {
		_ = os.Remove(tempPath)
		return err
	}
	return nil
}
