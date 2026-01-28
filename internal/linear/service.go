package linear

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"time"
)

// Service orchestrates Linear API usage across accounts.
type Service struct {
	config     *Config
	cache      *Cache
	clients    map[string]*Client
	viewerIDs  map[string]string
	teamStates map[string][]State
	backoff    map[string]time.Time
}

// NewService creates a Linear service.
func NewService(config *Config, cache *Cache) *Service {
	return &Service{
		config:     config,
		cache:      cache,
		clients:    make(map[string]*Client),
		viewerIDs:  make(map[string]string),
		teamStates: make(map[string][]State),
		backoff:    make(map[string]time.Time),
	}
}

// Config returns the current config.
func (s *Service) Config() *Config {
	return s.config
}

// ClientFor returns a client for the given account.
func (s *Service) ClientFor(account AccountConfig) *Client {
	token, tokenType := s.accountToken(account)
	if client, ok := s.clients[account.Name]; ok {
		client.Token = token
		client.TokenType = tokenType
		return client
	}
	client := NewClient(token)
	client.TokenType = tokenType
	s.clients[account.Name] = client
	return client
}

// ActiveAccounts returns the list of active accounts.
func (s *Service) ActiveAccounts() []AccountConfig {
	if s.config == nil {
		return nil
	}
	if len(s.config.ActiveAccounts) == 0 {
		return s.config.Accounts
	}
	active := make(map[string]bool, len(s.config.ActiveAccounts))
	for _, name := range s.config.ActiveAccounts {
		active[name] = true
	}
	var accounts []AccountConfig
	for _, acct := range s.config.Accounts {
		if active[acct.Name] {
			accounts = append(accounts, acct)
		}
	}
	return accounts
}

// RefreshMyIssues fetches issues for active accounts and updates cache.
func (s *Service) RefreshMyIssues(ctx context.Context) ([]Issue, error) {
	var all []Issue
	accounts := s.ActiveAccounts()
	for _, acct := range accounts {
		if token, _ := s.accountToken(acct); token == "" {
			continue
		}
		if until := s.backoff[acct.Name]; !until.IsZero() && time.Now().Before(until) {
			return nil, fmt.Errorf("linear: backoff active for %s until %s", acct.Name, until.Format(time.RFC3339))
		}
		issues, err := s.fetchAccountIssues(ctx, acct)
		if err != nil {
			return nil, err
		}
		all = append(all, issues...)
	}
	return all, nil
}

// LoadCachedIssues returns cached issues for active accounts.
func (s *Service) LoadCachedIssues() ([]Issue, error) {
	if s.cache == nil {
		return nil, nil
	}
	var all []Issue
	for _, acct := range s.ActiveAccounts() {
		viewerID := s.viewerIDs[acct.Name]
		issues, err := s.cache.LoadIssues(acct.Name, viewerID)
		if err != nil {
			return nil, err
		}
		all = append(all, issues...)
	}
	return all, nil
}

func (s *Service) fetchAccountIssues(ctx context.Context, acct AccountConfig) ([]Issue, error) {
	client := s.ClientFor(acct)
	viewerID, err := s.fetchViewer(ctx, acct, client)
	if err != nil {
		return nil, err
	}

	filter := buildIssueFilter(s.config.Scope, viewerID)
	variables := map[string]any{
		"first":  50,
		"after":  nil,
		"filter": filter,
	}

	var issues []Issue
	for {
		var resp struct {
			Issues struct {
				Nodes []struct {
					ID          string   `json:"id"`
					Identifier  string   `json:"identifier"`
					Title       string   `json:"title"`
					Description string   `json:"description"`
					URL         string   `json:"url"`
					Priority    int      `json:"priority"`
					State       State    `json:"state"`
					Team        Team     `json:"team"`
					Project     *Project `json:"project"`
					Assignee    *User    `json:"assignee"`
					Labels      struct {
						Nodes []Label `json:"nodes"`
					} `json:"labels"`
					UpdatedAt time.Time `json:"updatedAt"`
					CreatedAt time.Time `json:"createdAt"`
				} `json:"nodes"`
				PageInfo struct {
					HasNextPage bool   `json:"hasNextPage"`
					EndCursor   string `json:"endCursor"`
				} `json:"pageInfo"`
			} `json:"issues"`
		}

		if err := client.Do(ctx, queryMyIssues, variables, &resp); err != nil {
			if rl, ok := err.(*RateLimitError); ok {
				if !rl.Reset.IsZero() {
					s.backoff[acct.Name] = rl.Reset
				}
			}
			return nil, err
		}
		for _, node := range resp.Issues.Nodes {
			issues = append(issues, Issue{
				ID:          node.ID,
				Identifier:  node.Identifier,
				Title:       node.Title,
				Description: node.Description,
				URL:         node.URL,
				Priority:    node.Priority,
				State:       node.State,
				Team:        node.Team,
				Project:     node.Project,
				Assignee:    node.Assignee,
				Labels:      node.Labels.Nodes,
				UpdatedAt:   node.UpdatedAt,
				CreatedAt:   node.CreatedAt,
				Account:     acct.Name,
			})
		}
		if !resp.Issues.PageInfo.HasNextPage {
			break
		}
		variables["after"] = resp.Issues.PageInfo.EndCursor
	}

	if s.cache != nil {
		_ = s.cache.SaveIssues(acct.Name, viewerID, issues)
	}
	s.backoff[acct.Name] = time.Time{}
	return issues, nil
}

func (s *Service) fetchViewer(ctx context.Context, acct AccountConfig, client *Client) (string, error) {
	if id, ok := s.viewerIDs[acct.Name]; ok && id != "" {
		return id, nil
	}
	var resp struct {
		Viewer struct {
			ID    string `json:"id"`
			Name  string `json:"name"`
			Email string `json:"email"`
		} `json:"viewer"`
	}
	if err := client.Do(ctx, queryViewer, nil, &resp); err != nil {
		if rl, ok := err.(*RateLimitError); ok {
			if !rl.Reset.IsZero() {
				s.backoff[acct.Name] = rl.Reset
			}
		}
		return "", err
	}
	if resp.Viewer.ID == "" {
		return "", fmt.Errorf("linear: viewer id missing for account %s", acct.Name)
	}
	s.viewerIDs[acct.Name] = resp.Viewer.ID
	return resp.Viewer.ID, nil
}

// FetchIssueComments loads recent comments for an issue.
func (s *Service) FetchIssueComments(ctx context.Context, acct AccountConfig, issueID string) ([]Comment, error) {
	if issueID == "" {
		return nil, nil
	}
	client := s.ClientFor(acct)
	variables := map[string]any{"id": issueID, "first": 50}
	var resp struct {
		Issue struct {
			ID       string `json:"id"`
			Comments struct {
				Nodes []Comment `json:"nodes"`
			} `json:"comments"`
		} `json:"issue"`
	}
	if err := client.Do(ctx, queryIssueComments, variables, &resp); err != nil {
		if rl, ok := err.(*RateLimitError); ok {
			if !rl.Reset.IsZero() {
				s.backoff[acct.Name] = rl.Reset
			}
		}
		return nil, err
	}
	return resp.Issue.Comments.Nodes, nil
}

// BackoffUntil returns the backoff time for an account.
func (s *Service) BackoffUntil(account string) time.Time {
	return s.backoff[account]
}

// AnyBackoff returns true if any active account is backed off.
func (s *Service) AnyBackoff() (bool, time.Time) {
	var until time.Time
	for _, acct := range s.ActiveAccounts() {
		if t := s.backoff[acct.Name]; !t.IsZero() && time.Now().Before(t) {
			if until.IsZero() || t.After(until) {
				until = t
			}
		}
	}
	return !until.IsZero(), until
}

// MissingAuthAccounts returns active accounts without a usable token.
func (s *Service) MissingAuthAccounts() []string {
	missing := []string{}
	for _, acct := range s.ActiveAccounts() {
		if token, _ := s.accountToken(acct); token == "" {
			missing = append(missing, acct.Name)
		}
	}
	return missing
}

// FetchTeamStates fetches states for a team (cached in memory).
func (s *Service) FetchTeamStates(ctx context.Context, acct AccountConfig, teamID string) ([]State, error) {
	if states, ok := s.teamStates[teamID]; ok {
		return states, nil
	}
	client := s.ClientFor(acct)
	variables := map[string]any{"teamId": teamID}
	var resp struct {
		Team struct {
			ID     string `json:"id"`
			Name   string `json:"name"`
			States struct {
				Nodes []State `json:"nodes"`
			} `json:"states"`
		} `json:"team"`
	}
	if err := client.Do(ctx, queryTeamStates, variables, &resp); err != nil {
		return nil, err
	}
	s.teamStates[teamID] = resp.Team.States.Nodes
	return resp.Team.States.Nodes, nil
}

// UpdateIssueState updates an issue's state.
func (s *Service) UpdateIssueState(ctx context.Context, acct AccountConfig, issueID, stateID string) (State, error) {
	client := s.ClientFor(acct)
	variables := map[string]any{"id": issueID, "stateId": stateID}
	var resp struct {
		IssueUpdate struct {
			Success bool `json:"success"`
			Issue   struct {
				ID    string `json:"id"`
				State State  `json:"state"`
			} `json:"issue"`
		} `json:"issueUpdate"`
	}
	if err := client.Do(ctx, mutationIssueUpdate, variables, &resp); err != nil {
		return State{}, err
	}
	if !resp.IssueUpdate.Success {
		return State{}, fmt.Errorf("linear: issue update failed")
	}
	return resp.IssueUpdate.Issue.State, nil
}

// CreateComment adds a comment to an issue.
func (s *Service) CreateComment(ctx context.Context, acct AccountConfig, issueID, body string) error {
	client := s.ClientFor(acct)
	variables := map[string]any{"issueId": issueID, "body": body}
	var resp struct {
		CommentCreate struct {
			Success bool `json:"success"`
		} `json:"commentCreate"`
	}
	if err := client.Do(ctx, mutationCommentCreate, variables, &resp); err != nil {
		return err
	}
	if !resp.CommentCreate.Success {
		return fmt.Errorf("linear: comment create failed")
	}
	return nil
}

// CreateIssue creates a new Linear issue.
func (s *Service) CreateIssue(ctx context.Context, acct AccountConfig, teamID, title, description string) (Issue, error) {
	client := s.ClientFor(acct)
	variables := map[string]any{"teamId": teamID, "title": title, "description": description}
	var resp struct {
		IssueCreate struct {
			Success bool `json:"success"`
			Issue   struct {
				ID         string `json:"id"`
				Identifier string `json:"identifier"`
				Title      string `json:"title"`
				URL        string `json:"url"`
			} `json:"issue"`
		} `json:"issueCreate"`
	}
	if err := client.Do(ctx, mutationIssueCreate, variables, &resp); err != nil {
		return Issue{}, err
	}
	if !resp.IssueCreate.Success {
		return Issue{}, fmt.Errorf("linear: issue create failed")
	}
	return Issue{
		ID:         resp.IssueCreate.Issue.ID,
		Identifier: resp.IssueCreate.Issue.Identifier,
		Title:      resp.IssueCreate.Issue.Title,
		URL:        resp.IssueCreate.Issue.URL,
		Account:    acct.Name,
	}, nil
}

// UpdateIssue updates issue title/description.
func (s *Service) UpdateIssue(ctx context.Context, acct AccountConfig, issueID, title, description string) (Issue, error) {
	client := s.ClientFor(acct)
	variables := map[string]any{"id": issueID, "title": title, "description": description}
	var resp struct {
		IssueUpdate struct {
			Success bool `json:"success"`
			Issue   struct {
				ID          string `json:"id"`
				Title       string `json:"title"`
				Description string `json:"description"`
			} `json:"issue"`
		} `json:"issueUpdate"`
	}
	if err := client.Do(ctx, mutationIssueEdit, variables, &resp); err != nil {
		return Issue{}, err
	}
	if !resp.IssueUpdate.Success {
		return Issue{}, fmt.Errorf("linear: issue update failed")
	}
	return Issue{
		ID:          resp.IssueUpdate.Issue.ID,
		Title:       resp.IssueUpdate.Issue.Title,
		Description: resp.IssueUpdate.Issue.Description,
		Account:     acct.Name,
	}, nil
}

// ApplyScopeFilters filters issues based on scope config.
func ApplyScopeFilters(issues []Issue, scope ScopeConfig) []Issue {
	if len(issues) == 0 {
		return issues
	}
	projectAllow := make(map[string]bool)
	projectBlock := make(map[string]bool)
	teamAllow := make(map[string]bool)
	labelAllow := make(map[string]bool)
	for _, id := range scope.IncludeProjects {
		projectAllow[id] = true
	}
	for _, id := range scope.ExcludeProjects {
		projectBlock[id] = true
	}
	for _, id := range scope.IncludeTeams {
		teamAllow[id] = true
	}
	for _, name := range scope.Labels {
		labelAllow[strings.ToLower(name)] = true
	}
	cutoff := time.Time{}
	if scope.UpdatedWithinDays > 0 {
		cutoff = time.Now().AddDate(0, 0, -scope.UpdatedWithinDays)
	}

	filtered := issues[:0]
	for _, issue := range issues {
		if issue.Project != nil {
			if projectBlock[issue.Project.ID] {
				continue
			}
			if len(projectAllow) > 0 && !projectAllow[issue.Project.ID] {
				continue
			}
		}
		if len(teamAllow) > 0 && !teamAllow[issue.Team.ID] {
			continue
		}
		if !cutoff.IsZero() && issue.UpdatedAt.Before(cutoff) {
			continue
		}
		if len(labelAllow) > 0 {
			match := false
			for _, label := range issue.Labels {
				if labelAllow[strings.ToLower(label.Name)] {
					match = true
					break
				}
			}
			if !match {
				continue
			}
		}
		filtered = append(filtered, issue)
	}
	return filtered
}

// SortIssues sorts issues by updated time descending.
func SortIssues(issues []Issue) {
	sort.SliceStable(issues, func(i, j int) bool {
		return issues[i].UpdatedAt.After(issues[j].UpdatedAt)
	})
}

func (s *Service) accountToken(account AccountConfig) (string, string) {
	if strings.EqualFold(account.Auth.Mode, "oauth") {
		token := account.Auth.AccessToken
		if token == "" {
			if stored, err := LoadToken(account.Name); err == nil {
				token = stored
			}
		}
		return token, "Bearer"
	}
	return account.Auth.APIKey, ""
}

func buildIssueFilter(scope ScopeConfig, viewerID string) map[string]any {
	filter := map[string]any{
		"archived": map[string]any{"eq": false},
	}
	if scope.AssignedToMe {
		filter["assignee"] = map[string]any{"id": map[string]any{"eq": viewerID}}
	}
	return filter
}
