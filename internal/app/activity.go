package app

import (
	"strconv"
	"strings"
	"time"

	"github.com/andyrewlee/amux/internal/messages"
	"github.com/andyrewlee/amux/internal/ui/common"
)

type approvalState struct {
	ID          string
	Summary     string
	Details     []string
	RequestedAt time.Time
	ExpiresAt   time.Time
	Status      common.ActivityStatus
	EntryID     string
	Decision    string
	WorkspaceID string
	TabID       string
}

type processRecord struct {
	ID            string
	Name          string
	Kind          string
	WorkspaceRoot string
	WorkspaceID   string
	ScriptType    string
	StartedAt     time.Time
	CompletedAt   time.Time
	ExitCode      *int
	Status        string
}

func (a *App) logActivityEntry(entry common.ActivityEntry) string {
	if entry.Timestamp.IsZero() {
		entry.Timestamp = time.Now()
	}
	if entry.ID == "" {
		a.activitySeq++
		entry.ID = "act-" + strconv.Itoa(a.activitySeq)
	}
	a.activityLog = append(a.activityLog, entry)
	if len(a.activityLog) > 500 {
		a.activityLog = a.activityLog[len(a.activityLog)-500:]
	}
	a.refreshLogViews()
	return entry.ID
}

func (a *App) updateActivityEntry(id string, update func(entry *common.ActivityEntry)) {
	if id == "" {
		return
	}
	for i := range a.activityLog {
		if a.activityLog[i].ID == id {
			update(&a.activityLog[i])
			break
		}
	}
	a.refreshLogViews()
}

func (a *App) refreshLogViews() {
	if a.drawer != nil {
		a.drawer.SetLogs(a.activityLog)
	}
	if a.inspector != nil {
		a.inspector.SetLogs(a.activityLog)
	}
}

func (a *App) logActivityFromMessage(msg messages.LogActivity) {
	kind := parseActivityKind(msg.Kind)
	status := parseActivityStatus(msg.Status)
	entry := common.ActivityEntry{
		Kind:       kind,
		Summary:    msg.Line,
		Details:    msg.Details,
		Status:     status,
		ProcessID:  msg.ProcessID,
		ApprovalID: msg.ApprovalID,
	}
	a.logActivityEntry(entry)
}

func parseActivityKind(kind string) common.ActivityKind {
	switch strings.ToLower(strings.TrimSpace(kind)) {
	case "command":
		return common.ActivityCommand
	case "output":
		return common.ActivityOutput
	case "file":
		return common.ActivityFile
	case "tool":
		return common.ActivityTool
	case "plan":
		return common.ActivityPlan
	case "summary":
		return common.ActivitySummary
	case "approval":
		return common.ActivityApproval
	default:
		return common.ActivityInfo
	}
}

func parseActivityStatus(status string) common.ActivityStatus {
	switch strings.ToLower(strings.TrimSpace(status)) {
	case "running":
		return common.StatusRunning
	case "error":
		return common.StatusError
	case "pending":
		return common.StatusPending
	default:
		return common.StatusSuccess
	}
}
