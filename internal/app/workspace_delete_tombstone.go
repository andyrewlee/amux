package app

import (
	"fmt"
	"os"

	"github.com/andyrewlee/amux/internal/data"
	"github.com/andyrewlee/amux/internal/logging"
)

// workspaceTombstoneStore is the optional capability of a WorkspaceStore that can
// record a durable delete tombstone. The real data.WorkspaceStore implements it;
// lightweight test fakes need not, in which case tombstone bookkeeping is a
// silent no-op (the in-memory delete-in-flight guard still applies).
type workspaceTombstoneStore interface {
	MarkDeleting(id data.WorkspaceID) error
	IsDeleting(id data.WorkspaceID) bool
	ClearDeleting(id data.WorkspaceID) error
}

// dirExists reports whether path is an existing directory.
func dirExists(path string) bool {
	if path == "" {
		return false
	}
	info, err := os.Stat(path)
	return err == nil && info.IsDir()
}

// markDeleteTombstone records a durable tombstone before a destructive delete.
// Once the store supports tombstones, failure must abort before the worktree is
// removed; otherwise an interrupted session cleanup has no durable retry key.
func (s *workspaceService) markDeleteTombstone(id data.WorkspaceID) error {
	if s == nil || s.store == nil {
		return nil
	}
	if td, ok := s.store.(workspaceTombstoneStore); ok {
		if err := td.MarkDeleting(id); err != nil {
			logging.Warn("workspace delete: failed to write tombstone workspace_id=%s error=%v", id, err)
			return fmt.Errorf("write delete tombstone: %w", err)
		}
	}
	return nil
}

func (s *workspaceService) markWorkspaceDeleteTombstones(ws *data.Workspace) error {
	for _, id := range workspaceMetadataIDs(ws) {
		if err := s.markDeleteTombstone(id); err != nil {
			return err
		}
	}
	return nil
}

// clearDeleteTombstone removes a workspace's delete tombstone.
func (s *workspaceService) clearDeleteTombstone(id data.WorkspaceID) {
	if s == nil || s.store == nil {
		return
	}
	if td, ok := s.store.(workspaceTombstoneStore); ok {
		if err := td.ClearDeleting(id); err != nil {
			logging.Warn("workspace delete: failed to clear tombstone workspace_id=%s error=%v", id, err)
		}
	}
}

func (s *workspaceService) clearWorkspaceDeleteTombstones(ws *data.Workspace) {
	for _, id := range workspaceMetadataIDs(ws) {
		s.clearDeleteTombstone(id)
	}
}

// finishInterruptedDelete completes a delete that was tombstoned but interrupted
// (e.g. the process quit/crashed after the worktree was removed but before the
// metadata was). It only fires when a tombstone exists AND the worktree is gone,
// so a tombstone left by a delete that failed before removing the worktree (dir
// still present) keeps the workspace usable. Returns true when the caller should
// skip surfacing the workspace (the delete finished or cleanup is in progress
// for already-removed state); returns false when the workspace should surface
// because the branch could not be deleted and the user needs to see it.
func (s *workspaceService) finishInterruptedDelete(ws *data.Workspace) bool {
	if s == nil || s.store == nil || ws == nil {
		return false
	}
	td, ok := s.store.(workspaceTombstoneStore)
	if !ok {
		return false
	}
	deleting := false
	for _, id := range workspaceMetadataIDs(ws) {
		if td.IsDeleting(id) {
			deleting = true
			break
		}
	}
	if !deleting {
		return false
	}
	metadataID := ws.MetadataID()
	if dirExists(ws.Root) {
		// A surviving worktree means an earlier delete failed before removing it;
		// do not finish the delete — the workspace must stay usable.
		return false
	}
	// The prior process may have exited after removing the worktree but before
	// reaching the normal session cleanup. The missing root plus durable
	// tombstone proves deletion passed validation, so no live agent for this
	// workspace is safe to retain.
	if err := s.killWorkspaceSessionsForDeletedWorkspace(ws); err != nil {
		logging.Warn("startup recovery: failed to stop sessions for interrupted delete workspace_id=%s error=%v", metadataID, err)
		if markErr := s.markWorkspaceDeleteTombstones(ws); markErr != nil {
			logging.Warn("startup recovery: failed to preserve delete tombstone workspace_id=%s error=%v", metadataID, markErr)
		}
		return true
	}
	// Retry the branch deletion. The worktree is confirmed gone (checked above),
	// so the branch is no longer checked out and git branch -D is safe. The
	// tombstone proves this delete already passed validation, so the branch is
	// amux's own. If the branch was already removed externally the delete fails
	// harmlessly and the tombstone stays for a later retry.
	if ws.Branch != "" && ws.Repo != "" {
		if err := s.gitOps.DeleteBranch(ws.Repo, ws.Branch); err != nil {
			// Surface the workspace instead of suppressing it. The worktree is
			// gone but the branch and metadata survive, so hiding the workspace
			// creates a UI/disk mismatch (hidden now, reappears after restart).
			// Surfacing lets the user see the incomplete delete and retry it;
			// the tombstone stays so the next load retries the branch deletion.
			logging.Warn("startup recovery: failed to delete branch %s workspace_id=%s error=%v", ws.Branch, metadataID, err)
			if markErr := s.markWorkspaceDeleteTombstones(ws); markErr != nil {
				logging.Warn("startup recovery: failed to preserve delete tombstone workspace_id=%s error=%v", metadataID, markErr)
			}
			return false
		}
	}
	if err := s.deleteWorkspaceMetadata(ws); err != nil {
		logging.Warn("startup recovery: failed to finish interrupted delete workspace_id=%s error=%v", metadataID, err)
		if markErr := s.markWorkspaceDeleteTombstones(ws); markErr != nil {
			logging.Warn("startup recovery: failed to preserve delete tombstone workspace_id=%s error=%v", metadataID, markErr)
		}
		return true
	}
	logging.Info("startup recovery: finished interrupted delete workspace_id=%s", metadataID)
	return true
}
