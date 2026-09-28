package app

import (
	"testing"

	"github.com/andyrewlee/amux/internal/data"
	"github.com/andyrewlee/amux/internal/ui/center"
	"github.com/andyrewlee/amux/internal/ui/dashboard"
)

// TestHandleTabBellMarksAttention pins the app-side edge of the agent-bell
// channel: a TabBell must raise the workspace's unacked attention badge so it
// is reachable via the dashboard's `n` jump (JumpToNextAttention returns a
// non-nil cmd only when a badge is visible).
func TestHandleTabBellMarksAttention(t *testing.T) {
	app := &App{dashboard: dashboard.New()}
	app.dashboard.SetSize(30, 30)

	ws := data.Workspace{Name: "ws-a", Branch: "feat", Repo: "/repo", Root: "/repo/ws"}
	app.dashboard.SetProjects([]data.Project{{
		Name:       "proj",
		Path:       "/repo",
		Workspaces: []data.Workspace{ws},
	}})
	_ = app.dashboard.View()

	if cmd := app.dashboard.JumpToNextAttention(); cmd != nil {
		t.Fatal("no attention should exist before a bell")
	}

	app.handleTabBell(center.TabBell{WorkspaceID: string(ws.ID()), TabID: "tab-1"})

	if cmd := app.dashboard.JumpToNextAttention(); cmd == nil {
		t.Fatal("belled workspace is not an attention target")
	}
}

// TestHandleTabBellEmptyWorkspace is the guard case — a bell with no workspace
// binding must not latch a phantom badge.
func TestHandleTabBellEmptyWorkspace(t *testing.T) {
	app := &App{dashboard: dashboard.New()}
	app.handleTabBell(center.TabBell{})
	if cmd := app.dashboard.JumpToNextAttention(); cmd != nil {
		t.Fatal("empty-workspace bell must not create attention")
	}
}
