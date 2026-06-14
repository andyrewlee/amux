package app

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/andyrewlee/amux/internal/data"
)

// initTestRepo builds a minimal real git repository with a single commit on the
// given default branch and returns its path. The CLI-shelling git operations in
// workspace_service_init.go (defaultGitOps.CreateWorkspace /
// DiscoverWorkspaces) are exercised against repositories like this one, mirroring
// the convention used by service_git_test.go for methods that shell out.
func initTestRepo(t *testing.T, branch string) string {
	t.Helper()
	repo := t.TempDir()
	runGit(t, repo, "init", "-b", branch)
	if err := os.WriteFile(filepath.Join(repo, "README.md"), []byte("ok\n"), 0o644); err != nil {
		t.Fatalf("write README: %v", err)
	}
	runGit(t, repo, "add", "README.md")
	runGit(t, repo, "commit", "-m", "init")
	return repo
}

// TestDefaultGitOpsCreateWorkspace drives defaultGitOps.CreateWorkspace, the
// thin adapter newWorkspaceService wires in. It asserts the worktree is
// materialized, that a fresh branch is created from the requested base, and that
// the documented "reuse an existing branch" fallback succeeds instead of failing
// hard. Error paths assert the adapter surfaces git failures unchanged.
func TestDefaultGitOpsCreateWorkspace(t *testing.T) {
	skipIfNoGit(t)

	t.Run("creates worktree on a new branch from base", func(t *testing.T) {
		repo := initTestRepo(t, "main")
		ws := filepath.Join(t.TempDir(), "feature")

		if err := (defaultGitOps{}).CreateWorkspace(repo, ws, "feature", "main"); err != nil {
			t.Fatalf("CreateWorkspace: %v", err)
		}

		// The worktree directory must exist and carry the committed file.
		if _, err := os.Stat(filepath.Join(ws, "README.md")); err != nil {
			t.Fatalf("expected worktree to contain README.md: %v", err)
		}
		// The new branch must be discoverable on the parent repo.
		discovered, err := (defaultGitOps{}).DiscoverWorkspaces(&data.Project{Path: repo})
		if err != nil {
			t.Fatalf("DiscoverWorkspaces: %v", err)
		}
		if !containsBranch(discovered, "feature") {
			t.Fatalf("expected discovered worktrees to include branch %q, got %+v", "feature", discovered)
		}
	})

	t.Run("reuses an existing branch instead of failing", func(t *testing.T) {
		repo := initTestRepo(t, "main")
		// Pre-create the branch so the new-branch add fails and the adapter must
		// fall back to attaching the existing branch.
		runGit(t, repo, "branch", "existing", "main")
		ws := filepath.Join(t.TempDir(), "reuse")

		if err := (defaultGitOps{}).CreateWorkspace(repo, ws, "existing", "main"); err != nil {
			t.Fatalf("CreateWorkspace with existing branch should reuse it, got %v", err)
		}
		if _, err := os.Stat(filepath.Join(ws, "README.md")); err != nil {
			t.Fatalf("expected reused-branch worktree to be checked out: %v", err)
		}
	})

	t.Run("returns an error for an unknown base", func(t *testing.T) {
		repo := initTestRepo(t, "main")
		ws := filepath.Join(t.TempDir(), "bad-base")

		if err := (defaultGitOps{}).CreateWorkspace(repo, ws, "topic", "no-such-base"); err == nil {
			t.Fatal("CreateWorkspace from a nonexistent base should error")
		}
		if _, err := os.Stat(ws); !os.IsNotExist(err) {
			t.Fatalf("failed CreateWorkspace should not leave a worktree, stat err = %v", err)
		}
	})

	t.Run("returns an error for a non-repository path", func(t *testing.T) {
		notRepo := t.TempDir()
		ws := filepath.Join(t.TempDir(), "orphan")

		if err := (defaultGitOps{}).CreateWorkspace(notRepo, ws, "topic", "main"); err == nil {
			t.Fatal("CreateWorkspace against a non-git directory should error")
		}
	})
}

// TestDefaultGitOpsDiscoverWorkspaces drives defaultGitOps.DiscoverWorkspaces and
// asserts the minimal-field contract it promises (Name/Branch/Repo/Root), that
// the primary worktree and every added worktree are reported, and that the error
// path surfaces when the project path is not a git repository.
func TestDefaultGitOpsDiscoverWorkspaces(t *testing.T) {
	skipIfNoGit(t)

	t.Run("reports the primary worktree only for a fresh repo", func(t *testing.T) {
		repo := initTestRepo(t, "main")

		got, err := (defaultGitOps{}).DiscoverWorkspaces(&data.Project{Path: repo})
		if err != nil {
			t.Fatalf("DiscoverWorkspaces: %v", err)
		}
		if len(got) != 1 {
			t.Fatalf("expected exactly the primary worktree, got %d: %+v", len(got), got)
		}
		ws := got[0]
		if ws.Branch != "main" {
			t.Fatalf("primary worktree branch = %q, want %q", ws.Branch, "main")
		}
		if ws.Repo != repo {
			t.Fatalf("worktree Repo = %q, want project path %q", ws.Repo, repo)
		}
		if ws.Name != filepath.Base(ws.Root) {
			t.Fatalf("worktree Name = %q, want filepath.Base(Root) %q", ws.Name, filepath.Base(ws.Root))
		}
		if ws.Root == "" {
			t.Fatal("worktree Root must be populated")
		}
	})

	t.Run("reports primary and added worktrees", func(t *testing.T) {
		repo := initTestRepo(t, "main")
		added := filepath.Join(t.TempDir(), "added")
		runGit(t, repo, "worktree", "add", "-b", "topic", added, "main")

		got, err := (defaultGitOps{}).DiscoverWorkspaces(&data.Project{Path: repo})
		if err != nil {
			t.Fatalf("DiscoverWorkspaces: %v", err)
		}
		if len(got) != 2 {
			t.Fatalf("expected primary + added worktree, got %d: %+v", len(got), got)
		}
		if !containsBranch(got, "main") {
			t.Fatalf("expected discovered worktrees to include branch %q, got %+v", "main", got)
		}
		if !containsBranch(got, "topic") {
			t.Fatalf("expected discovered worktrees to include branch %q, got %+v", "topic", got)
		}
	})

	t.Run("returns an error for a non-repository path", func(t *testing.T) {
		notRepo := t.TempDir()

		got, err := (defaultGitOps{}).DiscoverWorkspaces(&data.Project{Path: notRepo})
		if err == nil {
			t.Fatal("DiscoverWorkspaces against a non-git directory should error")
		}
		if got != nil {
			t.Fatalf("error path must return nil workspaces, got %+v", got)
		}
	})
}

func containsBranch(workspaces []data.Workspace, branch string) bool {
	for _, ws := range workspaces {
		if ws.Branch == branch {
			return true
		}
	}
	return false
}

// fakeAssistantStore is a minimal WorkspaceStore that only answers
// ResolvedDefaultAssistant; every other method is unused by these tests and
// panics if reached, keeping the fixture honest about what it exercises.
type fakeAssistantStore struct {
	assistant string
}

func (f *fakeAssistantStore) ResolvedDefaultAssistant() string { return f.assistant }

func (f *fakeAssistantStore) ListByRepo(string) ([]*data.Workspace, error) {
	panic("unexpected ListByRepo")
}

func (f *fakeAssistantStore) ListByRepoIncludingArchived(string) ([]*data.Workspace, error) {
	panic("unexpected ListByRepoIncludingArchived")
}

func (f *fakeAssistantStore) LoadMetadataFor(*data.Workspace) (bool, error) {
	panic("unexpected LoadMetadataFor")
}

func (f *fakeAssistantStore) UpsertFromDiscovery(*data.Workspace) error {
	panic("unexpected UpsertFromDiscovery")
}

func (f *fakeAssistantStore) Save(*data.Workspace) error { panic("unexpected Save") }

func (f *fakeAssistantStore) Delete(data.WorkspaceID) error { panic("unexpected Delete") }

// TestWorkspaceServiceResolvedDefaultAssistant covers every branch of the
// nil-safe resolver: a nil receiver and a nil store both fall back to the package
// default, while a wired store is consulted verbatim.
func TestWorkspaceServiceResolvedDefaultAssistant(t *testing.T) {
	t.Run("nil receiver falls back to default", func(t *testing.T) {
		var svc *workspaceService
		if got := svc.resolvedDefaultAssistant(); got != data.DefaultAssistant {
			t.Fatalf("nil receiver resolvedDefaultAssistant = %q, want %q", got, data.DefaultAssistant)
		}
	})

	t.Run("nil store falls back to default", func(t *testing.T) {
		svc := &workspaceService{}
		if got := svc.resolvedDefaultAssistant(); got != data.DefaultAssistant {
			t.Fatalf("nil store resolvedDefaultAssistant = %q, want %q", got, data.DefaultAssistant)
		}
	})

	t.Run("delegates to the wired store", func(t *testing.T) {
		svc := &workspaceService{store: &fakeAssistantStore{assistant: "codex"}}
		if got := svc.resolvedDefaultAssistant(); got != "codex" {
			t.Fatalf("resolvedDefaultAssistant = %q, want %q", got, "codex")
		}
	})
}

// TestWorkspaceServiceRunUnlessDeleteInFlightGuards covers the two branches the
// existing inflight test leaves untouched: a nil receiver (which must report not
// run) and a nil callback on an unguarded service (which must still report run).
func TestWorkspaceServiceRunUnlessDeleteInFlightGuards(t *testing.T) {
	var nilService *workspaceService
	if nilService.runUnlessDeleteInFlight("ws", func() {}) {
		t.Fatal("nil service should report the callback as not run")
	}

	svc := &workspaceService{}
	if !svc.runUnlessDeleteInFlight("ws", nil) {
		t.Fatal("unguarded service with a nil callback should still report run")
	}
}

// TestNewWorkspaceServiceWiring asserts the constructor installs the real git
// adapter (a non-nil GitOperations, so production code never dereferences nil)
// and the default git-path wait budget the create flow relies on.
func TestNewWorkspaceServiceWiring(t *testing.T) {
	store := &fakeAssistantStore{assistant: "claude"}
	svc := newWorkspaceService(nil, store, nil, "/tmp/workspaces")

	if svc == nil {
		t.Fatal("newWorkspaceService returned nil")
	}
	if svc.gitOps == nil {
		t.Fatal("newWorkspaceService must wire a non-nil GitOperations adapter")
	}
	if _, ok := svc.gitOps.(defaultGitOps); !ok {
		t.Fatalf("newWorkspaceService gitOps = %T, want defaultGitOps", svc.gitOps)
	}
	if svc.gitPathWaitTimeout != 3*time.Second {
		t.Fatalf("gitPathWaitTimeout = %v, want %v", svc.gitPathWaitTimeout, 3*time.Second)
	}
	if svc.store != store {
		t.Fatalf("newWorkspaceService stored store %p, want %p", svc.store, store)
	}
	if svc.workspacesRoot != "/tmp/workspaces" {
		t.Fatalf("workspacesRoot = %q, want %q", svc.workspacesRoot, "/tmp/workspaces")
	}
}
