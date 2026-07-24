package git

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// workspaceCleanupPhase enumerates the phases of resuming an interrupted
// workspace cleanup. Phases run in order; each phase either advances, jumps
// ahead (e.g. nothing staged -> finalize), or fails. The explicit table keeps
// the resume flow auditable: every interruption point corresponds to a phase
// boundary and re-running the FSM from the persisted marker is idempotent.
type workspaceCleanupPhase uint8

const (
	// cleanupPhaseLegacyGuard handles legacy ambiguous markers (terminal).
	cleanupPhaseLegacyGuard workspaceCleanupPhase = iota
	// cleanupPhaseValidateStage cross-checks the staged cleanup path against
	// the live workspace path and detects a recoverable live workspace.
	cleanupPhaseValidateStage
	// cleanupPhaseUnregister retries the worktree admin unregister.
	cleanupPhaseUnregister
	// cleanupPhaseRemoveStage removes the staged cleanup directory (staging a
	// recoverable live workspace first if needed).
	cleanupPhaseRemoveStage
	// cleanupPhaseFinalize persists the remaining pending state or clears the
	// marker when everything is done.
	cleanupPhaseFinalize
	// cleanupPhaseDone terminates the run.
	cleanupPhaseDone
)

// workspaceCleanupRun threads the mutable state through the phases.
type workspaceCleanupRun struct {
	ctx                      context.Context
	repoPath                 string
	workspacePath            string
	state                    workspaceCleanupState
	recoverableLiveWorkspace bool
	unregisterSatisfied      bool
}

// workspaceCleanupPhaseSteps is the transition table: phase -> step function
// returning the next phase.
var workspaceCleanupPhaseSteps = map[workspaceCleanupPhase]func(*workspaceCleanupRun) (workspaceCleanupPhase, error){
	cleanupPhaseLegacyGuard:   cleanupLegacyGuardStep,
	cleanupPhaseValidateStage: cleanupValidateStageStep,
	cleanupPhaseUnregister:    cleanupUnregisterStep,
	cleanupPhaseRemoveStage:   cleanupRemoveStageStep,
	cleanupPhaseFinalize:      cleanupFinalizeStep,
}

func resumeWorkspaceCleanup(
	ctx context.Context,
	repoPath, workspacePath string,
	state workspaceCleanupState,
) error {
	run := &workspaceCleanupRun{
		ctx:                 ctx,
		repoPath:            repoPath,
		workspacePath:       workspacePath,
		state:               state,
		unregisterSatisfied: !state.NeedsUnregister,
	}
	phase := cleanupPhaseLegacyGuard
	for phase != cleanupPhaseDone {
		step, ok := workspaceCleanupPhaseSteps[phase]
		if !ok {
			return fmt.Errorf("workspace cleanup: no step for phase %d", phase)
		}
		next, err := step(run)
		if err != nil {
			return err
		}
		phase = next
	}
	return nil
}

func cleanupLegacyGuardStep(r *workspaceCleanupRun) (workspaceCleanupPhase, error) {
	if !r.state.LegacyAmbiguous {
		return cleanupPhaseValidateStage, nil
	}
	if _, statErr := os.Stat(r.workspacePath); statErr == nil {
		return cleanupPhaseDone, fmt.Errorf("workspace path %s has a legacy pending cleanup marker and requires manual cleanup", r.workspacePath)
	} else if !os.IsNotExist(statErr) {
		return cleanupPhaseDone, statErr
	}
	return cleanupPhaseDone, clearPrunedWorkspaceRetryMarker(r.workspacePath)
}

func cleanupValidateStageStep(r *workspaceCleanupRun) (workspaceCleanupPhase, error) {
	if r.state.CleanupPath == "" {
		return cleanupPhaseUnregister, nil
	}
	_, stageErr := os.Stat(r.state.CleanupPath)
	if stageErr != nil && !os.IsNotExist(stageErr) {
		return cleanupPhaseDone, stageErr
	}
	_, workspaceErr := os.Stat(r.workspacePath)
	if workspaceErr != nil && !os.IsNotExist(workspaceErr) {
		return cleanupPhaseDone, workspaceErr
	}
	workspaceAlive := workspaceErr == nil
	if !workspaceAlive {
		return cleanupPhaseUnregister, nil
	}
	// The workspace path is alive. Decide whether it is the workspace this
	// cleanup owns (fingerprint match) or an unrelated stray that some
	// background process recreated at the original path after staging — e.g. a
	// package manager or watcher writing `<ws>/apps/...`, the exact shape that
	// used to deadlock the delete forever ("workspace path X exists while
	// pending cleanup remains at Y" on every retry).
	canRecover, err := canRecoverPendingCleanupWorkspace(r.ctx, r.workspacePath, r.state)
	if err != nil {
		return cleanupPhaseDone, err
	}
	if stageErr == nil {
		// Both the staged dir and a live path exist. This is a genuine
		// ambiguity only when the live path IS this workspace re-created while
		// cleanup was pending (staging should have moved it away) — refuse
		// that. When the live path is instead an unrelated stray, the staged
		// worktree is still ours to remove; proceed and leave the stray
		// untouched rather than deadlocking.
		if canRecover {
			return cleanupPhaseDone, fmt.Errorf("workspace path %s exists while pending cleanup remains at %s", r.workspacePath, r.state.CleanupPath)
		}
		return cleanupPhaseUnregister, nil
	}
	// Stage is gone but the workspace lives: only recoverable workspaces may
	// proceed (they get re-staged by the remove phase).
	if !canRecover {
		return cleanupPhaseDone, fmt.Errorf("workspace path %s exists while pending cleanup remains at %s", r.workspacePath, r.state.CleanupPath)
	}
	r.recoverableLiveWorkspace = true
	return cleanupPhaseUnregister, nil
}

func cleanupUnregisterStep(r *workspaceCleanupRun) (workspaceCleanupPhase, error) {
	if !r.state.NeedsUnregister {
		return cleanupPhaseRemoveStage, nil
	}
	if r.state.CleanupPath == "" {
		if _, statErr := os.Stat(r.workspacePath); statErr == nil {
			return cleanupPhaseDone, fmt.Errorf("workspace path %s exists while pending cleanup remains", r.workspacePath)
		} else if !os.IsNotExist(statErr) {
			return cleanupPhaseDone, statErr
		}
	}
	unregisterRepoPath, err := repoPathForWorkspaceCleanupUnregister(r.ctx, r.repoPath, r.workspacePath, r.state)
	if err != nil {
		return cleanupPhaseDone, err
	}
	if unregisterRepoPath != "" {
		r.state.RepoPath = unregisterRepoPath
	}
	if err := unregisterWorktreeCtx(r.ctx, unregisterRepoPath, r.workspacePath); err != nil {
		if !canSkipUnregisterRetry(unregisterRepoPath, err) {
			return cleanupPhaseDone, err
		}
	} else {
		r.unregisterSatisfied = true
	}
	return cleanupPhaseRemoveStage, nil
}

func cleanupRemoveStageStep(r *workspaceCleanupRun) (workspaceCleanupPhase, error) {
	if r.state.CleanupPath == "" {
		return cleanupPhaseFinalize, nil
	}
	if !isReservedWorkspaceCleanupPath(r.workspacePath, r.state.CleanupPath) {
		return cleanupPhaseDone, fmt.Errorf("refusing unexpected pruned workspace cleanup path for %s: %s", r.workspacePath, r.state.CleanupPath)
	}
	if !isSafeWorkspaceCleanupPath(r.state.CleanupPath) {
		return cleanupPhaseDone, fmt.Errorf("refusing to remove unsafe pruned workspace cleanup path: %s", r.state.CleanupPath)
	}
	if next, handled, err := cleanupRestageMissingStage(r); handled {
		return next, err
	}
	if _, statErr := os.Stat(r.state.CleanupPath); os.IsNotExist(statErr) {
		if _, workspaceErr := os.Stat(r.workspacePath); workspaceErr == nil {
			return cleanupPhaseDone, fmt.Errorf("workspace path %s exists while pending cleanup remains at %s", r.workspacePath, r.state.CleanupPath)
		}
		// Interrupted after a previous run removed the staged directory but
		// before the marker rewrite: nothing left to remove.
		r.state.CleanupPath = ""
		return cleanupPhaseFinalize, nil
	} else if statErr != nil {
		return cleanupPhaseDone, statErr
	}
	if err := removeWorkspacePathCtx(r.ctx, r.state.CleanupPath); err != nil {
		return cleanupPhaseDone, err
	}
	r.state.CleanupPath = ""
	return cleanupPhaseFinalize, nil
}

// cleanupRestageMissingStage handles a missing staged directory while the
// workspace path is still alive: a recoverable workspace is re-staged for
// removal; anything else is an error. handled is true when this function's
// result should be returned (an error path); a successful restage or a
// not-applicable state falls through to the removal logic.
func cleanupRestageMissingStage(r *workspaceCleanupRun) (next workspaceCleanupPhase, handled bool, err error) {
	if _, statErr := os.Stat(r.state.CleanupPath); !os.IsNotExist(statErr) {
		return 0, false, nil
	}
	_, workspaceErr := os.Stat(r.workspacePath)
	if workspaceErr != nil {
		if os.IsNotExist(workspaceErr) {
			return 0, false, nil
		}
		return cleanupPhaseDone, true, workspaceErr
	}
	if !r.recoverableLiveWorkspace {
		return cleanupPhaseDone, true, fmt.Errorf("workspace path %s exists while pending cleanup remains at %s", r.workspacePath, r.state.CleanupPath)
	}
	if err := stageWorkspacePathForCleanupAtPath(r.workspacePath, r.state.CleanupPath); err != nil {
		return cleanupPhaseDone, true, err
	}
	return 0, false, nil
}

func cleanupFinalizeStep(r *workspaceCleanupRun) (workspaceCleanupPhase, error) {
	if !r.unregisterSatisfied {
		r.state.NeedsUnregister = true
		return cleanupPhaseDone, writePendingWorkspaceCleanupState(r.workspacePath, r.state)
	}
	r.state.NeedsUnregister = false
	// writeWorkspaceCleanupState clears the marker when nothing is pending.
	return cleanupPhaseDone, writeWorkspaceCleanupState(r.workspacePath, r.state)
}

func workspaceGitRefFingerprint(workspacePath string) (gitRef string, modTimeUnixNano int64, ok bool, err error) {
	gitFile := filepath.Join(workspacePath, ".git")
	info, content, readErr := statAndReadFileInParentRoot(gitFile)
	if os.IsNotExist(readErr) {
		return "", 0, false, nil
	}
	if readErr != nil {
		return "", 0, false, readErr
	}
	return strings.TrimSpace(string(content)), info.ModTime().UnixNano(), true, nil
}
