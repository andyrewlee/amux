package app

import (
	"testing"

	"github.com/andyrewlee/amux/internal/config"
	"github.com/andyrewlee/amux/internal/data"
	"github.com/andyrewlee/amux/internal/messages"
	"github.com/andyrewlee/amux/internal/ui/center"
	"github.com/andyrewlee/amux/internal/ui/dashboard"
	"github.com/andyrewlee/amux/internal/ui/sidebar"
)

func TestRebindActiveSelection_MergesExternalTabsForStatefulWorkspace(t *testing.T) {
	repo := "/tmp/repo"
	root := "/tmp/workspaces/repo/feature"

	oldWS := data.NewWorkspace("feature", "feat-branch", "main", repo, root)
	oldProject := data.NewProject(repo)
	oldProject.AddWorkspace(*oldWS)

	reloadedWS := data.NewWorkspace("feature", "feat-branch", "main", repo, root)
	reloadedWS.OpenTabs = []data.TabInfo{
		{
			Assistant:   "codex",
			Name:        "existing",
			SessionName: "amux-existing-session",
			Status:      "running",
		},
		{
			Assistant:   "codex",
			Name:        "external",
			SessionName: "amux-external-session",
			Status:      "running",
		},
	}
	reloadedProject := data.NewProject(repo)
	reloadedProject.AddWorkspace(*reloadedWS)

	centerModel := center.New(&config.Config{
		Assistants: map[string]config.AssistantConfig{
			"codex": {Command: "codex"},
		},
	})
	centerModel.SetWorkspace(oldWS)
	centerModel.AddTab(&center.Tab{
		ID:          center.TabID("existing"),
		Name:        "existing",
		Assistant:   "codex",
		SessionName: "amux-existing-session",
		Workspace:   oldWS,
		Running:     true,
	})
	if !centerModel.HasWorkspaceState(string(oldWS.ID())) {
		t.Fatal("expected existing workspace state before merge")
	}

	app := &App{
		dashboard:       dashboard.New(),
		center:          centerModel,
		sidebar:         sidebar.NewTabbedSidebar(),
		sidebarTerminal: sidebar.NewTerminalModel(),
		projects:        []data.Project{*oldProject},
		activeWorkspace: oldWS,
		activeProject:   oldProject,
		showWelcome:     false,
	}

	app.handleProjectsLoaded(messages.ProjectsLoaded{Projects: []data.Project{*reloadedProject}})

	tabs, _ := app.center.GetTabsInfoForWorkspace(string(reloadedWS.ID()))
	if len(tabs) != 2 {
		t.Fatalf("expected 2 tabs after merge, got %d", len(tabs))
	}
	hasExternal := false
	for _, tab := range tabs {
		if tab.SessionName == "amux-external-session" {
			hasExternal = true
			break
		}
	}
	if !hasExternal {
		t.Fatal("expected external tab to be merged for stateful workspace")
	}
}

func TestRebindActiveSelection_DoesNotRehydratePersistedTabsWhenWorkspaceStateExists(t *testing.T) {
	repo := "/tmp/repo"
	root := "/tmp/workspaces/repo/feature"

	oldWS := data.NewWorkspace("feature", "feat-branch", "main", repo, root)
	oldProject := data.NewProject(repo)
	oldProject.AddWorkspace(*oldWS)

	reloadedWS := data.NewWorkspace("feature", "feat-branch", "main", repo, root)
	reloadedWS.OpenTabs = []data.TabInfo{
		{
			Assistant:   "codex",
			Name:        "stale",
			SessionName: "amux-stale-session",
			Status:      "running",
		},
	}
	reloadedProject := data.NewProject(repo)
	reloadedProject.AddWorkspace(*reloadedWS)

	centerModel := center.New(&config.Config{
		Assistants: map[string]config.AssistantConfig{
			"codex": {Command: "codex"},
		},
	})
	centerModel.SetWorkspace(oldWS)
	centerModel.AddTab(&center.Tab{
		ID:        center.TabID("existing"),
		Name:      "existing",
		Assistant: "codex",
		Workspace: oldWS,
	})
	_ = centerModel.CloseActiveTab()
	if centerModel.HasTabs() {
		t.Fatal("expected no active tabs after closing placeholder tab")
	}
	if !centerModel.HasWorkspaceState(string(oldWS.ID())) {
		t.Fatal("expected workspace state to remain tracked after closing the last tab")
	}

	app := &App{
		dashboard:       dashboard.New(),
		center:          centerModel,
		sidebar:         sidebar.NewTabbedSidebar(),
		sidebarTerminal: sidebar.NewTerminalModel(),
		projects:        []data.Project{*oldProject},
		activeWorkspace: oldWS,
		activeProject:   oldProject,
		showWelcome:     false,
	}

	app.handleProjectsLoaded(messages.ProjectsLoaded{Projects: []data.Project{*reloadedProject}})

	if app.center.HasTabs() {
		t.Fatal("expected stale persisted tabs to not be rehydrated when workspace state already exists")
	}
}
