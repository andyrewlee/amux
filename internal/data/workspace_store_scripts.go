package data

import "fmt"

// SetScripts updates a workspace's user-entered script commands and run mode,
// persisting them. Same Tier-1 load-fresh-then-save shape as SetEnv/Rename, so
// a possibly-stale in-memory Workspace (e.g. one captured when the scripts
// dialog opened) cannot clobber a field another in-flight operation changed.
func (s *WorkspaceStore) SetScripts(id WorkspaceID, scripts ScriptsConfig, mode string) error {
	ws, err := s.Load(id)
	if err != nil {
		return fmt.Errorf("set scripts for workspace %s: %w", id, err)
	}
	if mode != "concurrent" {
		mode = "nonconcurrent"
	}
	if ws.Scripts == scripts && ws.ScriptMode == mode {
		return nil
	}
	ws.Scripts = scripts
	ws.ScriptMode = mode
	if err := s.Save(ws); err != nil {
		return fmt.Errorf("set scripts for workspace %s: %w", id, err)
	}
	return nil
}
