package app

import (
	"errors"
	"fmt"

	tea "charm.land/bubbletea/v2"

	"github.com/andyrewlee/amux/internal/data"
	"github.com/andyrewlee/amux/internal/messages"
)

// RemoveProject removes a project without deleting repository or worktree files.
func (s *workspaceService) RemoveProject(project *data.Project) tea.Cmd {
	if project == nil {
		return func() tea.Msg {
			return messages.Error{Err: errors.New("missing project"), Context: errorContext(errorServiceWorkspace, "removing project")}
		}
	}

	return func() tea.Msg {
		if s == nil || s.registry == nil {
			return messages.Error{Err: errors.New("registry unavailable"), Context: errorContext(errorServiceWorkspace, "removing project")}
		}
		if err := s.stopProjectScripts(project.Workspaces); err != nil {
			return messages.Error{Err: err, Context: errorContext(errorServiceWorkspace, "stopping project scripts")}
		}
		if err := s.registry.RemoveProject(project.Path); err != nil {
			return messages.Error{Err: err, Context: errorContext(errorServiceWorkspace, "removing project")}
		}
		s.releaseProjectPorts(project.Workspaces)
		// Discard amux's metadata and sessions while deliberately leaving the
		// repository and worktrees untouched, as promised by the dialog.
		s.removeProjectMetadata(project.Path, project.Workspaces...)
		return messages.ProjectRemoved{Path: project.Path}
	}
}

func (s *workspaceService) stopProjectScripts(workspaces []data.Workspace) error {
	if s == nil || s.scripts == nil {
		return nil
	}
	var errs []error
	for i := range workspaces {
		ws := &workspaces[i]
		if !s.scripts.IsRunning(ws) {
			continue
		}
		if err := s.scripts.Stop(ws); err != nil {
			errs = append(errs, fmt.Errorf("stop scripts for workspace %s: %w", ws.Name, err))
		}
	}
	return errors.Join(errs...)
}

func (s *workspaceService) releaseProjectPorts(workspaces []data.Workspace) {
	if s == nil || s.scripts == nil {
		return
	}
	for i := range workspaces {
		s.scripts.ReleaseWorkspace(&workspaces[i])
	}
}
