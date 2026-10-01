package workspacesvc

import (
	"github.com/andyrewlee/amux/internal/data"
)

// WorkspaceCleanup is the honest recovery state derived from a workspace's
// delete tombstone: the marker is a boolean, so no stage or error text can
// be derived — only whether a marker exists and whether the worktree root
// survived. The status dialog renders this verbatim; it must never
// fabricate a finer-grained stage.
type WorkspaceCleanup int

const (
	// WorkspaceCleanupNone — no tombstone under any metadata ID, or no store
	// to probe. Renders as no cleanup section at all: absence of the section
	// is the no-pending-cleanup signal.
	WorkspaceCleanupNone WorkspaceCleanup = iota
	// WorkspaceCleanupUnknown — the probe ran but the workspace has no
	// readable identity (an empty metadata-ID set), so marker absence and
	// read failure cannot be distinguished. Renders as an explicit
	// "unknown" row rather than guessing pending/clean.
	WorkspaceCleanupUnknown
	// WorkspaceCleanupInterrupted — a tombstone exists and the worktree
	// root is still present: a delete was interrupted before removal.
	WorkspaceCleanupInterrupted
	// WorkspaceCleanupPending — a tombstone exists and the root is gone:
	// metadata/branch cleanup remains and startup recovery retries it
	// automatically on the next load.
	WorkspaceCleanupPending
)

// WorkspaceCleanupSnapshot reads the recovery state for ws — strictly
// read-only: IsDeleting plus a single os.Stat on the root. It performs no
// mutation; it is a diagnostic probe, not a retry path. Every alias ID in
// WorkspaceMetadataIDs is checked (mirroring finishInterruptedDelete) so a
// tombstone written under a legacy identity still surfaces.
func (s *Service) WorkspaceCleanupSnapshot(ws *data.Workspace) WorkspaceCleanup {
	if s == nil || s.store == nil || ws == nil {
		return WorkspaceCleanupNone
	}
	ids := WorkspaceMetadataIDs(ws)
	if len(ids) == 0 {
		return WorkspaceCleanupUnknown
	}
	for _, id := range ids {
		if s.store.IsDeleting(id) {
			if DirExists(ws.Root) {
				return WorkspaceCleanupInterrupted
			}
			return WorkspaceCleanupPending
		}
	}
	return WorkspaceCleanupNone
}
