package app

import (
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/andyrewlee/amux/internal/data"
	"github.com/andyrewlee/amux/internal/sandbox"
	"github.com/andyrewlee/amux/internal/tmux"
)

func TestSandboxManagerSyncToLocalFromUsesSourceLookupAndTargetRoot(t *testing.T) {
	wd, err := os.Getwd()
	if err != nil {
		t.Fatalf("Getwd() error = %v", err)
	}

	base := t.TempDir()
	absRepo := filepath.Join(base, "repo")
	absRoot := filepath.Join(base, "workspaces", "repo", "feature")
	if err := os.MkdirAll(absRoot, 0o755); err != nil {
		t.Fatalf("MkdirAll(%q) error = %v", absRoot, err)
	}

	relRepo, err := filepath.Rel(wd, absRepo)
	if err != nil {
		t.Fatalf("Rel(repo) error = %v", err)
	}
	relRoot, err := filepath.Rel(wd, absRoot)
	if err != nil {
		t.Fatalf("Rel(root) error = %v", err)
	}

	source := data.NewWorkspace("feature", "feat-branch", "main", relRepo, relRoot)
	target := data.NewWorkspace("feature", "feat-branch", "main", absRepo, absRoot)
	manager := NewSandboxManager(nil)
	manager.storeSession(&sandboxSession{
		sandbox:       &rollbackSandbox{id: "sb-source-target"},
		worktreeID:    sandbox.ComputeWorktreeID(relRoot),
		workspaceRoot: relRoot,
		workspacePath: "/remote/ws",
		needsSyncDown: true,
	})

	var gotCwd, gotWorktreeID string
	manager.downloadWorkspace = func(computer sandbox.RemoteSandbox, opts sandbox.SyncOptions, verbose bool) error {
		gotCwd = opts.Cwd
		gotWorktreeID = opts.WorktreeID
		return nil
	}

	if err := manager.SyncToLocalFrom(source, target); err != nil {
		t.Fatalf("SyncToLocalFrom() error = %v", err)
	}
	if gotCwd != absRoot {
		t.Fatalf("download Cwd = %q, want %q", gotCwd, absRoot)
	}
	if gotWorktreeID != sandbox.ComputeWorktreeID(relRoot) {
		t.Fatalf("download WorktreeID = %q, want %q", gotWorktreeID, sandbox.ComputeWorktreeID(relRoot))
	}
	session := manager.sessionFor(sandbox.ComputeWorktreeID(relRoot))
	if session == nil {
		t.Fatal("expected tracked sandbox session after source-target sync")
	}
	if session.workspaceRoot != absRoot {
		t.Fatalf("session.workspaceRoot = %q, want %q", session.workspaceRoot, absRoot)
	}
	if session.workspaceID != target.ID() {
		t.Fatalf("session.workspaceID = %q, want %q", session.workspaceID, target.ID())
	}
}

func TestSandboxManagerSyncToLocalFromKeepsDirtySyncFailuresBoundToTargetRoot(t *testing.T) {
	skipIfNoGit(t)

	home := t.TempDir()
	t.Setenv("HOME", home)

	sourceBase := t.TempDir()
	sourceRepo := filepath.Join(sourceBase, "repo-old")
	sourceRoot := filepath.Join(sourceRepo, "feature")
	if err := os.MkdirAll(sourceRoot, 0o755); err != nil {
		t.Fatalf("MkdirAll(%q) error = %v", sourceRoot, err)
	}

	targetRepo := initRepo(t)
	if err := os.WriteFile(filepath.Join(targetRepo, "dirty.txt"), []byte("dirty"), 0o644); err != nil {
		t.Fatalf("WriteFile(dirty) error = %v", err)
	}

	source := data.NewWorkspace("feature", "feat-branch", "main", sourceRepo, sourceRoot)
	target := data.NewWorkspace("feature", "feat-branch", "main", targetRepo, targetRepo)
	worktreeID := sandbox.ComputeWorktreeID(source.Root)
	needsSync := true
	if err := sandbox.SaveSandboxMeta(target.Root, "fake", sandbox.SandboxMeta{
		SandboxID:     "sb-dirty-rebound",
		Agent:         sandbox.AgentShell,
		Provider:      "fake",
		WorktreeID:    worktreeID,
		WorkspaceRoot: target.Root,
		NeedsSyncDown: &needsSync,
		WorkspaceIDs:  []string{string(target.ID())},
	}); err != nil {
		t.Fatalf("SaveSandboxMeta() error = %v", err)
	}

	manager := NewSandboxManager(nil)
	manager.sessionsWithTags = func(match map[string]string, keys []string, opts tmux.Options) ([]tmux.SessionTagValues, error) {
		return nil, nil
	}
	session := &sandboxSession{
		sandbox:            &rollbackSandbox{id: "sb-dirty-rebound"},
		providerName:       "fake",
		worktreeID:         worktreeID,
		workspaceID:        target.ID(),
		workspaceIDAliases: map[string]struct{}{string(target.ID()): {}},
		workspaceRepo:      target.Repo,
		workspaceRoot:      target.Root,
		workspacePath:      "/remote/ws",
		needsSyncDown:      true,
	}
	manager.storeSession(session)

	err := manager.SyncToLocalFrom(source, target)
	if !errors.Is(err, errSandboxSyncConflict) {
		t.Fatalf("SyncToLocalFrom() error = %v, want errSandboxSyncConflict", err)
	}
	if session.workspaceRoot != target.Root {
		t.Fatalf("session.workspaceRoot = %q, want %q", session.workspaceRoot, target.Root)
	}
	if session.workspaceID != target.ID() {
		t.Fatalf("session.workspaceID = %q, want %q", session.workspaceID, target.ID())
	}

	metaTarget, err := sandbox.LoadSandboxMeta(target.Root, "fake")
	if err != nil {
		t.Fatalf("LoadSandboxMeta(target) error = %v", err)
	}
	if metaTarget == nil || metaTarget.SandboxID != "sb-dirty-rebound" {
		t.Fatalf("LoadSandboxMeta(target) = %#v, want rebound metadata", metaTarget)
	}
	if metaTarget.WorktreeID != worktreeID {
		t.Fatalf("target metadata worktreeID = %q, want %q", metaTarget.WorktreeID, worktreeID)
	}
	if metaTarget.WorkspaceRoot != sandboxCanonicalPath(target.Root) {
		t.Fatalf("target metadata workspaceRoot = %q, want %q", metaTarget.WorkspaceRoot, sandboxCanonicalPath(target.Root))
	}
}

func TestSandboxManagerSyncToLocalFromKeepsDownloadFailuresBoundToTargetRoot(t *testing.T) {
	skipIfNoGit(t)

	home := t.TempDir()
	t.Setenv("HOME", home)

	sourceBase := t.TempDir()
	sourceRepo := filepath.Join(sourceBase, "repo-old")
	sourceRoot := filepath.Join(sourceRepo, "feature")
	if err := os.MkdirAll(sourceRoot, 0o755); err != nil {
		t.Fatalf("MkdirAll(%q) error = %v", sourceRoot, err)
	}

	targetRepo := initRepo(t)
	source := data.NewWorkspace("feature", "feat-branch", "main", sourceRepo, sourceRoot)
	target := data.NewWorkspace("feature", "feat-branch", "main", targetRepo, targetRepo)
	worktreeID := sandbox.ComputeWorktreeID(source.Root)
	needsSync := true
	if err := sandbox.SaveSandboxMeta(target.Root, "fake", sandbox.SandboxMeta{
		SandboxID:     "sb-download-rebound",
		Agent:         sandbox.AgentShell,
		Provider:      "fake",
		WorktreeID:    worktreeID,
		WorkspaceRoot: target.Root,
		NeedsSyncDown: &needsSync,
		WorkspaceIDs:  []string{string(target.ID())},
	}); err != nil {
		t.Fatalf("SaveSandboxMeta() error = %v", err)
	}

	manager := NewSandboxManager(nil)
	manager.sessionsWithTags = func(match map[string]string, keys []string, opts tmux.Options) ([]tmux.SessionTagValues, error) {
		return nil, nil
	}
	downloadErr := errors.New("download failed")
	manager.downloadWorkspace = func(computer sandbox.RemoteSandbox, opts sandbox.SyncOptions, verbose bool) error {
		return downloadErr
	}
	session := &sandboxSession{
		sandbox:            &rollbackSandbox{id: "sb-download-rebound"},
		providerName:       "fake",
		worktreeID:         worktreeID,
		workspaceID:        target.ID(),
		workspaceIDAliases: map[string]struct{}{string(target.ID()): {}},
		workspaceRepo:      target.Repo,
		workspaceRoot:      target.Root,
		workspacePath:      "/remote/ws",
		needsSyncDown:      true,
	}
	manager.storeSession(session)

	err := manager.SyncToLocalFrom(source, target)
	if !errors.Is(err, downloadErr) {
		t.Fatalf("SyncToLocalFrom() error = %v, want %v", err, downloadErr)
	}
	if session.workspaceRoot != target.Root {
		t.Fatalf("session.workspaceRoot = %q, want %q", session.workspaceRoot, target.Root)
	}
	if session.workspaceID != target.ID() {
		t.Fatalf("session.workspaceID = %q, want %q", session.workspaceID, target.ID())
	}

	metaTarget, err := sandbox.LoadSandboxMeta(target.Root, "fake")
	if err != nil {
		t.Fatalf("LoadSandboxMeta(target) error = %v", err)
	}
	if metaTarget == nil || metaTarget.SandboxID != "sb-download-rebound" {
		t.Fatalf("LoadSandboxMeta(target) = %#v, want rebound metadata", metaTarget)
	}
	if metaTarget.WorktreeID != worktreeID {
		t.Fatalf("target metadata worktreeID = %q, want %q", metaTarget.WorktreeID, worktreeID)
	}
	if metaTarget.WorkspaceRoot != sandboxCanonicalPath(target.Root) {
		t.Fatalf("target metadata workspaceRoot = %q, want %q", metaTarget.WorkspaceRoot, sandboxCanonicalPath(target.Root))
	}
}

func TestSandboxManagerSyncToLocalFromSkipsAlreadySyncedReboundWorkspace(t *testing.T) {
	wd, err := os.Getwd()
	if err != nil {
		t.Fatalf("Getwd() error = %v", err)
	}

	base := t.TempDir()
	absRepo := filepath.Join(base, "repo")
	absRoot := filepath.Join(base, "workspaces", "repo", "feature")
	if err := os.MkdirAll(absRoot, 0o755); err != nil {
		t.Fatalf("MkdirAll(%q) error = %v", absRoot, err)
	}

	relRepo, err := filepath.Rel(wd, absRepo)
	if err != nil {
		t.Fatalf("Rel(repo) error = %v", err)
	}
	relRoot, err := filepath.Rel(wd, absRoot)
	if err != nil {
		t.Fatalf("Rel(root) error = %v", err)
	}

	source := data.NewWorkspace("feature", "feat-branch", "main", relRepo, relRoot)
	target := data.NewWorkspace("feature", "feat-branch", "main", absRepo, absRoot)
	manager := NewSandboxManager(nil)
	manager.storeSession(&sandboxSession{
		sandbox:       &rollbackSandbox{id: "sb-clean-source-target"},
		worktreeID:    sandbox.ComputeWorktreeID(relRoot),
		workspaceRoot: relRoot,
		workspacePath: "/remote/ws",
		needsSyncDown: false,
	})

	calls := 0
	manager.downloadWorkspace = func(computer sandbox.RemoteSandbox, opts sandbox.SyncOptions, verbose bool) error {
		calls++
		return nil
	}

	if err := manager.SyncToLocalFrom(source, target); err != nil {
		t.Fatalf("SyncToLocalFrom() error = %v", err)
	}
	if calls != 0 {
		t.Fatalf("downloadWorkspace() calls = %d, want 0 for already-synced rebound workspace", calls)
	}
	session := manager.sessionFor(sandbox.ComputeWorktreeID(relRoot))
	if session == nil {
		t.Fatal("expected tracked sandbox session after skipped source-target sync")
	}
	if session.workspaceRoot != absRoot {
		t.Fatalf("session.workspaceRoot = %q, want %q", session.workspaceRoot, absRoot)
	}
	if session.workspaceID != target.ID() {
		t.Fatalf("session.workspaceID = %q, want %q", session.workspaceID, target.ID())
	}
}
