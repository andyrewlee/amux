package app

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"

	"github.com/andyrewlee/amux/internal/data"
	"github.com/andyrewlee/amux/internal/messages"
)

func TestLoadProjects_StoreFirstMerge(t *testing.T) {
	skipIfNoGit(t)

	repo := t.TempDir()
	runGit(t, repo, "init", "-b", "main")
	if err := os.WriteFile(filepath.Join(repo, "README.md"), []byte("ok\n"), 0o644); err != nil {
		t.Fatalf("write README: %v", err)
	}
	runGit(t, repo, "add", "README.md")
	runGit(t, repo, "commit", "-m", "init")

	tmp := t.TempDir()
	workspacesRoot := filepath.Join(tmp, "workspaces")
	worktreePath := filepath.Join(workspacesRoot, filepath.Base(repo), "feature")
	runGit(t, repo, "worktree", "add", "-b", "feature", worktreePath, "main")

	registry := data.NewRegistry(filepath.Join(tmp, "projects.json"))
	if err := registry.AddProject(repo); err != nil {
		t.Fatalf("AddProject: %v", err)
	}

	store := data.NewWorkspaceStore(filepath.Join(tmp, "workspaces-metadata"))
	createdAt := time.Date(2024, 6, 15, 14, 30, 0, 0, time.UTC)
	stored := &data.Workspace{
		Name:       filepath.Base(worktreePath),
		Branch:     "feature",
		Repo:       repo,
		Root:       worktreePath,
		Created:    createdAt,
		Assistant:  "codex",
		ScriptMode: "nonconcurrent",
		Env:        map[string]string{},
		Runtime:    data.RuntimeLocalWorktree,
	}
	if err := store.Save(stored); err != nil {
		t.Fatalf("Save stored workspace: %v", err)
	}

	workspaceService := newWorkspaceService(registry, store, nil, workspacesRoot)
	app := &App{
		workspaceService: workspaceService,
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

	var (
		found     bool
		matchAsst string
		matchTime time.Time
		count     int
	)
	expectedRoot := normalizePath(worktreePath)
	for i := range project.Workspaces {
		ws := &project.Workspaces[i]
		if normalizePath(ws.Root) == expectedRoot {
			count++
			found = true
			matchAsst = ws.Assistant
			matchTime = ws.Created
		}
	}
	if !found {
		t.Fatalf("expected workspace for %s", worktreePath)
	}
	if count != 1 {
		t.Fatalf("expected 1 workspace entry for %s, got %d", worktreePath, count)
	}
	if matchAsst != "codex" {
		t.Fatalf("assistant = %q, want %q", matchAsst, "codex")
	}
	if !matchTime.Equal(createdAt) {
		t.Fatalf("created = %v, want %v", matchTime, createdAt)
	}
}

func TestRescanWorkspaces_ImportsDiscoveredWorkspaces(t *testing.T) {
	skipIfNoGit(t)

	repo := t.TempDir()
	runGit(t, repo, "init", "-b", "main")
	if err := os.WriteFile(filepath.Join(repo, "README.md"), []byte("ok\n"), 0o644); err != nil {
		t.Fatalf("write README: %v", err)
	}
	runGit(t, repo, "add", "README.md")
	runGit(t, repo, "commit", "-m", "init")

	tmp := t.TempDir()
	workspacesRoot := filepath.Join(tmp, "workspaces")
	worktreePath := filepath.Join(workspacesRoot, filepath.Base(repo), "feature")
	runGit(t, repo, "worktree", "add", "-b", "feature", worktreePath, "main")

	registry := data.NewRegistry(filepath.Join(tmp, "projects.json"))
	if err := registry.AddProject(repo); err != nil {
		t.Fatalf("AddProject: %v", err)
	}

	store := data.NewWorkspaceStore(filepath.Join(tmp, "workspaces-metadata"))
	workspaceService := newWorkspaceService(registry, store, nil, workspacesRoot)
	app := &App{
		workspaceService: workspaceService,
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

	var found bool
	expectedRoot := normalizePath(worktreePath)
	for i := range project.Workspaces {
		ws := &project.Workspaces[i]
		if normalizePath(ws.Root) == expectedRoot {
			found = true
		}
	}
	if found {
		t.Fatalf("did not expect workspace for %s before rescan", worktreePath)
	}

	rescanMsg := app.rescanWorkspaces()()
	if _, ok := rescanMsg.(messages.RefreshDashboard); !ok {
		t.Fatalf("expected RefreshDashboard from rescan, got %T", rescanMsg)
	}

	msg = app.loadProjects()()
	loaded, ok = msg.(messages.ProjectsLoaded)
	if !ok {
		t.Fatalf("expected ProjectsLoaded, got %T", msg)
	}

	project = nil
	for i := range loaded.Projects {
		if loaded.Projects[i].Path == repo {
			project = &loaded.Projects[i]
			break
		}
	}
	if project == nil {
		t.Fatalf("expected project %s to be loaded after rescan", repo)
	}

	var count int
	for i := range project.Workspaces {
		ws := &project.Workspaces[i]
		if normalizePath(ws.Root) == expectedRoot {
			found = true
			count++
		}
	}
	if !found {
		t.Fatalf("expected workspace for %s after rescan", worktreePath)
	}
	if count != 1 {
		t.Fatalf("expected 1 workspace entry for %s, got %d", worktreePath, count)
	}

	ws := &data.Workspace{
		Name:   filepath.Base(worktreePath),
		Branch: "feature",
		Repo:   repo,
		Root:   worktreePath,
	}
	_, err := store.LoadMetadataFor(ws)
	if err != nil {
		t.Fatalf("LoadMetadataFor: %v", err)
	}
	if ws.Created.IsZero() {
		t.Fatalf("expected imported metadata to set Created")
	}
	if ws.Assistant == "" {
		t.Fatalf("expected imported metadata to set Assistant")
	}
}

func TestLoadProjects_PrimaryLegacyMetadataUsesDefaultAssistant(t *testing.T) {
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

	storeRoot := filepath.Join(tmp, "workspaces-metadata")
	store := data.NewWorkspaceStore(storeRoot)

	primaryID := (&data.Workspace{Repo: repo, Root: repo}).ID()
	legacyDir := filepath.Join(storeRoot, string(primaryID))
	if err := os.MkdirAll(legacyDir, 0o755); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}

	legacy := `{
  "name": "primary",
  "branch": "main",
  "assistant": ""
}`
	if err := os.WriteFile(filepath.Join(legacyDir, "workspace.json"), []byte(legacy), 0o644); err != nil {
		t.Fatalf("WriteFile legacy metadata: %v", err)
	}

	workspaceService := newWorkspaceService(registry, store, nil, "")
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

	expectedRoot := normalizePath(repo)
	var primary *data.Workspace
	for i := range project.Workspaces {
		ws := &project.Workspaces[i]
		if normalizePath(ws.Root) == expectedRoot {
			primary = ws
			break
		}
	}
	if primary == nil {
		t.Fatalf("expected primary workspace for %s", repo)
	}
	if primary.Assistant != "claude" {
		t.Fatalf("assistant = %q, want %q", primary.Assistant, "claude")
	}
}

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

	origTimeout := gitPathWaitTimeout
	t.Cleanup(func() {
		gitPathWaitTimeout = origTimeout
	})

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

	gitPathWaitTimeout = 50 * time.Millisecond

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

func normalizePath(path string) string {
	if resolved, err := filepath.EvalSymlinks(path); err == nil {
		return filepath.Clean(resolved)
	}
	return filepath.Clean(path)
}

func skipIfNoGit(t *testing.T) {
	t.Helper()
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not installed")
	}
}

func runGit(t *testing.T, dir string, args ...string) {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	cmd.Env = append(os.Environ(),
		"GIT_AUTHOR_NAME=Test",
		"GIT_AUTHOR_EMAIL=test@example.com",
		"GIT_COMMITTER_NAME=Test",
		"GIT_COMMITTER_EMAIL=test@example.com",
	)
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git %v failed: %v\n%s", args, err, string(out))
	}
}
