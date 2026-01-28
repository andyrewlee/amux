package app

import (
	"fmt"
	"sort"
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/andyrewlee/amux/internal/messages"
	"github.com/andyrewlee/amux/internal/ui/center"
	"github.com/andyrewlee/amux/internal/ui/common"
	"github.com/andyrewlee/amux/internal/ui/drawer"
)

func (a *App) handleApprovalRequested(msg messages.ApprovalRequested) tea.Cmd {
	id := msg.ID
	if id == "" {
		id = fmt.Sprintf("approval-%d", time.Now().UnixNano())
	}
	entryID := a.logActivityEntry(common.ActivityEntry{
		Kind:       common.ActivityApproval,
		Summary:    msg.Summary,
		Details:    msg.Details,
		Status:     common.StatusPending,
		ApprovalID: id,
	})
	state := &approvalState{
		ID:          id,
		Summary:     msg.Summary,
		Details:     msg.Details,
		RequestedAt: time.Now(),
		Status:      common.StatusPending,
		EntryID:     entryID,
		WorkspaceID: msg.WorkspaceID,
		TabID:       msg.TabID,
	}
	if msg.Timeout > 0 {
		state.ExpiresAt = time.Now().Add(msg.Timeout)
	}
	a.approvals[id] = state
	a.updateApprovalsView()
	return a.startApprovalTicker()
}

func (a *App) resolveApproval(id string, approved bool, reason string) {
	if id == "" {
		return
	}
	state := a.approvals[id]
	if state == nil {
		return
	}
	status := common.StatusSuccess
	if !approved {
		status = common.StatusError
	}
	state.Status = status
	state.Decision = reason
	a.updateActivityEntry(state.EntryID, func(entry *common.ActivityEntry) {
		entry.Status = status
		if reason != "" {
			entry.Details = append(entry.Details, "Reason: "+reason)
		}
	})
	a.sendApprovalResult(state, approved, reason)
	delete(a.approvals, id)
	a.updateApprovalsView()
}

func (a *App) updateApprovalsView() {
	if a.drawer == nil {
		return
	}
	items := make([]drawer.ApprovalItem, 0, len(a.approvals))
	for _, approval := range a.approvals {
		if approval == nil || approval.Status != common.StatusPending {
			continue
		}
		items = append(items, drawer.ApprovalItem{
			ID:        approval.ID,
			Summary:   approval.Summary,
			Details:   approval.Details,
			Requested: approval.RequestedAt,
			ExpiresAt: approval.ExpiresAt,
		})
	}
	sort.SliceStable(items, func(i, j int) bool {
		return items[i].Requested.Before(items[j].Requested)
	})
	a.drawer.SetApprovals(items)
}

func (a *App) startApprovalTicker() tea.Cmd {
	if a.approvalsTickerActive {
		return nil
	}
	if len(a.approvals) == 0 {
		return nil
	}
	a.approvalsTickerActive = true
	return tea.Tick(time.Second, func(time.Time) tea.Msg { return messages.ApprovalTick{} })
}

func (a *App) handleApprovalTick() tea.Cmd {
	a.approvalsTickerActive = false
	if len(a.approvals) == 0 {
		return nil
	}
	now := time.Now()
	var expired []string
	for id, approval := range a.approvals {
		if approval == nil || approval.ExpiresAt.IsZero() {
			continue
		}
		if now.After(approval.ExpiresAt) {
			expired = append(expired, id)
		}
	}
	for _, id := range expired {
		a.resolveApproval(id, false, "Timed out")
	}
	a.updateApprovalsView()
	return a.startApprovalTicker()
}

func (a *App) sendApprovalResult(state *approvalState, approved bool, reason string) {
	if state == nil || a.center == nil {
		return
	}
	if state.WorkspaceID == "" {
		return
	}
	result := "DENIED"
	if approved {
		result = "APPROVED"
	}
	line := fmt.Sprintf("AMUX_APPROVAL_RESULT: %s | %s", state.ID, result)
	if reason != "" {
		line = line + " | " + reason
	}
	line = line + "\n"
	if state.TabID != "" {
		a.center.SendToTerminalForTab(state.WorkspaceID, center.TabID(state.TabID), line)
		return
	}
	a.center.SendToTerminalForWorktreeID(state.WorkspaceID, line)
}
