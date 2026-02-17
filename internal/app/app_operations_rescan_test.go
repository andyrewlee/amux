package app

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/andyrewlee/amux/internal/data"
	"github.com/andyrewlee/amux/internal/messages"
)

func TestRescanWorkspaces_ArchivesMissingWorkspaces(t *testing.T) {
	skipIfNoGit(t)

	repo := t.TempDir()
	runGit(t, repo, "init", "-b", "main")
	if err := os.WriteFile(filepath.Join(repo, "README.md"), []byte("ok\n"), 0o644); err != nil {
		t.Fatalf("write README: %v", err)
	}
	runGit(t, repo, "add", "README.md")
	runGit(t, repo, "commit", "-m", "init")

	tmp := t.TempDir()
	registry := data.NewRegistry(filepath.Join(tmp, "projects.json"))
	if err := registry.AddProject(repo); err != nil {
		t.Fatalf("AddProject: %v", err)
	}

	store := data.NewWorkspaceStore(filepath.Join(tmp, "workspaces-metadata"))
	workspacesRoot := filepath.Join(tmp, "workspaces")
	ghost := &data.Workspace{
		Name: "ghost",
		Repo: repo,
		Root: filepath.Join(workspacesRoot, filepath.Base(repo), "ghost"),
	}
	if err := store.Save(ghost); err != nil {
		t.Fatalf("Save ghost workspace: %v", err)
	}

	workspaceService := newWorkspaceService(registry, store, nil, workspacesRoot)
	app := &App{
		workspaceService: workspaceService,
	}

	rescanMsg := app.rescanWorkspaces()()
	if _, ok := rescanMsg.(messages.RefreshDashboard); !ok {
		t.Fatalf("expected RefreshDashboard from rescan, got %T", rescanMsg)
	}

	loaded, err := store.Load(ghost.ID())
	if err != nil {
		t.Fatalf("Load ghost workspace: %v", err)
	}
	if !loaded.Archived {
		t.Fatalf("expected ghost workspace to be archived after rescan")
	}
	if loaded.ArchivedAt.IsZero() {
		t.Fatalf("expected archived workspace to set ArchivedAt")
	}
}

func TestRescanWorkspaces_IgnoresExternalWorktrees(t *testing.T) {
	skipIfNoGit(t)

	repo := t.TempDir()
	runGit(t, repo, "init", "-b", "main")
	if err := os.WriteFile(filepath.Join(repo, "README.md"), []byte("ok\n"), 0o644); err != nil {
		t.Fatalf("write README: %v", err)
	}
	runGit(t, repo, "add", "README.md")
	runGit(t, repo, "commit", "-m", "init")

	externalRoot := filepath.Join(normalizePath(t.TempDir()), "external-feature")
	runGit(t, repo, "worktree", "add", "-b", "feature", externalRoot, "main")

	tmp := t.TempDir()
	workspacesRoot := filepath.Join(tmp, "workspaces")
	registry := data.NewRegistry(filepath.Join(tmp, "projects.json"))
	if err := registry.AddProject(repo); err != nil {
		t.Fatalf("AddProject: %v", err)
	}
	store := data.NewWorkspaceStore(filepath.Join(tmp, "workspaces-metadata"))

	workspaceService := newWorkspaceService(registry, store, nil, workspacesRoot)
	app := &App{workspaceService: workspaceService}

	rescanMsg := app.rescanWorkspaces()()
	if _, ok := rescanMsg.(messages.RefreshDashboard); !ok {
		t.Fatalf("expected RefreshDashboard from rescan, got %T", rescanMsg)
	}

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

	external := normalizePath(externalRoot)
	for i := range project.Workspaces {
		ws := &project.Workspaces[i]
		if normalizePath(ws.Root) == external {
			t.Fatalf("did not expect external worktree %s in project workspaces", externalRoot)
		}
	}

	discovered := &data.Workspace{
		Name: filepath.Base(externalRoot),
		Repo: repo,
		Root: externalRoot,
	}
	found, err := store.LoadMetadataFor(discovered)
	if err != nil {
		t.Fatalf("LoadMetadataFor(external) error = %v", err)
	}
	if found {
		t.Fatalf("did not expect external worktree metadata to be imported")
	}
}

func TestCreateWorkspaceMissingGitDoesNotPersist(t *testing.T) {
	repo := t.TempDir()
	tmp := t.TempDir()

	workspacesRoot := filepath.Join(tmp, "workspaces")
	metadataRoot := filepath.Join(tmp, "workspaces-metadata")

	store := data.NewWorkspaceStore(metadataRoot)
	workspaceService := newWorkspaceService(nil, store, nil, workspacesRoot)
	app := &App{
		workspaceService: workspaceService,
	}

	project := data.NewProject(repo)

	var removeCalled bool
	var deleteCalled bool
	workspaceService.gitOps = &mockGitOps{
		createWorkspace: func(repoPath, workspacePath, branch, base string) error {
			return os.MkdirAll(workspacePath, 0o755)
		},
		removeWorkspace: func(repoPath, workspacePath string) error {
			removeCalled = true
			return nil
		},
		deleteBranch: func(repoPath, branch string) error {
			deleteCalled = true
			return nil
		},
	}

	workspaceService.gitPathWaitTimeout = 50 * time.Millisecond

	msg := app.createWorkspace(project, "feature", "main", "claude")()
	failed, ok := msg.(messages.WorkspaceCreateFailed)
	if !ok {
		t.Fatalf("expected WorkspaceCreateFailed, got %T", msg)
	}
	if failed.Err == nil {
		t.Fatalf("expected WorkspaceCreateFailed to include error")
	}
	if !removeCalled {
		t.Fatalf("expected workspace rollback to remove worktree")
	}
	if !deleteCalled {
		t.Fatalf("expected workspace rollback to delete branch")
	}

	ids, err := store.List()
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(ids) != 0 {
		t.Fatalf("expected no persisted workspaces, got %d", len(ids))
	}
}
