package git

import (
	"os"
	"strconv"
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

func TestBranchChangesVsBase(t *testing.T) {
	skipIfNoGit(t)

	t.Run("lists added, modified, and deleted files vs base", func(t *testing.T) {
		repo := initRepo(t)
		writeFile(t, repo, "modified.go", "package main\n")
		writeFile(t, repo, "removed.go", "package doomed\n\nfunc DoNotKeep() int { return 1 }\n")
		runGit(t, repo, "add", "modified.go", "removed.go")
		runGit(t, repo, "commit", "-m", "seed files")

		runGit(t, repo, "checkout", "-b", "feature")
		writeFile(t, repo, "modified.go", "package main\n\nfunc main() {}\n")
		runGit(t, repo, "rm", "removed.go")
		// Content deliberately dissimilar from removed.go so git's rename
		// detection doesn't collapse this add+delete pair into a rename.
		writeFile(t, repo, "added.go", "package brandnew\n\ntype Widget struct{ Count int }\n")
		runGit(t, repo, "add", "modified.go", "added.go")
		runGit(t, repo, "commit", "-m", "change files")

		changes, err := BranchChangesVsBase(repo)
		if err != nil {
			t.Fatalf("BranchChangesVsBase() unexpected error: %v", err)
		}
		if len(changes) != 3 {
			t.Fatalf("BranchChangesVsBase() returned %d changes, want 3: %+v", len(changes), changes)
		}

		byPath := make(map[string]Change, len(changes))
		for _, c := range changes {
			byPath[c.Path] = c
		}

		if c, ok := byPath["added.go"]; !ok || c.Kind != ChangeAdded {
			t.Errorf("added.go = %+v, want ChangeAdded present", c)
		}
		if c, ok := byPath["modified.go"]; !ok || c.Kind != ChangeModified {
			t.Errorf("modified.go = %+v, want ChangeModified present", c)
		}
		if c, ok := byPath["removed.go"]; !ok || c.Kind != ChangeDeleted {
			t.Errorf("removed.go = %+v, want ChangeDeleted present", c)
		}

		// Sorted lexicographically, mirroring status parsing.
		for i := 1; i < len(changes); i++ {
			if changes[i-1].Path > changes[i].Path {
				t.Errorf("changes not sorted: %q before %q", changes[i-1].Path, changes[i].Path)
			}
		}
	})

	t.Run("returns no changes for a branch with nothing ahead of base", func(t *testing.T) {
		repo := initRepo(t)
		runGit(t, repo, "checkout", "-b", "feature")

		changes, err := BranchChangesVsBase(repo)
		if err != nil {
			t.Fatalf("BranchChangesVsBase() unexpected error: %v", err)
		}
		if len(changes) != 0 {
			t.Errorf("BranchChangesVsBase() = %+v, want no changes", changes)
		}
	})

	t.Run("propagates base-branch resolution error", func(t *testing.T) {
		repo := initRepo(t)
		runGit(t, repo, "branch", "-m", "main", "work")

		changes, err := BranchChangesVsBase(repo)
		if err == nil {
			t.Fatalf("BranchChangesVsBase() error = nil, want error from base resolution (changes=%+v)", changes)
		}
		if changes != nil {
			t.Errorf("BranchChangesVsBase() changes = %+v, want nil on error", changes)
		}
		if !strings.Contains(err.Error(), "unable to determine default branch") {
			t.Errorf("error = %q, want it to mention default branch", err.Error())
		}
	})
}

func TestAheadBehind(t *testing.T) {
	skipIfNoGit(t)

	t.Run("counts commits ahead of base", func(t *testing.T) {
		repo := initRepo(t)
		runGit(t, repo, "checkout", "-b", "feature")
		for i := 0; i < 3; i++ {
			writeFile(t, repo, "file.txt", strings.Repeat("x", i+1))
			runGit(t, repo, "add", "file.txt")
			runGit(t, repo, "commit", "-m", "commit "+strconv.Itoa(i))
		}

		ahead, behind, err := AheadBehind(repo)
		if err != nil {
			t.Fatalf("AheadBehind() unexpected error: %v", err)
		}
		if ahead != 3 {
			t.Errorf("ahead = %d, want 3", ahead)
		}
		if behind != 0 {
			t.Errorf("behind = %d, want 0", behind)
		}
	})

	t.Run("counts commits behind base", func(t *testing.T) {
		repo := initRepo(t)
		runGit(t, repo, "checkout", "-b", "feature")
		// Base (main) picks up two commits the feature branch never sees.
		runGit(t, repo, "checkout", "main")
		for i := 0; i < 2; i++ {
			writeFile(t, repo, "base.txt", strings.Repeat("y", i+1))
			runGit(t, repo, "add", "base.txt")
			runGit(t, repo, "commit", "-m", "base commit "+strconv.Itoa(i))
		}
		runGit(t, repo, "checkout", "feature")

		ahead, behind, err := AheadBehind(repo)
		if err != nil {
			t.Fatalf("AheadBehind() unexpected error: %v", err)
		}
		if ahead != 0 {
			t.Errorf("ahead = %d, want 0", ahead)
		}
		if behind != 2 {
			t.Errorf("behind = %d, want 2", behind)
		}
	})

	t.Run("counts both ahead and behind when diverged", func(t *testing.T) {
		repo := initRepo(t)
		runGit(t, repo, "checkout", "-b", "feature")
		writeFile(t, repo, "feature.txt", "one\n")
		runGit(t, repo, "add", "feature.txt")
		runGit(t, repo, "commit", "-m", "feature commit")

		runGit(t, repo, "checkout", "main")
		writeFile(t, repo, "base.txt", "one\n")
		runGit(t, repo, "add", "base.txt")
		runGit(t, repo, "commit", "-m", "base commit")
		runGit(t, repo, "checkout", "feature")

		ahead, behind, err := AheadBehind(repo)
		if err != nil {
			t.Fatalf("AheadBehind() unexpected error: %v", err)
		}
		if ahead != 1 {
			t.Errorf("ahead = %d, want 1", ahead)
		}
		if behind != 1 {
			t.Errorf("behind = %d, want 1", behind)
		}
	})

	t.Run("zero commits ahead or behind on a clean checkout of base", func(t *testing.T) {
		repo := initRepo(t)
		runGit(t, repo, "checkout", "-b", "feature")

		ahead, behind, err := AheadBehind(repo)
		if err != nil {
			t.Fatalf("AheadBehind() unexpected error: %v", err)
		}
		if ahead != 0 || behind != 0 {
			t.Errorf("ahead, behind = %d, %d, want 0, 0", ahead, behind)
		}
	})

	t.Run("propagates base-branch resolution error", func(t *testing.T) {
		repo := initRepo(t)
		runGit(t, repo, "branch", "-m", "main", "work")

		ahead, behind, err := AheadBehind(repo)
		if err == nil {
			t.Fatalf("AheadBehind() error = nil, want error from base resolution (ahead=%d behind=%d)", ahead, behind)
		}
		if ahead != 0 || behind != 0 {
			t.Errorf("AheadBehind() ahead, behind = %d, %d, want 0, 0 on error", ahead, behind)
		}
		if !strings.Contains(err.Error(), "unable to determine default branch") {
			t.Errorf("error = %q, want it to mention default branch", err.Error())
		}
	})
}
