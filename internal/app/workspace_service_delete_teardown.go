package app

import (
	tea "charm.land/bubbletea/v2"

	"github.com/andyrewlee/amux/internal/data"
	"github.com/andyrewlee/amux/internal/logging"
	"github.com/andyrewlee/amux/internal/process"
)

// This file holds the pre-removal teardown steps of workspace delete: stop
// managed scripts, kill the workspace's tmux sessions, then kill and verify
// orphaned service groups. Split out of workspace_service.go for the repo's
// 500-line file cap.

func (s *workspaceService) stopWorkspaceScriptsForDelete(ws *data.Workspace) error {
	if s == nil || s.scripts == nil {
		return nil
	}
	return s.scripts.Stop(ws)
}

func (s *workspaceService) killWorkspaceSessionsForDelete(wsID string) {
	if s != nil && s.killWorkspaceSessions != nil {
		s.killWorkspaceSessions(wsID)
	}
}

// teardownWorkspaceProcessesForDelete kills orphaned service process groups
// still referencing the workspace root and refuses the delete when any
// survive. It runs after the session kill, so surviving groups are exactly
// the ones that would otherwise be orphaned by the worktree removal.
func (s *workspaceService) teardownWorkspaceProcessesForDelete(
	ws *data.Workspace, wsID string, fail func(stage string, err error) tea.Msg,
) tea.Msg {
	if s == nil || s.teardownProcesses == nil {
		return nil
	}
	res, err := s.teardownProcesses(ws.Root)
	if len(res.Killed) > 0 {
		logging.Info(
			"workspace delete tore down %d orphaned service group(s) workspace_id=%s: %s",
			len(res.Killed), wsID, process.DescribeGroups(res.Killed),
		)
	}
	if err != nil {
		return fail("teardown_processes", err)
	}
	return nil
}
