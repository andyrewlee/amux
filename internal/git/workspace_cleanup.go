package git

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"github.com/andyrewlee/amux/internal/config"
)

var (
	errWorkspaceCleanupRepoUnavailable  = errors.New("workspace cleanup repo unavailable")
	errWorkspaceCleanupAdminDirNotFound = errors.New("workspace cleanup admin dir not found")
)

func reserveWorkspacePathForCleanup(workspacePath string) (string, error) {
	dir := filepath.Dir(workspacePath)
	base := filepath.Base(workspacePath)
	for attempt := 0; attempt < 8; attempt++ {
		candidate := filepath.Join(
			dir,
			fmt.Sprintf(".%s.amux-prune-%d-%d", base, time.Now().UnixNano(), attempt),
		)
		if _, err := os.Stat(candidate); err == nil {
			continue
		} else if !os.IsNotExist(err) {
			return "", err
		}
		return candidate, nil
	}
	return "", fmt.Errorf("failed to stage workspace %s for prune", workspacePath)
}

func isReservedWorkspaceCleanupPath(workspacePath, cleanupPath string) bool {
	if workspacePath == "" || cleanupPath == "" {
		return false
	}
	workspaceParentPaths := comparablePathSet(filepath.Dir(workspacePath))
	if !pathSetContains(workspaceParentPaths, filepath.Dir(cleanupPath)) {
		return false
	}

	expectedPrefixes := make(map[string]struct{}, 4)
	for _, candidate := range comparablePaths(workspacePath) {
		expectedPrefixes["."+filepath.Base(candidate)+".amux-prune-"] = struct{}{}
	}
	for _, candidate := range comparablePaths(cleanupPath) {
		cleanupBase := filepath.Base(candidate)
		for prefix := range expectedPrefixes {
			if strings.HasPrefix(cleanupBase, prefix) {
				return true
			}
		}
	}
	return false
}

func stageWorkspacePathForCleanupAtPath(workspacePath, stagedPath string) error {
	if err := os.Rename(workspacePath, stagedPath); err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return err
	}
	return nil
}

func unregisterWorktreeAdminDirWithContext(ctx context.Context, repoPath, workspacePath string) error {
	commonGitDir, adminDir, err := worktreeAdminDirForWorkspace(ctx, repoPath, workspacePath)
	if err != nil {
		return err
	}
	if adminDir == "" {
		registered, regErr := isRegisteredWorktreeCtx(ctx, repoPath, workspacePath)
		if regErr != nil {
			return regErr
		}
		if registered {
			return fmt.Errorf("%w: %s", errWorkspaceCleanupAdminDirNotFound, workspacePath)
		}
		return nil
	}
	if !isSafeWorktreeAdminCleanupPath(commonGitDir, adminDir) {
		return fmt.Errorf("refusing to remove unsafe worktree admin dir: %s", adminDir)
	}
	return removeWorkspacePathCtx(ctx, adminDir)
}

func worktreeAdminDirForWorkspace(ctx context.Context, repoPath, workspacePath string) (string, string, error) {
	commonGitDir, err := gitCommonDirWithContext(ctx, repoPath)
	if err != nil {
		return "", "", err
	}
	worktreesDir := filepath.Join(commonGitDir, "worktrees")
	entries, err := os.ReadDir(worktreesDir)
	if os.IsNotExist(err) {
		return commonGitDir, "", nil
	}
	if err != nil {
		return "", "", err
	}
	expectedPaths := comparablePathSet(filepath.Join(workspacePath, ".git"))
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		adminDir := filepath.Join(worktreesDir, entry.Name())
		gitdirFile := filepath.Join(adminDir, "gitdir")
		gitdirBytes, err := readFileInParentRoot(gitdirFile)
		if os.IsNotExist(err) {
			continue
		}
		if err != nil {
			return "", "", err
		}
		gitdirPath := strings.TrimSpace(string(gitdirBytes))
		if gitdirPath == "" {
			continue
		}
		resolvedGitdirPath := resolveWorktreeGitdirPath(gitdirFile, gitdirPath)
		if pathSetContains(expectedPaths, resolvedGitdirPath) {
			return commonGitDir, adminDir, nil
		}
	}
	return commonGitDir, "", nil
}

func resolveWorktreeGitdirPath(gitdirFilePath, gitdirPath string) string {
	if filepath.IsAbs(gitdirPath) {
		return filepath.Clean(gitdirPath)
	}
	return filepath.Clean(filepath.Join(filepath.Dir(gitdirFilePath), gitdirPath))
}

func gitCommonDirWithContext(ctx context.Context, repoPath string) (string, error) {
	if _, err := os.Stat(repoPath); os.IsNotExist(err) {
		return "", fmt.Errorf("%w: %s", errWorkspaceCleanupRepoUnavailable, repoPath)
	} else if err != nil {
		return "", err
	}
	output, err := runGitCtx(ctx, repoPath, "rev-parse", "--git-common-dir")
	if err != nil {
		if isGitRepoUnavailableError(err) {
			return "", errors.Join(errWorkspaceCleanupRepoUnavailable, err)
		}
		return "", err
	}
	commonGitDir := strings.TrimSpace(output)
	if commonGitDir == "" {
		return "", fmt.Errorf("git rev-parse --git-common-dir returned empty output for %s", repoPath)
	}
	if !filepath.IsAbs(commonGitDir) {
		commonGitDir = filepath.Join(repoPath, commonGitDir)
	}
	return filepath.Clean(commonGitDir), nil
}

func isGitRepoUnavailableError(err error) bool {
	msg := strings.ToLower(err.Error())
	return strings.Contains(msg, "not a git repository")
}

func repoPathForWorkspaceCleanupUnregister(
	ctx context.Context,
	repoPath, workspacePath string,
	state workspaceCleanupState,
) (string, error) {
	if state.RepoPath == "" {
		return "", fmt.Errorf("workspace cleanup marker for %s is missing repo path for unregister recovery", workspacePath)
	}
	for _, candidate := range []string{repoPath, state.RepoPath} {
		if candidate == "" {
			continue
		}
		_, adminDir, err := worktreeAdminDirForWorkspace(ctx, candidate, workspacePath)
		if err != nil {
			if canSkipUnregisterRetry(candidate, err) {
				continue
			}
			return "", err
		}
		if adminDir != "" || candidate == state.RepoPath {
			return candidate, nil
		}
	}
	return state.RepoPath, nil
}

func comparablePathSet(path string) map[string]struct{} {
	paths := map[string]struct{}{}
	for _, candidate := range comparablePaths(path) {
		paths[candidate] = struct{}{}
	}
	return paths
}

func pathSetContains(paths map[string]struct{}, path string) bool {
	for _, candidate := range comparablePaths(path) {
		if _, ok := paths[candidate]; ok {
			return true
		}
	}
	return false
}

func comparablePaths(path string) []string {
	var candidates []string
	seen := map[string]struct{}{}
	add := func(candidate string) {
		candidate = filepath.Clean(candidate)
		if candidate == "" {
			return
		}
		if _, ok := seen[candidate]; ok {
			return
		}
		seen[candidate] = struct{}{}
		candidates = append(candidates, candidate)
		if runtime.GOOS == "windows" {
			lower := strings.ToLower(candidate)
			if _, ok := seen[lower]; !ok {
				seen[lower] = struct{}{}
				candidates = append(candidates, lower)
			}
		}
	}

	add(path)
	if absPath, err := filepath.Abs(path); err == nil {
		add(absPath)
	}
	if resolvedPath, ok := resolvePathWithExistingPrefix(path); ok {
		add(resolvedPath)
	}
	if absPath, err := filepath.Abs(path); err == nil {
		if resolvedPath, ok := resolvePathWithExistingPrefix(absPath); ok {
			add(resolvedPath)
		}
	}
	return candidates
}

func resolvePathWithExistingPrefix(path string) (string, bool) {
	cleanPath := filepath.Clean(path)
	if cleanPath == "" {
		return "", false
	}
	current := cleanPath
	var suffix []string
	for {
		if _, err := os.Stat(current); err == nil {
			resolvedCurrent, err := filepath.EvalSymlinks(current)
			if err != nil {
				return "", false
			}
			parts := append([]string{resolvedCurrent}, suffix...)
			return filepath.Join(parts...), true
		}
		parent := filepath.Dir(current)
		if parent == current {
			return "", false
		}
		suffix = append([]string{filepath.Base(current)}, suffix...)
		current = parent
	}
}

func canSkipUnregisterRetry(repoPath string, err error) bool {
	return err != nil && errors.Is(err, errWorkspaceCleanupRepoUnavailable)
}

func isRegisteredWorktreeCtx(ctx context.Context, repoPath, workspacePath string) (bool, error) {
	output, err := runGitCtx(ctx, repoPath, "worktree", "list", "--porcelain")
	if err != nil {
		return false, err
	}
	expectedPaths := comparablePathSet(workspacePath)
	for _, line := range strings.Split(output, "\n") {
		trimmed := strings.TrimSpace(line)
		if strings.HasPrefix(trimmed, "worktree ") {
			wtPath := strings.TrimPrefix(trimmed, "worktree ")
			if pathSetContains(expectedPaths, wtPath) {
				return true, nil
			}
		}
	}
	return false, nil
}

func isSafeWorktreeAdminCleanupPath(commonGitDir, adminDir string) bool {
	worktreesDir := filepath.Clean(filepath.Join(commonGitDir, "worktrees"))
	cleanAdminDir := filepath.Clean(adminDir)
	if cleanAdminDir == worktreesDir {
		return false
	}
	rel, err := filepath.Rel(worktreesDir, cleanAdminDir)
	if err != nil {
		return false
	}
	return rel != "." && rel != ".." && !strings.HasPrefix(rel, ".."+string(os.PathSeparator))
}

func worktreeRemoveCommandTimeout() time.Duration {
	return worktreeTimeout
}

func worktreeRemoveRecoveryTimeout() time.Duration {
	if worktreeRecoveryReserve <= 0 {
		return worktreeTimeout
	}
	return worktreeTimeout + worktreeRecoveryReserve
}

func cleanupOrValidateUnregisteredWorkspacePath(repoPath, workspacePath string) error {
	gitFile := filepath.Join(workspacePath, ".git")
	if _, statErr := os.Stat(gitFile); statErr == nil {
		return validateUnregisteredWorkspacePath(workspacePath)
	} else if !os.IsNotExist(statErr) {
		return statErr
	}
	if retryMetadata, marked, err := readWorkspaceCleanupRetryMetadata(workspacePath); err != nil {
		return err
	} else if marked {
		if err := rejectReusedWorkspacePathForRetryMetadataCleanup(workspacePath, retryMetadata); err != nil {
			return err
		}
		cleanupRepoPath := repoPath
		if retryMetadata.RepoPath != "" {
			cleanupRepoPath = retryMetadata.RepoPath
		}
		ctx, cancel := context.WithTimeout(context.Background(), worktreeTimeout)
		defer cancel()
		return persistAndResumeWorkspaceCleanup(
			ctx,
			cleanupRepoPath,
			workspacePath,
			retryMetadata.NeedsUnregister,
		)
	}
	return validateUnregisteredWorkspacePath(workspacePath)
}

func managedWorkspacesRootAliases() []string {
	roots := make(map[string]struct{}, 8)
	if configuredRoot := strings.TrimSpace(os.Getenv(config.WorkspacesRootEnvVar)); configuredRoot != "" {
		for _, alias := range comparablePaths(configuredRoot) {
			roots[alias] = struct{}{}
		}
		result := make([]string, 0, len(roots))
		for root := range roots {
			result = append(result, root)
		}
		return result
	}
	paths, err := config.DefaultPaths()
	if err == nil && paths != nil && strings.TrimSpace(paths.WorkspacesRoot) != "" {
		for _, alias := range comparablePaths(paths.WorkspacesRoot) {
			roots[alias] = struct{}{}
		}
	}
	if len(roots) == 0 {
		return nil
	}
	result := make([]string, 0, len(roots))
	for root := range roots {
		result = append(result, root)
	}
	return result
}

func pathWithinManagedRoot(root, candidate string) (string, bool) {
	if root == "" || candidate == "" {
		return "", false
	}
	rel, err := filepath.Rel(root, candidate)
	if err != nil {
		return "", false
	}
	if rel == "." || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return "", false
	}
	return rel, true
}

func isGitContextError(err error) bool {
	return errors.Is(err, context.DeadlineExceeded) || errors.Is(err, context.Canceled)
}

func removeWorkspacePathWithContext(ctx context.Context, workspacePath string) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if _, err := os.Stat(workspacePath); err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return err
	}

	cmd := buildRemoveWorkspaceCommand(ctx, workspacePath)

	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		if ctxErr := ctx.Err(); ctxErr != nil {
			return ctxErr
		}
		if stderr.Len() > 0 {
			return fmt.Errorf("remove %s: %s", workspacePath, strings.TrimSpace(stderr.String()))
		}
		return err
	}
	return nil
}

func buildRemoveWorkspaceCommand(ctx context.Context, workspacePath string) *exec.Cmd {
	switch removeWorkspacePathGOOS {
	case "windows":
		cmd := exec.CommandContext(
			ctx,
			"powershell",
			"-NoProfile",
			"-NonInteractive",
			"-Command",
			"Remove-Item -LiteralPath $env:AMUX_REMOVE_PATH -Recurse -Force -ErrorAction Stop",
		)
		cmd.Env = append(os.Environ(), "AMUX_REMOVE_PATH="+workspacePath)
		return cmd
	default:
		return exec.CommandContext(ctx, "rm", "-rf", "--", workspacePath)
	}
}

func isSafeWorkspaceCleanupPath(path string) bool {
	if path == "" {
		return false
	}
	cleaned := filepath.Clean(path)
	if cleaned == "/" || cleaned == "." {
		return false
	}
	home, err := os.UserHomeDir()
	if err == nil && cleaned == filepath.Clean(home) {
		return false
	}
	return true
}
