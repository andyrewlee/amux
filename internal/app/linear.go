package app

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/andyrewlee/amux/internal/attempt"
	"github.com/andyrewlee/amux/internal/data"
	"github.com/andyrewlee/amux/internal/linear"
	"github.com/andyrewlee/amux/internal/messages"
	"github.com/andyrewlee/amux/internal/ui/board"
	"github.com/andyrewlee/amux/internal/ui/common"
)

type issueTeamOption struct {
	Account  string
	TeamID   string
	TeamKey  string
	TeamName string
}

type projectFilterOption struct {
	ID   string
	Name string
}

// refreshBoard loads cached issues and refreshes from Linear.
func (a *App) refreshBoard(force bool) tea.Cmd {
	var cmds []tea.Cmd
	if !force {
		cmds = append(cmds, func() tea.Msg {
			issues, err := a.linearService.LoadCachedIssues()
			return messages.BoardIssuesLoaded{Issues: issues, Cached: true, Err: err}
		})
	}
	cmds = append(cmds, func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
		defer cancel()
		issues, err := a.linearService.RefreshMyIssues(ctx)
		return messages.BoardIssuesLoaded{Issues: issues, Cached: false, Err: err}
	})
	return tea.Batch(cmds...)
}

func (a *App) updateAuthStatus() {
	if a.linearService == nil || a.board == nil {
		return
	}
	a.authMissingAccounts = a.linearService.MissingAuthAccounts()
	a.board.SetAuthMissing(a.authMissingAccounts)
	if a.selectedIssue != nil {
		a.inspector.SetAuthRequired(a.issueAuthRequired(a.selectedIssue))
	}
}

func (a *App) applyBoardFilters(issues []linear.Issue) []linear.Issue {
	filtered := linear.ApplyScopeFilters(issues, a.linearConfig.Scope)
	showCanceled := false
	if a.linearConfig != nil {
		showCanceled = a.linearConfig.Board.ShowCanceled
	}
	if a.board != nil && a.board.Filters.ShowCanceled {
		showCanceled = true
	}
	if !showCanceled {
		out := make([]linear.Issue, 0, len(filtered))
		for _, issue := range filtered {
			if strings.EqualFold(issue.State.Type, "canceled") {
				continue
			}
			out = append(out, issue)
		}
		filtered = out
	}
	if a.board != nil {
		query := strings.TrimSpace(a.board.Filters.Search)
		if query != "" {
			out := make([]linear.Issue, 0, len(filtered))
			for _, issue := range filtered {
				if containsInsensitive(issue.Identifier, query) || containsInsensitive(issue.Title, query) {
					out = append(out, issue)
				}
			}
			filtered = out
		}
		if a.board.Filters.ActiveOnly {
			filtered = a.filterActiveIssues(filtered)
		}
		if a.board.Filters.Account != "" {
			out := make([]linear.Issue, 0, len(filtered))
			for _, issue := range filtered {
				if issue.Account == a.board.Filters.Account {
					out = append(out, issue)
				}
			}
			filtered = out
		}
		if a.board.Filters.Project != "" {
			out := make([]linear.Issue, 0, len(filtered))
			for _, issue := range filtered {
				if issue.Project != nil && issue.Project.ID == a.board.Filters.Project {
					out = append(out, issue)
				}
			}
			filtered = out
		}
		if a.board.Filters.Label != "" {
			label := strings.ToLower(strings.TrimSpace(a.board.Filters.Label))
			out := make([]linear.Issue, 0, len(filtered))
			for _, issue := range filtered {
				for _, lbl := range issue.Labels {
					if strings.ToLower(lbl.Name) == label {
						out = append(out, issue)
						break
					}
				}
			}
			filtered = out
		}
		if a.board.Filters.Assignee != "" {
			assignee := strings.ToLower(strings.TrimSpace(a.board.Filters.Assignee))
			out := make([]linear.Issue, 0, len(filtered))
			for _, issue := range filtered {
				name := ""
				if issue.Assignee != nil {
					name = issue.Assignee.Name
				}
				if strings.ToLower(name) == assignee {
					out = append(out, issue)
				}
			}
			filtered = out
		}
		if a.board.Filters.UpdatedWithinDays > 0 {
			cutoff := time.Now().AddDate(0, 0, -a.board.Filters.UpdatedWithinDays)
			out := make([]linear.Issue, 0, len(filtered))
			for _, issue := range filtered {
				if issue.UpdatedAt.After(cutoff) {
					out = append(out, issue)
				}
			}
			filtered = out
		}
	}
	linear.SortIssues(filtered)
	return filtered
}

func (a *App) filterActiveIssues(issues []linear.Issue) []linear.Issue {
	if len(issues) == 0 {
		return issues
	}
	out := make([]linear.Issue, 0, len(issues))
	for _, issue := range issues {
		if a.issueHasActiveAttempt(issue.ID) {
			out = append(out, issue)
		}
	}
	return out
}

func (a *App) issueHasActiveAttempt(issueID string) bool {
	// Use running agent tabs as a proxy for active attempts.
	if a.center == nil {
		return false
	}
	return a.center.HasRunningIssue(issueID)
}

func (a *App) updateBoard(issues []linear.Issue) {
	if a.board == nil {
		return
	}

	filtered := a.applyBoardFilters(issues)
	columns := make([]board.BoardColumn, len(a.linearConfig.Board.Columns))
	for i, name := range a.linearConfig.Board.Columns {
		columns[i].Name = name
	}

	for _, issue := range filtered {
		colName := linear.MapStateToColumn(issue.State, issue.Team, a.linearConfig.Board)
		idx := linear.ColumnIndex(a.linearConfig.Board.Columns, colName)
		if idx == -1 {
			continue
		}
		card := board.IssueCard{
			IssueID:    issue.ID,
			Identifier: issue.Identifier,
			Title:      issue.Title,
			Labels:     issueLabelNames(issue),
			Assignee:   issueAssignee(issue),
			Badges:     a.issueBadges(issue),
			UpdatedAt:  issue.UpdatedAt,
			Account:    issue.Account,
			StateName:  issue.State.Name,
			PRURL:      a.issuePRURL(issue.ID),
		}
		columns[idx].Cards = append(columns[idx].Cards, card)
	}

	a.board.SetColumns(columns)
	if a.linearConfig != nil {
		a.board.SetWIPLimits(a.linearConfig.Board.WIPLimits)
	}
}

func (a *App) fetchIssueComments(issueID string) tea.Cmd {
	issue := a.findIssue(issueID)
	if issue == nil {
		return nil
	}
	acct := findAccountForIssue(a.linearService, issue)
	if acct.Name == "" {
		return nil
	}
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		comments, err := a.linearService.FetchIssueComments(ctx, acct, issue.ID)
		return messages.IssueCommentsLoaded{IssueID: issue.ID, Comments: comments, Err: err}
	}
}

func (a *App) applyWebhookEvent(msg messages.WebhookEvent) []tea.Cmd {
	cmds := []tea.Cmd{}
	a.logActivityEntry(common.ActivityEntry{
		Kind:    common.ActivityTool,
		Summary: fmt.Sprintf("Webhook: %s %s", msg.Type, msg.Action),
		Status:  common.StatusSuccess,
	})
	switch strings.ToLower(msg.Type) {
	case "issue":
		issue, ok := decodeWebhookIssue(msg.Data)
		if !ok {
			// Fall back to full refresh if payload is unexpected.
			cmds = append(cmds, a.refreshBoard(true))
			return cmds
		}
		issue.Account = msg.Account
		a.applyWebhookIssue(issue, msg.Action)
	case "comment":
		if a.selectedIssue != nil {
			if cmd := a.fetchIssueComments(a.selectedIssue.ID); cmd != nil {
				cmds = append(cmds, cmd)
			}
		}
	}
	return cmds
}

func decodeWebhookIssue(raw []byte) (linear.Issue, bool) {
	if len(raw) == 0 {
		return linear.Issue{}, false
	}
	var issue linear.Issue
	if err := json.Unmarshal(raw, &issue); err != nil {
		return linear.Issue{}, false
	}
	if issue.ID == "" {
		return linear.Issue{}, false
	}
	return issue, true
}

func (a *App) applyWebhookIssue(issue linear.Issue, action string) {
	if issue.ID == "" {
		return
	}
	removed := strings.EqualFold(action, "remove") || strings.EqualFold(action, "delete")
	if removed {
		filtered := a.boardIssues[:0]
		for _, existing := range a.boardIssues {
			if existing.ID != issue.ID {
				filtered = append(filtered, existing)
			}
		}
		a.boardIssues = filtered
	} else {
		updated := false
		for i := range a.boardIssues {
			if a.boardIssues[i].ID == issue.ID {
				a.boardIssues[i] = mergeIssue(a.boardIssues[i], issue)
				updated = true
				break
			}
		}
		if !updated {
			a.boardIssues = append(a.boardIssues, issue)
		}
	}
	a.updateBoard(a.boardIssues)
	a.updateTeamOptions(a.boardIssues)
	a.updateProjectOptions(a.boardIssues)
	if a.selectedIssue != nil && a.selectedIssue.ID == issue.ID {
		a.selectIssue(issue.ID)
	}
}

func mergeIssue(existing, update linear.Issue) linear.Issue {
	if update.Title != "" {
		existing.Title = update.Title
	}
	if update.Identifier != "" {
		existing.Identifier = update.Identifier
	}
	if update.Description != "" {
		existing.Description = update.Description
	}
	if update.URL != "" {
		existing.URL = update.URL
	}
	if update.State.ID != "" {
		existing.State = update.State
	}
	if update.Team.ID != "" {
		existing.Team = update.Team
	}
	if update.Project != nil {
		existing.Project = update.Project
	}
	if update.Assignee != nil {
		existing.Assignee = update.Assignee
	}
	if len(update.Labels) > 0 {
		existing.Labels = update.Labels
	}
	if !update.UpdatedAt.IsZero() {
		existing.UpdatedAt = update.UpdatedAt
	}
	if !update.CreatedAt.IsZero() {
		existing.CreatedAt = update.CreatedAt
	}
	return existing
}

func (a *App) updateTeamOptions(issues []linear.Issue) {
	a.createIssueTeams = collectTeamOptions(issues)
}

func (a *App) updateProjectOptions(issues []linear.Issue) {
	a.projectFilterOptions = collectProjectOptions(issues)
}

func collectTeamOptions(issues []linear.Issue) []issueTeamOption {
	seen := make(map[string]bool)
	options := make([]issueTeamOption, 0, len(issues))
	for _, issue := range issues {
		if issue.Team.ID == "" {
			continue
		}
		key := issue.Account + ":" + issue.Team.ID
		if seen[key] {
			continue
		}
		seen[key] = true
		options = append(options, issueTeamOption{
			Account:  issue.Account,
			TeamID:   issue.Team.ID,
			TeamKey:  issue.Team.Key,
			TeamName: issue.Team.Name,
		})
	}
	sort.Slice(options, func(i, j int) bool {
		if options[i].Account != options[j].Account {
			return options[i].Account < options[j].Account
		}
		if options[i].TeamKey != options[j].TeamKey {
			return options[i].TeamKey < options[j].TeamKey
		}
		return options[i].TeamName < options[j].TeamName
	})
	return options
}

func collectProjectOptions(issues []linear.Issue) []projectFilterOption {
	seen := make(map[string]bool)
	projects := make([]projectFilterOption, 0, len(issues))
	for _, issue := range issues {
		if issue.Project == nil || issue.Project.ID == "" {
			continue
		}
		if seen[issue.Project.ID] {
			continue
		}
		seen[issue.Project.ID] = true
		projects = append(projects, projectFilterOption{ID: issue.Project.ID, Name: issue.Project.Name})
	}
	sort.Slice(projects, func(i, j int) bool {
		return projects[i].Name < projects[j].Name
	})
	options := []projectFilterOption{{ID: "", Name: "All projects"}}
	options = append(options, projects...)
	return options
}

func issueLabelNames(issue linear.Issue) []string {
	labels := make([]string, 0, len(issue.Labels))
	for _, label := range issue.Labels {
		labels = append(labels, label.Name)
	}
	return labels
}

func issueAssignee(issue linear.Issue) string {
	if issue.Assignee == nil {
		return ""
	}
	return issue.Assignee.Name
}

func (a *App) issueBadges(issue linear.Issue) []string {
	var badges []string
	if meta := a.loadAttemptMetadata(issue.ID); meta != nil && strings.EqualFold(meta.Status, "failed") {
		badges = append(badges, "FAILED")
	}
	if a.issueHasActiveAttempt(issue.ID) {
		badges = append(badges, "RUNNING")
	}
	if a.issueHasDirtyWorktree(issue.ID) {
		badges = append(badges, "CHANGES")
	}
	if a.issueHasPR(issue.ID) {
		badges = append(badges, "PR")
	}
	return badges
}

func (a *App) issueHasDirtyWorktree(issueID string) bool {
	wt := a.findWorktreeByIssue(issueID)
	if wt == nil {
		return false
	}
	status := a.statusManager.GetCached(wt.Root)
	if status == nil {
		return false
	}
	return !status.Clean
}

func (a *App) issueHasPR(issueID string) bool {
	return a.issuePRURL(issueID) != ""
}

func (a *App) issuePRURL(issueID string) string {
	if info, ok := a.prStatus(issueID); ok && info.URL != "" {
		return info.URL
	}
	meta := a.loadAttemptMetadata(issueID)
	if meta == nil {
		return ""
	}
	return meta.PRURL
}

func (a *App) loadAttemptMetadata(issueID string) *attempt.Metadata {
	wt := a.findWorktreeByIssue(issueID)
	if wt == nil {
		return nil
	}
	meta, err := attempt.Load(wt.Root)
	if err != nil {
		return nil
	}
	return meta
}

func (a *App) findWorktreeByIssue(issueID string) *data.Workspace {
	for i := range a.projects {
		project := &a.projects[i]
		for j := range project.Workspaces {
			wt := &project.Workspaces[j]
			meta, err := attempt.Load(wt.Root)
			if err != nil || meta == nil {
				continue
			}
			if meta.IssueID == issueID {
				return wt
			}
		}
	}
	return nil
}

func containsInsensitive(haystack, needle string) bool {
	return strings.Contains(strings.ToLower(haystack), strings.ToLower(needle))
}
