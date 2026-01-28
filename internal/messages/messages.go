package messages

import (
	"time"

	"github.com/andyrewlee/amux/internal/data"
	"github.com/andyrewlee/amux/internal/diff"
	"github.com/andyrewlee/amux/internal/git"
	"github.com/andyrewlee/amux/internal/linear"
)

// PaneType identifies the focused pane
type PaneType int

const (
	PaneDashboard PaneType = iota
	PaneCenter
	PaneSidebar
	PaneSidebarTerminal
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
	Index        int
	Name         string
	WorktreeID   string
	WorktreeRoot string
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

// RefreshDashboard requests a dashboard refresh
type RefreshDashboard struct{}

// RescanWorkspaces requests a git worktree rescan/import.
type RescanWorkspaces struct{}

// RefreshBoard requests a Linear board refresh
type RefreshBoard struct{}

// BoardIssuesLoaded delivers issues for the board.
type BoardIssuesLoaded struct {
	Issues []linear.Issue
	Cached bool
	Err    error
}

// IssueCommentsLoaded delivers comments for an issue.
type IssueCommentsLoaded struct {
	IssueID  string
	Comments []linear.Comment
	Err      error
}

// IssueStatesLoaded delivers states for a team.
type IssueStatesLoaded struct {
	IssueID string
	States  []linear.State
	Err     error
}

// IssueSelected indicates a Linear issue selection
type IssueSelected struct {
	IssueID string
}

// ShowBoardSearchDialog requests opening the board search dialog
type ShowBoardSearchDialog struct{}

// BoardFilterChanged indicates board filters toggled
type BoardFilterChanged struct{}

// ShowCreateIssueDialog requests creating a new issue.
type ShowCreateIssueDialog struct{}

// ShowAccountFilterDialog requests selecting an account filter.
type ShowAccountFilterDialog struct{}

// ShowProjectFilterDialog requests selecting a project filter.
type ShowProjectFilterDialog struct{}

// ShowLabelFilterDialog opens the label filter selector.
type ShowLabelFilterDialog struct{}

// ShowRecentFilterDialog opens the updated-recently filter selector.
type ShowRecentFilterDialog struct{}

// ShowIssueMenu requests opening the issue actions menu.
type ShowIssueMenu struct {
	IssueID string
}

// ShowOAuthDialog requests starting Linear OAuth.
type ShowOAuthDialog struct{}

// ShowPRCommentsDialog requests importing PR comments.
type ShowPRCommentsDialog struct {
	IssueID string
}

// ShowDrawerPane requests opening the drawer to a specific pane.
type ShowDrawerPane struct {
	Pane string // "logs", "approvals", "processes"
}

// CreateSubtask requests creating a subtask from an issue.
type CreateSubtask struct {
	IssueID string
}

// StartOAuth begins OAuth for a specific account.
type StartOAuth struct {
	Account string
}

// OAuthCompleted delivers OAuth result for an account.
type OAuthCompleted struct {
	Account string
	Token   string
	Err     error
}

// CycleAuxView cycles preview/diff/attempt view
type CycleAuxView struct {
	Direction int
}

// ShowPreview requests opening preview pane.
type ShowPreview struct{}

// CloseAuxView requests closing aux pane.
type CloseAuxView struct{}

// WebhookEvent delivers a Linear webhook event.
type WebhookEvent struct {
	Account string
	Type    string
	Action  string
	Data    []byte
}

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

// ShowRemoveProjectDialog requests showing the remove project confirmation
type ShowRemoveProjectDialog struct {
	Project *data.Project
}

// CreateWorkspace requests creating a new workspace
type CreateWorkspace struct {
	Project *data.Project
	Name    string
	Branch  string
	Base    string
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

// ShowSelectAssistantDialog requests showing the assistant selection dialog
type ShowSelectAssistantDialog struct{}

// LaunchAgent requests launching an agent in a new tab
type LaunchAgent struct {
	Assistant string
	Workspace *data.Workspace
}

// StartIssueWork requests creating a workspace and starting work for an issue
type StartIssueWork struct {
	IssueID string
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
	IssueID    string // Optional: issue context for linear integration
}

// ResumeIssueWork requests opening an existing workspace for an issue
type ResumeIssueWork struct {
	IssueID string
}

// RehydrateIssueWorktree requests rehydrating a missing worktree.
type RehydrateIssueWorktree struct {
	IssueID string
}

// NewAttempt requests creating a new attempt for an issue
type NewAttempt struct {
	IssueID string
}

// SidebarPTYOutput contains PTY output for sidebar terminal
type SidebarPTYOutput struct {
	WorkspaceID string
	TabID       string
	Data        []byte
}

// RunAgentForIssue requests running an agent for an issue
type RunAgentForIssue struct {
	IssueID string
}

// CreatePRForIssue requests creating a PR for an issue
type CreatePRForIssue struct {
	IssueID string
}

// PushBranch requests pushing the current branch.
type PushBranch struct {
	IssueID string
}

// MergePullRequest requests merging the PR for an issue.
type MergePullRequest struct {
	IssueID string
}

// ChangeBaseBranch requests changing the base branch for an issue.
type ChangeBaseBranch struct {
	IssueID string
}

// OpenIssueDiff requests opening a diff for an issue
type OpenIssueDiff struct {
	IssueID string
}

// MoveIssueState requests moving an issue state
type MoveIssueState struct {
	IssueID string
}

// AddIssueComment requests adding a comment to an issue
type AddIssueComment struct {
	IssueID string
	Body    string
}

// SetIssueStateType requests moving issue to a state type (started/review/completed).
type SetIssueStateType struct {
	IssueID   string
	StateType string
}

// ShowAttemptsDialog requests opening the attempts selector
type ShowAttemptsDialog struct {
	IssueID string
}

// RebaseWorkspace requests rebase for an issue's workspace
type RebaseWorkspace struct {
	IssueID string
}

// ResolveConflicts requests conflict resolution UI for an issue
type ResolveConflicts struct {
	IssueID string
}

// OpenWorkspaceInEditor requests opening the workspace root in editor.
type OpenWorkspaceInEditor struct {
	IssueID string
}

// AbortRebase requests aborting a rebase for an issue's workspace.
type AbortRebase struct {
	IssueID string
}

// ShowCommentDialog requests opening a comment input dialog.
type ShowCommentDialog struct{}

// ShowAttemptPicker requests opening the attempts picker dialog.
type ShowAttemptPicker struct{}

// ReloadDiff requests reloading diff data with current options.
type ReloadDiff struct {
	IgnoreWhitespace bool
}

// DiffLoaded delivers diff data for rendering.
type DiffLoaded struct {
	Files []diff.File
	Err   error
}

// ShowDiffCommentDialog requests opening a diff comment dialog.
type ShowDiffCommentDialog struct {
	File string
	Line int
	Side string
}

// PRStatusLoaded delivers PR status for an issue.
type PRStatusLoaded struct {
	IssueID string
	URL     string
	State   string
	Number  int
	Err     error
}

// PRCommentsLoaded delivers PR comment options.
type PRCommentsLoaded struct {
	IssueID string
	Options []string
	Err     error
}

// OpenFileInEditor requests opening a file in the configured editor.
type OpenFileInEditor struct {
	File string
}

// SendReviewFeedback requests sending aggregated diff comments to the agent.
type SendReviewFeedback struct {
	IssueID string
}

// SendFollowUp sends a freeform follow-up message to the agent.
type SendFollowUp struct {
	IssueID string
	Body    string
}

// CancelQueuedMessage clears any queued follow-up for an issue.
type CancelQueuedMessage struct {
	IssueID string
}

// RefreshPreview requests preview refresh.
type RefreshPreview struct{}

// CopyPreviewURL requests copying preview URL.
type CopyPreviewURL struct{}

// EditPreviewURL requests editing preview URL.
type EditPreviewURL struct{}

// TogglePreviewLogs toggles preview logs drawer.
type TogglePreviewLogs struct{}

// StopPreviewServer requests stopping the dev server.
type StopPreviewServer struct{}

// LogActivity appends an activity log line.
type LogActivity struct {
	Line       string
	Kind       string
	Status     string
	Details    []string
	ProcessID  string
	ApprovalID string
}

// ApprovalRequested indicates an approval is pending.
type ApprovalRequested struct {
	ID          string
	Summary     string
	Details     []string
	Timeout     time.Duration
	WorkspaceID string
	TabID       string
	Data        []byte
}

// ApproveApproval marks an approval as approved.
type ApproveApproval struct {
	ID string
}

// DenyApproval marks an approval as denied.
type DenyApproval struct {
	ID     string
	Reason string
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

// ApprovalTick triggers approval countdown updates.
type ApprovalTick struct{}
