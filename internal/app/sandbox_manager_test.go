package app

import (
	"context"
	"errors"
	"io"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/andyrewlee/amux/internal/sandbox"
)

type rollbackProvider struct {
	deletedIDs []string
	deleteErr  error
}

func (p *rollbackProvider) Name() string { return "fake" }

func (p *rollbackProvider) CreateSandbox(context.Context, sandbox.SandboxCreateConfig) (sandbox.RemoteSandbox, error) {
	return nil, errors.New("not implemented")
}

func (p *rollbackProvider) GetSandbox(context.Context, string) (sandbox.RemoteSandbox, error) {
	return nil, errors.New("not implemented")
}

func (p *rollbackProvider) ListSandboxes(context.Context) ([]sandbox.RemoteSandbox, error) {
	return nil, errors.New("not implemented")
}

func (p *rollbackProvider) DeleteSandbox(_ context.Context, id string) error {
	p.deletedIDs = append(p.deletedIDs, id)
	return p.deleteErr
}

func (p *rollbackProvider) Volumes() sandbox.VolumeManager { return nil }

func (p *rollbackProvider) Snapshots() sandbox.SnapshotManager { return nil }

func (p *rollbackProvider) SupportsFeature(sandbox.ProviderFeature) bool { return false }

type rollbackSandbox struct {
	id        string
	stopCalls int
	stopErr   error
}

func (s *rollbackSandbox) ID() string { return s.id }

func (s *rollbackSandbox) State() sandbox.SandboxState { return sandbox.StateStarted }

func (s *rollbackSandbox) Labels() map[string]string { return nil }

func (s *rollbackSandbox) Start(context.Context) error { return nil }

func (s *rollbackSandbox) Stop(context.Context) error {
	s.stopCalls++
	return s.stopErr
}

func (s *rollbackSandbox) WaitReady(context.Context, time.Duration) error { return nil }

func (s *rollbackSandbox) Exec(context.Context, string, *sandbox.ExecOptions) (*sandbox.ExecResult, error) {
	return nil, nil
}

func (s *rollbackSandbox) ExecInteractive(context.Context, string, io.Reader, io.Writer, io.Writer, *sandbox.ExecOptions) (int, error) {
	return 0, nil
}

func (s *rollbackSandbox) UploadFile(context.Context, string, string) error { return nil }

func (s *rollbackSandbox) DownloadFile(context.Context, string, string) error { return nil }

func (s *rollbackSandbox) GetPreviewURL(context.Context, int) (string, error) { return "", nil }

func (s *rollbackSandbox) Refresh(context.Context) error { return nil }

func TestRollbackFailedSessionInitRemovesSandboxAndMetadata(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)

	workspaceRoot := filepath.Join(home, "repo")
	if err := os.MkdirAll(workspaceRoot, 0o755); err != nil {
		t.Fatalf("MkdirAll() error = %v", err)
	}

	const sandboxID = "sb-init-failure"
	err := sandbox.SaveSandboxMeta(workspaceRoot, "fake", sandbox.SandboxMeta{
		SandboxID: sandboxID,
		Agent:     sandbox.AgentShell,
		CreatedAt: time.Now().UTC().Format(time.RFC3339),
	})
	if err != nil {
		t.Fatalf("SaveSandboxMeta() error = %v", err)
	}

	provider := &rollbackProvider{}
	sb := &rollbackSandbox{id: sandboxID}
	session := &sandboxSession{
		sandbox:      sb,
		provider:     provider,
		providerName: provider.Name(),
		worktreeID:   sandbox.ComputeWorktreeID(workspaceRoot),
	}

	manager := NewSandboxManager(nil)
	manager.storeSession(session)

	manager.rollbackFailedSessionInit(session, workspaceRoot, errors.New("upload failed"))

	if sb.stopCalls != 1 {
		t.Fatalf("Stop() calls = %d, want 1", sb.stopCalls)
	}
	if len(provider.deletedIDs) != 1 || provider.deletedIDs[0] != sandboxID {
		t.Fatalf("DeleteSandbox() calls = %v, want [%q]", provider.deletedIDs, sandboxID)
	}
	meta, err := sandbox.LoadSandboxMeta(workspaceRoot, provider.Name())
	if err != nil {
		t.Fatalf("LoadSandboxMeta() error = %v", err)
	}
	if meta != nil {
		t.Fatalf("LoadSandboxMeta() = %+v, want nil", *meta)
	}
	if got := manager.sessionFor(session.worktreeID); got != nil {
		t.Fatalf("sessionFor() = %#v, want nil", got)
	}
}

func TestRollbackFailedSessionInitKeepsMetadataWhenDeleteFails(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)

	workspaceRoot := filepath.Join(home, "repo")
	if err := os.MkdirAll(workspaceRoot, 0o755); err != nil {
		t.Fatalf("MkdirAll() error = %v", err)
	}

	const sandboxID = "sb-delete-fail"
	err := sandbox.SaveSandboxMeta(workspaceRoot, "fake", sandbox.SandboxMeta{
		SandboxID: sandboxID,
		Agent:     sandbox.AgentShell,
		CreatedAt: time.Now().UTC().Format(time.RFC3339),
	})
	if err != nil {
		t.Fatalf("SaveSandboxMeta() error = %v", err)
	}

	provider := &rollbackProvider{deleteErr: errors.New("transient delete failure")}
	sb := &rollbackSandbox{id: sandboxID}
	session := &sandboxSession{
		sandbox:      sb,
		provider:     provider,
		providerName: provider.Name(),
		worktreeID:   sandbox.ComputeWorktreeID(workspaceRoot),
	}

	manager := NewSandboxManager(nil)
	manager.storeSession(session)

	manager.rollbackFailedSessionInit(session, workspaceRoot, errors.New("upload failed"))

	meta, err := sandbox.LoadSandboxMeta(workspaceRoot, provider.Name())
	if err != nil {
		t.Fatalf("LoadSandboxMeta() error = %v", err)
	}
	if meta == nil || meta.SandboxID != sandboxID {
		t.Fatalf("LoadSandboxMeta() = %#v, want sandboxID %q retained", meta, sandboxID)
	}
}

func TestRollbackFailedSessionInitRemovesMetadataWhenDeleteReturnsNotFound(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)

	workspaceRoot := filepath.Join(home, "repo")
	if err := os.MkdirAll(workspaceRoot, 0o755); err != nil {
		t.Fatalf("MkdirAll() error = %v", err)
	}

	const sandboxID = "sb-delete-missing"
	err := sandbox.SaveSandboxMeta(workspaceRoot, "fake", sandbox.SandboxMeta{
		SandboxID: sandboxID,
		Agent:     sandbox.AgentShell,
		CreatedAt: time.Now().UTC().Format(time.RFC3339),
	})
	if err != nil {
		t.Fatalf("SaveSandboxMeta() error = %v", err)
	}

	provider := &rollbackProvider{deleteErr: sandbox.ErrNotFound}
	sb := &rollbackSandbox{id: sandboxID}
	session := &sandboxSession{
		sandbox:      sb,
		provider:     provider,
		providerName: provider.Name(),
		worktreeID:   sandbox.ComputeWorktreeID(workspaceRoot),
	}

	manager := NewSandboxManager(nil)
	manager.storeSession(session)

	manager.rollbackFailedSessionInit(session, workspaceRoot, errors.New("upload failed"))

	meta, err := sandbox.LoadSandboxMeta(workspaceRoot, provider.Name())
	if err != nil {
		t.Fatalf("LoadSandboxMeta() error = %v", err)
	}
	if meta != nil {
		t.Fatalf("LoadSandboxMeta() = %+v, want nil", *meta)
	}
}

func TestRollbackFailedSessionInitDoesNotDeleteOtherActiveSession(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)

	workspaceRoot := filepath.Join(home, "repo")
	if err := os.MkdirAll(workspaceRoot, 0o755); err != nil {
		t.Fatalf("MkdirAll() error = %v", err)
	}

	const sandboxID = "sb-rollback-owner"
	err := sandbox.SaveSandboxMeta(workspaceRoot, "fake", sandbox.SandboxMeta{
		SandboxID: sandboxID,
		Agent:     sandbox.AgentShell,
		CreatedAt: time.Now().UTC().Format(time.RFC3339),
	})
	if err != nil {
		t.Fatalf("SaveSandboxMeta() error = %v", err)
	}

	provider := &rollbackProvider{deleteErr: errors.New("delete failed")}
	manager := NewSandboxManager(nil)
	worktreeID := sandbox.ComputeWorktreeID(workspaceRoot)

	failedSandbox := &rollbackSandbox{id: sandboxID}
	failedSession := &sandboxSession{
		sandbox:      failedSandbox,
		provider:     provider,
		providerName: provider.Name(),
		worktreeID:   worktreeID,
	}
	activeSession := &sandboxSession{
		sandbox:      &rollbackSandbox{id: "sb-active"},
		provider:     provider,
		providerName: provider.Name(),
		worktreeID:   worktreeID,
	}
	manager.storeSession(activeSession)

	manager.rollbackFailedSessionInit(failedSession, workspaceRoot, errors.New("upload failed"))

	if got := manager.sessionFor(worktreeID); got != activeSession {
		t.Fatalf("sessionFor() = %#v, want active session %#v", got, activeSession)
	}
	if failedSandbox.stopCalls != 0 {
		t.Fatalf("Stop() calls = %d, want 0", failedSandbox.stopCalls)
	}
	if len(provider.deletedIDs) != 0 {
		t.Fatalf("DeleteSandbox() calls = %v, want none", provider.deletedIDs)
	}
	meta, err := sandbox.LoadSandboxMeta(workspaceRoot, provider.Name())
	if err != nil {
		t.Fatalf("LoadSandboxMeta() error = %v", err)
	}
	if meta == nil || meta.SandboxID != sandboxID {
		t.Fatalf("LoadSandboxMeta() = %#v, want sandboxID %q retained", meta, sandboxID)
	}
}
