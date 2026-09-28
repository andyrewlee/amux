package messages

import (
	"github.com/andyrewlee/amux/internal/data"
)

// ShowShelveWorkspaceDialog requests the shelve confirmation dialog for a
// workspace — shelving removes the worktree but keeps the branch and
// metadata so it can be restored later.
type ShowShelveWorkspaceDialog struct {
	Project   *data.Project
	Workspace *data.Workspace
}

// ShelveWorkspace requests shelving a workspace (remove worktree, keep
// branch + metadata).
type ShelveWorkspace struct {
	Project   *data.Project
	Workspace *data.Workspace
}

// BulkWorkspaceItem is one marked row of a bulk lifecycle request: the
// project travels with the workspace because marks can span projects.
type BulkWorkspaceItem struct {
	Project   *data.Project
	Workspace *data.Workspace
}

// ShowBulkShelveWorkspaceDialog requests the bulk-shelve confirmation for
// the dashboard's marked live workspace rows. Confirmed, each item flows
// through the same ShelveWorkspace path a single shelve uses — the bulk
// dialog replaces N per-row confirms, not the per-row op.
type ShowBulkShelveWorkspaceDialog struct {
	Items []BulkWorkspaceItem
}

// ShowBulkRestoreWorkspaceDialog requests the bulk-restore confirmation
// for the dashboard's marked shelved rows. Confirmed, each item flows
// through the same RestoreWorkspace path a single restore uses.
type ShowBulkRestoreWorkspaceDialog struct {
	Items []BulkWorkspaceItem
}

// ShowBulkPurgeWorkspaceDialog requests the bulk-purge confirmation for
// the dashboard's marked shelved rows. Purge is the destructive op —
// branch, metadata, and settings go too — so the dialog requires typing
// the workspace count before the drain starts. Confirmed, each item
// flows through the same DeleteWorkspace path a single purge uses.
type ShowBulkPurgeWorkspaceDialog struct {
	Items []BulkWorkspaceItem
}

// WorkspaceShelved signals a shelve completed. Warning carries a best-effort
// archive-script notice, same as WorkspaceDeleted.Warning.
type WorkspaceShelved struct {
	Project   *data.Project
	Workspace *data.Workspace
	Warning   string
	// WorkspaceIDs carries the workspace's identity keys stamped BEFORE the
	// worktree was removed — see WorkspaceDeleted.WorkspaceIDs.
	WorkspaceIDs []string
}

// WorkspaceShelveFailed signals the shelve failed before or during worktree
// removal.
type WorkspaceShelveFailed struct {
	Project   *data.Project
	Workspace *data.Workspace
	Err       error
	// WorkspaceIDs: see WorkspaceDeleted.WorkspaceIDs.
	WorkspaceIDs []string
}

// RestoreWorkspace requests recreating a shelved workspace's worktree from
// its kept branch and returning the record to the live set.
type RestoreWorkspace struct {
	Project   *data.Project
	Workspace *data.Workspace
}

// WorkspaceRestored signals a shelved workspace's worktree was recreated;
// the app re-runs setup and reloads so the row becomes a normal workspace.
type WorkspaceRestored struct {
	Project   *data.Project
	Workspace *data.Workspace
	// WorkspaceIDs: see WorkspaceDeleted.WorkspaceIDs — stamped before the
	// worktree is recreated so guard cleanup covers every ID form.
	WorkspaceIDs []string
}

// WorkspaceRestoreFailed signals the restore failed (e.g. worktree add
// failed, path already exists, branch missing).
type WorkspaceRestoreFailed struct {
	Project   *data.Project
	Workspace *data.Workspace
	Err       error
	// WorkspaceIDs: see WorkspaceDeleted.WorkspaceIDs.
	WorkspaceIDs []string
}

// WorkspaceRestoreSkipped signals a restore request found the record already
// live — a stale duplicate, e.g. a second Enter racing the first restore's
// completion while its shelved-row snapshot was still on screen. It is a
// benign no-op: the workspace is already in the desired state, so the handler
// releases the lifecycle guard without re-running setup or surfacing an
// error.
type WorkspaceRestoreSkipped struct {
	Project   *data.Project
	Workspace *data.Workspace
	// WorkspaceIDs: see WorkspaceDeleted.WorkspaceIDs.
	WorkspaceIDs []string
}
