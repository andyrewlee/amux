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

func newWorkspaceService(registry ProjectRegistry, store WorkspaceStore, scripts *process.ScriptRunner, workspacesRoot string, defaultAssistant ...string) *workspaceService {
	_ = defaultAssistant
	return &workspaceService{
		registry:       registry,
		store:          store,
		scripts:        scripts,
		workspacesRoot: workspacesRoot,
	}
}

func (s *workspaceService) resolvedDefaultAssistant() string {
	return data.DefaultAssistant
}
