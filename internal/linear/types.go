package linear

import "time"

// Issue represents a Linear issue with the fields needed by amux.
type Issue struct {
	ID          string    `json:"id"`
	Identifier  string    `json:"identifier"`
	Title       string    `json:"title"`
	Description string    `json:"description"`
	URL         string    `json:"url"`
	Priority    int       `json:"priority"`
	State       State     `json:"state"`
	Team        Team      `json:"team"`
	Project     *Project  `json:"project,omitempty"`
	Assignee    *User     `json:"assignee,omitempty"`
	Labels      []Label   `json:"labels"`
	UpdatedAt   time.Time `json:"updatedAt"`
	CreatedAt   time.Time `json:"createdAt"`
	Account     string    `json:"account"`
}

// State represents a Linear workflow state.
type State struct {
	ID   string `json:"id"`
	Name string `json:"name"`
	Type string `json:"type"`
}

// Team represents a Linear team.
type Team struct {
	ID   string `json:"id"`
	Key  string `json:"key"`
	Name string `json:"name"`
}

// Project represents a Linear project.
type Project struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}

// User represents a Linear user.
type User struct {
	ID    string `json:"id"`
	Name  string `json:"name"`
	Email string `json:"email,omitempty"`
}

// Label represents a Linear label.
type Label struct {
	ID    string `json:"id"`
	Name  string `json:"name"`
	Color string `json:"color"`
}

// Comment represents a Linear comment.
type Comment struct {
	ID        string    `json:"id"`
	Body      string    `json:"body"`
	CreatedAt time.Time `json:"createdAt"`
	User      *User     `json:"user,omitempty"`
}
