package messages

import (
	"github.com/andyrewlee/amux/internal/data"
)

// ShowAgentProfileDialog opens the agent profile picker for an issue.
type ShowAgentProfileDialog struct {
	IssueID string
}

// ShowAttachDialog opens the attachment picker for an issue.
type ShowAttachDialog struct {
	IssueID string
}

// ApplyLabelFilter sets the board label filter.
type ApplyLabelFilter struct {
	Label string
}

// ApplyAssigneeFilter sets the board assignee filter.
type ApplyAssigneeFilter struct {
	Assignee string
}

// ClearReviewComments clears pending diff review comments.
type ClearReviewComments struct {
	IssueID string
}

// CopyProcessLogs requests copying logs for the selected process.
type CopyProcessLogs struct{}

// StopProcess requests stopping the selected process
type StopProcess struct{}

// OpenDevURL requests opening the dev server URL
type OpenDevURL struct{}

// OpenProjectConfig opens the current project's config file.
type OpenProjectConfig struct{}

// OpenURL requests opening a generic URL.
type OpenURL struct {
	URL string
}

// OpenCommitViewer requests opening the commit viewer
type OpenCommitViewer struct {
	Workspace *data.Workspace
}

// ViewCommitDiff requests viewing a specific commit's diff
type ViewCommitDiff struct {
	Workspace *data.Workspace
	Hash      string
}

// ScriptOutput contains output from a running script
type ScriptOutput struct {
	WorkspaceID string
	ScriptType  string
	Output      string
	Done        bool
	Err         error
}

// GitStatusTick triggers periodic git status refresh
type GitStatusTick struct{}

// FileWatcherEvent is sent when a watched file changes
type FileWatcherEvent struct {
	Root string
}
