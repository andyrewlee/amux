package app

import (
	"context"
	"fmt"
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/andyrewlee/amux/internal/attempt"
	"github.com/andyrewlee/amux/internal/data"
	"github.com/andyrewlee/amux/internal/git"
	"github.com/andyrewlee/amux/internal/logging"
	"github.com/andyrewlee/amux/internal/messages"
	"github.com/andyrewlee/amux/internal/ui/center"
	"github.com/andyrewlee/amux/internal/ui/common"
)

func (a *App) handleAgentStopped(msg center.PTYStopped) tea.Cmd {
	if msg.WorkspaceID == "" {
		return nil
	}
	wt := a.findWorktreeByID(msg.WorkspaceID)
	if wt == nil {
		return nil
	}
	meta, _ := attempt.Load(wt.Root)
	if meta == nil || meta.IssueID == "" {
		return nil
	}
	issue := a.findIssue(meta.IssueID)
	if issue == nil {
		return nil
	}

	return func() tea.Msg {
		if entryID := a.agentActivityIDs[string(wt.ID())]; entryID != "" {
			status := common.StatusSuccess
			if msg.Err != nil || msg.ExitCode != 0 {
				status = common.StatusError
			}
			a.updateActivityEntry(entryID, func(entry *common.ActivityEntry) {
				entry.Status = status
				if msg.ExitCode != 0 {
					entry.Details = append(entry.Details, fmt.Sprintf("Exit code: %d", msg.ExitCode))
				}
			})
			delete(a.agentActivityIDs, string(wt.ID()))
		}

		// If follow-up is queued, relaunch agent instead of moving to review.
		if _, ok := a.pendingAgentMessages[wt.Root]; ok {
			meta.Status = "in_progress"
			meta.LastSyncedAt = time.Now()
			_ = attempt.Save(wt.Root, meta)
			assistant := meta.AgentProfile
			if assistant == "" {
				assistant = "claude"
			}
			entryID := a.logActivityEntry(common.ActivityEntry{
				Kind:      common.ActivityCommand,
				Summary:   fmt.Sprintf("Run agent: %s", wt.Branch),
				Status:    common.StatusRunning,
				ProcessID: string(wt.ID()),
			})
			a.agentActivityIDs[string(wt.ID())] = entryID
			return messages.LaunchAgent{Assistant: assistant, Workspace: wt}
		}

		if msg.Err != nil {
			meta.Status = "failed"
			meta.LastSyncedAt = time.Now()
			_ = attempt.Save(wt.Root, meta)
			a.nextActions[issue.ID] = nextActionSummary{
				Summary: "Agent run failed",
				Status:  "failed",
			}
			if a.inspector != nil && a.selectedIssue != nil && a.selectedIssue.ID == issue.ID {
				a.inspector.SetNextActionSummary("Agent run failed", "failed")
			}
			a.logActivityEntry(common.ActivityEntry{
				Kind:      common.ActivitySummary,
				Summary:   "Agent run failed",
				Status:    common.StatusError,
				ProcessID: string(wt.ID()),
			})
			return messages.RefreshBoard{}
		}

		stats := diffShortStat(wt.Root, wt.Branch, wt.Base)
		comment := "Agent run complete."
		if stats != "" {
			comment = fmt.Sprintf("%s %s", comment, stats)
		}

		// Update attempt metadata
		meta.Status = "in_review"
		meta.LastSyncedAt = time.Now()
		_ = attempt.Save(wt.Root, meta)
		a.nextActions[issue.ID] = nextActionSummary{
			Summary: comment,
			Status:  "complete",
		}
		if a.inspector != nil && a.selectedIssue != nil && a.selectedIssue.ID == issue.ID {
			a.inspector.SetNextActionSummary(comment, "complete")
		}
		a.logActivityEntry(common.ActivityEntry{
			Kind:      common.ActivitySummary,
			Summary:   comment,
			Status:    common.StatusSuccess,
			ProcessID: string(wt.ID()),
			Details:   []string{stats},
		})

		acct := findAccountForIssue(a.linearService, issue)
		if acct.Name != "" {
			ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
			defer cancel()
			if stateID, _ := a.pickStateID(ctx, acct, issue.Team.ID, "review"); stateID != "" {
				if _, err := a.linearService.UpdateIssueState(ctx, acct, issue.ID, stateID); err != nil {
					logging.Warn("Failed to move issue to review: %v", err)
				}
			}
			if err := a.linearService.CreateComment(ctx, acct, issue.ID, comment); err != nil {
				logging.Warn("Failed to comment on issue: %v", err)
			}
		}

		return messages.RefreshBoard{}
	}
}

func (a *App) findWorktreeByID(id string) *data.Workspace {
	for i := range a.projects {
		project := &a.projects[i]
		for j := range project.Workspaces {
			wt := &project.Workspaces[j]
			if string(wt.ID()) == id {
				return wt
			}
		}
	}
	return nil
}

func diffShortStat(repoRoot, branch, base string) string {
	if repoRoot == "" {
		return ""
	}
	if base == "" {
		base = "HEAD"
	}
	mergeBase := base
	if branch != "" && base != "" {
		if mb, err := git.RunGit(repoRoot, "merge-base", branch, base); err == nil && mb != "" {
			mergeBase = mb
		}
	}
	args := []string{"diff", "--shortstat", mergeBase}
	if branch != "" {
		args = append(args, branch)
	}
	out, err := git.RunGit(repoRoot, args...)
	if err != nil {
		return ""
	}
	return out
}
