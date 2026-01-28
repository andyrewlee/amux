package tracker

import (
	"context"
	"time"
)

// Task represents a tracker-agnostic issue.
type Task struct {
	ID          string
	Identifier  string
	Title       string
	Description string
	URL         string
	StateID     string
	StateName   string
	StateType   string
	TeamID      string
	TeamKey     string
	TeamName    string
	ProjectID   string
	ProjectName string
	AssigneeID  string
	Assignee    string
	Labels      []string
	UpdatedAt   time.Time
	CreatedAt   time.Time
	Account     string
	Raw         any
}

// TaskFilter describes a query for tasks.
type TaskFilter struct {
	Query string
}

// Adapter defines a tracker integration.
type Adapter interface {
	ListTasks(ctx context.Context, filter TaskFilter) ([]Task, error)
	GetTask(ctx context.Context, id string) (Task, error)
	UpdateStatus(ctx context.Context, id string, stateID string) (Task, error)
	AddComment(ctx context.Context, id string, body string) error
	CreateTask(ctx context.Context, teamID string, title string, description string) (Task, error)
	LinkPR(ctx context.Context, id string, url string) error
	Search(ctx context.Context, query string) ([]Task, error)
}
