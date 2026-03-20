package app

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/andyrewlee/amux/internal/data"
	"github.com/andyrewlee/amux/internal/messages"
	"github.com/andyrewlee/amux/internal/sandbox"
	"github.com/andyrewlee/amux/internal/ui/dashboard"
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

func TestHandleProjectsLoadedSyncsWhenWorkspaceRebindLeavesSandboxRuntime(t *testing.T) {
	repo := t.TempDir()
	root := filepath.Join(repo, "feature")
	if err := os.MkdirAll(root, 0o755); err != nil {
		t.Fatalf("MkdirAll() error = %v", err)
	}

	oldWS := data.NewWorkspace("feature", "feat-branch", "main", repo, root)
	oldWS.Runtime = data.RuntimeCloudSandbox
	project := data.NewProject(repo)
	project.AddWorkspace(*oldWS)

	manager := NewSandboxManager(nil)
	manager.storeSession(&sandboxSession{
		sandbox:       sandbox.NewMockRemoteSandbox("sb-runtime-flip"),
		worktreeID:    sandbox.ComputeWorktreeID(root),
		workspaceRoot: root,
		workspacePath: "/home/daytona/.amux/workspaces/runtime-flip/repo",
		needsSyncDown: true,
	})

	synced := false
	manager.downloadWorkspace = func(computer sandbox.RemoteSandbox, opts sandbox.SyncOptions, verbose bool) error {
		synced = true
		if opts.Cwd != root {
			t.Fatalf("download Cwd = %q, want %q", opts.Cwd, root)
		}
		return nil
	}

	app := &App{
		dashboard:       dashboard.New(),
		projects:        []data.Project{*project},
		activeWorkspace: &project.Workspaces[0],
		activeProject:   project,
		showWelcome:     false,
		sandboxManager:  manager,
	}

	newWS := data.NewWorkspace("feature", "feat-branch", "main", repo, root)
	newWS.Runtime = data.RuntimeLocalWorktree
	newProject := data.NewProject(repo)
	newProject.AddWorkspace(*newWS)

	cmds := app.handleProjectsLoaded(messages.ProjectsLoaded{Projects: []data.Project{*newProject}})
	for _, cmd := range cmds {
		if cmd != nil {
			_ = cmd()
		}
	}

	if !synced {
		t.Fatal("expected runtime rebind to sync sandbox edits down to local")
	}
}

func TestHandleProjectsLoadedAvoidsDuplicateRecoveredSandboxSyncForActiveRebind(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)

	repo := t.TempDir()
	root := filepath.Join(repo, "feature")
	if err := os.MkdirAll(root, 0o755); err != nil {
		t.Fatalf("MkdirAll() error = %v", err)
	}

	oldWS := data.NewWorkspace("feature", "feat-branch", "main", repo, root)
	oldWS.Runtime = data.RuntimeCloudSandbox
	project := data.NewProject(repo)
	project.AddWorkspace(*oldWS)

	needsSync := true
	if err := sandbox.SaveSandboxMeta(root, "fake", sandbox.SandboxMeta{
		SandboxID:     "sb-runtime-flip-persisted",
		Agent:         sandbox.AgentShell,
		Provider:      "fake",
		WorktreeID:    sandbox.ComputeWorktreeID(root),
		NeedsSyncDown: &needsSync,
		WorkspaceIDs:  []string{string(oldWS.ID())},
	}); err != nil {
		t.Fatalf("SaveSandboxMeta() error = %v", err)
	}

	manager := NewSandboxManager(nil)
	manager.storeSession(&sandboxSession{
		sandbox:       sandbox.NewMockRemoteSandbox("sb-runtime-flip-persisted"),
		worktreeID:    sandbox.ComputeWorktreeID(root),
		workspaceRoot: root,
		workspacePath: "/home/daytona/.amux/workspaces/runtime-flip/repo",
		needsSyncDown: true,
	})

	downloadCalls := 0
	manager.downloadWorkspace = func(computer sandbox.RemoteSandbox, opts sandbox.SyncOptions, verbose bool) error {
		downloadCalls++
		if opts.Cwd != root {
			t.Fatalf("download Cwd = %q, want %q", opts.Cwd, root)
		}
		return nil
	}

	app := &App{
		dashboard:       dashboard.New(),
		projects:        []data.Project{*project},
		activeWorkspace: &project.Workspaces[0],
		activeProject:   project,
		showWelcome:     false,
		sandboxManager:  manager,
	}

	newWS := data.NewWorkspace("feature", "feat-branch", "main", repo, root)
	newWS.Runtime = data.RuntimeLocalWorktree
	newProject := data.NewProject(repo)
	newProject.AddWorkspace(*newWS)

	cmds := app.handleProjectsLoaded(messages.ProjectsLoaded{Projects: []data.Project{*newProject}})
	for _, cmd := range cmds {
		if cmd == nil {
			continue
		}
		if msg, ok := cmd().(sandboxSyncResultMsg); ok {
			_ = app.handleSandboxSyncResult(msg)
		}
	}

	if downloadCalls != 1 {
		t.Fatalf("downloadWorkspace() calls = %d, want 1 without duplicate recovered sync", downloadCalls)
	}
}

func TestRebindActiveSelectionPersistsSandboxSyncTargetBeforeAsyncRuntimeFlipSync(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)

	wd, err := os.Getwd()
	if err != nil {
		t.Fatalf("Getwd() error = %v", err)
	}
	base := t.TempDir()
	absRepo := filepath.Join(base, "repo")
	absRoot := filepath.Join(base, "workspaces", "repo", "feature")
	if err := os.MkdirAll(absRoot, 0o755); err != nil {
		t.Fatalf("MkdirAll(%q) error = %v", absRoot, err)
	}
	relRepo, err := filepath.Rel(wd, absRepo)
	if err != nil {
		t.Fatalf("Rel(repo) error = %v", err)
	}
	relRoot, err := filepath.Rel(wd, absRoot)
	if err != nil {
		t.Fatalf("Rel(root) error = %v", err)
	}

	oldWS := data.NewWorkspace("feature", "feat-branch", "main", relRepo, relRoot)
	oldWS.Runtime = data.RuntimeCloudSandbox
	project := data.NewProject(relRepo)
	project.AddWorkspace(*oldWS)

	newWS := data.NewWorkspace("feature", "feat-branch", "main", absRepo, absRoot)
	newWS.Runtime = data.RuntimeLocalWorktree
	newProject := data.NewProject(absRepo)
	newProject.AddWorkspace(*newWS)

	needsSync := true
	if err := sandbox.SaveSandboxMeta(oldWS.Root, "fake", sandbox.SandboxMeta{
		SandboxID:     "sb-runtime-flip-persist-before-queue",
		Agent:         sandbox.AgentShell,
		Provider:      "fake",
		WorktreeID:    sandbox.ComputeWorktreeID(oldWS.Root),
		NeedsSyncDown: &needsSync,
		WorkspaceIDs:  []string{string(oldWS.ID())},
	}); err != nil {
		t.Fatalf("SaveSandboxMeta() error = %v", err)
	}

	manager := NewSandboxManager(nil)
	manager.storeSession(&sandboxSession{
		sandbox:            sandbox.NewMockRemoteSandbox("sb-runtime-flip-persist-before-queue"),
		providerName:       "fake",
		worktreeID:         sandbox.ComputeWorktreeID(oldWS.Root),
		workspaceID:        oldWS.ID(),
		workspaceIDAliases: map[string]struct{}{string(oldWS.ID()): {}},
		workspaceRoot:      oldWS.Root,
		workspaceRepo:      oldWS.Repo,
		workspacePath:      "/remote/ws",
		needsSyncDown:      true,
	})
	manager.downloadWorkspace = func(computer sandbox.RemoteSandbox, opts sandbox.SyncOptions, verbose bool) error {
		t.Fatal("unexpected sync execution during persistence-only regression check")
		return nil
	}

	app := &App{
		dashboard:       dashboard.New(),
		projects:        []data.Project{*newProject},
		activeWorkspace: &project.Workspaces[0],
		activeProject:   project,
		showWelcome:     false,
		sandboxManager:  manager,
	}

	cmds, _ := app.rebindActiveSelectionWithRecoverySkips()
	if len(cmds) == 0 {
		t.Fatal("expected runtime flip to queue a sandbox sync command")
	}

	meta, err := sandbox.LoadSandboxMeta(newWS.Root, "fake")
	if err != nil {
		t.Fatalf("LoadSandboxMeta(new) error = %v", err)
	}
	if meta == nil || meta.SandboxID != "sb-runtime-flip-persist-before-queue" {
		t.Fatalf("new metadata = %#v, want persisted sync target metadata", meta)
	}
	foundNewID := false
	for _, id := range meta.WorkspaceIDs {
		if id == string(newWS.ID()) {
			foundNewID = true
			break
		}
	}
	if !foundNewID {
		t.Fatalf("expected persisted metadata to include rebound workspace ID %q, got %v", newWS.ID(), meta.WorkspaceIDs)
	}
}
