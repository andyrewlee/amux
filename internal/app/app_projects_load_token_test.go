package app

import (
	"testing"

	"github.com/andyrewlee/amux/internal/data"
	"github.com/andyrewlee/amux/internal/messages"
	"github.com/andyrewlee/amux/internal/ui/center"
	"github.com/andyrewlee/amux/internal/ui/dashboard"
	"github.com/andyrewlee/amux/internal/ui/sidebar"
)

// TestHandleProjectsLoaded_DropsStaleLoadToken proves an out-of-order reload is
// dropped: under rapid deletes an older LoadProjects goroutine (started before a
// later delete completed) could deliver last and resurrect the deleted workspace.
func TestHandleProjectsLoaded_DropsStaleLoadToken(t *testing.T) {
	app := &App{
		dashboard:       dashboard.New(),
		center:          center.New(nil),
		sidebar:         sidebar.NewTabbedSidebar(),
		sidebarTerminal: sidebar.NewTerminalModel(),
	}
	newer := []data.Project{{Name: "kept", Path: "/kept"}}
	older := []data.Project{{Name: "kept", Path: "/kept"}, {Name: "deleted", Path: "/deleted"}}

	app.handleProjectsLoaded(messages.ProjectsLoaded{Projects: newer, LoadToken: 5})
	if len(app.projects) != 1 {
		t.Fatalf("expected newer (higher-token) set applied, got %d projects", len(app.projects))
	}

	// A later-arriving but OLDER load (lower token) must be dropped.
	app.handleProjectsLoaded(messages.ProjectsLoaded{Projects: older, LoadToken: 3})
	if len(app.projects) != 1 {
		t.Fatalf("stale lower-token load must be dropped, got %d projects", len(app.projects))
	}

	// A zero-token message still applies (back-compat with existing callers/tests).
	app.handleProjectsLoaded(messages.ProjectsLoaded{Projects: older, LoadToken: 0})
	if len(app.projects) != 2 {
		t.Fatalf("zero-token load should apply, got %d projects", len(app.projects))
	}
}
