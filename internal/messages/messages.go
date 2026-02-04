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
	PaneTerminal // Terminal pane (below center pane)
	PaneMonitor
)

// ProjectsLoaded is sent when projects have been loaded/reloaded
type ProjectsLoaded struct {
	Projects []data.Project
}

// WorkspaceActivated is sent when a workspace is selected
type WorkspaceActivated struct {
	Project   *data.Project
	Workspace *data.Workspace
}

// WorkspacePreviewed is sent when a workspace is previewed (cursor movement)
type WorkspacePreviewed struct {
	Project   *data.Project
	Workspace *data.Workspace
}

// MarkWorkspaceReadTick is sent after a delay to mark a workspace as read.
// The WorkspaceID is checked against the pending state to avoid stale marks.
type MarkWorkspaceReadTick struct {
	WorkspaceID string
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
	Index int
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
}

func (e Error) Error() string {
	if e.Context != "" {
		return e.Context + ": " + e.Err.Error()
	}
	return e.Err.Error()
}

// ShowWelcome requests showing the welcome screen
type ShowWelcome struct{}

// ToggleMonitor requests toggling monitor mode
type ToggleMonitor struct{}

// ToggleHelp requests toggling the help overlay
type ToggleHelp struct{}

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

// ToggleTerminalCollapse toggles the terminal pane collapsed state
type ToggleTerminalCollapse struct{}

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

// ShowRenameWorkspaceDialog requests showing the rename workspace dialog
type ShowRenameWorkspaceDialog struct {
	Project   *data.Project
	Workspace *data.Workspace
}

// RenameWorkspace requests renaming a workspace
type RenameWorkspace struct {
	Project   *data.Project
	Workspace *data.Workspace
	NewName   string
}

// ShowRemoveProjectDialog requests showing the remove project confirmation
type ShowRemoveProjectDialog struct {
	Project *data.Project
}

// CreateWorkspace requests creating a new workspace
type CreateWorkspace struct {
	Project    *data.Project
	Name       string
	Base       string
	AllowEdits bool // Pre-grant Edit permission when true
}

// DeleteWorkspace requests deleting a workspace
type DeleteWorkspace struct {
	Project   *data.Project
	Workspace *data.Workspace
}

// RemoveProject requests removing a project from the registry
type RemoveProject struct {
	Project *data.Project
}

// AddProject requests adding a new project
type AddProject struct {
	Path string
}

// ShowSetProfileDialog requests showing the profile input dialog
type ShowSetProfileDialog struct {
	Project *data.Project
}

// SetProfile requests setting a profile on a project
type SetProfile struct {
	Project *data.Project
	Profile string
}

// ShowSelectAssistantDialog requests showing the assistant selection dialog.
// When ForceDialog is true, the picker is always shown regardless of any
// saved default agent preference.
type ShowSelectAssistantDialog struct {
	ForceDialog bool
}

// LaunchAgent requests launching an agent in a new tab
type LaunchAgent struct {
	Assistant string
	Workspace *data.Workspace
}

// OpenDiff requests opening a diff viewer for a file
type OpenDiff struct {
	// Legacy fields (for backwards compatibility with sidebar)
	File       string
	StatusCode string // Git status code (e.g., "M ", "??", "A ")

	// New fields
	Change    *git.Change  // Change object with full info
	Mode      git.DiffMode // Which diff mode to use
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

// RunScript requests running a script for the active workspace
type RunScript struct {
	ScriptType string // "setup", "run", or "archive"
}

// ScriptOutput contains output from a running script
type ScriptOutput struct {
	Output string
	Done   bool
	Err    error
}

// GitStatusTick triggers periodic git status refresh
type GitStatusTick struct{}

// FileWatcherEvent is sent when a watched file changes
type FileWatcherEvent struct {
	Root string
}

// SidebarPTYOutput contains PTY output for sidebar terminal
type SidebarPTYOutput struct {
	WorkspaceID string
	TabID       string
	Data        []byte
}

// SidebarPTYTick triggers a sidebar PTY read
type SidebarPTYTick struct {
	WorkspaceID string
	TabID       string
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

// SidebarTerminalCreated signals that the sidebar terminal was created
type SidebarTerminalCreated struct {
	WorkspaceID string
}

// SidebarTerminalTabCreated signals that a sidebar terminal tab was created
type SidebarTerminalTabCreated struct {
	WorkspaceID string
	TabID       string
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

// PermissionWatcherEvent is sent when a watched settings.local.json changes
type PermissionWatcherEvent struct {
	Root     string
	NewAllow []string // New permissions detected since we started watching
}

// PermissionDetected is sent when new permissions are found in a workspace
type PermissionDetected struct {
	WorkspaceRoot string
	WorkspaceName string
	NewAllow      []string
}

// ShowPermissionsDialog requests showing the pending permissions dialog
type ShowPermissionsDialog struct{}

// PermissionsDialogResult contains the user's actions on pending permissions
type PermissionsDialogResult struct {
	Actions []PermissionAction
}

// PermissionAction represents the user's choice for a single pending permission
type PermissionAction struct {
	Permission string
	Action     PermissionActionType
}

// PermissionActionType identifies how to handle a detected permission
type PermissionActionType int

const (
	PermissionAllow PermissionActionType = iota
	PermissionDeny
	PermissionSkip
)

// PermissionsEditorResult contains the updated allow/deny lists from the editor
type PermissionsEditorResult struct {
	Confirmed bool
	Allow     []string
	Deny      []string
}

// ActionBarCopyDir requests copying the workspace directory to clipboard
type ActionBarCopyDir struct {
	WorkspaceRoot string
}

// ActionBarOpenIDE requests opening the workspace folder in the user's IDE
type ActionBarOpenIDE struct {
	WorkspaceRoot string
}

// ActionBarMergeToMain requests merging the worktree branch into main
type ActionBarMergeToMain struct {
	RepoPath   string // Main repo path where main/master branch lives
	BranchName string // Branch to merge into main
}

// ActionBarCommit requests staging all changes and creating a commit
type ActionBarCommit struct {
	WorkspaceRoot string
	Message       string
}

// ActionBarCommitResult contains the result of a commit operation
type ActionBarCommitResult struct {
	Success    bool
	CommitHash string
	Err        error
}

// ActionBarMergeResult contains the result of a merge operation
type ActionBarMergeResult struct {
	Success bool
	Err     error
}

// ActionBarOpenMR requests opening a merge/pull request in browser
type ActionBarOpenMR struct {
	WorkspaceRoot string
	BranchName    string
}

// ShowCommitDialog requests showing the commit message dialog
type ShowCommitDialog struct {
	WorkspaceRoot string
}

// WorkspaceMarkUnread is sent when a workspace receives output while not active
type WorkspaceMarkUnread struct {
	WorkspaceID string
}
