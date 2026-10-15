package git

import (
	"strings"
	"testing"
)

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
