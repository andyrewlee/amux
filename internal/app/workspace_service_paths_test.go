package app

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"

	"github.com/andyrewlee/amux/internal/data"
	"github.com/andyrewlee/amux/internal/messages"
)

func TestLoadProjects_SymlinkedWorkspacesRootKeepsMissingManagedWorkspace(t *testing.T) {
	skipIfNoGit(t)
	if runtime.GOOS == "windows" {
		t.Skip("symlink creation can require elevated privileges on windows")
	}

	repo := t.TempDir()
	runGit(t, repo, "init", "-b", "main")
	if err := os.WriteFile(filepath.Join(repo, "README.md"), []byte("ok\n"), 0o644); err != nil {
		t.Fatalf("write README: %v", err)
	}
	runGit(t, repo, "add", "README.md")
	runGit(t, repo, "commit", "-m", "init")

	tmp := t.TempDir()
	realWorkspacesRoot := filepath.Join(tmp, "real-workspaces")
	if err := os.MkdirAll(realWorkspacesRoot, 0o755); err != nil {
		t.Fatalf("MkdirAll(realWorkspacesRoot): %v", err)
	}
	symlinkedWorkspacesRoot := filepath.Join(tmp, "workspaces-link")
	if err := os.Symlink(realWorkspacesRoot, symlinkedWorkspacesRoot); err != nil {
		t.Fatalf("Symlink(workspaces root) error = %v", err)
	}

	registry := data.NewRegistry(filepath.Join(tmp, "projects.json"))
	if err := registry.AddProject(repo); err != nil {
		t.Fatalf("AddProject: %v", err)
	}
	store := data.NewWorkspaceStore(filepath.Join(tmp, "workspaces-metadata"))

	managedMissingRoot := filepath.Join(symlinkedWorkspacesRoot, filepath.Base(repo), "missing-feature")
	stored := &data.Workspace{
		Name:   "missing-feature",
		Branch: "missing-feature",
		Repo:   repo,
		Root:   managedMissingRoot,
	}
	if err := store.Save(stored); err != nil {
		t.Fatalf("Save managed workspace: %v", err)
	}

	workspaceService := newWorkspaceService(registry, store, nil, symlinkedWorkspacesRoot)
	app := &App{workspaceService: workspaceService}

	msg := app.loadProjects()()
	loaded, ok := msg.(messages.ProjectsLoaded)
	if !ok {
		t.Fatalf("expected ProjectsLoaded, got %T", msg)
	}

	var project *data.Project
	for i := range loaded.Projects {
		if loaded.Projects[i].Path == repo {
			project = &loaded.Projects[i]
			break
		}
	}
	if project == nil {
		t.Fatalf("expected project %s to be loaded", repo)
	}

	var found bool
	expectedRoot := normalizePath(managedMissingRoot)
	for i := range project.Workspaces {
		ws := &project.Workspaces[i]
		if normalizePath(ws.Root) == expectedRoot {
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("expected managed workspace %s to be surfaced", managedMissingRoot)
	}
}

func TestLoadProjects_SymlinkedWorkspacesRootKeepsMissingResolvedManagedWorkspace(t *testing.T) {
	skipIfNoGit(t)
	if runtime.GOOS == "windows" {
		t.Skip("symlink creation can require elevated privileges on windows")
	}

	repo := t.TempDir()
	runGit(t, repo, "init", "-b", "main")
	if err := os.WriteFile(filepath.Join(repo, "README.md"), []byte("ok\n"), 0o644); err != nil {
		t.Fatalf("write README: %v", err)
	}
	runGit(t, repo, "add", "README.md")
	runGit(t, repo, "commit", "-m", "init")

	tmp := t.TempDir()
	realWorkspacesRoot := filepath.Join(tmp, "real-workspaces")
	if err := os.MkdirAll(realWorkspacesRoot, 0o755); err != nil {
		t.Fatalf("MkdirAll(realWorkspacesRoot): %v", err)
	}
	symlinkedWorkspacesRoot := filepath.Join(tmp, "workspaces-link")
	if err := os.Symlink(realWorkspacesRoot, symlinkedWorkspacesRoot); err != nil {
		t.Fatalf("Symlink(workspaces root) error = %v", err)
	}

	registry := data.NewRegistry(filepath.Join(tmp, "projects.json"))
	if err := registry.AddProject(repo); err != nil {
		t.Fatalf("AddProject: %v", err)
	}
	store := data.NewWorkspaceStore(filepath.Join(tmp, "workspaces-metadata"))

	// Store the path in resolved (real) form while configured workspacesRoot
	// uses a symlink form; missing directories should still be treated managed.
	managedMissingRootResolved := filepath.Join(realWorkspacesRoot, filepath.Base(repo), "missing-feature")
	stored := &data.Workspace{
		Name:   "missing-feature",
		Branch: "missing-feature",
		Repo:   repo,
		Root:   managedMissingRootResolved,
	}
	if err := store.Save(stored); err != nil {
		t.Fatalf("Save managed workspace: %v", err)
	}

	workspaceService := newWorkspaceService(registry, store, nil, symlinkedWorkspacesRoot)
	app := &App{workspaceService: workspaceService}

	msg := app.loadProjects()()
	loaded, ok := msg.(messages.ProjectsLoaded)
	if !ok {
		t.Fatalf("expected ProjectsLoaded, got %T", msg)
	}

	var project *data.Project
	for i := range loaded.Projects {
		if loaded.Projects[i].Path == repo {
			project = &loaded.Projects[i]
			break
		}
	}
	if project == nil {
		t.Fatalf("expected project %s to be loaded", repo)
	}

	var found bool
	expectedRoot := normalizePath(managedMissingRootResolved)
	for i := range project.Workspaces {
		ws := &project.Workspaces[i]
		if normalizePath(ws.Root) == expectedRoot {
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("expected managed workspace %s to be surfaced", managedMissingRootResolved)
	}
}

func TestLoadProjects_BrokenSymlinkedWorkspacesRootKeepsMissingResolvedManagedWorkspace(t *testing.T) {
	skipIfNoGit(t)
	if runtime.GOOS == "windows" {
		t.Skip("symlink creation can require elevated privileges on windows")
	}

	repo := t.TempDir()
	runGit(t, repo, "init", "-b", "main")
	if err := os.WriteFile(filepath.Join(repo, "README.md"), []byte("ok\n"), 0o644); err != nil {
		t.Fatalf("write README: %v", err)
	}
	runGit(t, repo, "add", "README.md")
	runGit(t, repo, "commit", "-m", "init")

	tmp := t.TempDir()
	brokenTargetRoot := filepath.Join(tmp, "missing-mount", "real-workspaces")
	symlinkedWorkspacesRoot := filepath.Join(tmp, "workspaces-link")
	if err := os.Symlink(brokenTargetRoot, symlinkedWorkspacesRoot); err != nil {
		t.Fatalf("Symlink(workspaces root) error = %v", err)
	}

	registry := data.NewRegistry(filepath.Join(tmp, "projects.json"))
	if err := registry.AddProject(repo); err != nil {
		t.Fatalf("AddProject: %v", err)
	}
	store := data.NewWorkspaceStore(filepath.Join(tmp, "workspaces-metadata"))

	managedMissingRootResolved := filepath.Join(brokenTargetRoot, filepath.Base(repo), "missing-feature")
	stored := &data.Workspace{
		Name:   "missing-feature",
		Branch: "missing-feature",
		Repo:   repo,
		Root:   managedMissingRootResolved,
	}
	if err := store.Save(stored); err != nil {
		t.Fatalf("Save managed workspace: %v", err)
	}

	workspaceService := newWorkspaceService(registry, store, nil, symlinkedWorkspacesRoot)
	app := &App{workspaceService: workspaceService}

	msg := app.loadProjects()()
	loaded, ok := msg.(messages.ProjectsLoaded)
	if !ok {
		t.Fatalf("expected ProjectsLoaded, got %T", msg)
	}

	var project *data.Project
	for i := range loaded.Projects {
		if loaded.Projects[i].Path == repo {
			project = &loaded.Projects[i]
			break
		}
	}
	if project == nil {
		t.Fatalf("expected project %s to be loaded", repo)
	}

	var found bool
	expectedRoot := normalizePath(managedMissingRootResolved)
	for i := range project.Workspaces {
		ws := &project.Workspaces[i]
		if normalizePath(ws.Root) == expectedRoot {
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("expected managed workspace %s to be surfaced", managedMissingRootResolved)
	}
}

func TestWorkspaceVisibilityAcrossRepoAliasBasenameChange(t *testing.T) {
	skipIfNoGit(t)
	if runtime.GOOS == "windows" {
		t.Skip("symlink creation can require elevated privileges on windows")
	}

	base := t.TempDir()
	repoReal := filepath.Join(base, "real-repo-name")
	if err := os.MkdirAll(repoReal, 0o755); err != nil {
		t.Fatalf("MkdirAll(repoReal): %v", err)
	}
	runGit(t, repoReal, "init", "-b", "main")
	if err := os.WriteFile(filepath.Join(repoReal, "README.md"), []byte("ok\n"), 0o644); err != nil {
		t.Fatalf("write README: %v", err)
	}
	runGit(t, repoReal, "add", "README.md")
	runGit(t, repoReal, "commit", "-m", "init")

	repoAlias := filepath.Join(base, "alias-repo-name")
	if err := os.Symlink(repoReal, repoAlias); err != nil {
		t.Fatalf("Symlink(repo alias) error = %v", err)
	}

	workspacesRoot := filepath.Join(base, "workspaces")
	managedRoot := filepath.Join(workspacesRoot, filepath.Base(repoReal), "feature")
	runGit(t, repoReal, "worktree", "add", "-b", "feature", managedRoot, "main")

	registry := data.NewRegistry(filepath.Join(base, "projects.json"))
	if err := registry.AddProject(repoAlias); err != nil {
		t.Fatalf("AddProject(alias): %v", err)
	}
	store := data.NewWorkspaceStore(filepath.Join(base, "workspaces-metadata"))

	stored := &data.Workspace{
		Name:   "feature",
		Branch: "feature",
		Repo:   repoReal,
		Root:   managedRoot,
	}
	if err := store.Save(stored); err != nil {
		t.Fatalf("Save managed workspace: %v", err)
	}

	workspaceService := newWorkspaceService(registry, store, nil, workspacesRoot)
	app := &App{workspaceService: workspaceService}

	msg := app.loadProjects()()
	loaded, ok := msg.(messages.ProjectsLoaded)
	if !ok {
		t.Fatalf("expected ProjectsLoaded, got %T", msg)
	}

	var project *data.Project
	for i := range loaded.Projects {
		if loaded.Projects[i].Path == repoAlias {
			project = &loaded.Projects[i]
			break
		}
	}
	if project == nil {
		t.Fatalf("expected project %s to be loaded", repoAlias)
	}
	foundManaged := false
	expectedRoot := normalizePath(managedRoot)
	for i := range project.Workspaces {
		if normalizePath(project.Workspaces[i].Root) == expectedRoot {
			foundManaged = true
			break
		}
	}
	if !foundManaged {
		t.Fatalf("expected managed workspace %s to be visible before rescan", managedRoot)
	}

	rescanMsg := app.rescanWorkspaces()()
	if _, ok := rescanMsg.(messages.RefreshDashboard); !ok {
		t.Fatalf("expected RefreshDashboard from rescan, got %T", rescanMsg)
	}

	after, err := store.Load(stored.ID())
	if err != nil {
		t.Fatalf("Load managed workspace after rescan: %v", err)
	}
	if after.Archived {
		t.Fatalf("expected managed workspace to remain unarchived after rescan")
	}
}
