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
