package workspacesvc

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/andyrewlee/amux/internal/data"
	"github.com/andyrewlee/amux/internal/logging"
	"github.com/andyrewlee/amux/internal/messages"
)

// ShelveWorkspace removes a workspace's worktree while keeping its branch and
// metadata, so the workspace can be restored later. It is the delete flow
// minus branch deletion and metadata removal, and it borrows delete's safety
// invariants: same validation guards, same script/session teardown, same
// per-repo git lock, same stale-path reconciliation.
//
// Ordering is crash-safe without a tombstone: the Shelved+Archived intent
// marker is persisted in ONE write BEFORE the worktree is removed. A process
// that dies mid-shelve leaves either a shelved record whose worktree still
// exists (removal never ran — discovery clears both flags, or restore adopts
// the existing worktree) or a prune-exempt shelved record (removal ran — the
// flags already exempt it from archive-retention and missing-root reaping).
// It never leaves Shelved-without-Archived — a record that is neither live
// (listByRepo filters Archived) nor shelved (the shelf requires both).
func (s *Service) ShelveWorkspace(project *data.Project, ws *data.Workspace) tea.Cmd {
	if project == nil || ws == nil {
		return func() tea.Msg {
			return messages.WorkspaceShelveFailed{
				Project:   project,
				Workspace: ws,
				Err:       errors.New("missing project or workspace"),
			}
		}
	}
	return func() tea.Msg {
		wsID := string(ws.ID())
		// Stamp the identity keys while the worktree still exists — see the
		// matching stamp in DeleteWorkspace.
		stampedIDs := WorkspaceIDStrings(ws)
		fail := func(stage string, err error) tea.Msg {
			logging.Error("workspace shelve failed workspace_id=%s stage=%s workspace_root=%s error=%v", wsID, stage, ws.Root, err)
			return messages.WorkspaceShelveFailed{Project: project, Workspace: ws, Err: err, WorkspaceIDs: stampedIDs}
		}
		// clearShelveIntent rolls the intent flags back when the shelve is
		// aborted while the worktree still exists — leaving them set would make
		// a live workspace prune-exempt and invisible (Archived filters it out
		// of the live list) for no reason. It is NOT used once the worktree is
		// gone: there the flags simply describe reality.
		clearShelveIntent := func() {
			// Revert the caller's in-memory flags too — they were set when the
			// intent marker persisted, and the worktree is still alive here.
			ws.Shelved = false
			ws.Archived = false
			ws.ArchivedAt = time.Time{}
			if s.store == nil {
				return
			}
			// Field transaction: only the flags change — a narrow write can't
			// resurrect stale fields the caller's snapshot never touched.
			err := s.store.Update(ws.MetadataID(), func(fresh *data.Workspace) (bool, error) {
				if !fresh.Shelved && !fresh.Archived && fresh.ArchivedAt.IsZero() {
					return false, nil
				}
				fresh.Shelved = false
				fresh.Archived = false
				fresh.ArchivedAt = time.Time{}
				return true, nil
			})
			if err != nil {
				logging.Error("workspace shelve intent rollback failed workspace_id=%s error=%v", wsID, err)
			}
		}

		if ws.IsPrimaryCheckout() {
			return fail("validate_primary_checkout", errors.New("cannot shelve primary checkout"))
		}
		if ws.Archived {
			return fail("validate_archived", errors.New("workspace is already archived"))
		}
		projectPath := data.NormalizePath(project.Path)
		if projectPath == "" || data.NormalizePath(ws.Repo) != projectPath {
			return fail("validate_repo_match", fmt.Errorf("workspace repo %s does not match project path %s", ws.Repo, project.Path))
		}
		if !isManagedWorkspacePathForProject(s.workspacesRoot, project, ws.Root) {
			return fail("validate_managed_root", fmt.Errorf("workspace root %s is outside managed project root", ws.Root))
		}

		// Intent marker first (see the doc comment for crash semantics). The
		// write sets Archived as well as Shelved in one store mutation: a
		// shelve IS an archive — the worktree is about to disappear — and a
		// crash or post-removal failure between the two writes would otherwise
		// leave Shelved-without-Archived, a record that is neither live
		// (listByRepo filters Archived) nor shelved (listShelvedWorkspaces
		// requires both) — a ghost row pointing at a missing dir.
		if s.store != nil {
			intentAt := time.Now()
			err := s.store.Update(ws.MetadataID(), func(fresh *data.Workspace) (bool, error) {
				fresh.Shelved = true
				fresh.Archived = true
				fresh.ArchivedAt = intentAt
				return true, nil
			})
			if err != nil {
				return fail("mark_shelved", err)
			}
			ws.Shelved = true
			ws.Archived = true
			ws.ArchivedAt = intentAt
		}

		// Teardown gate: seize lifecycle admission, drain every local lifecycle
		// process (in-flight setup, detached on-done hooks) and stop the run
		// script BEFORE the worktree is removed — it must not go away while a
		// child is still writing it. The gate rejects new starts until Finish.
		guard, err := s.beginWorkspaceTeardown(ws)
		if err != nil {
			clearShelveIntent()
			return fail("stop_scripts", err)
		}
		removed := false
		defer func() { guard.Finish(removed) }()

		// The archive script is the "worktree is about to disappear" hook —
		// shelving removes the worktree too, so it runs here under the held
		// gate on the same best-effort terms as delete.
		archiveWarning := s.runArchiveScriptForDelete(ws, guard)

		var stageFail tea.Msg
		func() {
			unlock := s.lockRepoGit(projectPath)
			defer unlock()
			if err := s.gitOps.RemoveWorkspace(projectPath, ws.Root); err != nil {
				stageFail = s.handleStaleRemoveError(project, ws, wsID, err, fail)
			}
		}()
		if stageFail != nil {
			clearShelveIntent()
			return stageFail
		}
		removed = true

		// Worktree is gone: the shelf is real from here on, so the intent flags
		// stay even if session teardown reports an error.
		if err := s.killWorkspaceSessionsForDeletedWorkspace(ws); err != nil {
			return fail("stop_sessions", err)
		}

		logging.Info("workspace shelved workspace_id=%s workspace_root=%s", wsID, ws.Root)
		return messages.WorkspaceShelved{Project: project, Workspace: ws, Warning: archiveWarning, WorkspaceIDs: stampedIDs}
	}
}

// RestoreWorkspace recreates a shelved workspace's worktree from its kept
// branch and returns the record to the live set. git.CreateWorkspace's
// branch-exists fallback (`worktree add -- <path> <branch>`) is exactly the
// restore primitive — the branch was deliberately kept by shelve.
func (s *Service) RestoreWorkspace(project *data.Project, ws *data.Workspace) tea.Cmd {
	if project == nil || ws == nil {
		return func() tea.Msg {
			return messages.WorkspaceRestoreFailed{
				Project:   project,
				Workspace: ws,
				Err:       errors.New("missing project or workspace"),
			}
		}
	}
	return func() tea.Msg {
		wsID := string(ws.ID())
		// Stamp the identity keys up front so result handlers can release the
		// lifecycle guard under every form the mark touched.
		stampedIDs := WorkspaceIDStrings(ws)
		fail := func(stage string, err error) tea.Msg {
			logging.Error("workspace restore failed workspace_id=%s stage=%s workspace_root=%s error=%v", wsID, stage, ws.Root, err)
			return messages.WorkspaceRestoreFailed{Project: project, Workspace: ws, Err: err, WorkspaceIDs: stampedIDs}
		}

		if !ws.Archived || !ws.Shelved {
			return fail("validate_shelved", errors.New("workspace is not shelved"))
		}
		// The request's snapshot flags passed validation, but they can be
		// stale: a second Enter racing this restore's own completion still
		// carries the pre-restore row. The store is authoritative — if the
		// record is already live, this is a duplicate: skip rather than
		// adopt/fail on the worktree the first restore just recreated.
		if s.store != nil {
			for _, id := range WorkspaceMetadataIDs(ws) {
				fresh, err := s.store.Load(id)
				if err != nil || fresh == nil {
					continue
				}
				if !fresh.Shelved || !fresh.Archived {
					logging.Info("workspace restore skipped: record already live workspace_id=%s workspace_root=%s", wsID, ws.Root)
					return messages.WorkspaceRestoreSkipped{Project: project, Workspace: ws, WorkspaceIDs: stampedIDs}
				}
				break
			}
		}
		projectPath := data.NormalizePath(project.Path)
		if projectPath == "" || data.NormalizePath(ws.Repo) != projectPath {
			return fail("validate_repo_match", fmt.Errorf("workspace repo %s does not match project path %s", ws.Repo, project.Path))
		}
		if !isManagedWorkspacePathForProject(s.workspacesRoot, project, ws.Root) {
			return fail("validate_managed_root", fmt.Errorf("workspace root %s is outside managed project root", ws.Root))
		}
		// An existing root is not always a rejection: a restore that crashed or
		// failed after `worktree add` leaves the dir behind while the record
		// stays shelved, and a hard "already exists" rejection would wedge every
		// retry. Adopt the dir only when it is already a git worktree (the
		// `.git` marker file/dir exists) — the managed-root check above already
		// bound it to this project's workspace area — otherwise keep failing.
		adopted := false
		if _, err := os.Stat(ws.Root); err == nil {
			if _, gerr := os.Stat(filepath.Join(ws.Root, ".git")); gerr != nil {
				return fail("validate_root_absent", fmt.Errorf("workspace path %s already exists", ws.Root))
			}
			adopted = true
		} else if !os.IsNotExist(err) {
			return fail("validate_root_absent", err)
		}
		branch := ws.Branch
		if branch == "" {
			return fail("validate_branch", errors.New("shelved workspace has no branch to restore from"))
		}
		base := ws.Base
		if base == "" {
			base = "HEAD"
		}

		created := false
		if !adopted {
			if err := s.createWorkspaceLocked(projectPath, ws.Root, branch, base); err != nil {
				return fail("worktree_add", err)
			}
			created = true
		}
		// Best-effort rollback of a worktree this call created when a later
		// stage fails — without it the dir lingers and every retry adopted or
		// rejected on it rather than recreating cleanly.
		rollback := func() {
			if !created {
				return
			}
			if err := s.gitOps.RemoveWorkspace(projectPath, ws.Root); err != nil {
				logging.Error("workspace restore rollback failed workspace_id=%s workspace_root=%s error=%v", wsID, ws.Root, err)
			}
		}
		if err := waitForGitPath(filepath.Join(ws.Root, ".git"), s.gitPathWaitTimeout); err != nil {
			rollback()
			return fail("wait_git", err)
		}

		if s.store != nil {
			err := s.store.Update(ws.MetadataID(), func(fresh *data.Workspace) (bool, error) {
				fresh.Archived = false
				fresh.Shelved = false
				fresh.ArchivedAt = time.Time{}
				return true, nil
			})
			if err != nil {
				rollback()
				return fail("unarchive", err)
			}
		}
		logging.Info("workspace restored workspace_id=%s workspace_root=%s", wsID, ws.Root)
		return messages.WorkspaceRestored{Project: project, Workspace: ws, WorkspaceIDs: stampedIDs}
	}
}

// listShelvedWorkspaces returns the repo's intentionally shelved workspaces
// (Archived && Shelved). Accidental archives are GC bookkeeping, not shelves,
// and are excluded.
func (s *Service) listShelvedWorkspaces(repoPath string) []data.Workspace {
	if s == nil || s.store == nil {
		return nil
	}
	all, err := s.store.ListByRepoIncludingArchived(repoPath)
	if err != nil {
		logging.Error("Failed to list shelved workspaces for %s: %v", repoPath, err)
		return nil
	}
	shelved := make([]data.Workspace, 0, len(all))
	for _, ws := range all {
		if ws != nil && ws.Archived && ws.Shelved {
			shelved = append(shelved, *ws)
		}
	}
	return shelved
}
