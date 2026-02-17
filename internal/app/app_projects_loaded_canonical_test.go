package app

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/andyrewlee/amux/internal/config"
	"github.com/andyrewlee/amux/internal/data"
	"github.com/andyrewlee/amux/internal/git"
	"github.com/andyrewlee/amux/internal/messages"
	"github.com/andyrewlee/amux/internal/ui/center"
	"github.com/andyrewlee/amux/internal/ui/dashboard"
	"github.com/andyrewlee/amux/internal/ui/sidebar"
)

func TestHandleProjectsLoadedCanonicalRebindMigratesCenterAndSidebarTerminalTabs(t *testing.T) {
	wd, err := os.Getwd()
	if err != nil {
		t.Fatalf("Getwd: %v", err)
	}

	base := t.TempDir()
	absRepo := filepath.Join(base, "repo")
	absRoot := filepath.Join(base, "workspaces", "repo", "feature")
	if err := os.MkdirAll(absRoot, 0o755); err != nil {
		t.Fatalf("MkdirAll(%q): %v", absRoot, err)
	}

	relRepo, err := filepath.Rel(wd, absRepo)
	if err != nil {
		t.Fatalf("Rel(repo): %v", err)
	}
	relRoot, err := filepath.Rel(wd, absRoot)
	if err != nil {
		t.Fatalf("Rel(root): %v", err)
	}

	oldWS := data.NewWorkspace("feature", "feat-branch", "main", relRepo, relRoot)
	oldProject := data.NewProject(relRepo)
	oldProject.AddWorkspace(*oldWS)
	activeOld := &oldProject.Workspaces[0]

	newWS := data.NewWorkspace("feature", "feat-branch", "main", absRepo, absRoot)
	newProject := data.NewProject(absRepo)
	newProject.AddWorkspace(*newWS)

	centerModel := center.New(nil)
	centerModel.SetWorkspace(activeOld)
	centerModel.AddTab(&center.Tab{
		ID:        center.TabID("tab-existing"),
		Name:      "tab-existing",
		Workspace: activeOld,
	})

	sidebarTerminal := sidebar.NewTerminalModel()
	sidebarTerminal.AddTerminalForHarness(activeOld)

	app := &App{
		dashboard:       dashboard.New(),
		center:          centerModel,
		sidebar:         sidebar.NewTabbedSidebar(),
		sidebarTerminal: sidebarTerminal,
		projects:        []data.Project{*oldProject},
		activeWorkspace: activeOld,
		activeProject:   oldProject,
		showWelcome:     false,
	}

	msg := messages.ProjectsLoaded{Projects: []data.Project{*newProject}}
	app.handleProjectsLoaded(msg)

	if app.activeWorkspace == nil {
		t.Fatal("expected activeWorkspace to remain bound")
	}
	if app.activeWorkspace.ID() != newWS.ID() {
		t.Fatalf("expected active workspace ID %q, got %q", newWS.ID(), app.activeWorkspace.ID())
	}
	if !app.center.HasTabs() {
		t.Fatal("expected center tabs to remain visible after workspace ID migration")
	}
	if cmd := app.sidebarTerminal.EnsureTerminalTab(); cmd != nil {
		t.Fatal("expected sidebar terminal tab to be migrated; got create command")
	}
}

func TestHandleProjectsLoadedCanonicalRebindMigratesDirtyWorkspaceID(t *testing.T) {
	wd, err := os.Getwd()
	if err != nil {
		t.Fatalf("Getwd: %v", err)
	}

	base := t.TempDir()
	absRepo := filepath.Join(base, "repo")
	absRoot := filepath.Join(base, "workspaces", "repo", "feature")
	if err := os.MkdirAll(absRoot, 0o755); err != nil {
		t.Fatalf("MkdirAll(%q): %v", absRoot, err)
	}

	relRepo, err := filepath.Rel(wd, absRepo)
	if err != nil {
		t.Fatalf("Rel(repo): %v", err)
	}
	relRoot, err := filepath.Rel(wd, absRoot)
	if err != nil {
		t.Fatalf("Rel(root): %v", err)
	}

	oldWS := data.NewWorkspace("feature", "feat-branch", "main", relRepo, relRoot)
	oldProject := data.NewProject(relRepo)
	oldProject.AddWorkspace(*oldWS)
	activeOld := &oldProject.Workspaces[0]

	newWS := data.NewWorkspace("feature", "feat-branch", "main", absRepo, absRoot)
	newProject := data.NewProject(absRepo)
	newProject.AddWorkspace(*newWS)

	oldID := string(activeOld.ID())
	newID := string(newWS.ID())
	if oldID == newID {
		t.Fatalf("expected old/new workspace IDs to differ, both %q", oldID)
	}

	app := &App{
		dashboard:        dashboard.New(),
		center:           center.New(nil),
		sidebar:          sidebar.NewTabbedSidebar(),
		sidebarTerminal:  sidebar.NewTerminalModel(),
		workspaceService: newWorkspaceService(nil, nil, nil, ""),
		projects:         []data.Project{*oldProject},
		activeWorkspace:  activeOld,
		activeProject:    oldProject,
		showWelcome:      false,
		dirtyWorkspaces: map[string]bool{
			oldID: true,
		},
		persistToken: 1,
	}

	msg := messages.ProjectsLoaded{Projects: []data.Project{*newProject}}
	app.handleProjectsLoaded(msg)

	if app.dirtyWorkspaces[oldID] {
		t.Fatalf("expected old dirty workspace key %q to be migrated", oldID)
	}
	if !app.dirtyWorkspaces[newID] {
		t.Fatalf("expected new dirty workspace key %q after migration", newID)
	}

	cmd := app.handlePersistDebounce(persistDebounceMsg{token: app.persistToken})
	if cmd == nil {
		t.Fatal("expected persist debounce command for migrated dirty workspace")
	}
}

func TestRebindActiveSelection_DoesNotRehydratePersistedTabsOnCanonicalIDMigrationWithEmptyState(t *testing.T) {
	wd, err := os.Getwd()
	if err != nil {
		t.Fatalf("Getwd: %v", err)
	}

	base := t.TempDir()
	absRepo := filepath.Join(base, "repo")
	absRoot := filepath.Join(base, "workspaces", "repo", "feature")
	if err := os.MkdirAll(absRoot, 0o755); err != nil {
		t.Fatalf("MkdirAll(%q): %v", absRoot, err)
	}

	relRepo, err := filepath.Rel(wd, absRepo)
	if err != nil {
		t.Fatalf("Rel(repo): %v", err)
	}
	relRoot, err := filepath.Rel(wd, absRoot)
	if err != nil {
		t.Fatalf("Rel(root): %v", err)
	}

	oldWS := data.NewWorkspace("feature", "feat-branch", "main", relRepo, relRoot)
	oldProject := data.NewProject(relRepo)
	oldProject.AddWorkspace(*oldWS)

	reloadedWS := data.NewWorkspace("feature", "feat-branch", "main", absRepo, absRoot)
	reloadedWS.OpenTabs = []data.TabInfo{
		{
			Assistant:   "codex",
			Name:        "stale",
			SessionName: "amux-stale-session",
			Status:      "running",
		},
	}
	reloadedProject := data.NewProject(absRepo)
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
		t.Fatal("expected old workspace to keep explicit empty tab state")
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
		t.Fatal("expected stale persisted tabs to remain hidden after canonical workspace ID migration")
	}
	if !app.center.HasWorkspaceState(string(reloadedWS.ID())) {
		t.Fatal("expected new canonical workspace ID to keep explicit empty tab state")
	}

	// A subsequent reload should still preserve explicit empty state and avoid
	// stale persisted tab rehydration.
	app.handleProjectsLoaded(messages.ProjectsLoaded{Projects: []data.Project{*reloadedProject}})
	if app.center.HasTabs() {
		t.Fatal("expected stale persisted tabs to remain hidden on subsequent reloads")
	}
}

func TestRebindActiveSelectionRewatchesActiveWorkspaceRootOnCanonicalIDChange(t *testing.T) {
	wd, err := os.Getwd()
	if err != nil {
		t.Fatalf("Getwd: %v", err)
	}

	base := t.TempDir()
	absRepo := filepath.Join(base, "repo")
	absRoot := filepath.Join(base, "workspaces", "repo", "feature")
	if err := os.MkdirAll(filepath.Join(absRoot, ".git"), 0o755); err != nil {
		t.Fatalf("MkdirAll(%q): %v", filepath.Join(absRoot, ".git"), err)
	}
	if err := os.MkdirAll(absRepo, 0o755); err != nil {
		t.Fatalf("MkdirAll(%q): %v", absRepo, err)
	}

	relRepo, err := filepath.Rel(wd, absRepo)
	if err != nil {
		t.Fatalf("Rel(repo): %v", err)
	}
	relRoot, err := filepath.Rel(wd, absRoot)
	if err != nil {
		t.Fatalf("Rel(root): %v", err)
	}

	oldWS := data.NewWorkspace("feature", "feat-branch", "main", relRepo, relRoot)
	oldProject := data.NewProject(relRepo)
	oldProject.AddWorkspace(*oldWS)

	newWS := data.NewWorkspace("feature", "feat-branch", "main", absRepo, absRoot)
	newProject := data.NewProject(absRepo)
	newProject.AddWorkspace(*newWS)

	fileWatcher, err := git.NewFileWatcher(func(string) {})
	if err != nil {
		t.Fatalf("NewFileWatcher: %v", err)
	}
	defer func() { _ = fileWatcher.Close() }()
	if err := fileWatcher.Watch(relRoot); err != nil {
		t.Fatalf("Watch(%q): %v", relRoot, err)
	}
	if !fileWatcher.IsWatching(relRoot) {
		t.Fatalf("expected watcher to track old root %q", relRoot)
	}

	app := &App{
		projects:        []data.Project{*newProject},
		activeWorkspace: &oldProject.Workspaces[0],
		activeProject:   oldProject,
		fileWatcher:     fileWatcher,
		dashboard:       dashboard.New(),
		dirtyWorkspaces: make(map[string]bool),
	}

	app.rebindActiveSelection()

	if app.activeWorkspace == nil || app.activeWorkspace.Root != absRoot {
		t.Fatalf("expected active workspace root %q, got %#v", absRoot, app.activeWorkspace)
	}
	if fileWatcher.IsWatching(relRoot) {
		t.Fatalf("expected old root %q to be unwatched after rebind", relRoot)
	}
	if !fileWatcher.IsWatching(absRoot) {
		t.Fatalf("expected new root %q to be watched after rebind", absRoot)
	}
}
