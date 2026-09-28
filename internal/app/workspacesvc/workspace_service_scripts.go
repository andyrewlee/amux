package workspacesvc

import (
	"errors"
	"fmt"

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
func (s *Service) RunSetupAsync(ws *data.Workspace) tea.Cmd {
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
func (s *Service) TrustRepoScriptsAndRunSetupAsync(ws *data.Workspace, expectedHash string) tea.Cmd {
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

// beginWorkspaceTeardown seizes the workspace's lifecycle gate and drains
// every local lifecycle process (in-flight setup, detached on-done hooks)
// plus the run script — hosted sessions and the local run slot alike — before
// the caller removes anything. The returned guard holds the admission gate
// across archive+removal so nothing new can start mid-teardown; callers must
// Finish it. A nil scripts service returns a nil guard, nil error.
func (s *Service) beginWorkspaceTeardown(ws *data.Workspace) (*process.TeardownGuard, error) {
	if s == nil || s.scripts == nil {
		return nil, nil
	}
	return s.scripts.BeginTeardown(ws)
}

// runArchiveScriptForDelete runs the workspace's `archive` script to completion
// and returns a user-facing warning describing anything that went wrong, or ""
// when the script succeeded or there was nothing to run. It never returns an
// error: the delete proceeds regardless of what the archive script did.
//
// The archive runs under the caller's held teardown guard — the only archive
// admission allowed while the gate is held — so it executes after prior work
// has drained and before the tree is removed, never racing a foreign teardown.
//
// A repo whose .amux/workspaces.json is untrusted gets the same treatment as
// setup — the command is skipped, not run, and the user is told so.
func (s *Service) runArchiveScriptForDelete(ws *data.Workspace, guard *process.TeardownGuard) string {
	if s == nil || s.scripts == nil || ws == nil || guard == nil {
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

	err := guard.RunArchive(ws)
	switch {
	case err == nil:
		return ""
	case errors.Is(err, process.ErrNoScriptConfigured):
		return ""
	case errors.Is(err, process.ErrScriptsNotTrusted):
		logging.Info("workspace delete skipped untrusted archive script workspace_id=%s", ws.ID())
		return fmt.Sprintf("Skipped the archive script for %s: repo not trusted yet", ws.Name)
	default:
		logging.Error("workspace archive script failed workspace_id=%s error=%v", ws.ID(), err)
		return fmt.Sprintf("Archive script failed for %s: %v", ws.Name, err)
	}
}

func (s *Service) StopAll() {
	if s == nil || s.scripts == nil {
		return
	}
	s.scripts.StopAll()
}

// IsScriptRunning reports whether the workspace's `run` script is live — the
// run-only query the sidebar badge and toggle decision use. Coordinator-tracked
// lifecycle work (setup/on-done) does not count; the runner's broader IsRunning
// covers that for release/teardown decisions.
func (s *Service) IsScriptRunning(ws *data.Workspace) bool {
	if s == nil || s.scripts == nil || ws == nil {
		return false
	}
	return s.scripts.RunActive(ws)
}

// RunScriptStatus reports the workspace's run state for UI surfacing:
// whether any run is alive and, when the last one died, its exit code (-1
// when no exit has been recorded). Used to turn a running→stopped transition
// into a "exited with status N" notice rather than silent badge-off.
func (s *Service) RunScriptStatus(ws *data.Workspace) (alive bool, lastExit int) {
	if s == nil || s.scripts == nil || ws == nil {
		return false, -1
	}
	return s.scripts.RunScriptStatus(ws)
}

// RunScriptOutput returns up to lines of the workspace's run output — the
// live pane tail while running, the remain-on-exit tail after it finishes.
// Empty under the subprocess fallback or when nothing has run.
func (s *Service) RunScriptOutput(ws *data.Workspace, lines int) string {
	if s == nil || s.scripts == nil || ws == nil {
		return ""
	}
	return s.scripts.RunScriptOutput(ws, lines)
}

// RunScriptOutputAndStatus is the one-sweep combined fetch — output and
// status from a single hosted scan, for the run-output viewer's open and
// refresh fetches which otherwise each pay two full tmux sweeps.
func (s *Service) RunScriptOutputAndStatus(ws *data.Workspace, lines int) (output string, alive bool, lastExit int) {
	if s == nil || s.scripts == nil || ws == nil {
		return "", false, -1
	}
	return s.scripts.RunScriptOutputAndStatus(ws, lines)
}

// ToggleScriptAsync starts ws's `run` script, or stops it when one is already
// running, off the UI goroutine. The started process is long-lived (a dev
// server), so the command reports only that the spawn succeeded — not that the
// script exited.
//
// Running is reported as the state the workspace actually ended up in: a failed
// start leaves it stopped, and a failed stop leaves it running, so the sidebar
// indicator stays truthful even on the error path.
func (s *Service) ToggleScriptAsync(ws *data.Workspace) tea.Cmd {
	if s == nil || s.scripts == nil || ws == nil {
		return nil
	}
	return func() tea.Msg {
		// RunActive is the run-only query: an in-flight setup or on-done hook
		// must not flip the toggle into "stop" when no run script is running.
		if s.scripts.RunActive(ws) {
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
func (s *Service) ReleaseWorkspacePort(ws *data.Workspace) {
	if s == nil || s.scripts == nil || ws == nil {
		return
	}
	s.scripts.ReleaseWorkspace(ws)
}

// RunSessionList enumerates the workspace's hosted run sessions with
// liveness, base-first then numeric suffix — the run-session picker's row
// set. Nil under the subprocess fallback.
func (s *Service) RunSessionList(ws *data.Workspace) []process.RunSessionEntry {
	if s == nil || s.scripts == nil {
		return nil
	}
	return s.scripts.RunSessionList(ws)
}

// RunScriptSessionTail returns up to lines of the NAMED run session's pane —
// the picker-targeted sibling of RunScriptOutput (which always tails newest).
func (s *Service) RunScriptSessionTail(name string, lines int) string {
	if s == nil || s.scripts == nil || name == "" {
		return ""
	}
	return s.scripts.RunSessionTail(name, lines)
}

// RunScriptSessionAlive reports whether the NAMED run session's pane is still
// running — the attach-path's recheck for a session that may have exited
// between picker enumeration and selection.
func (s *Service) RunScriptSessionAlive(name string) bool {
	if s == nil || s.scripts == nil || name == "" {
		return false
	}
	return s.scripts.RunSessionAlive(name)
}

// RunScriptAttachTarget returns the newest alive hosted run-session name for
// interactive attach; false under the subprocess fallback, a nil service, or
// when nothing is alive.
func (s *Service) RunScriptAttachTarget(ws *data.Workspace) (string, bool) {
	if s == nil || s.scripts == nil || ws == nil {
		return "", false
	}
	return s.scripts.RunScriptAttachTarget(ws)
}
