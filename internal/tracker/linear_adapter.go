package tracker

import (
	"context"
	"fmt"
	"strings"

	"github.com/andyrewlee/amux/internal/linear"
)

// LinearAdapter implements Adapter for Linear.
type LinearAdapter struct {
	service *linear.Service
}

// NewLinearAdapter creates a Linear adapter.
func NewLinearAdapter(service *linear.Service) *LinearAdapter {
	return &LinearAdapter{service: service}
}

// ListTasks returns tasks assigned to the current user across active accounts.
func (a *LinearAdapter) ListTasks(ctx context.Context, filter TaskFilter) ([]Task, error) {
	issues, err := a.service.RefreshMyIssues(ctx)
	if err != nil {
		return nil, err
	}
	issues = linear.ApplyScopeFilters(issues, a.service.Config().Scope)
	linear.SortIssues(issues)
	return mapIssuesToTasks(issues), nil
}

// GetTask fetches a single task by ID from cache only (Linear API lacks direct filter here).
func (a *LinearAdapter) GetTask(ctx context.Context, id string) (Task, error) {
	issues, err := a.service.RefreshMyIssues(ctx)
	if err != nil {
		return Task{}, err
	}
	for _, issue := range issues {
		if issue.ID == id {
			return mapIssuesToTasks([]linear.Issue{issue})[0], nil
		}
	}
	return Task{}, fmt.Errorf("task not found")
}

// UpdateStatus updates the issue state.
func (a *LinearAdapter) UpdateStatus(ctx context.Context, id string, stateID string) (Task, error) {
	issue, acct := a.findIssueAccount(id)
	if acct.Name == "" {
		return Task{}, fmt.Errorf("task account not found")
	}
	newState, err := a.service.UpdateIssueState(ctx, acct, id, stateID)
	if err != nil {
		return Task{}, err
	}
	issue.State = newState
	return mapIssuesToTasks([]linear.Issue{issue})[0], nil
}

// AddComment adds a comment to an issue.
func (a *LinearAdapter) AddComment(ctx context.Context, id string, body string) error {
	_, acct := a.findIssueAccount(id)
	if acct.Name == "" {
		return fmt.Errorf("task account not found")
	}
	return a.service.CreateComment(ctx, acct, id, body)
}

// CreateTask creates a new issue in Linear.
func (a *LinearAdapter) CreateTask(ctx context.Context, teamID string, title string, description string) (Task, error) {
	acct := a.service.ActiveAccounts()
	if len(acct) == 0 {
		return Task{}, fmt.Errorf("no active Linear accounts")
	}
	issue, err := a.service.CreateIssue(ctx, acct[0], teamID, title, description)
	if err != nil {
		return Task{}, err
	}
	return mapIssuesToTasks([]linear.Issue{issue})[0], nil
}

// LinkPR posts a PR URL to the issue comments.
func (a *LinearAdapter) LinkPR(ctx context.Context, id string, url string) error {
	return a.AddComment(ctx, id, "PR created: "+url)
}

// Search currently proxies to ListTasks and filters by query.
func (a *LinearAdapter) Search(ctx context.Context, query string) ([]Task, error) {
	tasks, err := a.ListTasks(ctx, TaskFilter{})
	if err != nil {
		return nil, err
	}
	if query == "" {
		return tasks, nil
	}
	filtered := make([]Task, 0, len(tasks))
	for _, task := range tasks {
		if containsInsensitive(task.Identifier, query) || containsInsensitive(task.Title, query) {
			filtered = append(filtered, task)
		}
	}
	return filtered, nil
}

func (a *LinearAdapter) findIssueAccount(id string) (linear.Issue, linear.AccountConfig) {
	issues, _ := a.service.LoadCachedIssues()
	for _, issue := range issues {
		if issue.ID == id {
			acct := findAccount(issue.Account, a.service.ActiveAccounts())
			return issue, acct
		}
	}
	return linear.Issue{}, linear.AccountConfig{}
}

func findAccount(name string, accounts []linear.AccountConfig) linear.AccountConfig {
	for _, acct := range accounts {
		if acct.Name == name {
			return acct
		}
	}
	return linear.AccountConfig{}
}

func mapIssuesToTasks(issues []linear.Issue) []Task {
	out := make([]Task, 0, len(issues))
	for _, issue := range issues {
		labels := make([]string, 0, len(issue.Labels))
		for _, label := range issue.Labels {
			labels = append(labels, label.Name)
		}
		task := Task{
			ID:          issue.ID,
			Identifier:  issue.Identifier,
			Title:       issue.Title,
			Description: issue.Description,
			URL:         issue.URL,
			StateID:     issue.State.ID,
			StateName:   issue.State.Name,
			StateType:   issue.State.Type,
			TeamID:      issue.Team.ID,
			TeamKey:     issue.Team.Key,
			TeamName:    issue.Team.Name,
			ProjectName: "",
			AssigneeID:  "",
			Assignee:    "",
			Labels:      labels,
			UpdatedAt:   issue.UpdatedAt,
			CreatedAt:   issue.CreatedAt,
			Account:     issue.Account,
			Raw:         issue,
		}
		if issue.Project != nil {
			task.ProjectID = issue.Project.ID
			task.ProjectName = issue.Project.Name
		}
		if issue.Assignee != nil {
			task.AssigneeID = issue.Assignee.ID
			task.Assignee = issue.Assignee.Name
		}
		out = append(out, task)
	}
	return out
}

func containsInsensitive(haystack, needle string) bool {
	if needle == "" {
		return true
	}
	return strings.Contains(strings.ToLower(haystack), strings.ToLower(needle))
}
