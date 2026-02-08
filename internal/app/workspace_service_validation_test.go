package app

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/andyrewlee/amux/internal/data"
	"github.com/andyrewlee/amux/internal/messages"
)

func TestAddProjectRejectsInvalidPath(t *testing.T) {
	tmp := t.TempDir()
	registry := data.NewRegistry(filepath.Join(tmp, "projects.json"))
	service := newWorkspaceService(registry, nil, nil, "")
	app := &App{workspaceService: service}

	filePath := filepath.Join(tmp, "not-a-directory")
	if err := os.WriteFile(filePath, []byte("x"), 0o644); err != nil {
		t.Fatalf("write file: %v", err)
	}

	msg := app.addProject(filePath)()
	if _, ok := msg.(messages.Error); !ok {
		t.Fatalf("expected messages.Error, got %T", msg)
	}
	paths, err := registry.Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if len(paths) != 0 {
		t.Fatalf("expected no registered projects, got %d", len(paths))
	}
}

func TestAddProjectRegistersGitRepo(t *testing.T) {
	skipIfNoGit(t)

	repo := t.TempDir()
	runGit(t, repo, "init", "-b", "main")

	tmp := t.TempDir()
	registry := data.NewRegistry(filepath.Join(tmp, "projects.json"))
	service := newWorkspaceService(registry, nil, nil, "")
	app := &App{workspaceService: service}

	msg := app.addProject(repo)()
	if _, ok := msg.(messages.RefreshDashboard); !ok {
		t.Fatalf("expected RefreshDashboard, got %T", msg)
	}
	paths, err := registry.Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if len(paths) != 1 {
		t.Fatalf("expected one registered project, got %d", len(paths))
	}
	if normalizePath(paths[0]) != normalizePath(repo) {
		t.Fatalf("registered path = %s, want %s", paths[0], repo)
	}
}

func TestAddProjectExpandsTildePath(t *testing.T) {
	skipIfNoGit(t)

	home := t.TempDir()
	repo := filepath.Join(home, "repo")
	if err := os.MkdirAll(repo, 0o755); err != nil {
		t.Fatalf("MkdirAll(repo): %v", err)
	}
	runGit(t, repo, "init", "-b", "main")
	t.Setenv("HOME", home)

	tmp := t.TempDir()
	registry := data.NewRegistry(filepath.Join(tmp, "projects.json"))
	service := newWorkspaceService(registry, nil, nil, "")
	app := &App{workspaceService: service}

	msg := app.addProject("~/repo")()
	if _, ok := msg.(messages.RefreshDashboard); !ok {
		t.Fatalf("expected RefreshDashboard, got %T", msg)
	}

	paths, err := registry.Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if len(paths) != 1 {
		t.Fatalf("expected one registered project, got %d", len(paths))
	}
	if normalizePath(paths[0]) != normalizePath(repo) {
		t.Fatalf("registered path = %s, want %s", paths[0], repo)
	}
}

func TestCreateWorkspaceRejectsInvalidName(t *testing.T) {
	repo := t.TempDir()
	tmp := t.TempDir()

	workspacesRoot := filepath.Join(tmp, "workspaces")
	store := data.NewWorkspaceStore(filepath.Join(tmp, "workspaces-metadata"))
	workspaceService := newWorkspaceService(nil, store, nil, workspacesRoot)
	app := &App{workspaceService: workspaceService}
	project := data.NewProject(repo)

	origCreate := createWorkspaceFn
	t.Cleanup(func() {
		createWorkspaceFn = origCreate
	})
	createCalled := false
	createWorkspaceFn = func(repoPath, workspacePath, branch, base string) error {
		createCalled = true
		return nil
	}

	msg := app.createWorkspace(project, "bad/name", "main")()
	failed, ok := msg.(messages.WorkspaceCreateFailed)
	if !ok {
		t.Fatalf("expected WorkspaceCreateFailed, got %T", msg)
	}
	if failed.Err == nil {
		t.Fatalf("expected validation error")
	}
	if createCalled {
		t.Fatalf("expected createWorkspaceFn not to run for invalid name")
	}
}

func TestCreateWorkspaceRejectsInvalidBaseRef(t *testing.T) {
	repo := t.TempDir()
	tmp := t.TempDir()

	workspacesRoot := filepath.Join(tmp, "workspaces")
	store := data.NewWorkspaceStore(filepath.Join(tmp, "workspaces-metadata"))
	workspaceService := newWorkspaceService(nil, store, nil, workspacesRoot)
	app := &App{workspaceService: workspaceService}
	project := data.NewProject(repo)

	origCreate := createWorkspaceFn
	t.Cleanup(func() {
		createWorkspaceFn = origCreate
	})
	createCalled := false
	createWorkspaceFn = func(repoPath, workspacePath, branch, base string) error {
		createCalled = true
		return nil
	}

	msg := app.createWorkspace(project, "feature", "bad ref")()
	failed, ok := msg.(messages.WorkspaceCreateFailed)
	if !ok {
		t.Fatalf("expected WorkspaceCreateFailed, got %T", msg)
	}
	if failed.Err == nil {
		t.Fatalf("expected validation error")
	}
	if createCalled {
		t.Fatalf("expected createWorkspaceFn not to run for invalid base ref")
	}
}

func TestDeleteWorkspaceRejectsPathOutsideManagedProjectRoot(t *testing.T) {
	repo := t.TempDir()
	tmp := t.TempDir()
	workspacesRoot := filepath.Join(tmp, "workspaces")

	store := data.NewWorkspaceStore(filepath.Join(tmp, "workspaces-metadata"))
	workspaceService := newWorkspaceService(nil, store, nil, workspacesRoot)
	project := data.NewProject(repo)
	workspace := data.NewWorkspace(
		"feature",
		"feature",
		"main",
		repo,
		filepath.Join(workspacesRoot, "other-project", "feature"),
	)

	origRemove := removeWorkspaceFn
	t.Cleanup(func() {
		removeWorkspaceFn = origRemove
	})
	removeCalled := false
	removeWorkspaceFn = func(repoPath, workspacePath string) error {
		removeCalled = true
		return nil
	}

	msg := workspaceService.DeleteWorkspace(project, workspace)()
	failed, ok := msg.(messages.WorkspaceDeleteFailed)
	if !ok {
		t.Fatalf("expected WorkspaceDeleteFailed, got %T", msg)
	}
	if failed.Err == nil {
		t.Fatalf("expected error for workspace path outside project root")
	}
	if removeCalled {
		t.Fatalf("expected removeWorkspaceFn not to be called for unmanaged project path")
	}
}

func TestDeleteWorkspaceRejectsUnsafeProjectNameSegment(t *testing.T) {
	repo := t.TempDir()
	tmp := t.TempDir()
	workspacesRoot := filepath.Join(tmp, "workspaces")

	store := data.NewWorkspaceStore(filepath.Join(tmp, "workspaces-metadata"))
	workspaceService := newWorkspaceService(nil, store, nil, workspacesRoot)
	project := &data.Project{
		Name: "../unsafe",
		Path: repo,
	}
	workspace := data.NewWorkspace(
		"feature",
		"feature",
		"main",
		repo,
		filepath.Join(workspacesRoot, "unsafe", "feature"),
	)

	origRemove := removeWorkspaceFn
	t.Cleanup(func() {
		removeWorkspaceFn = origRemove
	})
	removeCalled := false
	removeWorkspaceFn = func(repoPath, workspacePath string) error {
		removeCalled = true
		return nil
	}

	msg := workspaceService.DeleteWorkspace(project, workspace)()
	failed, ok := msg.(messages.WorkspaceDeleteFailed)
	if !ok {
		t.Fatalf("expected WorkspaceDeleteFailed, got %T", msg)
	}
	if failed.Err == nil {
		t.Fatalf("expected validation error for unsafe project name segment")
	}
	if removeCalled {
		t.Fatalf("expected removeWorkspaceFn not to run for unsafe project names")
	}
}

func TestCreateWorkspaceUsesProjectScopedHashedRoot(t *testing.T) {
	repo := filepath.Join(t.TempDir(), "repo")
	tmp := t.TempDir()
	workspacesRoot := filepath.Join(tmp, "workspaces")
	store := data.NewWorkspaceStore(filepath.Join(tmp, "workspaces-metadata"))
	workspaceService := newWorkspaceService(nil, store, nil, workspacesRoot)
	project := &data.Project{Name: "repo", Path: repo}

	origCreate := createWorkspaceFn
	t.Cleanup(func() { createWorkspaceFn = origCreate })
	createCalled := false
	createWorkspaceFn = func(repoPath, workspacePath, branch, base string) error {
		createCalled = true
		return os.ErrInvalid
	}

	msg := workspaceService.CreateWorkspace(project, "feature", "HEAD")()
	failed, ok := msg.(messages.WorkspaceCreateFailed)
	if !ok {
		t.Fatalf("expected WorkspaceCreateFailed, got %T", msg)
	}
	if failed.Workspace == nil {
		t.Fatalf("expected failed workspace payload")
	}
	expectedRoot := filepath.Join(workspaceService.primaryManagedProjectRoot(project), "feature")
	if normalizePath(failed.Workspace.Root) != normalizePath(expectedRoot) {
		t.Fatalf("workspace root = %s, want %s", failed.Workspace.Root, expectedRoot)
	}
	if !createCalled {
		t.Fatalf("expected createWorkspaceFn to be called")
	}
}

func TestDeleteWorkspaceAllowsLegacyProjectRoot(t *testing.T) {
	repo := t.TempDir()
	tmp := t.TempDir()
	workspacesRoot := filepath.Join(tmp, "workspaces")
	store := data.NewWorkspaceStore(filepath.Join(tmp, "workspaces-metadata"))
	workspaceService := newWorkspaceService(nil, store, nil, workspacesRoot)
	project := data.NewProject(repo)

	legacyRoot := filepath.Join(workspacesRoot, project.Name, "feature")
	workspace := data.NewWorkspace("feature", "feature", "main", repo, legacyRoot)

	origRemove := removeWorkspaceFn
	origDeleteBranch := deleteBranchFn
	t.Cleanup(func() {
		removeWorkspaceFn = origRemove
		deleteBranchFn = origDeleteBranch
	})

	removeCalled := false
	removeWorkspaceFn = func(repoPath, workspacePath string) error {
		removeCalled = true
		return nil
	}
	deleteBranchFn = func(repoPath, branch string) error { return nil }

	msg := workspaceService.DeleteWorkspace(project, workspace)()
	if _, ok := msg.(messages.WorkspaceDeleted); !ok {
		t.Fatalf("expected WorkspaceDeleted, got %T", msg)
	}
	if !removeCalled {
		t.Fatalf("expected legacy-root workspace to be deletable")
	}
}

func TestDeleteWorkspaceRejectsSameNameDifferentProjectScope(t *testing.T) {
	tmp := t.TempDir()
	workspacesRoot := filepath.Join(tmp, "workspaces")
	store := data.NewWorkspaceStore(filepath.Join(tmp, "workspaces-metadata"))
	workspaceService := newWorkspaceService(nil, store, nil, workspacesRoot)

	projectA := &data.Project{Path: "/repos/a/repo"}
	projectB := &data.Project{Name: "repo", Path: "/repos/b/repo"}
	workspace := data.NewWorkspace(
		"feature",
		"feature",
		"main",
		projectA.Path,
		filepath.Join(workspaceService.primaryManagedProjectRoot(projectA), "feature"),
	)

	origRemove := removeWorkspaceFn
	t.Cleanup(func() { removeWorkspaceFn = origRemove })
	removeCalled := false
	removeWorkspaceFn = func(repoPath, workspacePath string) error {
		removeCalled = true
		return nil
	}

	msg := workspaceService.DeleteWorkspace(projectB, workspace)()
	failed, ok := msg.(messages.WorkspaceDeleteFailed)
	if !ok {
		t.Fatalf("expected WorkspaceDeleteFailed, got %T", msg)
	}
	if failed.Err == nil {
		t.Fatalf("expected scoped-root validation error")
	}
	if removeCalled {
		t.Fatalf("expected removeWorkspaceFn not to run across same-name project scopes")
	}
}

func TestDeleteWorkspaceRejectsLegacyRootWhenRepoMismatchesProject(t *testing.T) {
	tmp := t.TempDir()
	workspacesRoot := filepath.Join(tmp, "workspaces")
	store := data.NewWorkspaceStore(filepath.Join(tmp, "workspaces-metadata"))
	workspaceService := newWorkspaceService(nil, store, nil, workspacesRoot)

	projectA := &data.Project{Path: "/repos/a/repo"}
	projectB := &data.Project{Name: "repo", Path: "/repos/b/repo"}
	workspace := data.NewWorkspace(
		"feature",
		"feature",
		"main",
		projectA.Path,
		filepath.Join(workspacesRoot, "repo", "feature"),
	)

	origRemove := removeWorkspaceFn
	t.Cleanup(func() { removeWorkspaceFn = origRemove })
	removeCalled := false
	removeWorkspaceFn = func(repoPath, workspacePath string) error {
		removeCalled = true
		return nil
	}

	msg := workspaceService.DeleteWorkspace(projectB, workspace)()
	failed, ok := msg.(messages.WorkspaceDeleteFailed)
	if !ok {
		t.Fatalf("expected WorkspaceDeleteFailed, got %T", msg)
	}
	if failed.Err == nil {
		t.Fatalf("expected repo mismatch validation error")
	}
	if removeCalled {
		t.Fatalf("expected removeWorkspaceFn not to run when workspace repo mismatches project")
	}
}

func TestDeleteWorkspaceAllowsLegacyShortHashProjectRoot(t *testing.T) {
	tmp := t.TempDir()
	workspacesRoot := filepath.Join(tmp, "workspaces")
	store := data.NewWorkspaceStore(filepath.Join(tmp, "workspaces-metadata"))
	workspaceService := newWorkspaceService(nil, store, nil, workspacesRoot)
	project := &data.Project{Name: "repo", Path: "/repos/a/repo"}

	legacyShortRoot := filepath.Join(
		workspacesRoot,
		project.Name+"-"+legacyShortProjectPathHash(project.Path),
		"feature",
	)
	workspace := data.NewWorkspace("feature", "feature", "main", project.Path, legacyShortRoot)

	origRemove := removeWorkspaceFn
	origDeleteBranch := deleteBranchFn
	t.Cleanup(func() {
		removeWorkspaceFn = origRemove
		deleteBranchFn = origDeleteBranch
	})
	removeCalled := false
	removeWorkspaceFn = func(repoPath, workspacePath string) error {
		removeCalled = true
		return nil
	}
	deleteBranchFn = func(repoPath, branch string) error { return nil }

	msg := workspaceService.DeleteWorkspace(project, workspace)()
	if _, ok := msg.(messages.WorkspaceDeleted); !ok {
		t.Fatalf("expected WorkspaceDeleted, got %T", msg)
	}
	if !removeCalled {
		t.Fatalf("expected legacy short-hash workspace to be deletable")
	}
}

func TestDeleteWorkspaceRejectsMissingWorkspaceRepo(t *testing.T) {
	tmp := t.TempDir()
	workspacesRoot := filepath.Join(tmp, "workspaces")
	store := data.NewWorkspaceStore(filepath.Join(tmp, "workspaces-metadata"))
	workspaceService := newWorkspaceService(nil, store, nil, workspacesRoot)
	project := &data.Project{Name: "repo", Path: "/repos/a/repo"}
	workspace := data.NewWorkspace(
		"feature",
		"feature",
		"main",
		"", // missing repo metadata
		filepath.Join(workspaceService.primaryManagedProjectRoot(project), "feature"),
	)

	origRemove := removeWorkspaceFn
	t.Cleanup(func() { removeWorkspaceFn = origRemove })
	removeCalled := false
	removeWorkspaceFn = func(repoPath, workspacePath string) error {
		removeCalled = true
		return nil
	}

	msg := workspaceService.DeleteWorkspace(project, workspace)()
	failed, ok := msg.(messages.WorkspaceDeleteFailed)
	if !ok {
		t.Fatalf("expected WorkspaceDeleteFailed, got %T", msg)
	}
	if failed.Err == nil {
		t.Fatalf("expected missing repo validation error")
	}
	if removeCalled {
		t.Fatalf("expected removeWorkspaceFn not to run when workspace repo metadata is missing")
	}
}

func TestDeleteWorkspaceRejectsMissingProjectPath(t *testing.T) {
	tmp := t.TempDir()
	workspacesRoot := filepath.Join(tmp, "workspaces")
	store := data.NewWorkspaceStore(filepath.Join(tmp, "workspaces-metadata"))
	workspaceService := newWorkspaceService(nil, store, nil, workspacesRoot)
	project := &data.Project{Name: "repo", Path: ""}
	workspace := data.NewWorkspace(
		"feature",
		"feature",
		"main",
		"/repos/a/repo",
		filepath.Join(workspacesRoot, "repo", "feature"),
	)

	origRemove := removeWorkspaceFn
	t.Cleanup(func() { removeWorkspaceFn = origRemove })
	removeCalled := false
	removeWorkspaceFn = func(repoPath, workspacePath string) error {
		removeCalled = true
		return nil
	}

	msg := workspaceService.DeleteWorkspace(project, workspace)()
	failed, ok := msg.(messages.WorkspaceDeleteFailed)
	if !ok {
		t.Fatalf("expected WorkspaceDeleteFailed, got %T", msg)
	}
	if failed.Err == nil {
		t.Fatalf("expected missing project path validation error")
	}
	if removeCalled {
		t.Fatalf("expected removeWorkspaceFn not to run when project path is missing")
	}
}

func TestWorkspaceServiceSaveRejectsNilWorkspace(t *testing.T) {
	store := data.NewWorkspaceStore(t.TempDir())
	service := newWorkspaceService(nil, store, nil, "")

	if err := service.Save(nil); err == nil {
		t.Fatalf("expected Save to reject nil workspace")
	}
}
