package git

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"
)

// mergeRefFixture builds the collision shape under test: a local "feature"
// branch with one commit on top of main, and a same-named tag pointing at the
// old main tip. It returns the repo root and the exact branch tip recorded
// through refs/heads/feature — the object the merge is required to land.
//
// Git's revision-resolution order lets refs/tags/<name> shadow the short
// branch name, so `git merge --no-ff -- feature` would merge the tag — which
// is already an ancestor — and report success without merging a single
// workspace commit.
func mergeRefFixture(t *testing.T, tagArgs ...string) (root, branchTip string) {
	t.Helper()
	root = repoWithFeatureBranch(t)
	branchTip = runGit(t, root, "rev-parse", "refs/heads/feature")
	args := append([]string{"tag"}, tagArgs...)
	args = append(args, "feature", "main")
	runGit(t, root, args...)
	return root, branchTip
}

// TestMergeWorkspaceBranchExactLocalBranch pins branch identity: the merge
// input must be the exact refs/heads/<branch> object, never whatever a
// same-named tag happens to point at — and a name with no local branch must
// fail rather than silently merging a tag.
func TestMergeWorkspaceBranchExactLocalBranch(t *testing.T) {
	skipIfNoGit(t)

	t.Run("lightweight tag collision", func(t *testing.T) {
		root, branchTip := mergeRefFixture(t)
		assertMergeLandsBranchTip(t, root, "feature", branchTip)
	})

	t.Run("annotated tag collision", func(t *testing.T) {
		root, branchTip := mergeRefFixture(t, "-a", "-m", "shadowing tag")
		assertMergeLandsBranchTip(t, root, "feature", branchTip)
	})

	t.Run("slashed branch collides with slashed tag", func(t *testing.T) {
		root := initRepo(t)
		runGit(t, root, "checkout", "-b", "feature/sub")
		commitFile(t, root, "feature.txt", "from the feature branch\n", "add feature file")
		runGit(t, root, "checkout", "main")
		branchTip := runGit(t, root, "rev-parse", "refs/heads/feature/sub")
		runGit(t, root, "tag", "feature/sub", "main")
		assertMergeLandsBranchTip(t, root, "feature/sub", branchTip)
	})

	t.Run("tag without local branch is rejected", func(t *testing.T) {
		root := initRepo(t)
		runGit(t, root, "tag", "feature", "main")
		assertMergeRefRejected(t, root, "feature")
	})

	t.Run("missing ref is rejected", func(t *testing.T) {
		root := initRepo(t)
		assertMergeRefRejected(t, root, "no-such-branch")
	})
}

// assertMergeLandsBranchTip merges branch and requires the merge commit's
// second parent to be exactly the recorded refs/heads tip, on the checkout the
// caller left in place.
func assertMergeLandsBranchTip(t *testing.T, root, branch, branchTip string) {
	t.Helper()

	if err := MergeWorkspaceBranch(context.Background(), root, branch); err != nil {
		t.Fatalf("MergeWorkspaceBranch(%q): unexpected error: %v", branch, err)
	}

	if _, err := os.Stat(filepath.Join(root, "feature.txt")); err != nil {
		t.Fatalf("feature.txt not present after merge: %v", err)
	}
	if got := runGit(t, root, "rev-parse", "HEAD^2"); got != branchTip {
		t.Fatalf("merged second parent = %q, want the exact local branch tip %q", got, branchTip)
	}
	if got := runGit(t, root, "symbolic-ref", "--short", "HEAD"); got != "main" {
		t.Fatalf("checkout moved to %q; the merge must land on the caller's HEAD", got)
	}
}

// assertMergeRefRejected requires a failed merge that changed nothing: same
// HEAD, clean tree, no merge left in progress, and no conflict
// misclassification.
func assertMergeRefRejected(t *testing.T, root, branch string) {
	t.Helper()

	headBefore := runGit(t, root, "rev-parse", "HEAD")
	err := MergeWorkspaceBranch(context.Background(), root, branch)
	if err == nil {
		t.Fatalf("MergeWorkspaceBranch(%q): expected an error with no local branch of that name", branch)
	}
	if errors.Is(err, ErrMergeConflict) {
		t.Fatalf("MergeWorkspaceBranch(%q): a missing local branch is not a conflict: %v", branch, err)
	}
	if got := runGit(t, root, "rev-parse", "HEAD"); got != headBefore {
		t.Fatalf("a rejected merge moved HEAD to %q", got)
	}
	if _, statErr := os.Stat(filepath.Join(root, ".git", "MERGE_HEAD")); !os.IsNotExist(statErr) {
		t.Fatal("a rejected merge left MERGE_HEAD behind")
	}
	if got := runGit(t, root, "status", "--porcelain"); got != "" {
		t.Fatalf("a rejected merge dirtied the tree: %q", got)
	}
}
