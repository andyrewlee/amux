package workspacesvc

import (
	"errors"

	"github.com/andyrewlee/amux/internal/data"
	"github.com/andyrewlee/amux/internal/process"
)

// App-facing mutation/read methods. These exist so internal/app never touches
// the raw store or runner: every method owns the identity choice
// (MetadataID(), the persisted store key) in one place, and the service can
// intercept writes later (cache invalidation, messaging, policy) without
// chasing escape-hatch callers. Read methods are nil-runner safe so callers
// don't need to probe internals.

var errWorkspaceStoreUnavailable = errors.New("workspace store unavailable")

// RenameWorkspace updates the workspace's display name under its persisted
// store key.
func (s *Service) RenameWorkspace(ws *data.Workspace, name string) error {
	if s == nil || s.store == nil || ws == nil {
		return errWorkspaceStoreUnavailable
	}
	return s.store.Rename(ws.MetadataID(), name)
}

// SetWorkspaceEnv replaces the workspace's env overrides under its persisted
// store key.
func (s *Service) SetWorkspaceEnv(ws *data.Workspace, env map[string]string) error {
	if s == nil || s.store == nil || ws == nil {
		return errWorkspaceStoreUnavailable
	}
	return s.store.SetEnv(ws.MetadataID(), env)
}

// SetWorkspaceScripts replaces the workspace's script overrides and mode
// under its persisted store key.
func (s *Service) SetWorkspaceScripts(ws *data.Workspace, scripts data.ScriptsConfig, mode string) error {
	if s == nil || s.store == nil || ws == nil {
		return errWorkspaceStoreUnavailable
	}
	return s.store.SetScripts(ws.MetadataID(), scripts, mode)
}

// ScriptConfig loads the repo-level script config (.amux/workspaces.json).
// A nil runner reports (nil, nil) — same shape LoadConfig uses for a repo
// with no config, so callers treat both as "nothing configured".
func (s *Service) ScriptConfig(repoPath string) (*process.WorkspaceConfig, error) {
	if s == nil || s.scripts == nil || repoPath == "" {
		return nil, nil
	}
	return s.scripts.LoadConfig(repoPath)
}

// WorkspaceScriptsTrusted reports whether the repo's scripts are trusted. A
// nil runner reports false — callers render that as untrusted, the safe
// default.
func (s *Service) WorkspaceScriptsTrusted(repoPath string) (bool, error) {
	if s == nil || s.scripts == nil {
		return false, nil
	}
	return s.scripts.ScriptsTrusted(repoPath)
}

// WorkspaceScriptPort returns the run-script port allocated to the workspace.
func (s *Service) WorkspaceScriptPort(ws *data.Workspace) (int, bool) {
	if s == nil || s.scripts == nil || ws == nil {
		return 0, false
	}
	return s.scripts.PortAllocated(ws)
}

// LastScriptOutputs returns the runner's recorded lifecycle-script
// transcripts for the workspace; nil runner reports an empty set.
func (s *Service) LastScriptOutputs(ws *data.Workspace) map[process.ScriptType]process.ScriptOutput {
	if s == nil || s.scripts == nil || ws == nil {
		return nil
	}
	return s.scripts.LastScriptOutputs(ws)
}

// RunOnDoneScript fires the workspace's on-done script for a session; a nil
// runner is a no-op.
func (s *Service) RunOnDoneScript(ws *data.Workspace, sessionName string) error {
	if s == nil || s.scripts == nil || ws == nil {
		return nil
	}
	return s.scripts.RunOnDone(ws, sessionName)
}
