package git

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"github.com/andyrewlee/amux/internal/data"
)

var (
	worktreeTimeout         = 30 * time.Second
	worktreeRecoveryReserve = 5 * time.Second
	runGitCtx               = RunGitCtx
	unregisterWorktreeCtx   = unregisterWorktreeAdminDirWithContext
	removeWorkspacePathCtx  = removeWorkspacePathWithContext
	removeWorkspacePathGOOS = runtime.GOOS
	removeRetryMetadataPath = os.RemoveAll
	writeRetryMarkerFile    = writeRetryMarkerFileAtomically
)

const prunedWorkspaceCleanupMarkerSuffix = ".amux-pruned-worktree"

// CreateWorkspace creates a new workspace backed by a git worktree
func CreateWorkspace(repoPath, workspacePath, branch, base string) error {
	if err := prepareWorkspacePathForCreate(repoPath, workspacePath); err != nil {
		return err
	}

	// Create branch from base and checkout into workspace path
	ctx, cancel := context.WithTimeout(context.Background(), worktreeTimeout)
	_, err := runGitCtx(ctx, repoPath, "worktree", "add", "-b", branch, workspacePath, base)
	cancel()
	if err == nil {
		return nil
	}
	if !isBranchAlreadyExistsError(err, branch) {
		return err
	}

	// If the branch already exists, reuse it instead of failing hard.
	// Retry with a fresh timeout context so a slow first attempt does not
	// consume the entire budget for the fallback path.
	retryCtx, retryCancel := context.WithTimeout(context.Background(), worktreeTimeout)
	_, retryErr := runGitCtx(retryCtx, repoPath, "worktree", "add", workspacePath, branch)
	retryCancel()
	if retryErr != nil {
		firstErrMsg := err.Error()
		return fmt.Errorf(
			"worktree add with new branch failed: %s; fallback add existing branch failed: %w",
			firstErrMsg,
			retryErr,
		)
	}
	return nil
}

func prepareWorkspacePathForCreate(repoPath, workspacePath string) error {
	if retryMetadata, marked, err := readWorkspaceCleanupRetryMetadata(workspacePath); err != nil {
		return err
	} else if marked {
		if err := rejectReusedWorkspacePathForRetryMetadataCleanup(workspacePath, retryMetadata); err != nil {
			return fmt.Errorf("workspace path %s has pending cleanup: %w", workspacePath, err)
		}
		cleanupRepoPath := repoPath
		if retryMetadata.RepoPath != "" {
			cleanupRepoPath = retryMetadata.RepoPath
		}
		ctx, cancel := context.WithTimeout(context.Background(), worktreeTimeout)
		defer cancel()
		if err := persistAndResumeWorkspaceCleanup(
			ctx,
			cleanupRepoPath,
			workspacePath,
			retryMetadata.NeedsUnregister,
		); err != nil {
			return fmt.Errorf("workspace path %s has pending cleanup: %w", workspacePath, err)
		}
	}

	state, marked, err := readWorkspaceCleanupState(workspacePath)
	if err != nil {
		return err
	}
	if !marked {
		return nil
	}
	ctx, cancel := context.WithTimeout(context.Background(), worktreeTimeout)
	defer cancel()
	if err := rejectReusedWorkspacePathDuringCleanup(workspacePath, state); err != nil {
		return err
	}
	if err := resumeWorkspaceCleanup(ctx, repoPath, workspacePath, state); err != nil {
		return fmt.Errorf("workspace path %s has pending cleanup: %w", workspacePath, err)
	}

	if marked, err := hasPendingWorkspaceCleanup(workspacePath); err != nil {
		return err
	} else if marked {
		return fmt.Errorf("workspace path %s has pending cleanup", workspacePath)
	}
	return nil
}

func isBranchAlreadyExistsError(err error, branch string) bool {
	if err == nil {
		return false
	}
	branch = strings.ToLower(strings.TrimSpace(branch))
	if branch == "" {
		return false
	}
	// Classify through the structured error: a failed git invocation with the
	// branch-exists message on stderr. Matching is restricted to stderr (never
	// the command line) so a branch name appearing in argv cannot false-match.
	var gitErr *Error
	if !errors.As(err, &gitErr) || gitErr.ExitCode == 0 {
		return false
	}
	// Normalize backtick quoting to a single straight quote so we match git's
	// "a branch named '<x>'" and "branch '<x>'" phrasings without enumerating
	// quote permutations.
	msg := strings.ReplaceAll(strings.ToLower(gitErr.Stderr), "`", "'")
	if strings.Contains(msg, "a branch named '"+branch+"' already exists") {
		return true
	}
	if strings.Contains(msg, "branch '"+branch+"' already exists") {
		return true
	}
	return strings.Contains(msg, "already exists") && strings.Contains(msg, branch)
}

// RemoveWorkspace removes a workspace backed by a git worktree
func RemoveWorkspace(repoPath, workspacePath string) error {
	state, marked, err := readWorkspaceCleanupState(workspacePath)
	if err != nil {
		return err
	}
	if marked {
		ctx, cancel := context.WithTimeout(context.Background(), worktreeTimeout)
		defer cancel()
		return resumeWorkspaceCleanup(ctx, repoPath, workspacePath, state)
	}

	regCtx, regCancel := context.WithTimeout(context.Background(), worktreeTimeout)
	registered, regErr := isRegisteredWorktreeCtx(regCtx, repoPath, workspacePath)
	regCancel()
	if regErr == nil && !registered {
		return cleanupOrValidateUnregisteredWorkspacePath(repoPath, workspacePath)
	}
	if regErr != nil {
		if _, statErr := os.Stat(workspacePath); os.IsNotExist(statErr) {
			return nil
		} else if statErr != nil {
			return statErr
		}
		gitFile := filepath.Join(workspacePath, ".git")
		if _, statErr := os.Stat(gitFile); os.IsNotExist(statErr) {
			if _, marked, retryErr := readWorkspaceCleanupRetryMetadata(workspacePath); retryErr != nil {
				return retryErr
			} else if marked {
				return cleanupOrValidateUnregisteredWorkspacePath(repoPath, workspacePath)
			}
			if isLegacyManagedWorkspacePathForRepo(repoPath, workspacePath) {
				recoveryCtx, recoveryCancel := context.WithTimeout(context.Background(), worktreeRemoveRecoveryTimeout())
				defer recoveryCancel()
				if removeErr := persistAndResumeWorkspaceCleanup(recoveryCtx, repoPath, workspacePath, true); removeErr != nil {
					return errors.Join(regErr, removeErr)
				}
				return nil
			}
			return cleanupOrValidateUnregisteredWorkspacePath(repoPath, workspacePath)
		} else if statErr != nil {
			return statErr
		}
		return regErr
	}

	ctx, cancel := context.WithTimeout(context.Background(), worktreeRemoveCommandTimeout())
	defer cancel()
	_, err = runGitCtx(ctx, repoPath, "worktree", "remove", workspacePath, "--force")
	if err != nil {
		if isGitContextError(err) {
			recoveryCtx, recoveryCancel := context.WithTimeout(context.Background(), worktreeRemoveRecoveryTimeout())
			defer recoveryCancel()
			if removeErr := persistAndResumeWorkspaceCleanup(recoveryCtx, repoPath, workspacePath, true); removeErr != nil {
				return errors.Join(err, removeErr)
			}
			return nil
		}

		// git worktree remove --force unregisters the workspace (removes .git file)
		// but fails to delete the directory if it contains untracked files.
		// If the .git file is gone, the workspace was successfully unregistered
		// and we can safely remove the remaining directory ourselves.
		gitFile := filepath.Join(workspacePath, ".git")
		if _, statErr := os.Stat(gitFile); os.IsNotExist(statErr) {
			if !isSafeWorkspaceCleanupPath(workspacePath) {
				return fmt.Errorf("refusing to remove unsafe path: %s", workspacePath)
			}
			cleanupCtx, cleanupCancel := context.WithTimeout(context.Background(), worktreeTimeout)
			defer cleanupCancel()
			return persistAndResumeWorkspaceCleanup(cleanupCtx, repoPath, workspacePath, false)
		}
		return err
	}
	return nil
}

func rejectReusedWorkspacePathDuringCleanup(workspacePath string, state workspaceCleanupState) error {
	if state.CleanupPath == "" && !state.LegacyAmbiguous {
		return nil
	}
	if _, statErr := os.Stat(workspacePath); statErr == nil {
		if state.LegacyAmbiguous {
			return fmt.Errorf("workspace path %s has a legacy pending cleanup marker and requires manual cleanup", workspacePath)
		}
		return fmt.Errorf("workspace path %s exists while pending cleanup remains at %s", workspacePath, state.CleanupPath)
	} else if !os.IsNotExist(statErr) {
		return statErr
	}
	return nil
}

func validateUnregisteredWorkspacePath(workspacePath string) error {
	gitFile := filepath.Join(workspacePath, ".git")
	if _, statErr := os.Stat(gitFile); statErr == nil {
		return fmt.Errorf("workspace %s has a .git file but is not a registered worktree", workspacePath)
	} else if !os.IsNotExist(statErr) {
		return statErr
	}
	if _, statErr := os.Stat(workspacePath); os.IsNotExist(statErr) {
		return nil
	} else if statErr != nil {
		return statErr
	}
	return newUnregisteredWorkspacePathError(workspacePath)
}

// workspaceCleanupPhase enumerates the phases of resuming an interrupted
// workspace cleanup. Phases run in order; each phase either advances, jumps
// ahead (e.g. nothing staged → finalize), or fails. The explicit table keeps
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

// workspaceCleanupPhaseSteps is the transition table: phase → step function
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
	if stageErr == nil {
		// Both the staged cleanup dir and the workspace path exist: ambiguous.
		return cleanupPhaseDone, fmt.Errorf("workspace path %s exists while pending cleanup remains at %s", r.workspacePath, r.state.CleanupPath)
	}
	// Stage is gone but the workspace lives: only recoverable workspaces may
	// proceed (they get re-staged by the remove phase).
	canRecover, err := canRecoverPendingCleanupWorkspace(r.ctx, r.workspacePath, r.state)
	if err != nil {
		return cleanupPhaseDone, err
	}
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
	info, statErr := os.Stat(gitFile)
	if os.IsNotExist(statErr) {
		return "", 0, false, nil
	}
	if statErr != nil {
		return "", 0, false, statErr
	}
	content, readErr := os.ReadFile(gitFile)
	if readErr != nil {
		return "", 0, false, readErr
	}
	return strings.TrimSpace(string(content)), info.ModTime().UnixNano(), true, nil
}

// DeleteBranch deletes a git branch
func DeleteBranch(repoPath, branch string) error {
	ctx, cancel := context.WithTimeout(context.Background(), worktreeTimeout)
	defer cancel()
	_, err := runGitCtx(ctx, repoPath, "branch", "-D", branch)
	return err
}

// DiscoverWorkspaces discovers git worktrees for a project.
// Returns workspaces with minimal fields populated (Name, Branch, Repo, Root).
// The caller should merge with stored metadata to get full workspace data.
func DiscoverWorkspaces(project *data.Project) ([]data.Workspace, error) {
	ctx, cancel := context.WithTimeout(context.Background(), worktreeTimeout)
	defer cancel()
	output, err := runGitCtx(ctx, project.Path, "worktree", "list", "--porcelain")
	if err != nil {
		return nil, err
	}

	return parseWorktreeList(output, project.Path), nil
}

// parseWorktreeList parses the output of `git worktree list --porcelain`
func parseWorktreeList(output, repoPath string) []data.Workspace {
	var workspaces []data.Workspace
	var current struct {
		path   string
		branch string
		bare   bool
	}

	for _, line := range strings.Split(output, "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			// End of entry, save if we have a path and it's not bare
			if current.path != "" && !current.bare {
				ws := data.Workspace{
					Name:   filepath.Base(current.path),
					Branch: current.branch,
					Repo:   repoPath,
					Root:   current.path,
				}
				workspaces = append(workspaces, ws)
			}
			current.path = ""
			current.branch = ""
			current.bare = false
			continue
		}

		if strings.HasPrefix(line, "worktree ") {
			current.path = strings.TrimPrefix(line, "worktree ")
		} else if strings.HasPrefix(line, "branch ") {
			// Format: "branch refs/heads/main"
			ref := strings.TrimPrefix(line, "branch ")
			current.branch = strings.TrimPrefix(ref, "refs/heads/")
		} else if line == "bare" {
			current.bare = true
		}
	}

	// Handle last entry (if no trailing newline)
	if current.path != "" && !current.bare {
		ws := data.Workspace{
			Name:   filepath.Base(current.path),
			Branch: current.branch,
			Repo:   repoPath,
			Root:   current.path,
		}
		workspaces = append(workspaces, ws)
	}

	return workspaces
}
