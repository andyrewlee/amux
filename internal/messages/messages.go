package messages

import (
	"github.com/andyrewlee/amux/internal/data"
	"github.com/andyrewlee/amux/internal/git"
)

// PaneType identifies the focused pane
type PaneType int

const (
	PaneDashboard PaneType = iota
	PaneCenter
	PaneSidebar
	PaneSidebarTerminal
)

// ProjectsLoaded is sent when projects have been loaded/reloaded
type ProjectsLoaded struct {
	Projects []data.Project
	// LoadToken identifies the load generation; handleProjectsLoaded drops a
	// result older than the last applied one so out-of-order reloads (e.g. under
	// rapid deletes) cannot resurrect a just-deleted workspace. Zero applies
	// unconditionally (back-compat).
	LoadToken int
}

// WorkspaceActivated is sent when a workspace is selected
type WorkspaceActivated struct {
	Project   *data.Project
	Workspace *data.Workspace
}

// WorkspaceCreated is sent when a new workspace is created
type WorkspaceCreated struct {
	Workspace *data.Workspace
}

// WorkspaceSetupComplete is sent when async setup scripts finish
type WorkspaceSetupComplete struct {
	Workspace *data.Workspace
	Err       error
	// Rerun marks the completion of a user-triggered re-run (as opposed to the
	// automatic create/restore/trust-approval run). The handler uses it to add
	// user-facing confirmation the silent initial run deliberately lacks.
	Rerun bool
}

// WorkspaceCreateFailed is sent when a workspace creation fails
type WorkspaceCreateFailed struct {
	Workspace *data.Workspace
	Err       error
}

// WorkspaceDeleted is sent when a workspace is deleted
type WorkspaceDeleted struct {
	Project   *data.Project
	Workspace *data.Workspace
	Err       error
	// Warning is a non-fatal note (e.g. an archive script returned a warning).
	// The workspace delete still succeeded; this is surfaced to the user as a toast.
	Warning string
	// WorkspaceIDs carries the workspace's identity keys stamped BEFORE the
	// worktree was removed. ws.ID() drifts post-removal (NormalizePath only
	// resolves existing paths), so cleanup must use this set rather than
	// recomputing — it contains both the resolved ID form the UI keyed tabs
	// under and the persisted MetadataID form.
	WorkspaceIDs []string
}

// WorkspaceDeleteFailed is sent when a workspace deletion fails
type WorkspaceDeleteFailed struct {
	Project   *data.Project
	Workspace *data.Workspace
	Err       error
	// WorkspaceIDs: see WorkspaceDeleted — stamped pre-removal so failure
	// cleanup can unmark every ID form the in-flight guard set.
	WorkspaceIDs []string
}

// ProjectRemoved is sent when a project is unregistered
type ProjectRemoved struct {
	Path string
}

// GitStatusRequest requests a git status refresh
type GitStatusRequest struct {
	Root string
}

// GitStatusResult contains the result of a git status command
type GitStatusResult struct {
	Root   string
	Status *git.StatusResult
	Err    error
	// Tracked marks a result produced by the app's status-request dedup
	// layer: only tracked results may clear the root's in-flight mark and
	// drain coalesced follow-ups. Cache-hit and externally produced results
	// leave it false.
	Tracked bool
}

// GitStatusBatchResult carries per-root status results from one batched
// refresh pass. A projects reload emits one batch instead of one message per
// workspace, so the UI pays a single render pass for the whole reload rather
// than one per workspace.
type GitStatusBatchResult struct {
	Results []GitStatusResult
}

// TabCreated is sent when a new tab is created
type TabCreated struct {
	Index int
	Name  string
}

// TabClosed is sent when a tab is closed
type TabClosed struct {
	Index int
}

// TabDetached is sent when a tab is detached (tmux session remains).
type TabDetached struct {
	WorkspaceID string
	Index       int
}

// TabReattached is sent when a detached tab is reattached.
type TabReattached struct {
	WorkspaceID string
	TabID       string
}

// TabStateChanged indicates a tab state change that should be persisted.
type TabStateChanged struct {
	WorkspaceID string
	TabID       string
}

// ToastLevel identifies the type of toast notification to display.
type ToastLevel string

const (
	ToastInfo    ToastLevel = "info"
	ToastSuccess ToastLevel = "success"
	ToastError   ToastLevel = "error"
	ToastWarning ToastLevel = "warning"
)

// Toast requests a toast notification in the UI.
type Toast struct {
	Message string
	Level   ToastLevel
}

// TabSessionStatus reports a tmux session status change for a tab.
type TabSessionStatus struct {
	WorkspaceID string
	SessionName string
	Status      string
}

// TabSelectionChanged indicates the active tab changed for a workspace.
type TabSelectionChanged struct {
	WorkspaceID string
	ActiveIndex int
}

// Error represents an application error
type Error struct {
	Err     error
	Context string
	Logged  bool
}

// MarkCriticalExternalMsg marks Error as a critical external message: the
// app msgpump must never evict it from the lossy external queue.
func (Error) MarkCriticalExternalMsg() {}

func (e Error) Error() string {
	if e.Context != "" {
		return e.Context + ": " + e.Err.Error()
	}
	return e.Err.Error()
}

// ShowWelcome requests showing the welcome screen
type ShowWelcome struct{}

// ShowCommandsPalette requests opening the bottom command palette.
type ShowCommandsPalette struct{}

// ShowQuitDialog requests showing the quit confirmation dialog
type ShowQuitDialog struct{}

// PTYWatchdogTick triggers a periodic check for stalled PTY readers.
type PTYWatchdogTick struct{}

// TmuxSyncTick triggers a periodic tmux session sync for the active workspace.
type TmuxSyncTick struct {
	Token int
}

// SidebarPTYRestart requests restarting a sidebar PTY reader.
type SidebarPTYRestart struct {
	WorkspaceID string
	TabID       string
}

// ToggleKeymapHints toggles display of keymap helper text
type ToggleKeymapHints struct{}

// RefreshDashboard requests a dashboard refresh
type RefreshDashboard struct{}

// RescanWorkspaces requests a git worktree rescan/import.
type RescanWorkspaces struct{}

// ShowAddProjectDialog requests showing the add project dialog
type ShowAddProjectDialog struct{}

// ShowSettingsDialog requests showing the settings dialog
type ShowSettingsDialog struct{}

// ShowCreateWorkspaceDialog requests showing the create workspace dialog
type ShowCreateWorkspaceDialog struct {
	Project *data.Project
}

// ShowDeleteWorkspaceDialog requests showing the delete workspace confirmation
type ShowDeleteWorkspaceDialog struct {
	Project   *data.Project
	Workspace *data.Workspace
}

// ShowRenameWorkspaceDialog requests showing the rename workspace input dialog
type ShowRenameWorkspaceDialog struct {
	Project   *data.Project
	Workspace *data.Workspace
}

// ShowWorkspaceEnvDialog requests showing the workspace environment-variable
// editor for the given workspace.
type ShowWorkspaceEnvDialog struct {
	Workspace *data.Workspace
}

// ShowProjectEnvDialog requests showing the project-level environment editor
// for the workspace's repo — the user-owned layer beneath ws.Env that every
// workspace of the project inherits.
type ShowProjectEnvDialog struct {
	Workspace *data.Workspace
}

// ShowRunScriptOutput requests showing the workspace run script's captured
// output (live pane or post-exit tail) in a read-only viewer.
type ShowRunScriptOutput struct {
	Workspace *data.Workspace
}

// ShowScriptOutput requests showing the workspace's recorded lifecycle-script
// transcripts (setup/archive/on-done — the last run of each, bounded) in a
// read-only viewer. Run output stays on ShowRunScriptOutput's live tail.
type ShowScriptOutput struct {
	Workspace *data.Workspace
}

// ShowWorkspaceStatus requests the workspace's operational snapshot —
// port allocation, run-session state, script config + trust, env key names,
// lifecycle state, open tabs — in a read-only dialog.
type ShowWorkspaceStatus struct {
	Workspace *data.Workspace
}

// ShowWorkspaceScriptsDialog requests showing the workspace scripts editor
// (user-entered setup/run/archive commands + run mode) for the given
// workspace.
type ShowWorkspaceScriptsDialog struct {
	Workspace *data.Workspace
}

// ShowTrustScriptsDialog requests confirmation before trusting repo scripts.
type ShowTrustScriptsDialog struct {
	Workspace  *data.Workspace
	ConfigHash string
}

// ShowCommitWorkspaceDialog requests showing the commit-message input dialog
// for a workspace's changes (git commit-all).
type ShowCommitWorkspaceDialog struct {
	Workspace *data.Workspace
}
