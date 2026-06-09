package app

import (
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"testing"

	tea "charm.land/bubbletea/v2"

	"github.com/andyrewlee/amux/internal/data"
	"github.com/andyrewlee/amux/internal/messages"
)

// TestDeleteWorkspace_ConcurrentSameRepoDeletesAllSucceed fires three real
// `git worktree remove` deletes for the same repository concurrently. Without
// the per-repo git lock they contend on .git locks (index.lock / packed-refs)
// and one intermittently fails as WorkspaceDeleteFailed; the lock serializes the
// mutations so all three succeed.
func TestDeleteWorkspace_ConcurrentSameRepoDeletesAllSucceed(t *testing.T) {
	skipIfNoGit(t)

	tmp := t.TempDir()
	repo := filepath.Join(tmp, "repo")
	if err := os.MkdirAll(repo, 0o755); err != nil {
		t.Fatalf("mkdir repo: %v", err)
	}
	runGit(t, repo, "init", "-b", "main")
	if err := os.WriteFile(filepath.Join(repo, "README.md"), []byte("ok\n"), 0o644); err != nil {
		t.Fatalf("write README: %v", err)
	}
	runGit(t, repo, "add", "README.md")
	runGit(t, repo, "commit", "-m", "init")

	workspacesRoot := filepath.Join(tmp, "workspaces")
	const n = 3
	workspaces := make([]*data.Workspace, n)
	for i := range workspaces {
		name := fmt.Sprintf("feature-%d", i)
		wsPath := filepath.Join(workspacesRoot, "repo", name)
		runGit(t, repo, "worktree", "add", "-b", name, wsPath, "main")
		workspaces[i] = data.NewWorkspace(name, name, "main", repo, wsPath)
	}

	svc := newWorkspaceService(nil, nil, nil, workspacesRoot)
	project := data.NewProject(repo)

	results := make([]tea.Msg, n)
	var wg sync.WaitGroup
	for i := range workspaces {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			results[i] = svc.DeleteWorkspace(project, workspaces[i])()
		}(i)
	}
	wg.Wait()

	for i, msg := range results {
		if failed, ok := msg.(messages.WorkspaceDeleteFailed); ok {
			t.Fatalf("workspace %d failed under concurrent same-repo delete: %v", i, failed.Err)
		}
		if _, ok := msg.(messages.WorkspaceDeleted); !ok {
			t.Fatalf("workspace %d: expected WorkspaceDeleted, got %T", i, msg)
		}
	}
}
