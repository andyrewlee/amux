package app

import (
	"errors"
	"fmt"
	"strings"

	tea "charm.land/bubbletea/v2"

	"github.com/andyrewlee/amux/internal/data"
	"github.com/andyrewlee/amux/internal/logging"
	"github.com/andyrewlee/amux/internal/messages"
	"github.com/andyrewlee/amux/internal/process"
)

// Script lifecycle for the workspace service: running a workspace's own
// setup/run/archive commands, and the process bookkeeping that hangs off them.
// Split from workspace_service.go, which owns workspace CRUD.

// RunSetupAsync runs setup scripts asynchronously and returns a WorkspaceSetupComplete message.
func (s *workspaceService) RunSetupAsync(ws *data.Workspace) tea.Cmd {
	return func() tea.Msg {
		if s == nil || s.scripts == nil {
			return messages.WorkspaceSetupComplete{Workspace: ws}
		}
		if err := s.scripts.RunSetup(ws); err != nil {
			return messages.WorkspaceSetupComplete{Workspace: ws, Err: err}
		}
		return messages.WorkspaceSetupComplete{Workspace: ws}
	}
}

// TrustRepoScriptsAndRunSetupAsync records trust for the reviewed repo config and retries setup.
func (s *workspaceService) TrustRepoScriptsAndRunSetupAsync(ws *data.Workspace, expectedHash string) tea.Cmd {
	return func() tea.Msg {
		if s == nil || s.scripts == nil {
			return messages.WorkspaceSetupComplete{Workspace: ws}
		}
		if ws == nil {
			return messages.WorkspaceSetupComplete{Workspace: ws, Err: errors.New("workspace is required")}
		}
		if err := s.scripts.TrustRepoScriptsIfHash(ws.Repo, expectedHash); err != nil {
			if errors.Is(err, process.ErrScriptsChangedSincePrompt) {
				if setupErr := s.scripts.RunSetup(ws); setupErr != nil {
					return messages.WorkspaceSetupComplete{Workspace: ws, Err: setupErr}
				}
				return messages.WorkspaceSetupComplete{Workspace: ws}
			}
			return messages.WorkspaceSetupComplete{Workspace: ws, Err: err}
		}
		if err := s.scripts.RunSetup(ws); err != nil {
			return messages.WorkspaceSetupComplete{Workspace: ws, Err: err}
		}
		return messages.WorkspaceSetupComplete{Workspace: ws}
	}
}

func (s *workspaceService) stopWorkspaceScriptsForDelete(ws *data.Workspace) error {
	if s == nil || s.scripts == nil {
		return nil
	}
	return s.scripts.Stop(ws)
}

// runArchiveScriptForDelete runs the workspace's `archive` script to completion
// and returns a user-facing warning describing anything that went wrong, or ""
// when the script succeeded or there was nothing to run. It never returns an
// error: the delete proceeds regardless of what the archive script did.
//
// A repo whose .amux/workspaces.json is untrusted gets the same treatment as
// setup — the command is skipped, not run, and the user is told so.
func (s *workspaceService) runArchiveScriptForDelete(ws *data.Workspace) string {
	if s == nil || s.scripts == nil || ws == nil {
		return ""
	}
	// The script runs in the worktree, so a worktree that is already gone (an
	// external `git worktree remove`, or a delete resumed after a crash) has
	// nothing to archive. Skipping is silent because there is nothing wrong
	// here; running anyway would fail on the missing directory and warn the
	// user about a delete that is otherwise proceeding normally.
	if workspacePathGone(ws.Root) {
		return ""
	}

	err := s.scripts.RunArchive(ws)
	switch {
	case err == nil:
		return ""
	case errors.Is(err, process.ErrNoScriptConfigured):
		return ""
	case errors.Is(err, process.ErrScriptsNotTrusted):
		logging.Info("workspace delete skipped untrusted archive script workspace_id=%s", ws.ID())
		return fmt.Sprintf("Skipped the archive script for %s: repo not trusted yet", ws.Name)
	default:
		logging.Warn("workspace archive script failed workspace_id=%s error=%v", ws.ID(), err)
		return fmt.Sprintf("Archive script failed for %s: %v", ws.Name, err)
	}
}

// joinWarnings combines non-empty warning strings into one line so a delete that
// produced both an archive-script warning and a branch-cleanup warning surfaces
// both rather than dropping one.
func joinWarnings(warnings ...string) string {
	present := make([]string, 0, len(warnings))
	for _, w := range warnings {
		if w != "" {
			present = append(present, w)
		}
	}
	return strings.Join(present, "; ")
}

func (s *workspaceService) StopAll() {
	if s == nil || s.scripts == nil {
		return
	}
	s.scripts.StopAll()
}

// IsScriptRunning reports whether a script is currently running for ws.
func (s *workspaceService) IsScriptRunning(ws *data.Workspace) bool {
	if s == nil || s.scripts == nil || ws == nil {
		return false
	}
	return s.scripts.IsRunning(ws)
}

// ToggleScriptAsync starts ws's `run` script, or stops it when one is already
// running, off the UI goroutine. The started process is long-lived (a dev
// server), so the command reports only that the spawn succeeded — not that the
// script exited.
//
// Running is reported as the state the workspace actually ended up in: a failed
// start leaves it stopped, and a failed stop leaves it running, so the sidebar
// indicator stays truthful even on the error path.
func (s *workspaceService) ToggleScriptAsync(ws *data.Workspace) tea.Cmd {
	if s == nil || s.scripts == nil || ws == nil {
		return nil
	}
	return func() tea.Msg {
		if s.scripts.IsRunning(ws) {
			if err := s.scripts.Stop(ws); err != nil {
				return messages.WorkspaceScriptStateChanged{Workspace: ws, Running: true, Err: err}
			}
			return messages.WorkspaceScriptStateChanged{Workspace: ws, Running: false}
		}
		if _, err := s.scripts.RunScript(ws, process.ScriptRun); err != nil {
			return messages.WorkspaceScriptStateChanged{Workspace: ws, Running: false, Err: err}
		}
		return messages.WorkspaceScriptStateChanged{Workspace: ws, Running: true}
	}
}

// ReleaseWorkspacePort releases the deleted workspace's port allocation. The
// ScriptRunner gates the release on no script running for the workspace, so a
// release here cannot strand a live script's port.
func (s *workspaceService) ReleaseWorkspacePort(ws *data.Workspace) {
	if s == nil || s.scripts == nil || ws == nil {
		return
	}
	s.scripts.ReleaseWorkspace(ws)
}
