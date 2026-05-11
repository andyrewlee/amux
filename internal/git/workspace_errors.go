package git

import (
	"errors"
	"fmt"
)

var (
	ErrUnregisteredWorkspacePath = errors.New("workspace is not a registered worktree but still exists on disk")
	ErrWorkspaceCleanupPending   = errors.New("workspace cleanup is still pending")
)

func IsUnregisteredWorkspacePathError(err error) bool {
	return errors.Is(err, ErrUnregisteredWorkspacePath)
}

func IsWorkspaceCleanupPendingError(err error) bool {
	return errors.Is(err, ErrWorkspaceCleanupPending)
}

func newUnregisteredWorkspacePathError(workspacePath string) error {
	return fmt.Errorf("%w: %s", ErrUnregisteredWorkspacePath, workspacePath)
}

func newWorkspaceCleanupPendingError(workspacePath string) error {
	return fmt.Errorf("%w: %s", ErrWorkspaceCleanupPending, workspacePath)
}
