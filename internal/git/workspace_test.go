package git

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestParseWorktreeList(t *testing.T) {
	tests := []struct {
		name     string
		output   string
		repoPath string
		want     int // number of workspaces expected
		wantBare bool
	}{
		{
			name:     "empty output",
			output:   "",
			repoPath: "/repo",
			want:     0,
		},
		{
			name: "single worktree",
			output: `worktree /home/user/myrepo
HEAD abc123
branch refs/heads/main

`,
			repoPath: "/home/user/myrepo",
			want:     1,
		},
		{
			name: "multiple worktrees",
			output: `worktree /home/user/myrepo
HEAD abc123
branch refs/heads/main

worktree /home/user/.amux/workspaces/myrepo/feature
HEAD def456
branch refs/heads/feature

`,
			repoPath: "/home/user/myrepo",
			want:     2,
		},
		{
			name: "bare repository filtered out",
			output: `worktree /home/user/myrepo.git
bare

worktree /home/user/.amux/workspaces/myrepo/feature
HEAD def456
branch refs/heads/feature

`,
			repoPath: "/home/user/myrepo.git",
			want:     1, // bare entry should be filtered
		},
		{
			name: "detached HEAD worktree",
			output: `worktree /home/user/myrepo
HEAD abc123
branch refs/heads/main

worktree /home/user/.amux/workspaces/myrepo/detached
HEAD def456
detached

`,
			repoPath: "/home/user/myrepo",
			want:     2, // detached worktree should be included
		},
		{
			name: "no trailing newline",
			output: `worktree /home/user/myrepo
HEAD abc123
branch refs/heads/main`,
			repoPath: "/home/user/myrepo",
			want:     1,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			workspaces := parseWorktreeList(tt.output, tt.repoPath)
			if len(workspaces) != tt.want {
				t.Errorf("parseWorktreeList() returned %d workspaces, want %d", len(workspaces), tt.want)
			}
		})
	}
}

func TestParseWorktreeList_Fields(t *testing.T) {
	output := `worktree /home/user/myrepo
HEAD abc123
branch refs/heads/main

worktree /home/user/.amux/workspaces/myrepo/feature-branch
HEAD def456
branch refs/heads/feature-branch

`
	workspaces := parseWorktreeList(output, "/home/user/myrepo")

	if len(workspaces) != 2 {
		t.Fatalf("expected 2 workspaces, got %d", len(workspaces))
	}

	// Check first workspace (primary)
	if workspaces[0].Root != "/home/user/myrepo" {
		t.Errorf("ws[0].Root = %q, want %q", workspaces[0].Root, "/home/user/myrepo")
	}
	if workspaces[0].Branch != "main" {
		t.Errorf("ws[0].Branch = %q, want %q", workspaces[0].Branch, "main")
	}
	if workspaces[0].Name != "myrepo" {
		t.Errorf("ws[0].Name = %q, want %q", workspaces[0].Name, "myrepo")
	}
	if workspaces[0].Repo != "/home/user/myrepo" {
		t.Errorf("ws[0].Repo = %q, want %q", workspaces[0].Repo, "/home/user/myrepo")
	}

	// Check second workspace (worktree)
	if workspaces[1].Root != "/home/user/.amux/workspaces/myrepo/feature-branch" {
		t.Errorf("ws[1].Root = %q, want %q", workspaces[1].Root, "/home/user/.amux/workspaces/myrepo/feature-branch")
	}
	if workspaces[1].Branch != "feature-branch" {
		t.Errorf("ws[1].Branch = %q, want %q", workspaces[1].Branch, "feature-branch")
	}
	if workspaces[1].Name != "feature-branch" {
		t.Errorf("ws[1].Name = %q, want %q", workspaces[1].Name, "feature-branch")
	}
}

func TestIsBranchAlreadyExistsError(t *testing.T) {
	gitErr := func(stderr string) error {
		return &Error{Command: "worktree add", ExitCode: 128, Stderr: stderr, Err: errors.New("exit status 128")}
	}
	err := gitErr("fatal: a branch named 'feature-a' already exists")
	if !isBranchAlreadyExistsError(err, "feature-a") {
		t.Fatalf("expected branch already exists error to match")
	}
	if !isBranchAlreadyExistsError(gitErr("fatal: a branch named `feature-a` already exists"), "feature-a") {
		t.Fatalf("expected backtick-quoted branch error to match after normalization")
	}
	if isBranchAlreadyExistsError(err, "feature-b") {
		t.Fatalf("expected non-matching branch name to return false")
	}
	if isBranchAlreadyExistsError(gitErr("fatal: branch lock failed"), "feature-a") {
		t.Fatalf("expected unrelated branch error to return false")
	}
	if isBranchAlreadyExistsError(gitErr("fatal: already exists"), "") {
		t.Fatalf("expected empty branch name to return false")
	}
	// Unstructured errors never classify: the branch-exists decision flows
	// through the structured Error's stderr/exit code only.
	if isBranchAlreadyExistsError(errors.New("fatal: a branch named 'feature-a' already exists"), "feature-a") {
		t.Fatalf("expected plain (non-*Error) error to return false")
	}
	// The branch name appearing only in the command line must not match.
	if isBranchAlreadyExistsError(&Error{Command: "worktree add -b feature-a", ExitCode: 128, Stderr: "fatal: permission denied"}, "feature-a") {
		t.Fatalf("expected command-line-only match to return false")
	}
}

func TestCreateWorkspace_RetryUsesFreshContext(t *testing.T) {
	origRunGitCtx := runGitCtx
	defer func() {
		runGitCtx = origRunGitCtx
	}()

	var firstCtx context.Context
	call := 0
	runGitCtx = func(ctx context.Context, _ string, args ...string) (string, error) {
		call++
		switch call {
		case 1:
			firstCtx = ctx
			if got, want := strings.Join(args, " "), "worktree add -b feature-ws /tmp/ws HEAD"; got != want {
				t.Fatalf("first call args = %q, want %q", got, want)
			}
			return "", &Error{Command: "worktree add", ExitCode: 128, Stderr: "fatal: a branch named 'feature-ws' already exists", Err: errors.New("exit status 128")}
		case 2:
			if firstCtx == nil {
				t.Fatalf("expected first context to be captured")
			}
			if ctx == firstCtx {
				t.Fatalf("expected retry to use a fresh context")
			}
			if got, want := strings.Join(args, " "), "worktree add /tmp/ws feature-ws"; got != want {
				t.Fatalf("retry args = %q, want %q", got, want)
			}
			return "", nil
		default:
			t.Fatalf("unexpected call %d", call)
			return "", nil
		}
	}

	if err := CreateWorkspace("/tmp/repo", "/tmp/ws", "feature-ws", "HEAD"); err != nil {
		t.Fatalf("CreateWorkspace() error = %v", err)
	}
	if call != 2 {
		t.Fatalf("runGitCtx calls = %d, want 2", call)
	}
}

func TestCreateWorkspaceClearsOrphanedCleanupMarker(t *testing.T) {
	origRunGitCtx := runGitCtx
	origRemoveWorkspacePathCtx := removeWorkspacePathCtx
	defer func() {
		runGitCtx = origRunGitCtx
		removeWorkspacePathCtx = origRemoveWorkspacePathCtx
	}()

	workspacePath := filepath.Join(t.TempDir(), "pending-cleanup")
	if err := writeWorkspaceCleanupState(workspacePath, workspaceCleanupState{
		RepoPath:        "/tmp/repo",
		CleanupPath:     filepath.Join(filepath.Dir(workspacePath), ".pending-cleanup.amux-prune-1"),
		NeedsUnregister: false,
	}); err != nil {
		t.Fatalf("writeWorkspaceCleanupState() error = %v", err)
	}
	runGitCtx = func(ctx context.Context, _ string, args ...string) (string, error) {
		if got, want := strings.Join(args, " "), "worktree add -b feature-ws "+workspacePath+" HEAD"; got != want {
			t.Fatalf("worktree add args = %q, want %q", got, want)
		}
		return "", nil
	}

	err := CreateWorkspace("/tmp/repo", workspacePath, "feature-ws", "HEAD")
	if err != nil {
		t.Fatalf("expected orphaned cleanup marker to be cleared, got %v", err)
	}
	if _, err := os.Stat(prunedWorkspaceRetryMarkerPath(workspacePath)); !os.IsNotExist(err) {
		t.Fatalf("expected cleanup marker to be cleared, err=%v", err)
	}
}

func TestCreateWorkspaceRejectsMarkerWrittenBeforeRenameEvenIfLivePathIsRegistered(t *testing.T) {
	origUnregisterWorktreeCtx := unregisterWorktreeCtx
	origRemoveWorkspacePathCtx := removeWorkspacePathCtx
	defer func() {
		unregisterWorktreeCtx = origUnregisterWorktreeCtx
		removeWorkspacePathCtx = origRemoveWorkspacePathCtx
	}()

	workspaceRoot := t.TempDir()
	workspacePath := filepath.Join(workspaceRoot, "pending-cleanup")
	stagedPath := filepath.Join(workspaceRoot, ".pending-cleanup.amux-prune-1")
	if err := os.MkdirAll(filepath.Join(workspacePath, "nested"), 0o755); err != nil {
		t.Fatalf("MkdirAll(workspacePath) error = %v", err)
	}
	if err := os.WriteFile(filepath.Join(workspacePath, "nested", "file.txt"), []byte("content"), 0o644); err != nil {
		t.Fatalf("WriteFile(file.txt) error = %v", err)
	}
	if err := writeWorkspaceCleanupState(workspacePath, workspaceCleanupState{
		RepoPath:        "/tmp/repo",
		CleanupPath:     stagedPath,
		NeedsUnregister: true,
	}); err != nil {
		t.Fatalf("writeWorkspaceCleanupState() error = %v", err)
	}

	unregisterWorktreeCtx = func(context.Context, string, string) error {
		t.Fatal("expected same-path registered workspace to be rejected before unregister")
		return nil
	}
	removeWorkspacePathCtx = func(context.Context, string) error {
		t.Fatal("expected same-path registered workspace to be rejected before cleanup delete")
		return nil
	}

	err := CreateWorkspace("/tmp/repo", workspacePath, "feature-ws", "HEAD")
	if err == nil {
		t.Fatal("expected CreateWorkspace() to reject same-path registered workspace during pending cleanup")
	}
	if !strings.Contains(err.Error(), "pending cleanup remains") {
		t.Fatalf("expected pending cleanup conflict, got %v", err)
	}
}

func TestCreateWorkspaceRejectsReusedPathDuringPendingCleanup(t *testing.T) {
	workspaceRoot := t.TempDir()
	workspacePath := filepath.Join(workspaceRoot, "pending-cleanup")
	stagedPath := filepath.Join(workspaceRoot, ".pending-cleanup.amux-prune-1")
	if err := os.MkdirAll(workspacePath, 0o755); err != nil {
		t.Fatalf("MkdirAll(workspacePath) error = %v", err)
	}
	if err := os.MkdirAll(stagedPath, 0o755); err != nil {
		t.Fatalf("MkdirAll(stagedPath) error = %v", err)
	}
	if err := ensurePrunedWorkspaceRetryMarker(workspacePath, stagedPath); err != nil {
		t.Fatalf("ensurePrunedWorkspaceRetryMarker() error = %v", err)
	}

	err := CreateWorkspace("/tmp/repo", workspacePath, "feature-ws", "HEAD")
	if err == nil {
		t.Fatal("expected CreateWorkspace() to reject reused workspace path during pending cleanup")
	}
	if !strings.Contains(err.Error(), "pending cleanup remains") {
		t.Fatalf("expected reused path error, got %v", err)
	}
}

func TestCreateWorkspaceRejectsMarkerWrittenBeforeRenameWhenLivePathNotRegistered(t *testing.T) {
	origRunGitCtx := runGitCtx
	defer func() {
		runGitCtx = origRunGitCtx
	}()

	workspaceRoot := t.TempDir()
	workspacePath := filepath.Join(workspaceRoot, "pending-cleanup")
	stagedPath := filepath.Join(workspaceRoot, ".pending-cleanup.amux-prune-1")
	if err := os.MkdirAll(workspacePath, 0o755); err != nil {
		t.Fatalf("MkdirAll(workspacePath) error = %v", err)
	}
	if err := writeWorkspaceCleanupState(workspacePath, workspaceCleanupState{
		RepoPath:        "/tmp/repo",
		CleanupPath:     stagedPath,
		NeedsUnregister: false,
	}); err != nil {
		t.Fatalf("writeWorkspaceCleanupState() error = %v", err)
	}
	runGitCtx = func(ctx context.Context, repoPath string, args ...string) (string, error) {
		if repoPath != "/tmp/repo" {
			t.Fatalf("repo path = %q, want %q", repoPath, "/tmp/repo")
		}
		if got, want := strings.Join(args, " "), "worktree list --porcelain"; got != want {
			t.Fatalf("git args = %q, want %q", got, want)
		}
		return "", nil
	}

	err := CreateWorkspace("/tmp/repo", workspacePath, "feature-ws", "HEAD")
	if err == nil {
		t.Fatal("expected CreateWorkspace() to reject unregistered live path for missing cleanup target")
	}
	if !strings.Contains(err.Error(), "pending cleanup remains") {
		t.Fatalf("expected pending cleanup conflict, got %v", err)
	}
}

func TestCreateWorkspaceRejectsLegacyUnregisterMarkerWithoutRepoPath(t *testing.T) {
	origRunGitCtx := runGitCtx
	origUnregisterWorktreeCtx := unregisterWorktreeCtx
	defer func() {
		runGitCtx = origRunGitCtx
		unregisterWorktreeCtx = origUnregisterWorktreeCtx
	}()

	workspacePath := filepath.Join(t.TempDir(), "pending-cleanup")
	if err := os.WriteFile(prunedWorkspaceRetryMarkerPath(workspacePath), []byte("u:/tmp/staged\n"), 0o600); err != nil {
		t.Fatalf("WriteFile(markerPath) error = %v", err)
	}
	unregisterWorktreeCtx = func(context.Context, string, string) error {
		t.Fatal("expected repo-less legacy marker to fail before unregister")
		return nil
	}
	runGitCtx = func(context.Context, string, ...string) (string, error) {
		t.Fatal("expected CreateWorkspace() to fail before worktree add")
		return "", nil
	}

	err := CreateWorkspace("/tmp/repo-b", workspacePath, "feature-ws", "HEAD")
	if err == nil {
		t.Fatal("expected CreateWorkspace() to reject repo-less legacy unregister marker")
	}
	if !strings.Contains(err.Error(), "missing repo path") {
		t.Fatalf("expected missing repo path error, got %v", err)
	}
}

func TestCreateWorkspaceRejectsLegacyPendingCleanupMarkerWhenPathExists(t *testing.T) {
	origRunGitCtx := runGitCtx
	defer func() {
		runGitCtx = origRunGitCtx
	}()

	workspacePath := filepath.Join(t.TempDir(), "pending-cleanup")
	if err := os.MkdirAll(workspacePath, 0o755); err != nil {
		t.Fatalf("MkdirAll(workspacePath) error = %v", err)
	}
	if err := os.WriteFile(prunedWorkspaceRetryMarkerPath(workspacePath), []byte("pruned workspace cleanup pending\n"), 0o600); err != nil {
		t.Fatalf("WriteFile(markerPath) error = %v", err)
	}
	runGitCtx = func(context.Context, string, ...string) (string, error) {
		t.Fatal("expected CreateWorkspace() to fail before worktree add")
		return "", nil
	}

	err := CreateWorkspace("/tmp/repo", workspacePath, "feature-ws", "HEAD")
	if err == nil {
		t.Fatal("expected CreateWorkspace() to reject legacy pending cleanup marker with live path")
	}
	if !strings.Contains(err.Error(), "legacy pending cleanup marker") {
		t.Fatalf("expected legacy cleanup error, got %v", err)
	}
}

func TestCreateWorkspaceRejectsReusedPathDuringDeferredUnregisterRecovery(t *testing.T) {
	origRunGitCtx := runGitCtx
	origUnregisterWorktreeCtx := unregisterWorktreeCtx
	defer func() {
		runGitCtx = origRunGitCtx
		unregisterWorktreeCtx = origUnregisterWorktreeCtx
	}()

	workspacePath := filepath.Join(t.TempDir(), "pending-cleanup")
	if err := os.MkdirAll(workspacePath, 0o755); err != nil {
		t.Fatalf("MkdirAll(workspacePath) error = %v", err)
	}
	if err := os.WriteFile(filepath.Join(workspacePath, ".git"), []byte("gitdir: /tmp/new-admin\n"), 0o644); err != nil {
		t.Fatalf("WriteFile(.git) error = %v", err)
	}
	if err := writeWorkspaceCleanupState(workspacePath, workspaceCleanupState{
		RepoPath:        "/tmp/repo",
		CleanupPath:     "",
		NeedsUnregister: true,
	}); err != nil {
		t.Fatalf("writeWorkspaceCleanupState() error = %v", err)
	}

	runGitCtx = func(context.Context, string, ...string) (string, error) {
		t.Fatal("expected CreateWorkspace() to reject reused live path before git lookup")
		return "", nil
	}
	unregisterWorktreeCtx = func(context.Context, string, string) error {
		t.Fatal("expected CreateWorkspace() to reject reused live path before unregister")
		return nil
	}

	err := CreateWorkspace("/tmp/repo", workspacePath, "feature-ws", "HEAD")
	if err == nil {
		t.Fatal("expected CreateWorkspace() to reject reused live path during deferred unregister recovery")
	}
	if !strings.Contains(err.Error(), "pending cleanup remains") {
		t.Fatalf("expected pending cleanup conflict, got %v", err)
	}
}

func TestReadWorkspaceCleanupStateRejectsIncompleteMarker(t *testing.T) {
	workspacePath := filepath.Join(t.TempDir(), "pending-cleanup")
	markerPath := prunedWorkspaceRetryMarkerPath(workspacePath)
	if err := os.WriteFile(markerPath, []byte("cleanup_path=/tmp/staged\n"), 0o600); err != nil {
		t.Fatalf("WriteFile(markerPath) error = %v", err)
	}

	_, marked, err := readWorkspaceCleanupState(workspacePath)
	if err == nil {
		t.Fatal("expected incomplete marker to fail parsing")
	}
	if marked {
		t.Fatal("expected incomplete marker to be rejected")
	}
	if !strings.Contains(err.Error(), "incomplete workspace cleanup marker") {
		t.Fatalf("expected incomplete marker error, got %v", err)
	}
}

func TestWriteWorkspaceCleanupStateRoundTripsRepoPath(t *testing.T) {
	workspacePath := filepath.Join(t.TempDir(), "pending-cleanup")
	want := workspaceCleanupState{
		RepoPath:        "/tmp/repo-a",
		CleanupPath:     filepath.Join(filepath.Dir(workspacePath), ".pending-cleanup.amux-prune-1"),
		NeedsUnregister: true,
	}
	if err := writeWorkspaceCleanupState(workspacePath, want); err != nil {
		t.Fatalf("writeWorkspaceCleanupState() error = %v", err)
	}

	got, marked, err := readWorkspaceCleanupState(workspacePath)
	if err != nil {
		t.Fatalf("readWorkspaceCleanupState() error = %v", err)
	}
	if !marked {
		t.Fatal("expected cleanup marker to be present")
	}
	if got != want {
		t.Fatalf("cleanup state = %+v, want %+v", got, want)
	}
}
