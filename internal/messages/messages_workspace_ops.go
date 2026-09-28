package messages

import (
	"github.com/andyrewlee/amux/internal/data"
	"github.com/andyrewlee/amux/internal/git"
	"github.com/andyrewlee/amux/internal/process"
)

// WorkspaceCommitted is sent when a commit-all attempt finishes. Err is non-nil
// on failure (surfaced via ReportError); on success the sidebar diff/status view
// is refreshed for the workspace.
type WorkspaceCommitted struct {
	Workspace *data.Workspace
	Err       error
}

// MergeWorkspace requests merging a workspace's branch into its base branch in
// the project's primary checkout. The precondition check (is the base actually
// checked out?) runs in the handler, before any confirm dialog is shown.
//
// The workspace alone identifies the merge: Repo names the primary checkout to
// merge in, Branch what to merge, and Base what to merge into. No project is
// carried because none is needed.
type MergeWorkspace struct {
	Workspace *data.Workspace
}

// ShowMergeWorkspaceDialog requests the merge confirmation dialog. Base is the
// local branch the merge will land on, resolved and verified by the handler, so
// the dialog can state the exact command that will run.
type ShowMergeWorkspaceDialog struct {
	Workspace *data.Workspace
	Base      string
}

// MergeWorkspaceRefused reports that the merge precondition did not hold, so no
// dialog is shown and nothing is written. Reason is the user-facing
// explanation; Err is set only when the check itself failed (as opposed to
// answering "no"), so the app can tell a refusal apart from a fault.
type MergeWorkspaceRefused struct {
	Workspace *data.Workspace
	Reason    string
	Err       error
}

// WorkspaceMerged is sent when a merge attempt finishes. A conflict arrives
// here too, as an Err wrapping git.ErrMergeConflict, because a stopped merge is
// an outcome the user must act on rather than a silent failure.
type WorkspaceMerged struct {
	Workspace *data.Workspace
	Base      string
	Err       error
}

// AbortWorkspaceMerge requests abandoning the merge left in progress in the
// workspace's primary checkout.
type AbortWorkspaceMerge struct {
	Workspace *data.Workspace
}

// WorkspaceMergeAborted reports the outcome of an AbortWorkspaceMerge.
type WorkspaceMergeAborted struct {
	Workspace *data.Workspace
	Err       error
}

// ShowRemoveProjectDialog requests showing the remove project confirmation
type ShowRemoveProjectDialog struct {
	Project *data.Project
}

// CreateWorkspace requests creating a new workspace
type CreateWorkspace struct {
	Project   *data.Project
	Name      string
	Base      string
	Assistant string
}

// DeleteWorkspace requests deleting a workspace
type DeleteWorkspace struct {
	Project   *data.Project
	Workspace *data.Workspace
}

// RenameWorkspace requests renaming a workspace's display label (Tier-1). Only
// the human Name changes; the git branch, worktree, and workspace ID are left
// untouched.
type RenameWorkspace struct {
	Project   *data.Project
	Workspace *data.Workspace
	NewName   string
}

// RemoveProject requests removing a project from the registry
type RemoveProject struct {
	Project *data.Project
}

// AddProject requests adding a new project
type AddProject struct {
	Path string
}

// ShowSelectAssistantDialog requests showing the assistant selection dialog
type ShowSelectAssistantDialog struct{}

// LaunchAgent requests launching an agent in a new tab
type LaunchAgent struct {
	Assistant string
	Workspace *data.Workspace
}

// OpenDiff requests opening a diff viewer for a file
type OpenDiff struct {
	Change    *git.Change
	Mode      git.DiffMode
	Workspace *data.Workspace
}

// CloseTab requests closing the current tab
type CloseTab struct{}

// ShowCleanupTmuxDialog requests confirmation before cleaning tmux sessions.
type ShowCleanupTmuxDialog struct{}

// CleanupTmuxSessions requests cleanup of amux tmux sessions.
type CleanupTmuxSessions struct{}

// WorkspaceCreatedWithWarning indicates workspace was created but setup had issues
type WorkspaceCreatedWithWarning struct {
	Workspace *data.Workspace
	Warning   string
}

// ToggleWorkspaceScript requests starting a workspace's `run` script, or
// stopping it when it is already running. The app owns the ScriptRunner and so
// decides which of the two applies; the sender only names the workspace.
type ToggleWorkspaceScript struct {
	Workspace *data.Workspace
}

// RerunWorkspaceScript requests re-running a workspace's lifecycle script on
// demand — today only `setup` is user-triggerable this way (a failed or
// post-edit setup needs a retry that doesn't require shelve+restore churn).
// `archive` stays non-triggerable deliberately: it is a destructive teardown
// hook, not a retryable provisioning step.
type RerunWorkspaceScript struct {
	Workspace *data.Workspace
	Script    process.ScriptType
}

// WorkspaceScriptStateChanged reports the outcome of a ToggleWorkspaceScript.
// Running is the state the workspace's run script ended up in, so the sidebar
// can show an accurate indicator even when the request failed.
type WorkspaceScriptStateChanged struct {
	Workspace *data.Workspace
	Running   bool
	Err       error
}

// WorkspaceOnDoneResult reports the spawn outcome of a workspace's `on-done`
// hook, fired by the activity loop on a strict working→done session edge.
// Err is nil on success; the hook itself runs detached, so this is only the
// resolution/spawn result (untrusted-repo skip or exec failure), not the
// hook's own exit.
type WorkspaceOnDoneResult struct {
	Workspace   *data.Workspace
	SessionName string
	Err         error
}

// GitStatusTick triggers periodic git status refresh
type GitStatusTick struct{}

// OrphanGCTick triggers periodic tmux orphan session cleanup.
type OrphanGCTick struct{}
