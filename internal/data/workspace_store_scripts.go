package data

import "fmt"

// SetScripts updates a workspace's user-entered script commands and run mode,
// persisting them. Same Tier-1 load-fresh-then-save shape as SetEnv/Rename, so
// a possibly-stale in-memory Workspace (e.g. one captured when the scripts
// dialog opened) cannot clobber a field another in-flight operation changed.
func (s *WorkspaceStore) SetScripts(id WorkspaceID, scripts ScriptsConfig, mode string) error {
	if mode != "concurrent" {
		mode = "nonconcurrent"
	}
	err := s.Update(id, func(ws *Workspace) (bool, error) {
		if ws.Scripts == scripts && ws.ScriptMode == mode {
			return false, nil
		}
		ws.Scripts = scripts
		ws.ScriptMode = mode
		return true, nil
	})
	if err != nil {
		return fmt.Errorf("set scripts for workspace %s: %w", id, err)
	}
	return nil
}
