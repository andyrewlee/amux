package git

import (
	"context"
	"errors"
	"path/filepath"
	"sort"
	"strings"
	"testing"

	"github.com/andyrewlee/amux/internal/data"
)

// branchExists reports whether the given branch resolves in repo.
func branchExists(t *testing.T, repo, branch string) bool {
	t.Helper()
	_, err := RunGit(repo, "rev-parse", "--verify", "refs/heads/"+branch)
	return err == nil
}

func TestDeleteBranch(t *testing.T) {
	skipIfNoGit(t)

	tests := []struct {
		name string
		// setup mutates a freshly initialized repo (current branch "main")
		// and returns the branch name to delete.
		setup   func(t *testing.T, repo string) string
		wantErr bool
		// errContains, when set, must appear in the returned error message.
		errContains string
	}{
		{
			name: "deletes a merged local branch",
			setup: func(t *testing.T, repo string) string {
				// Branch points at the same commit as main, so -D succeeds.
				runGit(t, repo, "branch", "feature-merged")
				return "feature-merged"
			},
			wantErr: false,
		},
		{
			name: "force-deletes an unmerged branch",
			setup: func(t *testing.T, repo string) string {
				runGit(t, repo, "checkout", "-b", "feature-unmerged")
				writeFile(t, repo, "extra.txt", "unmerged work")
				runGit(t, repo, "add", "extra.txt")
				runGit(t, repo, "commit", "-m", "unmerged commit")
				// Move off the branch so it can be deleted.
				runGit(t, repo, "checkout", "main")
				return "feature-unmerged"
			},
			// branch -D force-deletes even unmerged branches.
			wantErr: false,
		},
		{
			name: "errors on a missing branch",
			setup: func(t *testing.T, _ string) string {
				return "does-not-exist"
			},
			wantErr:     true,
			errContains: "does-not-exist",
		},
		{
			name: "errors when deleting the checked-out branch",
			setup: func(t *testing.T, _ string) string {
				// main is the current branch and cannot be deleted.
				return "main"
			},
			wantErr: true,
		},
		{
			name: "errors on an empty branch name",
			setup: func(t *testing.T, _ string) string {
				return ""
			},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			repo := initRepo(t)
			branch := tt.setup(t, repo)

			// Record whether the branch existed before the delete so we can
			// assert it is actually gone on the success path.
			existedBefore := branch != "" && branchExists(t, repo, branch)

			err := DeleteBranch(repo, branch)

			if tt.wantErr {
				if err == nil {
					t.Fatalf("DeleteBranch(%q) = nil, want error", branch)
				}
				if tt.errContains != "" && !strings.Contains(err.Error(), tt.errContains) {
					t.Fatalf("DeleteBranch(%q) error = %q, want it to contain %q", branch, err, tt.errContains)
				}
				return
			}

			if err != nil {
				t.Fatalf("DeleteBranch(%q) = %v, want nil", branch, err)
			}
			if !existedBefore {
				t.Fatalf("test setup error: branch %q did not exist before delete", branch)
			}
			if branchExists(t, repo, branch) {
				t.Fatalf("DeleteBranch(%q) returned nil but branch still exists", branch)
			}
		})
	}
}

// TestDeleteBranchClassifiesUnderlyingError confirms the structured git error
// (exit code + stderr) flows back unchanged to the caller.
func TestDeleteBranchClassifiesUnderlyingError(t *testing.T) {
	origRunGitCtx := runGitCtx
	defer func() { runGitCtx = origRunGitCtx }()

	wantErr := &Error{
		Command:  "branch -D feature",
		Args:     []string{"branch", "-D", "feature"},
		ExitCode: 1,
		Stderr:   "error: branch 'feature' not found",
		Err:      errors.New("exit status 1"),
	}
	var gotArgs []string
	runGitCtx = func(_ context.Context, dir string, args ...string) (string, error) {
		if dir != "/tmp/repo" {
			t.Fatalf("dir = %q, want %q", dir, "/tmp/repo")
		}
		gotArgs = args
		return "", wantErr
	}

	err := DeleteBranch("/tmp/repo", "feature")
	if !errors.Is(err, wantErr) {
		t.Fatalf("DeleteBranch() error = %v, want %v", err, wantErr)
	}
	if got, want := strings.Join(gotArgs, " "), "branch -D feature"; got != want {
		t.Fatalf("git args = %q, want %q", got, want)
	}

	// The structured error must survive so callers can classify by exit code.
	var gitErr *Error
	if !errors.As(err, &gitErr) {
		t.Fatalf("DeleteBranch() error is not *Error: %v", err)
	}
	if gitErr.ExitCode != 1 {
		t.Fatalf("exit code = %d, want 1", gitErr.ExitCode)
	}
}

func TestDiscoverWorkspaces(t *testing.T) {
	skipIfNoGit(t)

	t.Run("primary repo only", func(t *testing.T) {
		repo := initRepo(t)

		got, err := DiscoverWorkspaces(&data.Project{Path: repo})
		if err != nil {
			t.Fatalf("DiscoverWorkspaces() error = %v", err)
		}
		if len(got) != 1 {
			t.Fatalf("DiscoverWorkspaces() returned %d workspaces, want 1", len(got))
		}

		ws := got[0]
		// The primary worktree's Root is the repo path itself; git may report a
		// symlink-resolved path, so compare on the base name and the metadata
		// fields we populate deterministically.
		if ws.Name != filepath.Base(repo) {
			t.Errorf("Name = %q, want %q", ws.Name, filepath.Base(repo))
		}
		if ws.Branch != "main" {
			t.Errorf("Branch = %q, want %q", ws.Branch, "main")
		}
		if ws.Repo != repo {
			t.Errorf("Repo = %q, want %q", ws.Repo, repo)
		}
		if ws.Root == "" {
			t.Errorf("Root = %q, want non-empty", ws.Root)
		}
	})

	t.Run("primary plus added worktrees", func(t *testing.T) {
		repo := initRepo(t)
		wtRoot := t.TempDir()
		featurePath := filepath.Join(wtRoot, "feature")
		bugfixPath := filepath.Join(wtRoot, "bugfix")
		runGit(t, repo, "worktree", "add", "-b", "feature", featurePath)
		runGit(t, repo, "worktree", "add", "-b", "bugfix", bugfixPath)

		got, err := DiscoverWorkspaces(&data.Project{Path: repo})
		if err != nil {
			t.Fatalf("DiscoverWorkspaces() error = %v", err)
		}
		if len(got) != 3 {
			t.Fatalf("DiscoverWorkspaces() returned %d workspaces, want 3", len(got))
		}

		branches := make([]string, 0, len(got))
		for _, ws := range got {
			branches = append(branches, ws.Branch)
			if ws.Repo != repo {
				t.Errorf("workspace %q Repo = %q, want %q", ws.Name, ws.Repo, repo)
			}
		}
		sort.Strings(branches)
		want := []string{"bugfix", "feature", "main"}
		if strings.Join(branches, ",") != strings.Join(want, ",") {
			t.Fatalf("branches = %v, want %v", branches, want)
		}
	})

	t.Run("errors on a non-repository path", func(t *testing.T) {
		// A plain temp dir is not a git repository, so worktree list fails.
		notARepo := t.TempDir()
		got, err := DiscoverWorkspaces(&data.Project{Path: notARepo})
		if err == nil {
			t.Fatalf("DiscoverWorkspaces() = %v, want error for non-repo path", got)
		}
		if got != nil {
			t.Fatalf("DiscoverWorkspaces() returned %v on error, want nil", got)
		}
	})
}

// TestDiscoverWorkspacesPropagatesGitError confirms a git failure is returned
// verbatim (and as nil workspaces) without being parsed into an empty slice.
func TestDiscoverWorkspacesPropagatesGitError(t *testing.T) {
	origRunGitCtx := runGitCtx
	defer func() { runGitCtx = origRunGitCtx }()

	wantErr := errors.New("boom: worktree list failed")
	runGitCtx = func(_ context.Context, dir string, args ...string) (string, error) {
		if dir != "/tmp/project" {
			t.Fatalf("dir = %q, want %q", dir, "/tmp/project")
		}
		if got, want := strings.Join(args, " "), "worktree list --porcelain"; got != want {
			t.Fatalf("git args = %q, want %q", got, want)
		}
		return "", wantErr
	}

	got, err := DiscoverWorkspaces(&data.Project{Path: "/tmp/project"})
	if !errors.Is(err, wantErr) {
		t.Fatalf("DiscoverWorkspaces() error = %v, want %v", err, wantErr)
	}
	if got != nil {
		t.Fatalf("DiscoverWorkspaces() returned %v on error, want nil", got)
	}
}

// TestDiscoverWorkspacesParsesStubbedOutput exercises DiscoverWorkspaces end to
// end against canned porcelain output, decoupling field-mapping coverage from
// the host git's exact path reporting.
func TestDiscoverWorkspacesParsesStubbedOutput(t *testing.T) {
	origRunGitCtx := runGitCtx
	defer func() { runGitCtx = origRunGitCtx }()

	runGitCtx = func(_ context.Context, _ string, _ ...string) (string, error) {
		return `worktree /home/user/myrepo
HEAD abc123
branch refs/heads/main

worktree /home/user/.amux/workspaces/myrepo/feature
HEAD def456
branch refs/heads/feature
`, nil
	}

	got, err := DiscoverWorkspaces(&data.Project{Path: "/home/user/myrepo"})
	if err != nil {
		t.Fatalf("DiscoverWorkspaces() error = %v", err)
	}
	if len(got) != 2 {
		t.Fatalf("DiscoverWorkspaces() returned %d workspaces, want 2", len(got))
	}
	if got[1].Name != "feature" || got[1].Branch != "feature" {
		t.Fatalf("second workspace = %+v, want Name/Branch feature", got[1])
	}
	if got[1].Root != "/home/user/.amux/workspaces/myrepo/feature" {
		t.Fatalf("second workspace Root = %q, want stubbed path", got[1].Root)
	}
	// Repo is always set from the project path regardless of git output.
	if got[0].Repo != "/home/user/myrepo" {
		t.Fatalf("first workspace Repo = %q, want %q", got[0].Repo, "/home/user/myrepo")
	}
}
