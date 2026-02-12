package cli

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/andyrewlee/amux/internal/data"
	"github.com/andyrewlee/amux/internal/git"
	"github.com/andyrewlee/amux/internal/validation"
)

const (
	workspaceCreateReadyAttempts = 100
	workspaceCreateReadyDelay    = 50 * time.Millisecond
)

func cmdWorkspaceCreate(w, wErr io.Writer, gf GlobalFlags, args []string, version string) int {
	const usage = "Usage: amux workspace create <name> --project <path> [--base <branch>] [--idempotency-key <key>] [--json]"
	fs := newFlagSet("workspace create")
	project := fs.String("project", "", "project repo path (required)")
	base := fs.String("base", "", "base branch (auto-detected if omitted)")
	idempotencyKey := fs.String("idempotency-key", "", "idempotency key for safe retries")
	name, err := parseSinglePositionalWithFlags(fs, args)
	if err != nil {
		return returnUsageError(w, wErr, gf, usage, version, err)
	}
	if name == "" || *project == "" {
		return returnUsageError(w, wErr, gf, usage, version, nil)
	}
	if err := validation.ValidateWorkspaceName(name); err != nil {
		return returnUsageError(
			w,
			wErr,
			gf,
			usage,
			version,
			fmt.Errorf("invalid workspace name: %w", err),
		)
	}
	if *base != "" {
		if err := validation.ValidateBaseRef(*base); err != nil {
			return returnUsageError(
				w,
				wErr,
				gf,
				usage,
				version,
				fmt.Errorf("invalid --base: %w", err),
			)
		}
	}
	if handled, code := maybeReplayIdempotentResponse(
		w, wErr, gf, version, "workspace.create", *idempotencyKey,
	); handled {
		return code
	}

	projectPath, err := canonicalizeProjectPath(*project)
	if err != nil {
		if gf.JSON {
			return returnJSONErrorMaybeIdempotent(
				w, wErr, gf, version, "workspace.create", *idempotencyKey,
				ExitUsage, "invalid_project_path", err.Error(), map[string]any{"project": *project},
			)
		}
		Errorf(wErr, "invalid --project path: %v", err)
		return ExitUsage
	}

	if !git.IsGitRepository(projectPath) {
		if gf.JSON {
			return returnJSONErrorMaybeIdempotent(
				w, wErr, gf, version, "workspace.create", *idempotencyKey,
				ExitUsage, "not_git_repo", projectPath+" is not a git repository", nil,
			)
		}
		Errorf(wErr, "%s is not a git repository", projectPath)
		return ExitUsage
	}

	svc, err := NewServices(version)
	if err != nil {
		if gf.JSON {
			return returnJSONErrorMaybeIdempotent(
				w, wErr, gf, version, "workspace.create", *idempotencyKey,
				ExitInternalError, "init_failed", err.Error(), nil,
			)
		}
		Errorf(wErr, "failed to initialize: %v", err)
		return ExitInternalError
	}

	// Determine base branch
	baseBranch := *base
	if baseBranch == "" {
		baseBranch, err = git.GetBaseBranch(projectPath)
		baseBranch = resolveWorkspaceBaseFallback(projectPath, baseBranch, err)
	}

	// Compute workspace path
	projectName := filepath.Base(projectPath)
	wsPath := filepath.Join(svc.Config.Paths.WorkspacesRoot, projectName, name)

	// Create the worktree
	if err := git.CreateWorkspace(projectPath, wsPath, name, baseBranch); err != nil {
		if gf.JSON {
			return returnJSONErrorMaybeIdempotent(
				w, wErr, gf, version, "workspace.create", *idempotencyKey,
				ExitInternalError, "create_failed", err.Error(), nil,
			)
		}
		Errorf(wErr, "failed to create workspace: %v", err)
		return ExitInternalError
	}

	// Wait for .git file to appear (same pattern as workspace_service.go)
	gitFile := filepath.Join(wsPath, ".git")
	if err := waitForPath(gitFile, workspaceCreateReadyAttempts, workspaceCreateReadyDelay); err != nil {
		cleanupErr := rollbackWorkspaceCreate(projectPath, wsPath, name)
		msg := fmt.Sprintf("workspace setup incomplete: %v", err)
		if cleanupErr != nil {
			msg = fmt.Sprintf("%s (cleanup failed: %v)", msg, cleanupErr)
		}
		if gf.JSON {
			details := map[string]any{
				"workspace_root": wsPath,
				"workspace_id":   name,
				"git_file":       gitFile,
			}
			if cleanupErr != nil {
				details["cleanup_error"] = cleanupErr.Error()
			}
			return returnJSONErrorMaybeIdempotent(
				w, wErr, gf, version, "workspace.create", *idempotencyKey,
				ExitInternalError, "workspace_not_ready", msg, details,
			)
		}
		Errorf(wErr, "%s", msg)
		return ExitInternalError
	}

	// Save metadata
	ws := data.NewWorkspace(name, name, baseBranch, projectPath, wsPath)
	ws.Assistant = svc.Config.ResolvedDefaultAssistant()
	if err := svc.Store.Save(ws); err != nil {
		cleanupErr := rollbackWorkspaceCreate(projectPath, wsPath, name)
		msg := err.Error()
		if cleanupErr != nil {
			msg = fmt.Sprintf("%s (cleanup failed: %v)", msg, cleanupErr)
		}
		if gf.JSON {
			details := map[string]any{
				"workspace_root": wsPath,
				"workspace_id":   name,
			}
			if cleanupErr != nil {
				details["cleanup_error"] = cleanupErr.Error()
			}
			return returnJSONErrorMaybeIdempotent(
				w, wErr, gf, version, "workspace.create", *idempotencyKey,
				ExitInternalError, "save_failed", msg, details,
			)
		}
		Errorf(wErr, "failed to save workspace metadata: %v", err)
		if cleanupErr != nil {
			Errorf(wErr, "cleanup failed: %v", cleanupErr)
		}
		return ExitInternalError
	}

	info := workspaceToInfo(ws)

	if gf.JSON {
		return returnJSONSuccessWithIdempotency(
			w, wErr, gf, version, "workspace.create", *idempotencyKey, info,
		)
	}

	PrintHuman(w, func(w io.Writer) {
		fmt.Fprintf(w, "Created workspace %s (%s) at %s\n", info.Name, info.ID, info.Root)
	})
	return ExitOK
}

func rollbackWorkspaceCreate(repoPath, workspacePath, branch string) error {
	var errs []string

	if err := git.RemoveWorkspace(repoPath, workspacePath); err != nil {
		errs = append(errs, fmt.Sprintf("remove worktree: %v", err))
	}
	if err := git.DeleteBranch(repoPath, branch); err != nil {
		errs = append(errs, fmt.Sprintf("delete branch: %v", err))
	}

	if len(errs) == 0 {
		return nil
	}
	return errors.New(strings.Join(errs, "; "))
}

func canonicalizeProjectPath(projectPath string) (string, error) {
	absPath, err := filepath.Abs(projectPath)
	if err != nil {
		return "", err
	}
	canonicalPath, err := filepath.EvalSymlinks(absPath)
	if err != nil {
		return "", err
	}
	return canonicalPath, nil
}

func waitForPath(path string, attempts int, delay time.Duration) error {
	if attempts <= 0 {
		return fmt.Errorf("%s did not appear in time", path)
	}
	for i := 0; i < attempts; i++ {
		_, err := os.Stat(path)
		if err == nil {
			return nil
		}
		if !os.IsNotExist(err) {
			return err
		}
		time.Sleep(delay)
	}
	return fmt.Errorf("%s did not appear in time", path)
}

func resolveWorkspaceBaseFallback(projectPath, detected string, detectedErr error) string {
	if detectedErr != nil {
		return "HEAD"
	}

	base := strings.TrimSpace(detected)
	if base == "" {
		return "HEAD"
	}
	if gitRefExists(projectPath, base) {
		return base
	}

	remoteBase := "origin/" + base
	if gitRefExists(projectPath, remoteBase) {
		return remoteBase
	}
	return "HEAD"
}

func gitRefExists(repoPath, ref string) bool {
	ref = strings.TrimSpace(ref)
	if ref == "" {
		return false
	}
	_, err := git.RunGitCtx(context.Background(), repoPath, "rev-parse", "--verify", ref)
	return err == nil
}
