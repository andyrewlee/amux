package common

import "time"

// ActivityKind identifies the kind of activity entry.
type ActivityKind string

const (
	ActivityInfo     ActivityKind = "info"
	ActivityCommand  ActivityKind = "command"
	ActivityOutput   ActivityKind = "output"
	ActivityFile     ActivityKind = "file"
	ActivityTool     ActivityKind = "tool"
	ActivityPlan     ActivityKind = "plan"
	ActivitySummary  ActivityKind = "summary"
	ActivityApproval ActivityKind = "approval"
)

// ActivityStatus describes status for activity entries.
type ActivityStatus string

const (
	StatusRunning ActivityStatus = "running"
	StatusSuccess ActivityStatus = "success"
	StatusError   ActivityStatus = "error"
	StatusPending ActivityStatus = "pending"
)

// ActivityEntry represents a log entry for the activity stream.
type ActivityEntry struct {
	ID         string
	Kind       ActivityKind
	Summary    string
	Details    []string
	Status     ActivityStatus
	Timestamp  time.Time
	ProcessID  string
	ApprovalID string
}

// ActivityPrefix returns a short prefix for an activity kind.
func ActivityPrefix(kind ActivityKind) string {
	switch kind {
	case ActivityCommand:
		return "CMD"
	case ActivityOutput:
		return "OUT"
	case ActivityFile:
		return "FILE"
	case ActivityTool:
		return "TOOL"
	case ActivityPlan:
		return "PLAN"
	case ActivitySummary:
		return "SUM"
	case ActivityApproval:
		return "APR"
	default:
		return "INFO"
	}
}

// CopyState tracks copy mode state for terminal scrollback.
type CopyState struct {
	CursorX      int
	CursorY      int
	SelectActive bool
	SelectStartX int
	SelectStartY int
	SelectEndX   int
	SelectEndY   int
	SearchQuery  string
	SearchDir    int // 1 for forward, -1 for backward
}
