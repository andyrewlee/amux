package workspacesvc

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/andyrewlee/amux/internal/data"
	"github.com/andyrewlee/amux/internal/messages"
	"github.com/andyrewlee/amux/internal/testutil"
)

// TestCreateWorkspaceRollbackBranchOwnership pins rollback to what the
// create call actually owns: attaching to a pre-existing branch must not
// give rollback license to force-delete it, while a branch this create made
// must still be removed. The save-failure cases run the real default Git
// adapter against a real repo so the asserted refs are ground truth; the
// readiness cases stay fake-driven so they do not wait on a filesystem that
// always exposes .git promptly.
func TestCreateWorkspaceRollbackBranchOwnership(t *testing.T) {
	tmp := t.TempDir()
	workspacesRoot := filepath.Join(tmp, "managed")
	repo := testutil.InitRepo(t)
	project := data.NewProject(repo)

	failingSaveSvc := func() (*Service, *testutil.FakeWorkspaceStore) {
		store := &testutil.FakeWorkspaceStore{
			SaveFunc: func(*data.Workspace) error { return errors.New("metadata save boom") },
		}
		// Default gitOps: the real adapter, so branch ownership comes from
		// the actual git command path.
		return New(nil, store, nil, workspacesRoot), store
	}

	assertNoWorktreeRegistration := func(t *testing.T, wsPath string) {
		t.Helper()
		list := testutil.RunGit(t, repo, "worktree", "list", "--porcelain")
		for _, line := range strings.Split(list, "\n") {
			if line == "worktree "+wsPath {
				t.Fatalf("worktree registration survived rollback: %s", line)
			}
		}
	}
	assertNoMetadata := func(t *testing.T, store *testutil.FakeWorkspaceStore, failed messages.WorkspaceCreateFailed) {
		t.Helper()
		if failed.Workspace == nil {
			t.Fatal("expected the pending workspace on the failure message")
		}
		if loaded, _ := store.Load(failed.Workspace.MetadataID()); loaded != nil {
			t.Fatalf("metadata record survived failed create: %+v", loaded)
		}
	}

	t.Run("save failure preserves a pre-existing branch", func(t *testing.T) {
		// A feature branch with a unique tip already exists before create;
		// the create will attach to it rather than create it.
		testutil.RunGit(t, repo, "checkout", "-b", "feature")
		if err := os.WriteFile(filepath.Join(repo, "unique.txt"), []byte("unique\n"), 0o600); err != nil {
			t.Fatalf("write unique.txt: %v", err)
		}
		testutil.RunGit(t, repo, "add", "unique.txt")
		testutil.RunGit(t, repo, "commit", "-m", "unique feature commit")
		testutil.RunGit(t, repo, "checkout", "main")
		wantOID := testutil.RunGit(t, repo, "rev-parse", "refs/heads/feature")

		svc, store := failingSaveSvc()
		msg := svc.CreateWorkspace(project, "feature", "main")()
		failed, ok := msg.(messages.WorkspaceCreateFailed)
		if !ok {
			t.Fatalf("expected WorkspaceCreateFailed, got %T", msg)
		}
		assertNoMetadata(t, store, failed)
		assertNoWorktreeRegistration(t, failed.Workspace.Root)
		if got := testutil.RunGit(t, repo, "rev-parse", "refs/heads/feature"); got != wantOID {
			t.Fatalf("rollback destroyed pre-existing refs/heads/feature: got %s, want %s", got, wantOID)
		}
	})

	t.Run("save failure removes an owned branch", func(t *testing.T) {
		svc, store := failingSaveSvc()
		msg := svc.CreateWorkspace(project, "owned-branch", "main")()
		failed, ok := msg.(messages.WorkspaceCreateFailed)
		if !ok {
			t.Fatalf("expected WorkspaceCreateFailed, got %T", msg)
		}
		assertNoMetadata(t, store, failed)
		assertNoWorktreeRegistration(t, failed.Workspace.Root)
		if ref := testutil.RunGit(t, repo, "for-each-ref", "refs/heads/owned-branch"); ref != "" {
			t.Fatalf("branch created by the failed create survived rollback: %s", ref)
		}
	})

	t.Run("readiness failure respects branch ownership", func(t *testing.T) {
		for _, tc := range []struct {
			name          string
			branchCreated bool
			wantDeletes   int
		}{
			{"owned branch is deleted", true, 1},
			{"reused branch is preserved", false, 0},
		} {
			t.Run(tc.name, func(t *testing.T) {
				var removes, deletes int
				svc := New(nil, nil, nil, workspacesRoot)
				svc.gitPathWaitTimeout = 50 * time.Millisecond
				svc.gitOps = &testutil.FakeGitOps{
					CreateWorkspaceFunc: func(_, _, _, _ string) (bool, error) {
						// Success without .git so waitForGitPath fails and
						// rollback runs.
						return tc.branchCreated, nil
					},
					RemoveWorkspaceFunc: func(_, _ string) error {
						removes++
						return nil
					},
					DeleteBranchFunc: func(_, _ string) error {
						deletes++
						return nil
					},
				}
				msg := svc.CreateWorkspace(project, "readiness-branch", "main")()
				if _, ok := msg.(messages.WorkspaceCreateFailed); !ok {
					t.Fatalf("expected WorkspaceCreateFailed, got %T", msg)
				}
				if removes != 1 {
					t.Fatalf("RemoveWorkspace calls = %d, want 1 — removal runs for both ownership values", removes)
				}
				if deletes != tc.wantDeletes {
					t.Fatalf("DeleteBranch calls = %d, want %d (branchCreated=%v)", deletes, tc.wantDeletes, tc.branchCreated)
				}
			})
		}
	})
}
