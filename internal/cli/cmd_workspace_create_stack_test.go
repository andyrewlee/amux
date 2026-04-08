package cli

import (
	"bytes"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/andyrewlee/amux/internal/data"
)

func TestCmdWorkspaceCreateFromWorkspaceUsesParentTip(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not available")
	}

	home := t.TempDir()
	t.Setenv("HOME", home)

	repoRoot := filepath.Join(t.TempDir(), "repo")
	if err := os.MkdirAll(repoRoot, 0o755); err != nil {
		t.Fatalf("MkdirAll(repoRoot) error = %v", err)
	}
	runGit(t, repoRoot, "init")
	runGit(t, repoRoot, "config", "user.email", "test@example.com")
	runGit(t, repoRoot, "config", "user.name", "amux-test")
	if err := os.WriteFile(filepath.Join(repoRoot, "README.md"), []byte("hello\n"), 0o644); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}
	runGit(t, repoRoot, "add", "README.md")
	runGit(t, repoRoot, "commit", "-m", "init")

	registerProject(t, home, repoRoot)

	var out, errOut bytes.Buffer
	createParentCode := cmdWorkspaceCreate(
		&out,
		&errOut,
		GlobalFlags{JSON: true},
		[]string{"feature", "--project", repoRoot, "--assistant", "claude"},
		"test-v1",
	)
	if createParentCode != ExitOK {
		t.Fatalf("create parent code = %d; stderr: %s; stdout: %s", createParentCode, errOut.String(), out.String())
	}

	var env Envelope
	if err := json.Unmarshal(out.Bytes(), &env); err != nil {
		t.Fatalf("json.Unmarshal(create parent) error = %v", err)
	}
	parentData, ok := env.Data.(map[string]any)
	if !ok {
		t.Fatalf("expected create parent data object, got %T", env.Data)
	}
	parentID, _ := parentData["id"].(string)
	parentRoot, _ := parentData["root"].(string)
	if parentID == "" || parentRoot == "" {
		t.Fatalf("expected parent id/root, got %#v", parentData)
	}

	if err := os.WriteFile(filepath.Join(parentRoot, "child.txt"), []byte("stack child base\n"), 0o644); err != nil {
		t.Fatalf("WriteFile(parent child.txt) error = %v", err)
	}
	runGit(t, parentRoot, "add", "child.txt")
	runGit(t, parentRoot, "commit", "-m", "parent tip")
	parentCommit := runGitOutput(t, parentRoot, "rev-parse", "HEAD")

	out.Reset()
	errOut.Reset()
	createChildCode := cmdWorkspaceCreate(
		&out,
		&errOut,
		GlobalFlags{JSON: true},
		[]string{"refactor", "--from-workspace", parentID, "--assistant", "claude"},
		"test-v1",
	)
	if createChildCode != ExitOK {
		t.Fatalf("create child code = %d; stderr: %s; stdout: %s", createChildCode, errOut.String(), out.String())
	}

	env = Envelope{}
	if err := json.Unmarshal(out.Bytes(), &env); err != nil {
		t.Fatalf("json.Unmarshal(create child) error = %v", err)
	}
	childData, ok := env.Data.(map[string]any)
	if !ok {
		t.Fatalf("expected create child data object, got %T", env.Data)
	}
	if got, _ := childData["name"].(string); got != "feature.refactor" {
		t.Fatalf("child name = %q, want %q", got, "feature.refactor")
	}
	if got, _ := childData["base"].(string); got != "feature" {
		t.Fatalf("child base = %q, want %q", got, "feature")
	}
	if got, _ := childData["base_commit"].(string); got != parentCommit {
		t.Fatalf("child base_commit = %q, want %q", got, parentCommit)
	}
	if got, _ := childData["parent_workspace_id"].(string); got != parentID {
		t.Fatalf("parent_workspace_id = %q, want %q", got, parentID)
	}
	if got, _ := childData["parent_branch"].(string); got != "feature" {
		t.Fatalf("parent_branch = %q, want %q", got, "feature")
	}
	if got, _ := childData["stack_root_workspace_id"].(string); got != parentID {
		t.Fatalf("stack_root_workspace_id = %q, want %q", got, parentID)
	}
	if got, _ := childData["stack_depth"].(float64); int(got) != 1 {
		t.Fatalf("stack_depth = %v, want %d", got, 1)
	}

	childHead := runGitOutput(t, repoRoot, "rev-parse", "feature.refactor")
	if childHead != parentCommit {
		t.Fatalf("child branch head = %q, want parent commit %q", childHead, parentCommit)
	}
}

func TestCmdWorkspaceCreateReuseLegacyBaseCommitUsesMergeBase(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not available")
	}

	home := t.TempDir()
	t.Setenv("HOME", home)
	repoRoot := initRegisteredRepo(t, home)

	workspace := createWorkspaceForTest(
		t,
		"test-v1",
		"feature", "--project", repoRoot, "--assistant", "claude",
	)
	originalBaseCommit := workspace.BaseCommit
	if originalBaseCommit == "" {
		t.Fatal("expected initial base_commit to be populated")
	}

	if err := os.WriteFile(filepath.Join(repoRoot, "README.md"), []byte("hello\nmain-2\n"), 0o644); err != nil {
		t.Fatalf("WriteFile(main) error = %v", err)
	}
	runGit(t, repoRoot, "add", "README.md")
	runGit(t, repoRoot, "commit", "-m", "main 2")
	movedBaseCommit := runGitOutput(t, repoRoot, "rev-parse", "main")
	if movedBaseCommit == originalBaseCommit {
		t.Fatal("expected main to advance")
	}

	store := data.NewWorkspaceStore(filepath.Join(home, ".amux", "workspaces-metadata"))
	loaded := loadWorkspaceFromHome(t, home, workspace.ID)
	loaded.BaseCommit = ""
	if err := store.Save(loaded); err != nil {
		t.Fatalf("store.Save() error = %v", err)
	}

	reused := createWorkspaceForTest(
		t,
		"test-v1",
		"feature", "--project", repoRoot, "--assistant", "claude",
	)
	if reused.BaseCommit != originalBaseCommit {
		t.Fatalf("reused base_commit = %q, want %q", reused.BaseCommit, originalBaseCommit)
	}
	if reused.BaseCommit == movedBaseCommit {
		t.Fatalf("reused base_commit = %q, should not track moved base %q", reused.BaseCommit, movedBaseCommit)
	}
}

func TestCmdWorkspaceCreateReuseFromWorkspacePreservesStackMetadata(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not available")
	}

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
	originalBaseCommit := child.BaseCommit
	if originalBaseCommit == "" {
		t.Fatal("expected child base_commit to be populated")
	}

	if err := os.WriteFile(filepath.Join(parent.Root, "parent.txt"), []byte("parent-1\nparent-2\n"), 0o644); err != nil {
		t.Fatalf("WriteFile(parent-2) error = %v", err)
	}
	runGit(t, parent.Root, "add", "parent.txt")
	runGit(t, parent.Root, "commit", "-m", "parent 2")
	movedParentHead := runGitOutput(t, parent.Root, "rev-parse", "feature")
	if movedParentHead == originalBaseCommit {
		t.Fatal("expected parent branch to advance")
	}

	store := data.NewWorkspaceStore(filepath.Join(home, ".amux", "workspaces-metadata"))
	if err := store.Delete(data.WorkspaceID(child.ID)); err != nil {
		t.Fatalf("store.Delete(%s) error = %v", child.ID, err)
	}

	reused := createWorkspaceForTest(
		t,
		"test-v1",
		"refactor", "--from-workspace", parent.ID, "--assistant", "claude",
	)
	if reused.ID != child.ID {
		t.Fatalf("reused workspace id = %q, want %q", reused.ID, child.ID)
	}
	if reused.BaseCommit != originalBaseCommit {
		t.Fatalf("reused child base_commit = %q, want %q", reused.BaseCommit, originalBaseCommit)
	}
	if reused.BaseCommit == movedParentHead {
		t.Fatalf("reused child base_commit = %q, should not track moved parent head %q", reused.BaseCommit, movedParentHead)
	}

	reloaded := loadWorkspaceFromHome(t, home, child.ID)
	if got := string(reloaded.ParentWorkspaceID); got != parent.ID {
		t.Fatalf("reloaded parent_workspace_id = %q, want %q", got, parent.ID)
	}
	if got := reloaded.ParentBranch; got != parent.Branch {
		t.Fatalf("reloaded parent_branch = %q, want %q", got, parent.Branch)
	}
	if got := string(reloaded.StackRootWorkspaceID); got != parent.ID {
		t.Fatalf("reloaded stack_root_workspace_id = %q, want %q", got, parent.ID)
	}
	if got := reloaded.StackDepth; got != 1 {
		t.Fatalf("reloaded stack_depth = %d, want %d", got, 1)
	}
}

func TestCmdWorkspaceCreateFromWorkspaceRejectsStandaloneReuse(t *testing.T) {
	requireGit(t)

	home := t.TempDir()
	t.Setenv("HOME", home)
	repoRoot := initRegisteredRepo(t, home)

	standalone := createWorkspaceForTest(
		t,
		"test-v1",
		"feature.refactor", "--project", repoRoot, "--assistant", "claude",
	)
	parent := createWorkspaceForTest(
		t,
		"test-v1",
		"feature", "--project", repoRoot, "--assistant", "claude",
	)

	var out, errOut bytes.Buffer
	code := cmdWorkspaceCreate(
		&out,
		&errOut,
		GlobalFlags{JSON: true},
		[]string{"refactor", "--from-workspace", parent.ID, "--assistant", "claude"},
		"test-v1",
	)
	if code != ExitInternalError {
		t.Fatalf("cmdWorkspaceCreate() code = %d, want %d; stderr: %s; stdout: %s", code, ExitInternalError, errOut.String(), out.String())
	}

	var env Envelope
	if err := json.Unmarshal(out.Bytes(), &env); err != nil {
		t.Fatalf("json.Unmarshal(error) error = %v\nraw: %s", err, out.String())
	}
	if env.OK || env.Error == nil {
		t.Fatalf("expected error envelope, got %#v", env)
	}
	if env.Error.Code != "existing_workspace_check_failed" {
		t.Fatalf("error code = %q, want %q", env.Error.Code, "existing_workspace_check_failed")
	}
	if got := env.Error.Message; got == "" || !strings.Contains(got, "standalone") || !strings.Contains(got, "reparent") {
		t.Fatalf("error message = %q, want mention of standalone reuse and reparent", got)
	}

	reloaded := loadWorkspaceFromHome(t, home, standalone.ID)
	if reloaded.HasStackParent() {
		t.Fatalf("standalone workspace unexpectedly gained parent %q", reloaded.ParentWorkspaceID)
	}
}

func TestCmdWorkspaceCreateStandaloneRejectsStackedReuse(t *testing.T) {
	requireGit(t)

	home := t.TempDir()
	t.Setenv("HOME", home)
	repoRoot := initRegisteredRepo(t, home)

	parent := createWorkspaceForTest(
		t,
		"test-v1",
		"feature", "--project", repoRoot, "--assistant", "claude",
	)
	child := createWorkspaceForTest(
		t,
		"test-v1",
		"refactor", "--from-workspace", parent.ID, "--assistant", "claude",
	)

	var out, errOut bytes.Buffer
	code := cmdWorkspaceCreate(
		&out,
		&errOut,
		GlobalFlags{JSON: true},
		[]string{child.Name, "--project", repoRoot, "--assistant", "claude"},
		"test-v1",
	)
	if code != ExitInternalError {
		t.Fatalf("cmdWorkspaceCreate() code = %d, want %d; stderr: %s; stdout: %s", code, ExitInternalError, errOut.String(), out.String())
	}

	var env Envelope
	if err := json.Unmarshal(out.Bytes(), &env); err != nil {
		t.Fatalf("json.Unmarshal(error) error = %v\nraw: %s", err, out.String())
	}
	if env.OK || env.Error == nil {
		t.Fatalf("expected error envelope, got %#v", env)
	}
	if env.Error.Code != "existing_workspace_check_failed" {
		t.Fatalf("error code = %q, want %q", env.Error.Code, "existing_workspace_check_failed")
	}
	if got := env.Error.Message; got == "" || !strings.Contains(got, "standalone workspace") || !strings.Contains(got, "reparent") {
		t.Fatalf("error message = %q, want mention of standalone reuse rejection and reparent", got)
	}

	reloaded := loadWorkspaceFromHome(t, home, child.ID)
	if got := string(reloaded.ParentWorkspaceID); got != parent.ID {
		t.Fatalf("reloaded parent_workspace_id = %q, want %q", got, parent.ID)
	}
}

func TestCmdWorkspaceCreateReuseDetachedWorkspaceFails(t *testing.T) {
	requireGit(t)

	home := t.TempDir()
	t.Setenv("HOME", home)
	repoRoot := initRegisteredRepo(t, home)

	workspace := createWorkspaceForTest(
		t,
		"test-v1",
		"feature", "--project", repoRoot, "--assistant", "claude",
	)
	runGit(t, workspace.Root, "checkout", "--detach", "HEAD")

	var out, errOut bytes.Buffer
	code := cmdWorkspaceCreate(
		&out,
		&errOut,
		GlobalFlags{JSON: true},
		[]string{"feature", "--project", repoRoot, "--assistant", "claude"},
		"test-v1",
	)
	if code != ExitInternalError {
		t.Fatalf("cmdWorkspaceCreate() code = %d, want %d; stderr: %s; stdout: %s", code, ExitInternalError, errOut.String(), out.String())
	}

	var env Envelope
	if err := json.Unmarshal(out.Bytes(), &env); err != nil {
		t.Fatalf("json.Unmarshal(error) error = %v\nraw: %s", err, out.String())
	}
	if env.OK || env.Error == nil {
		t.Fatalf("expected error envelope, got %#v", env)
	}
	if env.Error.Code != "existing_workspace_check_failed" {
		t.Fatalf("error code = %q, want %q", env.Error.Code, "existing_workspace_check_failed")
	}
	if got := env.Error.Message; !strings.Contains(got, "detached HEAD") {
		t.Fatalf("error message = %q, want detached HEAD mention", got)
	}
}
