package git

import (
	"context"
	"fmt"
	"path/filepath"
)

func persistAndResumeWorkspaceCleanup(
	ctx context.Context,
	repoPath, workspacePath string,
	needsUnregister bool,
) error {
	state, err := stageWorkspaceForPendingCleanup(ctx, repoPath, workspacePath, needsUnregister)
	if err != nil {
		return err
	}
	return resumeWorkspaceCleanup(ctx, repoPath, workspacePath, state)
}

func writePendingWorkspaceCleanupState(workspacePath string, state workspaceCleanupState) error {
	if err := writeWorkspaceCleanupState(workspacePath, state); err != nil {
		return err
	}
	return newWorkspaceCleanupPendingError(workspacePath)
}

func stageWorkspaceForPendingCleanup(
	ctx context.Context,
	repoPath, workspacePath string,
	needsUnregister bool,
) (workspaceCleanupState, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	if err := ctx.Err(); err != nil {
		return workspaceCleanupState{}, err
	}
	state := workspaceCleanupState{
		NeedsUnregister: needsUnregister,
	}
	if repoPath != "" {
		state.RepoPath = filepath.Clean(repoPath)
	}
	if gitRef, gitRefModTime, ok, err := workspaceGitRefFingerprint(workspacePath); err != nil {
		return workspaceCleanupState{}, err
	} else if ok {
		state.WorkspaceGitRef = gitRef
		state.WorkspaceGitRefModTime = gitRefModTime
	}
	if err := ctx.Err(); err != nil {
		return workspaceCleanupState{}, err
	}
	if !isSafeWorkspaceCleanupPath(workspacePath) {
		return workspaceCleanupState{}, fmt.Errorf("refusing to remove unsafe path: %s", workspacePath)
	}
	retryMetadata, err := ensureWorkspaceCleanupRetryMetadataWithContext(ctx, workspacePath, state.RepoPath, needsUnregister)
	if err != nil {
		return workspaceCleanupState{}, err
	}
	if retryMetadata.WorkspaceFingerprint != "" {
		state.WorkspaceFingerprint = retryMetadata.WorkspaceFingerprint
	}
	if err := ctx.Err(); err != nil {
		return workspaceCleanupState{}, err
	}
	stagedPath, err := reserveWorkspacePathForCleanup(workspacePath)
	if err != nil {
		return workspaceCleanupState{}, err
	}
	if err := ctx.Err(); err != nil {
		return workspaceCleanupState{}, err
	}
	state.CleanupPath = stagedPath
	if err := writeWorkspaceCleanupState(workspacePath, state); err != nil {
		return workspaceCleanupState{}, err
	}
	if err := ctx.Err(); err != nil {
		return workspaceCleanupState{}, err
	}
	if err := stageWorkspacePathForCleanupAtPath(workspacePath, stagedPath); err != nil {
		return workspaceCleanupState{}, err
	}
	return state, nil
}

func canRecoverPendingCleanupWorkspace(ctx context.Context, workspacePath string, state workspaceCleanupState) (bool, error) {
	if state.WorkspaceFingerprint != "" {
		currentFingerprint, err := workspaceCleanupRetryFingerprintCtx(ctx, workspacePath)
		if err != nil {
			return false, err
		}
		return currentFingerprint == state.WorkspaceFingerprint, nil
	}
	retryMetadata, marked, err := readWorkspaceCleanupRetryMetadata(workspacePath)
	if err != nil || !marked {
		return false, err
	}
	return workspacePathMatchesRetryMetadataCleanupWithContext(ctx, workspacePath, retryMetadata)
}
