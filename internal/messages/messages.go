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
}

// WorkspaceDeleteFailed is sent when a workspace deletion fails
type WorkspaceDeleteFailed struct {
	Project   *data.Project
	Workspace *data.Workspace
	Err       error
}

// ProjectAdded is sent when a new project is registered
type ProjectAdded struct {
	Project *data.Project
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
}

// FocusPane requests focus change to a specific pane
type FocusPane struct {
	Pane PaneType
}

// CreateAgentTab requests creation of a new agent tab
type CreateAgentTab struct {
	Assistant string
	Workspace *data.Workspace
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

// SwitchTab requests switching to a specific tab
type SwitchTab struct {
	Index int
}

// Error represents an application error
type Error struct {
	Err     error
	Context string
	Logged  bool
}

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
//
// Only `run` is user-triggerable: `setup` fires automatically on workspace
// creation and `archive` fires on delete, so neither needs a request message.
type ToggleWorkspaceScript struct {
	Workspace *data.Workspace
}

// WorkspaceScriptStateChanged reports the outcome of a ToggleWorkspaceScript.
// Running is the state the workspace's run script ended up in, so the sidebar
// can show an accurate indicator even when the request failed.
type WorkspaceScriptStateChanged struct {
	Workspace *data.Workspace
	Running   bool
	Err       error
}

// GitStatusTick triggers periodic git status refresh
type GitStatusTick struct{}

// OrphanGCTick triggers periodic tmux orphan session cleanup.
type OrphanGCTick struct{}

// FileWatcherEvent is sent when a watched file changes
type FileWatcherEvent struct {
	Root string
}

// StateWatcherEvent is sent when amux state files change on disk.
type StateWatcherEvent struct {
	Reason string
	Paths  []string
}

// SidebarPTYOutput contains PTY output for sidebar terminal
type SidebarPTYOutput struct {
	WorkspaceID string
	TabID       string
	Data        []byte
}

// SidebarPTYFlush applies buffered PTY output for sidebar terminal
type SidebarPTYFlush struct {
	WorkspaceID string
	TabID       string
}

// SidebarPTYStopped signals that the sidebar PTY read loop has stopped
type SidebarPTYStopped struct {
	WorkspaceID string
	TabID       string
	Err         error
}

// UpdateCheckComplete is sent when the background update check finishes
type UpdateCheckComplete struct {
	CurrentVersion  string
	LatestVersion   string
	UpdateAvailable bool
	ReleaseNotes    string
	Err             error
}

// TriggerUpgrade is sent when the user requests an upgrade
type TriggerUpgrade struct{}

// UpgradeComplete is sent when the upgrade finishes
type UpgradeComplete struct {
	NewVersion string
	Err        error
}

// OpenFileInVim requests opening a file in vim in the center pane
type OpenFileInVim struct {
	Path      string
	Workspace *data.Workspace
}
