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

	"github.com/andyrewlee/amux/internal/config"
	"github.com/andyrewlee/amux/internal/data"
)

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

// pathWithinManagedRoot returns candidate's path relative to root when it is
// strictly nested under root (the root itself does not count).
func pathWithinManagedRoot(root, candidate string) (string, bool) {
	rel, inside := data.PathWithin(root, candidate)
	if !inside || rel == "." {
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
