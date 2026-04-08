package cli

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/andyrewlee/amux/internal/data"
)

func TestCmdWorkspaceRestackRecursive(t *testing.T) {
	requireGit(t)

	home := t.TempDir()
	t.Setenv("HOME", home)
	repoRoot := initRegisteredRepo(t, home)

	parent := createWorkspaceForTest(
		t,
		"test-v1",
		"feature", "--project", repoRoot, "--assistant", "claude",
	)
	if got := parent.BaseCommit; got == "" {
		t.Fatalf("parent base_commit empty")
	}
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

	grandchild := createWorkspaceForTest(
		t,
		"test-v1",
		"api", "--from-workspace", child.ID, "--assistant", "claude",
	)
	if err := os.WriteFile(filepath.Join(grandchild.Root, "grandchild.txt"), []byte("grandchild-1\n"), 0o644); err != nil {
		t.Fatalf("WriteFile(grandchild-1) error = %v", err)
	}
	runGit(t, grandchild.Root, "add", "grandchild.txt")
	runGit(t, grandchild.Root, "commit", "-m", "grandchild 1")

	if err := os.WriteFile(filepath.Join(parent.Root, "parent.txt"), []byte("parent-2\n"), 0o644); err != nil {
		t.Fatalf("WriteFile(parent-2) error = %v", err)
	}
	runGit(t, parent.Root, "add", "parent.txt")
	runGit(t, parent.Root, "commit", "-m", "parent 2")

	oldChildHead := runGitOutput(t, repoRoot, "rev-parse", child.Branch)
	oldGrandchildHead := runGitOutput(t, repoRoot, "rev-parse", grandchild.Branch)

	var out, errOut bytes.Buffer
	code := cmdWorkspaceRestack(
		&out,
		&errOut,
		GlobalFlags{JSON: true},
		[]string{child.ID, "--recursive"},
		"test-v1",
	)
	if code != ExitOK {
		t.Fatalf("cmdWorkspaceRestack() code = %d; stderr: %s; stdout: %s", code, errOut.String(), out.String())
	}

	var env Envelope
	if err := json.Unmarshal(out.Bytes(), &env); err != nil {
		t.Fatalf("json.Unmarshal() error = %v\nraw: %s", err, out.String())
	}
	payload, ok := env.Data.(map[string]any)
	if !ok {
		t.Fatalf("expected data object, got %T", env.Data)
	}
	restacked, ok := payload["restacked"].([]any)
	if !ok || len(restacked) != 2 {
		t.Fatalf("expected 2 restacked workspaces, got %#v", payload["restacked"])
	}

	parentHead := runGitOutput(t, repoRoot, "rev-parse", parent.Branch)
	childHead := runGitOutput(t, repoRoot, "rev-parse", child.Branch)
	grandchildHead := runGitOutput(t, repoRoot, "rev-parse", grandchild.Branch)
	if childHead == oldChildHead {
		t.Fatalf("child head did not change after restack")
	}
	if grandchildHead == oldGrandchildHead {
		t.Fatalf("grandchild head did not change after recursive restack")
	}
	if got := runGitOutput(t, repoRoot, "merge-base", child.Branch, parent.Branch); got != parentHead {
		t.Fatalf("child merge-base = %q, want parent head %q", got, parentHead)
	}
	if got := runGitOutput(t, repoRoot, "merge-base", grandchild.Branch, child.Branch); got != childHead {
		t.Fatalf("grandchild merge-base = %q, want child head %q", got, childHead)
	}

	loadedChild := loadWorkspaceFromHome(t, home, child.ID)
	if loadedChild.BaseCommit != parentHead {
		t.Fatalf("child base_commit = %q, want %q", loadedChild.BaseCommit, parentHead)
	}
	loadedGrandchild := loadWorkspaceFromHome(t, home, grandchild.ID)
	if loadedGrandchild.BaseCommit != childHead {
		t.Fatalf("grandchild base_commit = %q, want %q", loadedGrandchild.BaseCommit, childHead)
	}
}

func TestCmdWorkspaceReparentUpdatesDescendants(t *testing.T) {
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
	if err := os.WriteFile(filepath.Join(child.Root, "child.txt"), []byte("child-1\n"), 0o644); err != nil {
		t.Fatalf("WriteFile(child-1) error = %v", err)
	}
	runGit(t, child.Root, "add", "child.txt")
	runGit(t, child.Root, "commit", "-m", "child 1")

	grandchild := createWorkspaceForTest(
		t,
		"test-v1",
		"api", "--from-workspace", child.ID, "--assistant", "claude",
	)
	if err := os.WriteFile(filepath.Join(grandchild.Root, "grandchild.txt"), []byte("grandchild-1\n"), 0o644); err != nil {
		t.Fatalf("WriteFile(grandchild-1) error = %v", err)
	}
	runGit(t, grandchild.Root, "add", "grandchild.txt")
	runGit(t, grandchild.Root, "commit", "-m", "grandchild 1")

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
	childHead := runGitOutput(t, repoRoot, "rev-parse", child.Branch)
	if got := runGitOutput(t, repoRoot, "merge-base", child.Branch, right.Branch); got != rightHead {
		t.Fatalf("child merge-base = %q, want right head %q", got, rightHead)
	}
	if got := runGitOutput(t, repoRoot, "merge-base", grandchild.Branch, child.Branch); got != childHead {
		t.Fatalf("grandchild merge-base = %q, want child head %q", got, childHead)
	}

	loadedChild := loadWorkspaceFromHome(t, home, child.ID)
	if loadedChild.ParentWorkspaceID != data.WorkspaceID(right.ID) {
		t.Fatalf("child parent_workspace_id = %q, want %q", loadedChild.ParentWorkspaceID, right.ID)
	}
	if loadedChild.StackRootWorkspaceID != data.WorkspaceID(right.ID) {
		t.Fatalf("child stack_root_workspace_id = %q, want %q", loadedChild.StackRootWorkspaceID, right.ID)
	}
	if loadedChild.StackDepth != 1 {
		t.Fatalf("child stack_depth = %d, want %d", loadedChild.StackDepth, 1)
	}
	if loadedChild.BaseCommit != rightHead {
		t.Fatalf("child base_commit = %q, want %q", loadedChild.BaseCommit, rightHead)
	}

	loadedGrandchild := loadWorkspaceFromHome(t, home, grandchild.ID)
	if loadedGrandchild.StackRootWorkspaceID != data.WorkspaceID(right.ID) {
		t.Fatalf("grandchild stack_root_workspace_id = %q, want %q", loadedGrandchild.StackRootWorkspaceID, right.ID)
	}
	if loadedGrandchild.StackDepth != 2 {
		t.Fatalf("grandchild stack_depth = %d, want %d", loadedGrandchild.StackDepth, 2)
	}
	if loadedGrandchild.BaseCommit != childHead {
		t.Fatalf("grandchild base_commit = %q, want %q", loadedGrandchild.BaseCommit, childHead)
	}
}

func TestCmdWorkspaceRestackRecomputesStaleBaseCommitAfterManualRebase(t *testing.T) {
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
	originalStoredBaseCommit := child.BaseCommit
	if originalStoredBaseCommit == "" {
		t.Fatal("expected child base_commit to be populated")
	}
	if err := os.WriteFile(filepath.Join(child.Root, "child.txt"), []byte("child-1\n"), 0o644); err != nil {
		t.Fatalf("WriteFile(child-1) error = %v", err)
	}
	runGit(t, child.Root, "add", "child.txt")
	runGit(t, child.Root, "commit", "-m", "child 1")

	if err := os.WriteFile(filepath.Join(parent.Root, "parent.txt"), []byte("parent-1\nparent-2\n"), 0o644); err != nil {
		t.Fatalf("WriteFile(parent-2) error = %v", err)
	}
	runGit(t, parent.Root, "add", "parent.txt")
	runGit(t, parent.Root, "commit", "-m", "parent 2")
	parentHead := runGitOutput(t, repoRoot, "rev-parse", parent.Branch)

	runGit(t, child.Root, "rebase", parent.Branch)
	manuallyRebasedChildHead := runGitOutput(t, repoRoot, "rev-parse", child.Branch)
	if manuallyRebasedChildHead == "" {
		t.Fatal("expected child head after manual rebase")
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

	if got := runGitOutput(t, repoRoot, "rev-parse", child.Branch); got != manuallyRebasedChildHead {
		t.Fatalf("child head after restack = %q, want unchanged %q", got, manuallyRebasedChildHead)
	}
	loadedChild := loadWorkspaceFromHome(t, home, child.ID)
	if loadedChild.BaseCommit != parentHead {
		t.Fatalf("child base_commit = %q, want %q", loadedChild.BaseCommit, parentHead)
	}
	if loadedChild.BaseCommit == originalStoredBaseCommit {
		t.Fatalf("child base_commit remained stale at %q", loadedChild.BaseCommit)
	}
}

func TestCmdWorkspaceReparentRollsBackEarlierDescendantsOnConflict(t *testing.T) {
	requireGit(t)

	home := t.TempDir()
	t.Setenv("HOME", home)
	repoRoot := initRegisteredRepo(t, home)

	if err := os.WriteFile(filepath.Join(repoRoot, "shared.txt"), []byte("base\n"), 0o644); err != nil {
		t.Fatalf("WriteFile(shared.txt) error = %v", err)
	}
	runGit(t, repoRoot, "add", "shared.txt")
	runGit(t, repoRoot, "commit", "-m", "add shared file")

	left := createWorkspaceForTest(
		t,
		"test-v1",
		"feature", "--project", repoRoot, "--assistant", "claude",
	)
	if err := os.WriteFile(filepath.Join(left.Root, "shared.txt"), []byte("left\n"), 0o644); err != nil {
		t.Fatalf("WriteFile(left shared.txt) error = %v", err)
	}
	runGit(t, left.Root, "add", "shared.txt")
	runGit(t, left.Root, "commit", "-m", "left shared")
	leftHead := runGitOutput(t, repoRoot, "rev-parse", left.Branch)

	right := createWorkspaceForTest(
		t,
		"test-v1",
		"alt", "--project", repoRoot, "--assistant", "claude",
	)
	if err := os.WriteFile(filepath.Join(right.Root, "shared.txt"), []byte("right\n"), 0o644); err != nil {
		t.Fatalf("WriteFile(right shared.txt) error = %v", err)
	}
	runGit(t, right.Root, "add", "shared.txt")
	runGit(t, right.Root, "commit", "-m", "right shared")

	child := createWorkspaceForTest(
		t,
		"test-v1",
		"refactor", "--from-workspace", left.ID, "--assistant", "claude",
	)
	if err := os.WriteFile(filepath.Join(child.Root, "child.txt"), []byte("child\n"), 0o644); err != nil {
		t.Fatalf("WriteFile(child.txt) error = %v", err)
	}
	runGit(t, child.Root, "add", "child.txt")
	runGit(t, child.Root, "commit", "-m", "child change")

	grandchild := createWorkspaceForTest(
		t,
		"test-v1",
		"api", "--from-workspace", child.ID, "--assistant", "claude",
	)
	if err := os.WriteFile(filepath.Join(grandchild.Root, "shared.txt"), []byte("grandchild\n"), 0o644); err != nil {
		t.Fatalf("WriteFile(grandchild shared.txt) error = %v", err)
	}
	runGit(t, grandchild.Root, "add", "shared.txt")
	runGit(t, grandchild.Root, "commit", "-m", "grandchild shared")

	oldChildHead := runGitOutput(t, repoRoot, "rev-parse", child.Branch)
	oldGrandchildHead := runGitOutput(t, repoRoot, "rev-parse", grandchild.Branch)

	var out, errOut bytes.Buffer
	code := cmdWorkspaceReparent(
		&out,
		&errOut,
		GlobalFlags{JSON: true},
		[]string{child.ID, "--parent", right.ID},
		"test-v1",
	)
	if code != ExitInternalError {
		t.Fatalf("cmdWorkspaceReparent() code = %d, want %d; stderr: %s; stdout: %s", code, ExitInternalError, errOut.String(), out.String())
	}

	var env Envelope
	if err := json.Unmarshal(out.Bytes(), &env); err != nil {
		t.Fatalf("json.Unmarshal(error) error = %v\nraw: %s", err, out.String())
	}
	if env.OK || env.Error == nil {
		t.Fatalf("expected error envelope, got %#v", env)
	}
	if env.Error.Code != "reparent_failed" {
		t.Fatalf("error code = %q, want %q", env.Error.Code, "reparent_failed")
	}

	if got := runGitOutput(t, repoRoot, "rev-parse", child.Branch); got != oldChildHead {
		t.Fatalf("child head = %q, want rollback to %q", got, oldChildHead)
	}
	if got := runGitOutput(t, repoRoot, "rev-parse", grandchild.Branch); got != oldGrandchildHead {
		t.Fatalf("grandchild head = %q, want rollback to %q", got, oldGrandchildHead)
	}
	if got := runGitOutput(t, repoRoot, "merge-base", child.Branch, left.Branch); got != leftHead {
		t.Fatalf("child merge-base after rollback = %q, want left head %q", got, leftHead)
	}
	if got := runGitOutput(t, child.Root, "status", "--short"); got != "" {
		t.Fatalf("child git status after rollback = %q, want clean", got)
	}
	if got := runGitOutput(t, grandchild.Root, "status", "--short"); got != "" {
		t.Fatalf("grandchild git status after rollback = %q, want clean", got)
	}

	loadedChild := loadWorkspaceFromHome(t, home, child.ID)
	if loadedChild.ParentWorkspaceID != data.WorkspaceID(left.ID) {
		t.Fatalf("child parent_workspace_id = %q, want %q", loadedChild.ParentWorkspaceID, left.ID)
	}
	if loadedChild.BaseCommit != leftHead {
		t.Fatalf("child base_commit = %q, want %q", loadedChild.BaseCommit, leftHead)
	}

	loadedGrandchild := loadWorkspaceFromHome(t, home, grandchild.ID)
	if loadedGrandchild.ParentWorkspaceID != data.WorkspaceID(child.ID) {
		t.Fatalf("grandchild parent_workspace_id = %q, want %q", loadedGrandchild.ParentWorkspaceID, child.ID)
	}
	if loadedGrandchild.BaseCommit != oldChildHead {
		t.Fatalf("grandchild base_commit = %q, want %q", loadedGrandchild.BaseCommit, oldChildHead)
	}
}
