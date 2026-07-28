package git

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// commitFile writes path with content and commits it on the current branch.
func commitFile(t *testing.T, root, path, content, message string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(root, path), []byte(content), 0o600); err != nil {
		t.Fatalf("write %s: %v", path, err)
	}
	runGit(t, root, "add", path)
	runGit(t, root, "commit", "-m", message)
}

// repoWithFeatureBranch builds a repo on "main" with a "feature" branch holding
// one extra commit, leaving main checked out — the exact shape the merge action
// runs against.
func repoWithFeatureBranch(t *testing.T) string {
	t.Helper()
	root := initRepo(t)
	runGit(t, root, "checkout", "-b", "feature")
	commitFile(t, root, "feature.txt", "from the feature branch\n", "add feature file")
	runGit(t, root, "checkout", "main")
	return root
}

func TestMergeWorkspaceBranchCreatesMergeCommit(t *testing.T) {
	skipIfNoGit(t)
	root := repoWithFeatureBranch(t)

	if err := MergeWorkspaceBranch(context.Background(), root, "feature"); err != nil {
		t.Fatalf("MergeWorkspaceBranch: unexpected error: %v", err)
	}

	// The branch's file landed on main.
	if _, err := os.Stat(filepath.Join(root, "feature.txt")); err != nil {
		t.Fatalf("feature.txt not present on main after merge: %v", err)
	}
	// --no-ff means an explicit merge commit, i.e. HEAD has two parents.
	parents := runGit(t, root, "rev-list", "--parents", "-n", "1", "HEAD")
	if got := len(strings.Fields(parents)); got != 3 {
		t.Fatalf("HEAD has %d entries (commit + parents) = %q; want a 2-parent merge commit", got, parents)
	}
	if got := runGit(t, root, "status", "--porcelain"); got != "" {
		t.Fatalf("working tree not clean after a successful merge: %q", got)
	}
}

// TestMergeWorkspaceBranchNeverFastForwards pins --no-ff specifically: even
// when main has not moved (a fast-forward would be possible), the merge must
// still record a merge commit so the topology stays auditable.
func TestMergeWorkspaceBranchNeverFastForwards(t *testing.T) {
	skipIfNoGit(t)
	root := repoWithFeatureBranch(t)

	if err := MergeWorkspaceBranch(context.Background(), root, "feature"); err != nil {
		t.Fatalf("MergeWorkspaceBranch: unexpected error: %v", err)
	}

	parents := strings.Fields(runGit(t, root, "rev-list", "--parents", "-n", "1", "HEAD"))
	if len(parents) != 3 {
		t.Fatalf("merge fast-forwarded instead of recording a merge commit: %q", parents)
	}
}

// TestMergeWorkspaceBranchMergesIntoCheckedOutBranch pins the "never check the
// base out implicitly" invariant: the merge lands on whatever HEAD is, and the
// branch amux was told to merge is left untouched.
func TestMergeWorkspaceBranchMergesIntoCheckedOutBranch(t *testing.T) {
	skipIfNoGit(t)
	root := repoWithFeatureBranch(t)

	// Deliberately merge while a third branch is checked out.
	runGit(t, root, "checkout", "-b", "integration")
	before := runGit(t, root, "rev-parse", "main")

	if err := MergeWorkspaceBranch(context.Background(), root, "feature"); err != nil {
		t.Fatalf("MergeWorkspaceBranch: unexpected error: %v", err)
	}

	if got := runGit(t, root, "symbolic-ref", "--short", "HEAD"); got != "integration" {
		t.Fatalf("merge moved HEAD to %q; it must never check out another branch", got)
	}
	if after := runGit(t, root, "rev-parse", "main"); after != before {
		t.Fatal("merge advanced main even though main was not checked out")
	}
}

// TestMergeWorkspaceBranchConflictReportsFiles asserts a conflicting merge is
// reported as a conflict — with the paths that need resolving — rather than as
// an opaque git failure, and that the merge is left in progress to resolve.
func TestMergeWorkspaceBranchConflictReportsFiles(t *testing.T) {
	skipIfNoGit(t)
	root := initRepo(t)
	commitFile(t, root, "shared.txt", "base\n", "add shared")

	runGit(t, root, "checkout", "-b", "feature")
	commitFile(t, root, "shared.txt", "feature version\n", "feature edit")

	runGit(t, root, "checkout", "main")
	commitFile(t, root, "shared.txt", "main version\n", "main edit")

	err := MergeWorkspaceBranch(context.Background(), root, "feature")
	if err == nil {
		t.Fatal("MergeWorkspaceBranch: expected a conflict error")
	}
	if !errors.Is(err, ErrMergeConflict) {
		t.Fatalf("MergeWorkspaceBranch: got %v, want ErrMergeConflict", err)
	}

	var conflict *MergeConflictError
	if !errors.As(err, &conflict) {
		t.Fatalf("error %v does not carry a *MergeConflictError", err)
	}
	if conflict.Branch != "feature" {
		t.Fatalf("conflict names branch %q, want feature", conflict.Branch)
	}
	if len(conflict.Files) != 1 || conflict.Files[0] != "shared.txt" {
		t.Fatalf("conflicted files = %v, want [shared.txt]", conflict.Files)
	}

	// The merge is left in progress so the user can resolve it or abort.
	if _, statErr := os.Stat(filepath.Join(root, ".git", "MERGE_HEAD")); statErr != nil {
		t.Fatalf("no merge in progress after a conflict: %v", statErr)
	}
}

// TestAbortMergeRestoresPreMergeState asserts the escape hatch works: after a
// conflict, AbortMerge returns the tree and HEAD to exactly where they were.
func TestAbortMergeRestoresPreMergeState(t *testing.T) {
	skipIfNoGit(t)
	root := initRepo(t)
	commitFile(t, root, "shared.txt", "base\n", "add shared")

	runGit(t, root, "checkout", "-b", "feature")
	commitFile(t, root, "shared.txt", "feature version\n", "feature edit")

	runGit(t, root, "checkout", "main")
	commitFile(t, root, "shared.txt", "main version\n", "main edit")

	headBefore := runGit(t, root, "rev-parse", "HEAD")

	if err := MergeWorkspaceBranch(context.Background(), root, "feature"); !errors.Is(err, ErrMergeConflict) {
		t.Fatalf("setup: expected a conflict, got %v", err)
	}
	if err := AbortMerge(context.Background(), root); err != nil {
		t.Fatalf("AbortMerge: unexpected error: %v", err)
	}

	if got := runGit(t, root, "rev-parse", "HEAD"); got != headBefore {
		t.Fatal("AbortMerge left HEAD moved")
	}
	if got := runGit(t, root, "status", "--porcelain"); got != "" {
		t.Fatalf("working tree not clean after AbortMerge: %q", got)
	}
	content, err := os.ReadFile(filepath.Join(root, "shared.txt"))
	if err != nil {
		t.Fatalf("read shared.txt: %v", err)
	}
	if string(content) != "main version\n" {
		t.Fatalf("shared.txt = %q after abort, want main's version restored", content)
	}
	if _, statErr := os.Stat(filepath.Join(root, ".git", "MERGE_HEAD")); !os.IsNotExist(statErr) {
		t.Fatal("a merge is still in progress after AbortMerge")
	}
}

// TestMergeWorkspaceBranchReportsAnInterruptedMerge covers the gap between
// "has unmerged files" and "has a merge to clean up". A merge that git left
// half-done — MERGE_HEAD written, nothing yet marked unmerged — still needs the
// abort the conflict dialog offers, so it must be reported as a conflict rather
// than as an opaque failure the user cannot act on.
func TestMergeWorkspaceBranchReportsAnInterruptedMerge(t *testing.T) {
	skipIfNoGit(t)
	root := repoWithFeatureBranch(t)

	// Stage the shape an interrupted merge leaves behind: MERGE_HEAD present,
	// index clean. A second merge attempt then fails with a merge already in
	// progress, which is the failure path under test.
	featureSHA := runGit(t, root, "rev-parse", "feature")
	if err := os.WriteFile(filepath.Join(root, ".git", "MERGE_HEAD"), []byte(featureSHA+"\n"), 0o600); err != nil {
		t.Fatalf("write MERGE_HEAD: %v", err)
	}

	err := MergeWorkspaceBranch(context.Background(), root, "feature")
	if err == nil {
		t.Fatal("expected an error: a merge is already in progress")
	}
	if !errors.Is(err, ErrMergeConflict) {
		t.Fatalf("an interrupted merge reported %v; want ErrMergeConflict so the user is offered Abort", err)
	}

	// And the abort has to actually clear it.
	if err := AbortMerge(context.Background(), root); err != nil {
		t.Fatalf("AbortMerge on an interrupted merge: %v", err)
	}
	if _, statErr := os.Stat(filepath.Join(root, ".git", "MERGE_HEAD")); !os.IsNotExist(statErr) {
		t.Fatal("AbortMerge left MERGE_HEAD in place")
	}
}

// TestMergeWorkspaceBranchUnknownRefIsNotAConflict asserts a plain failure is
// not misreported as a conflict — there is nothing to resolve or abort, so the
// UI must show the git error rather than a conflict modal.
func TestMergeWorkspaceBranchUnknownRefIsNotAConflict(t *testing.T) {
	skipIfNoGit(t)
	root := initRepo(t)

	err := MergeWorkspaceBranch(context.Background(), root, "no-such-branch")
	if err == nil {
		t.Fatal("expected an error merging a branch that does not exist")
	}
	if errors.Is(err, ErrMergeConflict) {
		t.Fatalf("an unknown ref was reported as a conflict: %v", err)
	}
}

// TestMergeWorkspaceBranchRejectsEmptyBranch asserts the guard fires before git
// runs, so an empty ref cannot turn into `git merge --no-ff --`.
func TestMergeWorkspaceBranchRejectsEmptyBranch(t *testing.T) {
	skipIfNoGit(t)
	root := initRepo(t)
	before := runGit(t, root, "rev-parse", "HEAD")

	for _, branch := range []string{"", "   "} {
		if err := MergeWorkspaceBranch(context.Background(), root, branch); err == nil {
			t.Fatalf("MergeWorkspaceBranch(%q): expected an error", branch)
		}
	}
	if after := runGit(t, root, "rev-parse", "HEAD"); after != before {
		t.Fatal("an empty branch name still moved HEAD")
	}
}

// TestMergeWorkspaceBranchDashTerminatesOptions asserts a branch name that
// looks like a flag is treated as a ref, not reparsed by git as an option.
func TestMergeWorkspaceBranchDashTerminatesOptions(t *testing.T) {
	skipIfNoGit(t)
	root := initRepo(t)

	err := MergeWorkspaceBranch(context.Background(), root, "--no-verify")
	if err == nil {
		t.Fatal("expected an error: --no-verify is not a branch in this repo")
	}
	// The tell for a mis-parse would be git complaining about the option itself
	// (or silently accepting it); after `--` git reports it as an unknown ref.
	if errors.Is(err, ErrMergeConflict) {
		t.Fatalf("flag-like ref produced a conflict: %v", err)
	}
	if got := runGit(t, root, "status", "--porcelain"); got != "" {
		t.Fatalf("repo was modified by a flag-like ref: %q", got)
	}
}

func TestCheckedOutBranch(t *testing.T) {
	skipIfNoGit(t)
	root := initRepo(t)

	got, err := CheckedOutBranch(root)
	if err != nil {
		t.Fatalf("CheckedOutBranch: %v", err)
	}
	if got != "main" {
		t.Fatalf("CheckedOutBranch = %q, want main", got)
	}

	// A detached HEAD has no branch: the error is the answer the precondition
	// needs, because there is nothing to merge into.
	runGit(t, root, "checkout", "--detach")
	if _, err := CheckedOutBranch(root); err == nil {
		t.Fatal("CheckedOutBranch: expected an error on a detached HEAD")
	}
}

func TestLocalBaseBranch(t *testing.T) {
	skipIfNoGit(t)

	// A repo whose default branch is a plain name. No remote is configured,
	// which is deliberate: stored metadata routinely outlives the remote it
	// named, and "origin/main" must still resolve to the local "main".
	plain := initRepo(t)

	// A repo whose local default branch itself contains a slash. This is the
	// case a naive "strip everything before the first /" gets wrong: the base is
	// already local, and mangling it into "2.0" makes every merge refuse.
	slashed := initRepoWithBranch(t, "release/2.0")

	cases := []struct {
		name string
		repo string
		base string
		want string
	}{
		{"remote-qualified", plain, "origin/main", "main"},
		{"already local", plain, "main", "main"},
		{"local branch containing a slash", slashed, "release/2.0", "release/2.0"},
		{"no local branch to resolve to is left alone", plain, "origin/nonexistent", "origin/nonexistent"},
		{"unknown prefix with no local match is left alone", plain, "notaremote/thing", "notaremote/thing"},
		{"empty", plain, "", ""},
		{"whitespace is trimmed", plain, "  origin/main  ", "main"},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := LocalBaseBranch(tc.repo, tc.base); got != tc.want {
				t.Fatalf("LocalBaseBranch(%q) = %q, want %q", tc.base, got, tc.want)
			}
		})
	}
}

// TestLocalBaseBranchPrefersExactLocalBranch pins rule 1 against rule 2 in the
// one case where they disagree: a repo holding both "release/2.0" and "2.0".
// The exact match must win, or the merge lands on the wrong branch.
func TestLocalBaseBranchPrefersExactLocalBranch(t *testing.T) {
	skipIfNoGit(t)
	root := initRepoWithBranch(t, "release/2.0")
	runGit(t, root, "branch", "2.0")

	if got := LocalBaseBranch(root, "release/2.0"); got != "release/2.0" {
		t.Fatalf("LocalBaseBranch = %q, want the exact local branch release/2.0", got)
	}
}

// TestMergePreconditionAcceptsSlashedLocalBase is the end-to-end consequence of
// the case above: a repo on a slash-named default branch must be mergeable, not
// refused because its base was mangled.
func TestMergePreconditionAcceptsSlashedLocalBase(t *testing.T) {
	skipIfNoGit(t)
	root := initRepoWithBranch(t, "release/2.0")
	runGit(t, root, "checkout", "-b", "feature")
	commitFile(t, root, "feature.txt", "work\n", "feature work")
	runGit(t, root, "checkout", "release/2.0")

	base := LocalBaseBranch(root, "release/2.0")
	head, err := CheckedOutBranch(root)
	if err != nil {
		t.Fatalf("CheckedOutBranch: %v", err)
	}
	if head != base {
		t.Fatalf("precondition would refuse: HEAD %q != resolved base %q", head, base)
	}
	if err := MergeWorkspaceBranch(context.Background(), root, "feature"); err != nil {
		t.Fatalf("MergeWorkspaceBranch onto a slash-named base: %v", err)
	}
}
