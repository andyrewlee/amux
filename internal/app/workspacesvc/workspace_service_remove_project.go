package workspacesvc

import (
	"errors"
	"fmt"

	tea "charm.land/bubbletea/v2"

	"github.com/andyrewlee/amux/internal/data"
	"github.com/andyrewlee/amux/internal/messages"
)

// RemoveProject removes a project without deleting repository or worktree files.
func (s *Service) RemoveProject(project *data.Project) tea.Cmd {
	if project == nil {
		return func() tea.Msg {
			return messages.Error{Err: errors.New("missing project"), Context: workspaceErrContext("removing project")}
		}
	}

	return func() tea.Msg {
		if s == nil || s.registry == nil {
			return messages.Error{Err: errors.New("registry unavailable"), Context: workspaceErrContext("removing project")}
		}
		if err := s.stopProjectScripts(project.Workspaces); err != nil {
			return messages.Error{Err: err, Context: workspaceErrContext("stopping project scripts")}
		}
		if err := s.registry.RemoveProject(project.Path); err != nil {
			return messages.Error{Err: err, Context: workspaceErrContext("removing project")}
		}
		s.releaseProjectPorts(project.Workspaces)
		// Discard amux's metadata and sessions while deliberately leaving the
		// repository and worktrees untouched, as promised by the dialog.
		s.removeProjectMetadata(project.Path, project.Workspaces...)
		return messages.ProjectRemoved{Path: project.Path}
	}
}

// stopProjectScripts drains every workspace's lifecycle work (in-flight setup,
// on-done hooks, the run script — hosted sessions included) before the
// project's metadata is removed. Each workspace's teardown gate is held for
// the duration of its drain and released immediately after: remove keeps the
// worktrees, so the gate does not need to outlive the stop.
func (s *Service) stopProjectScripts(workspaces []data.Workspace) error {
	if s == nil || s.scripts == nil {
		return nil
	}
	var errs []error
	for i := range workspaces {
		ws := &workspaces[i]
		if !s.scripts.IsRunning(ws) {
			continue
		}
		guard, err := s.scripts.BeginTeardown(ws)
		if err != nil {
			errs = append(errs, fmt.Errorf("stop scripts for workspace %s: %w", ws.Name, err))
			continue
		}
		// The worktree is kept — release admission rather than drop state.
		guard.Finish(false)
	}
	return errors.Join(errs...)
}

func (s *Service) releaseProjectPorts(workspaces []data.Workspace) {
	if s == nil || s.scripts == nil {
		return
	}
	for i := range workspaces {
		s.scripts.ReleaseWorkspace(&workspaces[i])
	}
}
