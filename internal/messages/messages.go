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
)

// ProjectsLoaded is sent when projects have been loaded/reloaded
type ProjectsLoaded struct {
	Projects []data.Project
}

// WorktreeActivated is sent when a worktree is selected
type WorktreeActivated struct {
	Project  *data.Project
	Worktree *data.Worktree
}

// WorktreeCreated is sent when a new worktree is created
type WorktreeCreated struct {
	Worktree *data.Worktree
}

// WorktreeDeleted is sent when a worktree is deleted
type WorktreeDeleted struct {
	Root string
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
	Worktree  *data.Worktree
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

// RefreshDashboard requests a dashboard refresh
type RefreshDashboard struct{}

// ShowAddProjectDialog requests showing the add project dialog
type ShowAddProjectDialog struct{}

// ShowCreateWorktreeDialog requests showing the create worktree dialog
type ShowCreateWorktreeDialog struct {
	Project *data.Project
	Base    string // Optional: branch to base new worktree on (for nested worktrees)
}

// ShowDeleteWorktreeDialog requests showing the delete worktree confirmation
type ShowDeleteWorktreeDialog struct {
	Project  *data.Project
	Worktree *data.Worktree
}

// CreateWorktree requests creating a new worktree
type CreateWorktree struct {
	Project *data.Project
	Name    string
	Base    string
}

// DeleteWorktree requests deleting a worktree
type DeleteWorktree struct {
	Project  *data.Project
	Worktree *data.Worktree
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
	Worktree  *data.Worktree
}

// WorktreeCreatedWithWarning indicates worktree was created but setup had issues
type WorktreeCreatedWithWarning struct {
	Worktree *data.Worktree
	Warning  string
}

// RunScript requests running a script for the active worktree
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

// AgentCountUpdated is sent when the number of agents for a worktree changes
type AgentCountUpdated struct {
	WorktreeRoot string
	Count        int
}
