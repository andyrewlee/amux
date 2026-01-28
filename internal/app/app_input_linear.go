package app

import (
	tea "charm.land/bubbletea/v2"

	"github.com/andyrewlee/amux/internal/messages"
)

// handleLinearMsg dispatches Linear-specific messages to their handlers.
// Returns (cmd, handled). When handled is true, the caller should not
// forward the message elsewhere.
func (a *App) handleLinearMsg(msg tea.Msg) (tea.Cmd, bool) {
	switch msg := msg.(type) {

	// ── Board & Issues ──────────────────────────────────────────────────

	case messages.RefreshBoard:
		return a.refreshBoard(true), true

	case messages.BoardIssuesLoaded:
		return a.handleBoardIssuesLoaded(msg), true

	case messages.BoardFilterChanged:
		a.updateBoard(a.boardIssues)
		return nil, true

	case messages.IssueSelected:
		a.selectIssue(msg.IssueID)
		return nil, true

	case messages.IssueCommentsLoaded:
		if msg.Err == nil && a.inspector != nil {
			a.inspector.SetComments(msg.Comments)
		}
		return nil, true

	case messages.IssueStatesLoaded:
		if msg.Err == nil {
			a.statePickerIssueID = msg.IssueID
			a.statePickerStates = msg.States
		}
		return nil, true

	// ── Issue Actions ───────────────────────────────────────────────────

	case messages.StartIssueWork:
		return a.startIssueWork(msg.IssueID, false), true

	case messages.NewAttempt:
		return a.startIssueWork(msg.IssueID, true), true

	case messages.ResumeIssueWork:
		return a.resumeIssueWork(msg.IssueID), true

	case messages.RunAgentForIssue:
		return a.runAgentForIssue(msg.IssueID), true

	case messages.CreatePRForIssue:
		return a.createPRForIssue(msg.IssueID), true

	case messages.ShowIssueMenu:
		return a.showIssueMenu(msg.IssueID), true

	case messages.MoveIssueState:
		return a.handleMoveIssueState(msg.IssueID), true

	case messages.SetIssueStateType:
		return a.handleSetIssueStateType(msg.IssueID, msg.StateType), true

	case messages.AddIssueComment:
		return a.handleAddIssueComment(msg.IssueID, msg.Body), true

	// ── Approvals ───────────────────────────────────────────────────────

	case messages.ApprovalRequested:
		return a.handleApprovalRequested(msg), true

	case messages.ApproveApproval:
		a.resolveApproval(msg.ID, true, "")
		return nil, true

	case messages.DenyApproval:
		a.resolveApproval(msg.ID, false, msg.Reason)
		return nil, true

	case messages.ApprovalTick:
		return a.handleApprovalTick(), true

	// ── Activity & Webhooks ─────────────────────────────────────────────

	case messages.LogActivity:
		a.logActivityFromMessage(msg)
		return nil, true

	case messages.WebhookEvent:
		cmds := a.applyWebhookEvent(msg)
		// Continue listening for more webhook events.
		cmds = append(cmds, a.listenWebhook())
		return tea.Batch(cmds...), true

	// ── Git Ops ─────────────────────────────────────────────────────────

	case messages.PushBranch:
		return a.pushBranch(msg.IssueID), true

	case messages.MergePullRequest:
		return a.mergePullRequest(msg.IssueID), true

	case messages.ChangeBaseBranch:
		return a.showChangeBaseDialog(msg.IssueID), true

	case messages.RebaseWorkspace:
		return a.handleRebaseWorkspace(msg.IssueID), true

	case messages.AbortRebase:
		return a.handleAbortRebase(msg.IssueID), true

	case messages.ResolveConflicts:
		return a.toast.ShowInfo("Resolve conflicts in your editor, then commit"), true

	// ── Diff ────────────────────────────────────────────────────────────

	case messages.OpenIssueDiff:
		return a.handleOpenIssueDiff(msg.IssueID), true

	case messages.ReloadDiff:
		return a.refreshDiff(msg.IgnoreWhitespace), true

	case messages.DiffLoaded:
		return a.handleDiffLoaded(msg), true

	// ── PR ───────────────────────────────────────────────────────────────

	case messages.PRStatusLoaded:
		if msg.Err == nil {
			a.setPRStatus(msg.IssueID, prInfo{URL: msg.URL, State: msg.State, Number: msg.Number})
		}
		return nil, true

	case messages.PRCommentsLoaded:
		return a.handlePRCommentsLoaded(msg), true

	// ── Review & Follow-up ──────────────────────────────────────────────

	case messages.SendReviewFeedback:
		return a.sendReviewFeedback(msg.IssueID), true

	case messages.SendFollowUp:
		return a.handleSendFollowUp(msg.IssueID, msg.Body), true

	case messages.CancelQueuedMessage:
		return a.cancelQueuedMessage(msg.IssueID), true

	// ── Workspace Ops ───────────────────────────────────────────────────

	case messages.RehydrateIssueWorktree:
		return a.rehydrateIssueWorktree(msg.IssueID), true

	case messages.CreateSubtask:
		return a.createSubtask(msg.IssueID), true

	case messages.RunScript:
		return a.runIssueScript(msg.IssueID, msg.ScriptType), true

	case messages.ScriptOutput:
		a.handleScriptOutput(msg)
		cmds := []tea.Cmd{a.listenScriptOutput()}
		return tea.Batch(cmds...), true

	// ── OAuth ───────────────────────────────────────────────────────────

	case messages.ShowOAuthDialog:
		return a.showOAuthDialog(), true

	case messages.StartOAuth:
		return a.startOAuth(msg.Account), true

	case messages.OAuthCompleted:
		return a.handleOAuthCompleted(msg), true

	// ── Filter Dialogs ──────────────────────────────────────────────────

	case messages.ShowBoardSearchDialog:
		return a.handleShowBoardSearchDialog(), true

	case messages.ShowAccountFilterDialog:
		return a.handleShowAccountFilterDialog(), true

	case messages.ShowProjectFilterDialog:
		return a.handleShowProjectFilterDialog(), true

	case messages.ShowLabelFilterDialog:
		return a.showLabelFilterDialog(), true

	case messages.ShowRecentFilterDialog:
		return a.showRecentFilterDialog(), true

	// ── Issue Dialogs ───────────────────────────────────────────────────

	case messages.ShowCreateIssueDialog:
		return a.handleShowCreateIssueDialog(), true

	case messages.ShowCommentDialog:
		return a.handleShowCommentDialog(), true

	case messages.ShowDiffCommentDialog:
		return a.handleShowDiffCommentDialog(msg), true

	case messages.ShowPRCommentsDialog:
		return a.fetchPRComments(msg.IssueID), true

	case messages.ShowAttemptsDialog:
		return a.handleShowAttemptsDialog(msg.IssueID), true

	case messages.ShowAttemptPicker:
		return a.handleShowAttemptPicker(), true

	// ── Drawer ──────────────────────────────────────────────────────────

	case messages.ShowDrawerPane:
		a.handleShowDrawerPane(msg.Pane)
		return nil, true

	// ── Editor ──────────────────────────────────────────────────────────

	case messages.OpenWorkspaceInEditor:
		return a.handleOpenWorkspaceInEditor(msg.IssueID), true

	case messages.OpenFileInEditor:
		return a.openFileInEditor(msg.File), true

	case messages.OpenURL:
		return a.openURL(msg.URL), true

	// ── Pane / Tab Navigation ───────────────────────────────────────────

	case messages.FocusPane:
		return a.focusPane(msg.Pane), true

	case messages.SwitchTab:
		a.center.SelectTab(msg.Index)
		return nil, true

	case messages.CreateAgentTab:
		newCenter, cmd := a.center.Update(msg)
		a.center = newCenter
		return cmd, true

	case messages.CycleAuxView:
		a.cycleAux(msg.Direction)
		return nil, true

	case messages.CloseAuxView:
		a.auxMode = AuxNone
		return nil, true

	case messages.ShowPreview:
		a.auxMode = AuxPreview
		return nil, true

	// ── Preview ─────────────────────────────────────────────────────────

	case messages.RefreshPreview:
		newCenter, cmd := a.center.Update(msg)
		a.center = newCenter
		return cmd, true

	case messages.CopyPreviewURL:
		return a.handleCopyPreviewURL(), true

	case messages.EditPreviewURL:
		return a.handleEditPreviewURL(), true

	case messages.TogglePreviewLogs:
		a.drawerOpen = !a.drawerOpen
		return nil, true

	case messages.StopPreviewServer:
		return a.handleStopPreviewServer(), true

	default:
		return nil, false
	}
}
