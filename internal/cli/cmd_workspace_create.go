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
	const usage = "Usage: amux workspace create <name> --project <path> [--assistant <name>] [--base <branch>] [--idempotency-key <key>] [--json]"
	fs := newFlagSet("workspace create")
	project := fs.String("project", "", "project repo path (required)")
	assistant := fs.String("assistant", "", "assistant name (defaults to configured default assistant)")
	base := fs.String("base", "", "base branch (auto-detected if omitted)")
	idempotencyKey := fs.String("idempotency-key", "", "idempotency key for safe retries")
	name, err := parseSinglePositionalWithFlags(fs, args)
	if err != nil {
		return returnUsageError(w, wErr, gf, usage, version, err)
	}
	assistantName := strings.ToLower(strings.TrimSpace(*assistant))
	if name == "" || *project == "" {
		return returnUsageError(w, wErr, gf, usage, version, nil)
	}
	if assistantName != "" {
		if err := validation.ValidateAssistant(assistantName); err != nil {
			return returnUsageError(
				w,
				wErr,
				gf,
				usage,
				version,
				fmt.Errorf("invalid --assistant: %w", err),
			)
		}
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
	if assistantName == "" {
		assistantName = svc.Config.ResolvedDefaultAssistant()
	}
	if !svc.Config.IsAssistantKnown(assistantName) {
		if gf.JSON {
			return returnJSONErrorMaybeIdempotent(
				w, wErr, gf, version, "workspace.create", *idempotencyKey,
				ExitUsage, "unknown_assistant", "unknown assistant: "+assistantName, nil,
			)
		}
		Errorf(wErr, "unknown assistant: %s", assistantName)
		return ExitUsage
	}

	// Require project to be registered before creating a workspace.
	registered, err := svc.Registry.Projects()
	if err != nil {
		if gf.JSON {
			return returnJSONErrorMaybeIdempotent(
				w, wErr, gf, version, "workspace.create", *idempotencyKey,
				ExitInternalError, "registry_read_failed", err.Error(), nil,
			)
		}
		Errorf(wErr, "failed to read project registry: %v", err)
		return ExitInternalError
	}
	if !isProjectRegistered(registered, projectPath) {
		msg := fmt.Sprintf("project %s is not registered; run `amux project add %s` first", projectPath, projectPath)
		if gf.JSON {
			return returnJSONErrorMaybeIdempotent(
				w, wErr, gf, version, "workspace.create", *idempotencyKey,
				ExitUsage, "project_not_registered", msg, map[string]any{"project": projectPath},
			)
		}
		Errorf(wErr, "%s", msg)
		return ExitUsage
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
	branchExistedBefore := gitRefExists(projectPath, name)

	// Idempotent path: if the target worktree already exists for this repo, reuse it.
	existingWS, found, err := loadExistingWorkspaceAtPath(svc, projectPath, wsPath, name, baseBranch, assistantName)
	if err != nil {
		if gf.JSON {
			return returnJSONErrorMaybeIdempotent(
				w, wErr, gf, version, "workspace.create", *idempotencyKey,
				ExitInternalError, "existing_workspace_check_failed", err.Error(), nil,
			)
		}
		Errorf(wErr, "failed to check existing workspace: %v", err)
		return ExitInternalError
	}
	if found {
		info := workspaceToInfo(existingWS)
		if gf.JSON {
			return returnJSONSuccessWithIdempotency(
				w, wErr, gf, version, "workspace.create", *idempotencyKey, info,
			)
		}
		PrintHuman(w, func(w io.Writer) {
			fmt.Fprintf(w, "Using existing workspace %s (%s) at %s\n", info.Name, info.ID, info.Root)
		})
		return ExitOK
	}

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
		cleanupErr := rollbackWorkspaceCreate(projectPath, wsPath, name, !branchExistedBefore)
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
	ws.Assistant = assistantName
	if err := svc.Store.Save(ws); err != nil {
		cleanupErr := rollbackWorkspaceCreate(projectPath, wsPath, name, !branchExistedBefore)
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

func rollbackWorkspaceCreate(repoPath, workspacePath, branch string, deleteBranch bool) error {
	var errs []string

	if err := git.RemoveWorkspace(repoPath, workspacePath); err != nil {
		errs = append(errs, fmt.Sprintf("remove worktree: %v", err))
	}
	if deleteBranch {
		if err := git.DeleteBranch(repoPath, branch); err != nil {
			errs = append(errs, fmt.Sprintf("delete branch: %v", err))
		}
	}

	if len(errs) == 0 {
		return nil
	}
	return errors.New(strings.Join(errs, "; "))
}

func loadExistingWorkspaceAtPath(
	svc *Services,
	projectPath, wsPath, name, baseBranch, assistantName string,
) (*data.Workspace, bool, error) {
	gitFile := filepath.Join(wsPath, ".git")
	if _, err := os.Stat(gitFile); err != nil {
		if os.IsNotExist(err) {
			return nil, false, nil
		}
		return nil, false, err
	}

	workspaceCommonDir, err := canonicalizeGitCommonDir(wsPath)
	if err != nil {
		return nil, false, err
	}
	projectCommonDir, err := canonicalizeGitCommonDir(projectPath)
	if err != nil {
		return nil, false, err
	}
	if workspaceCommonDir != projectCommonDir {
		return nil, false, nil
	}

	branch, err := git.GetCurrentBranch(wsPath)
	if err != nil || strings.TrimSpace(branch) == "" {
		branch = strings.TrimSpace(baseBranch)
		if branch == "" {
			branch = "HEAD"
		}
	}

	id := data.Workspace{Repo: projectPath, Root: wsPath}.ID()
	stored, err := svc.Store.Load(id)
	if err != nil && !os.IsNotExist(err) {
		return nil, false, err
	}

	if os.IsNotExist(err) {
		ws := data.NewWorkspace(name, branch, baseBranch, projectPath, wsPath)
		ws.Assistant = assistantName
		if err := svc.Store.Save(ws); err != nil {
			return nil, false, err
		}
		return ws, true, nil
	}

	if strings.TrimSpace(stored.Name) == "" {
		stored.Name = name
	}
	if strings.TrimSpace(stored.Branch) == "" {
		stored.Branch = branch
	}
	if strings.TrimSpace(stored.Repo) == "" {
		stored.Repo = projectPath
	}
	if strings.TrimSpace(stored.Root) == "" {
		stored.Root = wsPath
	}
	if strings.TrimSpace(stored.Base) == "" {
		stored.Base = baseBranch
	}
	if strings.TrimSpace(stored.Assistant) == "" {
		stored.Assistant = assistantName
	}
	if err := svc.Store.Save(stored); err != nil {
		return nil, false, err
	}
	return stored, true, nil
}

func canonicalizeGitCommonDir(repoPath string) (string, error) {
	raw, err := git.RunGitCtx(context.Background(), repoPath, "rev-parse", "--git-common-dir")
	if err != nil {
		return "", err
	}
	commonDir := strings.TrimSpace(raw)
	if commonDir == "" {
		return "", fmt.Errorf("empty git common dir for %s", repoPath)
	}
	if !filepath.IsAbs(commonDir) {
		commonDir = filepath.Join(repoPath, commonDir)
	}
	resolved, err := filepath.EvalSymlinks(commonDir)
	if err != nil {
		return "", err
	}
	return resolved, nil
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

func isProjectRegistered(registered []string, projectPath string) bool {
	for _, p := range registered {
		canon, err := canonicalizeProjectPath(p)
		if err != nil {
			continue
		}
		if canon == projectPath {
			return true
		}
	}
	return false
}

func gitRefExists(repoPath, ref string) bool {
	ref = strings.TrimSpace(ref)
	if ref == "" {
		return false
	}
	_, err := git.RunGitCtx(context.Background(), repoPath, "rev-parse", "--verify", ref)
	return err == nil
}
