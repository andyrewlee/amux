package app

import (
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/andyrewlee/amux/internal/data"
	"github.com/andyrewlee/amux/internal/git"
)

func TestRollbackWorkspaceCreationCleansRecoverableUnregisteredWorkspace(t *testing.T) {
	tmp := t.TempDir()
	workspacesRoot := filepath.Join(tmp, "workspaces")
	projectPath := filepath.Join(tmp, "repo-real")
	workspacePath := filepath.Join(workspacesRoot, "repo-real", "feature")
	if err := os.MkdirAll(filepath.Join(workspacePath, "nested"), 0o755); err != nil {
		t.Fatalf("MkdirAll(workspacePath) error = %v", err)
	}

	var deleteBranchCalls int
	mock := &mockGitOps{
		removeWorkspace: func(repoPath, gotWorkspacePath string) error {
			if repoPath != projectPath {
				t.Fatalf("repoPath = %q, want %q", repoPath, projectPath)
			}
			if gotWorkspacePath != workspacePath {
				t.Fatalf("workspacePath = %q, want %q", gotWorkspacePath, workspacePath)
			}
			return git.ErrUnregisteredWorkspacePath
		},
		deleteBranch: func(repoPath, branch string) error {
			deleteBranchCalls++
			if repoPath != projectPath {
				t.Fatalf("deleteBranch repoPath = %q, want %q", repoPath, projectPath)
			}
			if branch != "feature" {
				t.Fatalf("branch = %q, want %q", branch, "feature")
			}
			return nil
		},
	}

	project := &data.Project{Name: "repo-link", Path: projectPath}
	svc := newWorkspaceService(nil, nil, nil, workspacesRoot)
	svc.gitOps = mock
	svc.rollbackWorkspaceCreation(project, projectPath, workspacePath, "feature")

	if _, err := os.Stat(workspacePath); !os.IsNotExist(err) {
		t.Fatalf("expected stale workspace path to be removed, err=%v", err)
	}
	if deleteBranchCalls != 1 {
		t.Fatalf("deleteBranch calls = %d, want 1", deleteBranchCalls)
	}
}

// TestRollbackWorkspaceCreationSerializesAgainstSameRepoGitMutation asserts that
// a rollback does not run its RemoveWorkspace/DeleteBranch git mutations while a
// concurrent locked git op for the same repo is still in flight. Both must hold
// lockRepoGit(repoPath), so the fake should never observe overlapping calls.
func TestRollbackWorkspaceCreationSerializesAgainstSameRepoGitMutation(t *testing.T) {
	const repoPath = "/tmp/repo-serialize"
	workspacesRoot := "/tmp/workspaces"
	workspacePath := filepath.Join(workspacesRoot, "repo-serialize", "feature")

	var (
		mu             sync.Mutex
		active         bool // a fake git op is currently executing
		overlap        bool // two fake git ops were active at once
		order          []string
		createBlocking = make(chan struct{}) // closed to release the in-flight create
		createEntered  = make(chan struct{}) // closed once the create holds the lock
		removeEntered  = make(chan struct{}) // closed once the rollback's RemoveWorkspace runs
	)

	enter := func(name string) {
		mu.Lock()
		if active {
			overlap = true
		}
		active = true
		order = append(order, name)
		mu.Unlock()
	}
	leave := func() {
		mu.Lock()
		active = false
		mu.Unlock()
	}

	mock := &mockGitOps{
		createWorkspace: func(_, _, _, _ string) error {
			enter("create")
			defer leave()
			close(createEntered)
			<-createBlocking // hold the repo lock until released
			return nil
		},
		removeWorkspace: func(_, _ string) error {
			close(removeEntered) // signal entry so the test synchronizes on the fake, not a sleep
			enter("rollback-remove")
			defer leave()
			return nil
		},
		deleteBranch: func(_, _ string) error {
			enter("rollback-branch")
			defer leave()
			return nil
		},
	}

	svc := newWorkspaceService(nil, nil, nil, workspacesRoot)
	svc.gitOps = mock
	project := &data.Project{Name: "repo-serialize", Path: repoPath}

	createDone := make(chan struct{})
	go func() {
		defer close(createDone)
		// Holds lockRepoGit(repoPath) for the whole call (blocks until released).
		_ = svc.createWorkspaceLocked(repoPath, workspacePath, "feature", "main")
	}()

	// Wait until the create is confirmed holding the lock.
	<-createEntered

	rollbackDone := make(chan struct{})
	go func() {
		defer close(rollbackDone)
		svc.rollbackWorkspaceCreation(project, repoPath, workspacePath, "feature")
	}()

	// The rollback must block on lockRepoGit(repoPath), which the create provably
	// holds (we waited for createEntered). RemoveWorkspace therefore cannot have
	// run: this non-blocking check is anchored to the held lock, not to wall-clock
	// time, so it is deterministic rather than a race against a fixed sleep.
	select {
	case <-removeEntered:
		close(createBlocking)
		t.Fatal("rollback RemoveWorkspace ran while the create still held the repo lock")
	default:
	}

	// Release the create; the rollback should now proceed.
	close(createBlocking)

	// Confirm RemoveWorkspace ran only after the lock was free (positive sync on
	// the fake's entry, with a failsafe so a regression fails fast instead of
	// hanging).
	select {
	case <-removeEntered:
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for rollback RemoveWorkspace after releasing the create lock")
	}

	<-createDone
	<-rollbackDone

	mu.Lock()
	defer mu.Unlock()
	if overlap {
		t.Fatalf("fake git ops overlapped; rollback did not serialize against the create: order=%v", order)
	}
	want := []string{"create", "rollback-remove", "rollback-branch"}
	if len(order) != len(want) {
		t.Fatalf("call order = %v, want %v", order, want)
	}
	for i := range want {
		if order[i] != want[i] {
			t.Fatalf("call order = %v, want %v", order, want)
		}
	}
}
