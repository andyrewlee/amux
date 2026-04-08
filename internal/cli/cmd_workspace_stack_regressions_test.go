package cli

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"

	"github.com/andyrewlee/amux/internal/data"
)

func TestCmdWorkspaceRestackIgnoresUnrelatedBrokenWorkspaceSnapshots(t *testing.T) {
	requireGit(t)

	home := t.TempDir()
	t.Setenv("HOME", home)
	repoRoot := initRegisteredRepo(t, home)

	parent := createWorkspaceForTest(
		t,
		"test-v1",
		"feature", "--project", repoRoot, "--assistant", "claude",
	)
	if err := os.WriteFile(filepath.Join(parent.Root, "parent.txt"), []byte("parent-1\n"), 0o644); err != nil {
		t.Fatalf("WriteFile(parent-1) error = %v", err)
	}
	runGit(t, parent.Root, "add", "parent.txt")
	runGit(t, parent.Root, "commit", "-m", "parent 1")

	child := createWorkspaceForTest(
		t,
		"test-v1",
		"refactor", "--from-workspace", parent.ID, "--assistant", "claude",
	)
	if err := os.WriteFile(filepath.Join(child.Root, "child.txt"), []byte("child-1\n"), 0o644); err != nil {
		t.Fatalf("WriteFile(child-1) error = %v", err)
	}
	runGit(t, child.Root, "add", "child.txt")
	runGit(t, child.Root, "commit", "-m", "child 1")

	unrelated := createWorkspaceForTest(
		t,
		"test-v1",
		"side", "--project", repoRoot, "--assistant", "claude",
	)
	if err := os.RemoveAll(unrelated.Root); err != nil {
		t.Fatalf("RemoveAll(unrelated.Root) error = %v", err)
	}

	if err := os.WriteFile(filepath.Join(parent.Root, "parent.txt"), []byte("parent-1\nparent-2\n"), 0o644); err != nil {
		t.Fatalf("WriteFile(parent-2) error = %v", err)
	}
	runGit(t, parent.Root, "add", "parent.txt")
	runGit(t, parent.Root, "commit", "-m", "parent 2")

	var out, errOut bytes.Buffer
	code := cmdWorkspaceRestack(
		&out,
		&errOut,
		GlobalFlags{JSON: true},
		[]string{child.ID},
		"test-v1",
	)
	if code != ExitOK {
		t.Fatalf("cmdWorkspaceRestack() code = %d; stderr: %s; stdout: %s", code, errOut.String(), out.String())
	}

	parentHead := runGitOutput(t, repoRoot, "rev-parse", parent.Branch)
	if got := runGitOutput(t, repoRoot, "merge-base", child.Branch, parent.Branch); got != parentHead {
		t.Fatalf("child merge-base = %q, want parent head %q", got, parentHead)
	}
}

func TestCmdWorkspaceReparentOrphanedChildUsesStoredBaseCommit(t *testing.T) {
	requireGit(t)

	home := t.TempDir()
	t.Setenv("HOME", home)
	repoRoot := initRegisteredRepo(t, home)

	left := createWorkspaceForTest(
		t,
		"test-v1",
		"feature", "--project", repoRoot, "--assistant", "claude",
	)
	if err := os.WriteFile(filepath.Join(left.Root, "left.txt"), []byte("left-1\n"), 0o644); err != nil {
		t.Fatalf("WriteFile(left-1) error = %v", err)
	}
	runGit(t, left.Root, "add", "left.txt")
	runGit(t, left.Root, "commit", "-m", "left 1")
	leftHead := runGitOutput(t, repoRoot, "rev-parse", left.Branch)

	right := createWorkspaceForTest(
		t,
		"test-v1",
		"alt", "--project", repoRoot, "--assistant", "claude",
	)
	if err := os.WriteFile(filepath.Join(right.Root, "right.txt"), []byte("right-1\n"), 0o644); err != nil {
		t.Fatalf("WriteFile(right-1) error = %v", err)
	}
	runGit(t, right.Root, "add", "right.txt")
	runGit(t, right.Root, "commit", "-m", "right 1")

	child := createWorkspaceForTest(
		t,
		"test-v1",
		"refactor", "--from-workspace", left.ID, "--assistant", "claude",
	)
	if child.BaseCommit != leftHead {
		t.Fatalf("child base_commit = %q, want %q before orphaning", child.BaseCommit, leftHead)
	}
	if err := os.WriteFile(filepath.Join(child.Root, "child.txt"), []byte("child-1\n"), 0o644); err != nil {
		t.Fatalf("WriteFile(child-1) error = %v", err)
	}
	runGit(t, child.Root, "add", "child.txt")
	runGit(t, child.Root, "commit", "-m", "child 1")

	store := data.NewWorkspaceStore(filepath.Join(home, ".amux", "workspaces-metadata"))
	if err := store.Delete(data.WorkspaceID(left.ID)); err != nil {
		t.Fatalf("store.Delete(%s) error = %v", left.ID, err)
	}

	var out, errOut bytes.Buffer
	code := cmdWorkspaceReparent(
		&out,
		&errOut,
		GlobalFlags{JSON: true},
		[]string{child.ID, "--parent", right.ID},
		"test-v1",
	)
	if code != ExitOK {
		t.Fatalf("cmdWorkspaceReparent() code = %d; stderr: %s; stdout: %s", code, errOut.String(), out.String())
	}

	rightHead := runGitOutput(t, repoRoot, "rev-parse", right.Branch)
	if got := runGitOutput(t, repoRoot, "merge-base", child.Branch, right.Branch); got != rightHead {
		t.Fatalf("child merge-base = %q, want right head %q", got, rightHead)
	}
	if got := runGitOutput(t, repoRoot, "log", "--format=%s", right.Branch+".."+child.Branch); got != "child 1" {
		t.Fatalf("child commits ahead of right = %q, want only child commit", got)
	}

	loadedChild := loadWorkspaceFromHome(t, home, child.ID)
	if loadedChild.ParentWorkspaceID != data.WorkspaceID(right.ID) {
		t.Fatalf("child parent_workspace_id = %q, want %q", loadedChild.ParentWorkspaceID, right.ID)
	}
	if loadedChild.BaseCommit != rightHead {
		t.Fatalf("child base_commit = %q, want %q", loadedChild.BaseCommit, rightHead)
	}
}

func TestCmdWorkspaceRestackPrefersStoredBaseCommitForRewrittenParent(t *testing.T) {
	requireGit(t)

	home := t.TempDir()
	t.Setenv("HOME", home)
	repoRoot := initRegisteredRepo(t, home)

	parent := createWorkspaceForTest(
		t,
		"test-v1",
		"feature", "--project", repoRoot, "--assistant", "claude",
	)
	if err := os.WriteFile(filepath.Join(parent.Root, "parent.txt"), []byte("parent-1\n"), 0o644); err != nil {
		t.Fatalf("WriteFile(parent-1) error = %v", err)
	}
	runGit(t, parent.Root, "add", "parent.txt")
	runGit(t, parent.Root, "commit", "-m", "parent 1")
	originalParentHead := runGitOutput(t, repoRoot, "rev-parse", parent.Branch)

	child := createWorkspaceForTest(
		t,
		"test-v1",
		"refactor", "--from-workspace", parent.ID, "--assistant", "claude",
	)
	if child.BaseCommit != originalParentHead {
		t.Fatalf("child base_commit = %q, want original parent head %q", child.BaseCommit, originalParentHead)
	}
	if err := os.WriteFile(filepath.Join(child.Root, "child.txt"), []byte("child-1\n"), 0o644); err != nil {
		t.Fatalf("WriteFile(child-1) error = %v", err)
	}
	runGit(t, child.Root, "add", "child.txt")
	runGit(t, child.Root, "commit", "-m", "child 1")

	if err := os.WriteFile(filepath.Join(parent.Root, "parent.txt"), []byte("parent-rewritten\n"), 0o644); err != nil {
		t.Fatalf("WriteFile(parent rewritten) error = %v", err)
	}
	runGit(t, parent.Root, "add", "parent.txt")
	runGit(t, parent.Root, "commit", "--amend", "-m", "parent rewritten")
	rewrittenParentHead := runGitOutput(t, repoRoot, "rev-parse", parent.Branch)
	if rewrittenParentHead == originalParentHead {
		t.Fatal("expected parent branch head to change after amend")
	}

	var out, errOut bytes.Buffer
	code := cmdWorkspaceRestack(
		&out,
		&errOut,
		GlobalFlags{JSON: true},
		[]string{child.ID},
		"test-v1",
	)
	if code != ExitOK {
		t.Fatalf("cmdWorkspaceRestack() code = %d; stderr: %s; stdout: %s", code, errOut.String(), out.String())
	}

	if got := runGitOutput(t, repoRoot, "merge-base", child.Branch, parent.Branch); got != rewrittenParentHead {
		t.Fatalf("child merge-base = %q, want rewritten parent head %q", got, rewrittenParentHead)
	}
	if got := runGitOutput(t, repoRoot, "log", "--format=%s", parent.Branch+".."+child.Branch); got != "child 1" {
		t.Fatalf("child commits ahead of parent = %q, want only child commit", got)
	}

	loadedChild := loadWorkspaceFromHome(t, home, child.ID)
	if loadedChild.BaseCommit != rewrittenParentHead {
		t.Fatalf("child base_commit = %q, want %q", loadedChild.BaseCommit, rewrittenParentHead)
	}
}

func TestCmdWorkspaceRestackPrefersStoredBaseCommitForRewrittenRepoBase(t *testing.T) {
	requireGit(t)

	home := t.TempDir()
	t.Setenv("HOME", home)
	repoRoot := initRegisteredRepo(t, home)

	if err := os.WriteFile(filepath.Join(repoRoot, "base.txt"), []byte("base-1\n"), 0o644); err != nil {
		t.Fatalf("WriteFile(base-1) error = %v", err)
	}
	runGit(t, repoRoot, "add", "base.txt")
	runGit(t, repoRoot, "commit", "-m", "base 1")
	originalBaseHead := runGitOutput(t, repoRoot, "rev-parse", "main")

	workspace := createWorkspaceForTest(
		t,
		"test-v1",
		"feature", "--project", repoRoot, "--assistant", "claude",
	)
	if workspace.BaseCommit != originalBaseHead {
		t.Fatalf("workspace base_commit = %q, want original main head %q", workspace.BaseCommit, originalBaseHead)
	}
	if err := os.WriteFile(filepath.Join(workspace.Root, "feature.txt"), []byte("feature-1\n"), 0o644); err != nil {
		t.Fatalf("WriteFile(feature-1) error = %v", err)
	}
	runGit(t, workspace.Root, "add", "feature.txt")
	runGit(t, workspace.Root, "commit", "-m", "feature 1")

	if err := os.WriteFile(filepath.Join(repoRoot, "base.txt"), []byte("base-rewritten\n"), 0o644); err != nil {
		t.Fatalf("WriteFile(base rewritten) error = %v", err)
	}
	runGit(t, repoRoot, "add", "base.txt")
	runGit(t, repoRoot, "commit", "--amend", "-m", "base rewritten")
	rewrittenBaseHead := runGitOutput(t, repoRoot, "rev-parse", "main")
	if rewrittenBaseHead == originalBaseHead {
		t.Fatal("expected main head to change after amend")
	}

	var out, errOut bytes.Buffer
	code := cmdWorkspaceRestack(
		&out,
		&errOut,
		GlobalFlags{JSON: true},
		[]string{workspace.ID},
		"test-v1",
	)
	if code != ExitOK {
		t.Fatalf("cmdWorkspaceRestack() code = %d; stderr: %s; stdout: %s", code, errOut.String(), out.String())
	}

	if got := runGitOutput(t, repoRoot, "merge-base", workspace.Branch, "main"); got != rewrittenBaseHead {
		t.Fatalf("workspace merge-base = %q, want rewritten main head %q", got, rewrittenBaseHead)
	}
	if got := runGitOutput(t, repoRoot, "log", "--format=%s", "main.."+workspace.Branch); got != "feature 1" {
		t.Fatalf("workspace commits ahead of main = %q, want only feature commit", got)
	}

	loadedWorkspace := loadWorkspaceFromHome(t, home, workspace.ID)
	if loadedWorkspace.BaseCommit != rewrittenBaseHead {
		t.Fatalf("workspace base_commit = %q, want %q", loadedWorkspace.BaseCommit, rewrittenBaseHead)
	}
}

func TestResolveWorkspaceRootBaseRefResolvesHeadToDetectedBase(t *testing.T) {
	requireGit(t)

	home := t.TempDir()
	t.Setenv("HOME", home)
	repoRoot := initRegisteredRepo(t, home)

	if got := resolveWorkspaceRootBaseRef(repoRoot, "HEAD"); got != "main" {
		t.Fatalf("resolveWorkspaceRootBaseRef(HEAD) = %q, want %q", got, "main")
	}
}

func TestResolveWorkspaceRootBaseRefRejectsHeadWithoutStableBase(t *testing.T) {
	requireGit(t)

	repoRoot := filepath.Join(t.TempDir(), "repo")
	if err := os.MkdirAll(repoRoot, 0o755); err != nil {
		t.Fatalf("MkdirAll(repoRoot) error = %v", err)
	}
	runGit(t, repoRoot, "init", "-b", "trunk")
	runGit(t, repoRoot, "config", "user.email", "test@example.com")
	runGit(t, repoRoot, "config", "user.name", "amux-test")
	if err := os.WriteFile(filepath.Join(repoRoot, "README.md"), []byte("hello\n"), 0o644); err != nil {
		t.Fatalf("WriteFile(README.md) error = %v", err)
	}
	runGit(t, repoRoot, "add", "README.md")
	runGit(t, repoRoot, "commit", "-m", "init")
	runGit(t, repoRoot, "checkout", "--detach", "HEAD")

	if got := resolveWorkspaceRootBaseRef(repoRoot, "HEAD"); got != "" {
		t.Fatalf("resolveWorkspaceRootBaseRef(HEAD) = %q, want empty unresolved base", got)
	}
}
