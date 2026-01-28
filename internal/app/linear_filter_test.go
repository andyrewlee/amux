package app

import (
	"testing"
	"time"

	"github.com/andyrewlee/amux/internal/linear"
	"github.com/andyrewlee/amux/internal/ui/board"
)

func TestApplyBoardFilters_LabelAndAssignee(t *testing.T) {
	cfg := linear.DefaultConfig()
	cfg.Scope.UpdatedWithinDays = 0
	app := &App{
		linearConfig: cfg,
		board:        board.New(),
	}

	now := time.Now()
	issues := []linear.Issue{
		{
			ID:        "1",
			Title:     "Bug fix",
			Labels:    []linear.Label{{Name: "bug"}},
			Assignee:  &linear.User{Name: "Alice"},
			UpdatedAt: now,
		},
		{
			ID:        "2",
			Title:     "Feature",
			Labels:    []linear.Label{{Name: "feature"}},
			Assignee:  &linear.User{Name: "Bob"},
			UpdatedAt: now,
		},
	}

	app.board.Filters.Label = "bug"
	filtered := app.applyBoardFilters(issues)
	if len(filtered) != 1 || filtered[0].ID != "1" {
		t.Fatalf("expected label filter to return issue 1, got %#v", filtered)
	}

	app.board.Filters.Label = ""
	app.board.Filters.Assignee = "Bob"
	filtered = app.applyBoardFilters(issues)
	if len(filtered) != 1 || filtered[0].ID != "2" {
		t.Fatalf("expected assignee filter to return issue 2, got %#v", filtered)
	}
}

func TestApplyBoardFilters_UpdatedWithinDays(t *testing.T) {
	cfg := linear.DefaultConfig()
	cfg.Scope.UpdatedWithinDays = 0
	app := &App{
		linearConfig: cfg,
		board:        board.New(),
	}

	now := time.Now()
	issues := []linear.Issue{
		{ID: "1", UpdatedAt: now},
		{ID: "2", UpdatedAt: now.AddDate(0, 0, -10)},
	}

	app.board.Filters.UpdatedWithinDays = 5
	filtered := app.applyBoardFilters(issues)
	if len(filtered) != 1 || filtered[0].ID != "1" {
		t.Fatalf("expected recent filter to return issue 1, got %#v", filtered)
	}
}
