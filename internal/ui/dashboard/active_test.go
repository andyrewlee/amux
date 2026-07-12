package dashboard

import (
	"testing"

	"github.com/andyrewlee/amux/internal/data"
	"github.com/andyrewlee/amux/internal/messages"
)

func TestDashboardGetMainWorkspace(t *testing.T) {
	project := data.Project{
		Workspaces: []data.Workspace{
			{Name: "feature", Branch: "feature", Repo: "/repo", Root: "/repo/feature"},
			{Name: "main-wt", Branch: "main", Repo: "/repo", Root: "/repo"},
		},
	}

	m := New()
	main := m.getMainWorkspace(&project)
	if main == nil {
		t.Fatalf("expected main workspace to be found")
	}
	if main.Branch != "main" {
		t.Errorf("expected main branch, got %s", main.Branch)
	}
}

func TestDashboardHomeActive(t *testing.T) {
	m := New()

	// Initially home is active (activeRoot is empty)
	if m.activeRoot != "" {
		t.Errorf("expected activeRoot to be empty initially")
	}

	// Activate a workspace
	m.Update(messages.WorkspaceActivated{
		Workspace: &data.Workspace{Root: "/some/root"},
	})
	if m.activeRoot != "/some/root" {
		t.Errorf("expected activeRoot to be /some/root")
	}

	// Show welcome (go home)
	m.Update(messages.ShowWelcome{})
	if m.activeRoot != "" {
		t.Errorf("expected activeRoot to be empty after ShowWelcome")
	}
}

func TestDashboardProjectRowActive_DeletingSupersedesActive(t *testing.T) {
	t.Parallel()
	main := &data.Workspace{Name: "p", Branch: "main", Repo: "/p", Root: "/p"}
	mainID := string(main.ID())

	m := New()
	m.activeWorkspaceIDs = map[string]bool{mainID: true}

	if !m.projectRowActive(mainID, main) {
		t.Fatal("expected an active main workspace to render the project row active")
	}

	// Mark the project deleting: the row must no longer render active even though
	// the workspace is still in the active set.
	m.deletingWorkspaces = map[string]bool{main.Root: true}
	if m.projectRowActive(mainID, main) {
		t.Fatal("a deleting project must not render active")
	}
}
