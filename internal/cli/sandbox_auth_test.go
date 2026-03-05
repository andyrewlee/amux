package cli

import (
	"errors"
	"testing"

	"github.com/andyrewlee/amux/internal/sandbox"
)

func TestHandleAgentExitWrapsWorkspaceSyncErrors(t *testing.T) {
	sb := sandbox.NewMockRemoteSandbox("sb-1")
	err := handleAgentExit(sb, sandbox.AgentClaude, 0, true, t.TempDir())
	if err == nil {
		t.Fatal("expected sync error")
	}

	var syncErr *workspaceSyncError
	if !errors.As(err, &syncErr) {
		t.Fatalf("expected workspaceSyncError, got %T (%v)", err, err)
	}
}

func TestShouldPreserveSandboxOnExitError(t *testing.T) {
	if shouldPreserveSandboxOnExitError(nil) {
		t.Fatal("nil error should not preserve sandbox")
	}
	if !shouldPreserveSandboxOnExitError(&workspaceSyncError{err: errors.New("sync failed")}) {
		t.Fatal("workspace sync errors should preserve sandbox")
	}
	if shouldPreserveSandboxOnExitError(errors.New("other")) {
		t.Fatal("non-sync errors should not preserve sandbox")
	}
}
