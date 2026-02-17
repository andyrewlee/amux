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

func TestHandleProjectsLoadedRebindsActiveWorkspace(t *testing.T) {
	repo := "/tmp/repo"
	root := "/tmp/workspaces/repo/feature"

	oldWS := data.NewWorkspace("feature", "feat-branch", "main", repo, root)
	oldWS.Assistant = "claude"
	project := data.NewProject(repo)
	project.AddWorkspace(*oldWS)

	app := &App{
		dashboard:       dashboard.New(),
		projects:        []data.Project{*project},
		activeWorkspace: &project.Workspaces[0],
		activeProject:   project,
		showWelcome:     false,
	}

	// Simulate reload with updated workspace (assistant changed)
	newWS := data.NewWorkspace("feature", "feat-branch", "main", repo, root)
	newWS.Assistant = "codex"
	newProject := data.NewProject(repo)
	newProject.AddWorkspace(*newWS)

	msg := messages.ProjectsLoaded{Projects: []data.Project{*newProject}}
	app.handleProjectsLoaded(msg)

	if app.activeWorkspace == nil {
		t.Fatal("expected activeWorkspace to be rebound, got nil")
	}
	if app.activeWorkspace.Assistant != "codex" {
		t.Fatalf("expected assistant %q, got %q", "codex", app.activeWorkspace.Assistant)
	}
	if app.activeProject == nil {
		t.Fatal("expected activeProject to be rebound, got nil")
	}
	if app.showWelcome {
		t.Fatal("expected showWelcome to remain false after rebind")
	}
}

func TestHandleProjectsLoadedClearsMissingActiveWorkspace(t *testing.T) {
	repo := "/tmp/repo"
	root := "/tmp/workspaces/repo/feature"

	oldWS := data.NewWorkspace("feature", "feat-branch", "main", repo, root)
	project := data.NewProject(repo)
	project.AddWorkspace(*oldWS)

	app := &App{
		dashboard:       dashboard.New(),
		projects:        []data.Project{*project},
		activeWorkspace: &project.Workspaces[0],
		activeProject:   project,
		showWelcome:     false,
	}

	// Reload with empty projects — workspace disappeared
	msg := messages.ProjectsLoaded{Projects: []data.Project{}}
	app.handleProjectsLoaded(msg)

	if app.activeWorkspace != nil {
		t.Fatalf("expected activeWorkspace nil, got %+v", app.activeWorkspace)
	}
	if app.activeProject != nil {
		t.Fatalf("expected activeProject nil, got %+v", app.activeProject)
	}
	if !app.showWelcome {
		t.Fatal("expected showWelcome true after workspace disappeared")
	}
}

func TestHandleProjectsLoadedRebindsActiveProjectByCanonicalPath(t *testing.T) {
	// Use a relative path for the active project
	relPath := "./repo"
	absPath, err := filepath.Abs(relPath)
	if err != nil {
		t.Fatalf("Abs(%q): %v", relPath, err)
	}

	oldProject := &data.Project{Name: "repo", Path: relPath}

	app := &App{
		dashboard:     dashboard.New(),
		projects:      []data.Project{*oldProject},
		activeProject: oldProject,
		showWelcome:   true,
	}

	// Reload with absolute path version of the same project
	newProject := data.NewProject(absPath)
	msg := messages.ProjectsLoaded{Projects: []data.Project{*newProject}}
	app.handleProjectsLoaded(msg)

	if app.activeProject == nil {
		t.Fatal("expected activeProject to be rebound via canonical path, got nil")
	}
	if app.activeProject.Path != absPath {
		t.Fatalf("expected activeProject.Path %q, got %q", absPath, app.activeProject.Path)
	}
}

func TestHandleProjectsLoadedRebindsActiveWorkspaceByCanonicalPathsOnIDMiss(t *testing.T) {
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

	app := &App{
		dashboard:       dashboard.New(),
		projects:        []data.Project{*oldProject},
		activeWorkspace: &oldProject.Workspaces[0],
		activeProject:   oldProject,
		showWelcome:     false,
	}

	// Simulate discovery rewriting relative workspace metadata to absolute paths.
	newWS := data.NewWorkspace("feature", "feat-branch", "main", absRepo, absRoot)
	newWS.Assistant = "codex"
	newProject := data.NewProject(absRepo)
	newProject.AddWorkspace(*newWS)

	msg := messages.ProjectsLoaded{Projects: []data.Project{*newProject}}
	app.handleProjectsLoaded(msg)

	if app.activeWorkspace == nil {
		t.Fatal("expected activeWorkspace to stay bound after ID miss fallback")
	}
	if app.activeWorkspace.Root != absRoot {
		t.Fatalf("expected activeWorkspace.Root %q, got %q", absRoot, app.activeWorkspace.Root)
	}
	if app.activeWorkspace.Assistant != "codex" {
		t.Fatalf("expected assistant %q, got %q", "codex", app.activeWorkspace.Assistant)
	}
	if app.activeProject == nil {
		t.Fatal("expected activeProject to stay bound after workspace rebind")
	}
	if app.activeProject.Path != absRepo {
		t.Fatalf("expected activeProject.Path %q, got %q", absRepo, app.activeProject.Path)
	}
	if app.showWelcome {
		t.Fatal("expected showWelcome to remain false after canonical path rebind")
	}
}

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
