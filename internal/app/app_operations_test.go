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
	if err := os.WriteFile(filepath.Join(repo, "README.md"), []byte("ok\n"), 0644); err != nil {
		t.Fatalf("write README: %v", err)
	}
	runGit(t, repo, "add", "README.md")
	runGit(t, repo, "commit", "-m", "init")

	worktreeDir := normalizePath(t.TempDir())
	worktreePath := filepath.Join(worktreeDir, "feature")
	runGit(t, repo, "worktree", "add", "-b", "feature", worktreePath, "main")

	tmp := t.TempDir()
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

	app := &App{
		registry:   registry,
		workspaces: store,
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

func TestLoadProjects_BackfillDiscoveredWorkspace(t *testing.T) {
	skipIfNoGit(t)

	repo := t.TempDir()
	runGit(t, repo, "init", "-b", "main")
	if err := os.WriteFile(filepath.Join(repo, "README.md"), []byte("ok\n"), 0644); err != nil {
		t.Fatalf("write README: %v", err)
	}
	runGit(t, repo, "add", "README.md")
	runGit(t, repo, "commit", "-m", "init")

	worktreeDir := normalizePath(t.TempDir())
	worktreePath := filepath.Join(worktreeDir, "feature")
	runGit(t, repo, "worktree", "add", "-b", "feature", worktreePath, "main")

	tmp := t.TempDir()
	registry := data.NewRegistry(filepath.Join(tmp, "projects.json"))
	if err := registry.AddProject(repo); err != nil {
		t.Fatalf("AddProject: %v", err)
	}

	store := data.NewWorkspaceStore(filepath.Join(tmp, "workspaces-metadata"))
	app := &App{
		registry:   registry,
		workspaces: store,
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
		found bool
		count int
	)
	expectedRoot := normalizePath(worktreePath)
	for i := range project.Workspaces {
		ws := &project.Workspaces[i]
		if normalizePath(ws.Root) == expectedRoot {
			found = true
			count++
		}
	}
	if !found {
		t.Fatalf("expected workspace for %s", worktreePath)
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
		t.Fatalf("expected backfilled metadata to set Created")
	}
	if ws.Assistant == "" {
		t.Fatalf("expected backfilled metadata to set Assistant")
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
