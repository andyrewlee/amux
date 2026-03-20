package app

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/andyrewlee/amux/internal/data"
	"github.com/andyrewlee/amux/internal/sandbox"
)

func TestEnsureSessionRetargetsAttachedSandboxWithoutChangingRemoteWorktreeID(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)

	base := t.TempDir()
	realRepo := filepath.Join(base, "repo-real")
	realRoot := filepath.Join(realRepo, "feature")
	if err := os.MkdirAll(realRoot, 0o755); err != nil {
		t.Fatalf("MkdirAll(%q) error = %v", realRoot, err)
	}

	linkRepo := filepath.Join(base, "repo-link")
	if err := os.Symlink(realRepo, linkRepo); err != nil {
		t.Fatalf("Symlink(%q -> %q) error = %v", linkRepo, realRepo, err)
	}
	linkRoot := filepath.Join(linkRepo, "feature")

	oldWS := data.NewWorkspace("feature", "main", "main", linkRepo, linkRoot)
	newWS := data.NewWorkspace("feature", "main", "main", realRepo, realRoot)
	oldWorktreeID := sandbox.ComputeWorktreeID(oldWS.Root)
	newWorktreeID := sandbox.ComputeWorktreeID(newWS.Root)
	if oldWorktreeID == newWorktreeID {
		t.Fatalf("expected distinct worktree IDs for symlink and canonical roots, both were %q", oldWorktreeID)
	}

	needsSync := true
	if err := sandbox.SaveSandboxMeta(oldWS.Root, "fake", sandbox.SandboxMeta{
		SandboxID:     "sb-rekey",
		Agent:         sandbox.AgentShell,
		Provider:      "fake",
		NeedsSyncDown: &needsSync,
		WorkspaceIDs:  []string{string(oldWS.ID())},
	}); err != nil {
		t.Fatalf("SaveSandboxMeta() error = %v", err)
	}

	manager := NewSandboxManager(nil)
	session := &sandboxSession{
		sandbox:            &rollbackSandbox{id: "sb-rekey"},
		providerName:       "fake",
		worktreeID:         oldWorktreeID,
		workspaceID:        oldWS.ID(),
		workspaceIDAliases: map[string]struct{}{string(oldWS.ID()): {}},
		workspaceRepo:      oldWS.Repo,
		workspaceRoot:      oldWS.Root,
		workspacePath:      "/remote/ws",
	}
	manager.storeSession(session)

	got, err := manager.ensureSession(newWS, sandbox.AgentShell)
	if err != nil {
		t.Fatalf("ensureSession() error = %v", err)
	}
	if got != session {
		t.Fatal("expected existing attached sandbox session to be reused")
	}
	if manager.sessionFor(oldWorktreeID) != session {
		t.Fatalf("expected original worktree key %q to remain mapped to the existing session", oldWorktreeID)
	}
	if manager.sessionFor(newWorktreeID) != nil {
		t.Fatalf("expected canonical-path worktree key %q to stay unused for remote sync identity", newWorktreeID)
	}
	if session.worktreeID != oldWorktreeID {
		t.Fatalf("session.worktreeID = %q, want original %q", session.worktreeID, oldWorktreeID)
	}
	if session.workspaceRoot != newWS.Root {
		t.Fatalf("session.workspaceRoot = %q, want %q", session.workspaceRoot, newWS.Root)
	}

	metaNew, err := sandbox.LoadSandboxMeta(newWS.Root, "fake")
	if err != nil {
		t.Fatalf("LoadSandboxMeta(new) error = %v", err)
	}
	if metaNew == nil || metaNew.SandboxID != "sb-rekey" {
		t.Fatalf("new-root metadata = %#v, want sandbox sb-rekey", metaNew)
	}
	if metaNew.WorktreeID != oldWorktreeID {
		t.Fatalf("new-root metadata worktreeID = %q, want original %q", metaNew.WorktreeID, oldWorktreeID)
	}
	if metaNew.WorkspaceRoot != sandboxCanonicalPath(newWS.Root) {
		t.Fatalf("new-root metadata workspaceRoot = %q, want current canonical root %q", metaNew.WorkspaceRoot, sandboxCanonicalPath(newWS.Root))
	}
	metaOld, err := sandbox.LoadSandboxMeta(oldWS.Root, "fake")
	if err != nil {
		t.Fatalf("LoadSandboxMeta(old) error = %v", err)
	}
	if metaOld == nil || metaOld.SandboxID != "sb-rekey" {
		t.Fatalf("old-root metadata = %#v, want original metadata to remain addressable", metaOld)
	}
}

func sandboxCanonicalPath(path string) string {
	abs, err := filepath.Abs(path)
	if err != nil {
		abs = path
	}
	if resolved, err := filepath.EvalSymlinks(abs); err == nil {
		abs = resolved
	}
	return filepath.Clean(abs)
}
