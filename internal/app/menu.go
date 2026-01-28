package app

import (
	"fmt"

	tea "charm.land/bubbletea/v2"

	"github.com/andyrewlee/amux/internal/messages"
	"github.com/andyrewlee/amux/internal/ui/common"
)

type issueMenuAction struct {
	ID    string
	Label string
}

func (a *App) showIssueMenu(issueID string) tea.Cmd {
	issue := a.findIssue(issueID)
	if issue == nil {
		return a.toast.ShowError("Issue not found")
	}

	actions := a.buildIssueMenu(issueID)
	if len(actions) == 0 {
		return a.toast.ShowInfo("No actions available")
	}
	options := make([]string, 0, len(actions))
	for _, action := range actions {
		options = append(options, action.Label)
	}
	a.issueMenuIssueID = issueID
	a.issueMenuActions = actions

	a.dialog = common.NewSelectDialog("issue-menu", "Issue Actions", "Select action:", options)

	a.dialog.SetShowKeymapHints(a.config.UI.ShowKeymapHints)
	a.dialog.Show()
	return nil
}

func (a *App) buildIssueMenu(issueID string) []issueMenuAction {
	issue := a.findIssue(issueID)
	if issue == nil {
		return nil
	}
	wt := a.findWorktreeByIssue(issueID)
	hasWorktree := wt != nil
	authRequired := a.issueAuthRequired(issue)

	actions := []issueMenuAction{
		{ID: "open", Label: "Open"},
	}
	if hasWorktree {
		actions = append(actions, issueMenuAction{ID: "resume", Label: "Resume work"})
	} else {
		actions = append(actions, issueMenuAction{ID: "start", Label: "Start work"})
	}
	actions = append(actions, issueMenuAction{ID: "new_attempt", Label: "New attempt"})
	if !authRequired {
		actions = append(actions, issueMenuAction{ID: "edit", Label: "Edit issue"})
		actions = append(actions, issueMenuAction{ID: "move", Label: "Move state"})
		actions = append(actions, issueMenuAction{ID: "duplicate", Label: "Duplicate issue"})
		actions = append(actions, issueMenuAction{ID: "cancel", Label: "Delete (cancel) issue"})
	}
	actions = append(actions, issueMenuAction{ID: "open_issue", Label: "Open in browser"})
	if hasWorktree {
		actions = append(actions,
			issueMenuAction{ID: "run_agent", Label: "Run agent"},
			issueMenuAction{ID: "diff", Label: "Open diff"},
			issueMenuAction{ID: "pr", Label: "Create PR"},
			issueMenuAction{ID: "rebase", Label: "Rebase"},
			issueMenuAction{ID: "resolve", Label: "Resolve conflicts"},
			issueMenuAction{ID: "open_editor", Label: "Open in editor"},
			issueMenuAction{ID: "rename_branch", Label: "Rename branch"},
		)
		if !authRequired {
			actions = append(actions, issueMenuAction{ID: "subtask", Label: "Create subtask"})
		}
	}

	return actions
}

func (a *App) handleIssueMenuSelection(index int) tea.Cmd {
	if a.issueMenuIssueID == "" || index < 0 || index >= len(a.issueMenuActions) {
		return nil
	}
	issueID := a.issueMenuIssueID
	action := a.issueMenuActions[index]
	a.issueMenuIssueID = ""
	a.issueMenuActions = nil

	switch action.ID {
	case "open":
		return func() tea.Msg { return messages.IssueSelected{IssueID: issueID} }
	case "start":
		return func() tea.Msg { return messages.StartIssueWork{IssueID: issueID} }
	case "resume":
		return func() tea.Msg { return messages.ResumeIssueWork{IssueID: issueID} }
	case "new_attempt":
		return func() tea.Msg { return messages.NewAttempt{IssueID: issueID} }
	case "run_agent":
		return func() tea.Msg { return messages.RunAgentForIssue{IssueID: issueID} }
	case "diff":
		return func() tea.Msg { return messages.OpenIssueDiff{IssueID: issueID} }
	case "pr":
		return func() tea.Msg { return messages.CreatePRForIssue{IssueID: issueID} }
	case "move":
		return func() tea.Msg { return messages.MoveIssueState{IssueID: issueID} }
	case "edit":
		return a.editIssue(issueID)
	case "rebase":
		return func() tea.Msg { return messages.RebaseWorkspace{IssueID: issueID} }
	case "resolve":
		return func() tea.Msg { return messages.ResolveConflicts{IssueID: issueID} }
	case "open_editor":
		return func() tea.Msg { return messages.OpenWorkspaceInEditor{IssueID: issueID} }
	case "rename_branch":
		return a.renameBranch(issueID)
	case "open_issue":
		issue := a.findIssue(issueID)
		if issue != nil && issue.URL != "" {
			return func() tea.Msg { return messages.OpenURL{URL: issue.URL} }
		}
		return a.toast.ShowInfo("No issue URL")
	case "duplicate":
		return a.duplicateIssue(issueID)
	case "cancel":
		return a.cancelIssue(issueID)
	case "subtask":
		return a.createSubtask(issueID)
	default:
		return a.toast.ShowInfo(fmt.Sprintf("Action %s not implemented", action.Label))
	}
}
