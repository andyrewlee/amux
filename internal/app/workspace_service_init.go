package app

import (
	"github.com/andyrewlee/amux/internal/data"
	"github.com/andyrewlee/amux/internal/process"
)

type workspaceService struct {
	registry       ProjectRegistry
	store          WorkspaceStore
	scripts        *process.ScriptRunner
	workspacesRoot string
}

func newWorkspaceService(registry ProjectRegistry, store WorkspaceStore, scripts *process.ScriptRunner, workspacesRoot string) *workspaceService {
	return &workspaceService{
		registry:       registry,
		store:          store,
		scripts:        scripts,
		workspacesRoot: workspacesRoot,
	}
}

func (s *workspaceService) resolvedDefaultAssistant() string {
	if s != nil && s.store != nil {
		return s.store.ResolvedDefaultAssistant()
	}
	return data.DefaultAssistant
}
