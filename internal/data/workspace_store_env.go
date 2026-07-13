package data

import (
	"fmt"
	"maps"
)

// SetEnv updates a workspace's environment-variable map and persists it. Like
// Rename (the same Tier-1 single-field-update shape, in workspace_store.go),
// it loads the workspace fresh from disk before mutating and saving in place,
// so a caller holding a possibly-stale in-memory Workspace (e.g. one captured
// when a dialog opened) cannot clobber a field another in-flight operation
// changed concurrently in the meantime. Split into its own file to keep
// workspace_store.go under the repo's 500-line file cap.
//
// Reserved-key exclusion is the caller's responsibility: internal/process
// already imports internal/data for data.Workspace, so this package importing
// internal/process back to reuse its reserved-key list would cycle. See
// process.IsReservedScriptEnvKey.
func (s *WorkspaceStore) SetEnv(id WorkspaceID, env map[string]string) error {
	ws, err := s.Load(id)
	if err != nil {
		return fmt.Errorf("set env for workspace %s: %w", id, err)
	}
	// No-op guard, mirroring Rename's same-name check: writing an identical
	// map would only rewrite the file and emit a spurious watch event.
	if maps.Equal(ws.Env, env) {
		return nil
	}
	ws.Env = env
	if err := s.Save(ws); err != nil {
		return fmt.Errorf("set env for workspace %s: %w", id, err)
	}
	return nil
}
