package git

import (
	"os"
	"strings"
	"testing"
)

// writeFile writes content to a path inside repo, failing the test on error.
func writeFile(t *testing.T, repo, name, content string) {
	t.Helper()
	if err := os.WriteFile(repo+"/"+name, []byte(content), 0o644); err != nil {
		t.Fatalf("write %s: %v", name, err)
	}
}

func TestGetBaseBranch(t *testing.T) {
	skipIfNoGit(t)

	tests := []struct {
		name string
		// setup mutates a freshly initialized repo (created on "main") and
		// returns the base branch we expect GetBaseBranch to report.
		setup   func(t *testing.T, repo string)
		want    string
		wantErr bool
	}{
		{
			name:    "prefers main when present",
			setup:   func(t *testing.T, repo string) {},
			want:    "main",
			wantErr: false,
		},
		{
			name: "falls back to master when main absent",
			setup: func(t *testing.T, repo string) {
				runGit(t, repo, "branch", "-m", "main", "master")
			},
			want:    "master",
			wantErr: false,
		},
		{
			name: "falls back to develop when main and master absent",
			setup: func(t *testing.T, repo string) {
				runGit(t, repo, "branch", "-m", "main", "develop")
			},
			want:    "develop",
			wantErr: false,
		},
		{
			name: "falls back to dev when only dev exists",
			setup: func(t *testing.T, repo string) {
				runGit(t, repo, "branch", "-m", "main", "dev")
			},
			want:    "dev",
			wantErr: false,
		},
		{
			name: "main wins over master when both exist",
			setup: func(t *testing.T, repo string) {
				runGit(t, repo, "branch", "master")
			},
			want:    "main",
			wantErr: false,
		},
		{
			name: "master wins over develop in candidate order",
			setup: func(t *testing.T, repo string) {
				// Rename the default away from main so the local check starts
				// at master, then add develop too.
				runGit(t, repo, "branch", "-m", "main", "master")
				runGit(t, repo, "branch", "develop")
			},
			want:    "master",
			wantErr: false,
		},
		{
			name: "uses remote tracking branch when no local candidate",
			setup: func(t *testing.T, repo string) {
				// Move the only local branch to a non-candidate name, then
				// fabricate a remote-tracking ref origin/main pointing at HEAD.
				runGit(t, repo, "branch", "-m", "main", "work")
				runGit(t, repo, "update-ref", "refs/remotes/origin/main", "HEAD")
			},
			want:    "origin/main",
			wantErr: false,
		},
		{
			name: "resolves default branch via symbolic-ref to local branch",
			setup: func(t *testing.T, repo string) {
				// No candidate local or remote-candidate branch exists, but a
				// local "trunk" branch is pointed to by origin/HEAD.
				runGit(t, repo, "branch", "trunk")
				runGit(t, repo, "branch", "-m", "main", "work")
				runGit(t, repo, "symbolic-ref", "refs/remotes/origin/HEAD", "refs/remotes/origin/trunk")
				runGit(t, repo, "update-ref", "refs/heads/trunk", "HEAD")
			},
			want:    "trunk",
			wantErr: false,
		},
		{
			name: "resolves default branch via symbolic-ref to remote branch",
			setup: func(t *testing.T, repo string) {
				// origin/HEAD points at origin/trunk, but there is no local
				// trunk branch, so the remote-tracking ref is returned.
				runGit(t, repo, "branch", "-m", "main", "work")
				runGit(t, repo, "update-ref", "refs/remotes/origin/trunk", "HEAD")
				runGit(t, repo, "symbolic-ref", "refs/remotes/origin/HEAD", "refs/remotes/origin/trunk")
			},
			want:    "origin/trunk",
			wantErr: false,
		},
		{
			name: "errors when no default branch can be determined",
			setup: func(t *testing.T, repo string) {
				// Only a non-candidate local branch, no remote refs at all.
				runGit(t, repo, "branch", "-m", "main", "work")
			},
			want:    "",
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			repo := initRepo(t)
			tt.setup(t, repo)

			got, err := GetBaseBranch(repo)
			if tt.wantErr {
				if err == nil {
					t.Fatalf("GetBaseBranch() error = nil, want error (got %q)", got)
				}
				if got != "" {
					t.Errorf("GetBaseBranch() = %q on error, want empty string", got)
				}
				return
			}
			if err != nil {
				t.Fatalf("GetBaseBranch() unexpected error: %v", err)
			}
			if got != tt.want {
				t.Errorf("GetBaseBranch() = %q, want %q", got, tt.want)
			}
		})
	}
}

// TestGetBaseBranchInvalidRepo verifies the error path when the path is not a
// git repository at all (every git invocation fails).
func TestGetBaseBranchInvalidRepo(t *testing.T) {
	skipIfNoGit(t)

	got, err := GetBaseBranch(t.TempDir())
	if err == nil {
		t.Fatalf("GetBaseBranch() error = nil, want error for non-repo (got %q)", got)
	}
	if got != "" {
		t.Errorf("GetBaseBranch() = %q, want empty string on error", got)
	}
	if !strings.Contains(err.Error(), "unable to determine default branch") {
		t.Errorf("GetBaseBranch() error = %q, want it to mention default branch", err.Error())
	}
}

func TestGetBranchFileDiff(t *testing.T) {
	skipIfNoGit(t)

	t.Run("returns diff for file changed on feature branch", func(t *testing.T) {
		repo := initRepo(t)
		// Seed a tracked file on main.
		writeFile(t, repo, "app.go", "package main\n\nfunc main() {}\n")
		runGit(t, repo, "add", "app.go")
		runGit(t, repo, "commit", "-m", "add app")

		// Branch off and change the file.
		runGit(t, repo, "checkout", "-b", "feature")
		writeFile(t, repo, "app.go", "package main\n\nfunc main() { println(\"hi\") }\n")
		runGit(t, repo, "add", "app.go")
		runGit(t, repo, "commit", "-m", "change app")

		result, err := GetBranchFileDiff(repo, "app.go")
		if err != nil {
			t.Fatalf("GetBranchFileDiff() unexpected error: %v", err)
		}
		if result == nil {
			t.Fatal("GetBranchFileDiff() returned nil result")
		}
		if result.Error != "" {
			t.Fatalf("GetBranchFileDiff() result.Error = %q, want empty", result.Error)
		}
		if result.Path != "app.go" {
			t.Errorf("result.Path = %q, want app.go", result.Path)
		}
		if result.Empty {
			t.Error("result.Empty = true, want false for a changed file")
		}
		if len(result.Hunks) == 0 {
			t.Error("result.Hunks is empty, want at least one hunk")
		}
		if result.AddedLines() == 0 {
			t.Errorf("result.AddedLines() = 0, want > 0; content:\n%s", result.Content)
		}
	})

	t.Run("returns empty diff for unchanged file on branch", func(t *testing.T) {
		repo := initRepo(t)
		writeFile(t, repo, "stable.txt", "unchanged\n")
		runGit(t, repo, "add", "stable.txt")
		runGit(t, repo, "commit", "-m", "add stable")

		// Branch but only change a different file, leaving stable.txt alone.
		runGit(t, repo, "checkout", "-b", "feature")
		writeFile(t, repo, "other.txt", "new\n")
		runGit(t, repo, "add", "other.txt")
		runGit(t, repo, "commit", "-m", "add other")

		result, err := GetBranchFileDiff(repo, "stable.txt")
		if err != nil {
			t.Fatalf("GetBranchFileDiff() unexpected error: %v", err)
		}
		if result == nil {
			t.Fatal("GetBranchFileDiff() returned nil result")
		}
		if result.Error != "" {
			t.Fatalf("result.Error = %q, want empty", result.Error)
		}
		if !result.Empty {
			t.Errorf("result.Empty = false, want true for unchanged file; content:\n%s", result.Content)
		}
		if len(result.Hunks) != 0 {
			t.Errorf("result.Hunks count = %d, want 0", len(result.Hunks))
		}
	})

	t.Run("reports diff for file added on the branch", func(t *testing.T) {
		repo := initRepo(t)
		runGit(t, repo, "checkout", "-b", "feature")
		writeFile(t, repo, "added.txt", "line one\nline two\n")
		runGit(t, repo, "add", "added.txt")
		runGit(t, repo, "commit", "-m", "add file")

		result, err := GetBranchFileDiff(repo, "added.txt")
		if err != nil {
			t.Fatalf("GetBranchFileDiff() unexpected error: %v", err)
		}
		if result.Empty {
			t.Error("result.Empty = true, want false for newly added file")
		}
		if result.AddedLines() != 2 {
			t.Errorf("result.AddedLines() = %d, want 2", result.AddedLines())
		}
		if result.DeletedLines() != 0 {
			t.Errorf("result.DeletedLines() = %d, want 0", result.DeletedLines())
		}
	})

	t.Run("nonexistent file yields empty diff without error", func(t *testing.T) {
		repo := initRepo(t)
		// repo already has a base (main) so GetBaseBranch succeeds; git diff on
		// an unknown path simply produces no output.
		result, err := GetBranchFileDiff(repo, "does-not-exist.txt")
		if err != nil {
			t.Fatalf("GetBranchFileDiff() unexpected error: %v", err)
		}
		if result == nil {
			t.Fatal("GetBranchFileDiff() returned nil result")
		}
		if result.Error != "" {
			t.Errorf("result.Error = %q, want empty", result.Error)
		}
		if !result.Empty {
			t.Errorf("result.Empty = false, want true for nonexistent file; content:\n%s", result.Content)
		}
	})

	t.Run("propagates base-branch resolution error", func(t *testing.T) {
		// A repo with no candidate/remote branch makes GetBaseBranch fail, and
		// that error must surface from GetBranchFileDiff.
		repo := initRepo(t)
		runGit(t, repo, "branch", "-m", "main", "work")

		result, err := GetBranchFileDiff(repo, "README.md")
		if err == nil {
			t.Fatalf("GetBranchFileDiff() error = nil, want error from base resolution (result=%+v)", result)
		}
		if result != nil {
			t.Errorf("GetBranchFileDiff() result = %+v, want nil on error", result)
		}
		if !strings.Contains(err.Error(), "unable to determine default branch") {
			t.Errorf("error = %q, want it to mention default branch", err.Error())
		}
	})
}
