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
	ctx := &cmdCtx{w: w, wErr: wErr, gf: gf, version: version, cmd: "workspace.create", idemKey: *idempotencyKey}

	if handled, code := ctx.maybeReplay(); handled {
		return code
	}

	projectPath, err := canonicalizeProjectPath(*project)
	if err != nil {
		return ctx.errResult(ExitUsage, "invalid_project_path", err.Error(), map[string]any{"project": *project}, fmt.Sprintf("invalid --project path: %v", err))
	}

	if !git.IsGitRepository(projectPath) {
		return ctx.errResult(ExitUsage, "not_git_repo", projectPath+" is not a git repository", nil)
	}

	svc, err := NewServices(version)
	if err != nil {
		return ctx.errResult(ExitInternalError, "init_failed", err.Error(), nil, fmt.Sprintf("failed to initialize: %v", err))
	}
	assistantExplicit := assistantName != ""
	if assistantName == "" {
		assistantName = svc.Config.ResolvedDefaultAssistant()
	}
	if !svc.Config.IsAssistantKnown(assistantName) {
		return ctx.errResult(ExitUsage, "unknown_assistant", "unknown assistant: "+assistantName, nil)
	}

	// Require project to be registered before creating a workspace.
	registered, err := svc.Registry.Projects()
	if err != nil {
		return ctx.errResult(ExitInternalError, "registry_read_failed", err.Error(), nil, fmt.Sprintf("failed to read project registry: %v", err))
	}
	if !isProjectRegistered(registered, projectPath) {
		msg := fmt.Sprintf("project %s is not registered; run `amux project add %s` first", projectPath, projectPath)
		return ctx.errResult(ExitUsage, "project_not_registered", msg, map[string]any{"project": projectPath})
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
	branchExistedBefore := gitLocalBranchExists(projectPath, name)

	// Idempotent path: if the target worktree already exists for this repo, reuse it.
	existingWS, found, err := loadExistingWorkspaceAtPath(svc, projectPath, wsPath, name, baseBranch, assistantName, assistantExplicit)
	if err != nil {
		return ctx.errResult(ExitInternalError, "existing_workspace_check_failed", err.Error(), nil, fmt.Sprintf("failed to check existing workspace: %v", err))
	}
	if found {
		info := workspaceToInfo(existingWS)
		if gf.JSON {
			return ctx.successResult(info)
		}
		PrintHuman(w, func(w io.Writer) {
			fmt.Fprintf(w, "Using existing workspace %s (%s) at %s\n", info.Name, info.ID, info.Root)
		})
		return ExitOK
	}

	// Create the worktree
	if err := git.CreateWorkspace(projectPath, wsPath, name, baseBranch); err != nil {
		return ctx.errResult(ExitInternalError, "create_failed", err.Error(), nil, fmt.Sprintf("failed to create workspace: %v", err))
	}

	// Wait for .git file to appear (same pattern as workspace_service.go)
	gitFile := filepath.Join(wsPath, ".git")
	if err := waitForPath(gitFile, workspaceCreateReadyAttempts, workspaceCreateReadyDelay); err != nil {
		cleanupErr := rollbackWorkspaceCreate(projectPath, wsPath, name, !branchExistedBefore)
		msg := fmt.Sprintf("workspace setup incomplete: %v", err)
		if cleanupErr != nil {
			msg = fmt.Sprintf("%s (cleanup failed: %v)", msg, cleanupErr)
		}
		details := map[string]any{
			"workspace_root": wsPath,
			"workspace_id":   name,
			"git_file":       gitFile,
		}
		if cleanupErr != nil {
			details["cleanup_error"] = cleanupErr.Error()
		}
		return ctx.errResult(ExitInternalError, "workspace_not_ready", msg, details)
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
		details := map[string]any{
			"workspace_root": wsPath,
			"workspace_id":   name,
		}
		if cleanupErr != nil {
			details["cleanup_error"] = cleanupErr.Error()
		}
		return ctx.errResult(
			ExitInternalError,
			"save_failed",
			msg,
			details,
			workspaceCreateSaveFailedHumanMessage(err, cleanupErr),
		)
	}

	info := workspaceToInfo(ws)

	if gf.JSON {
		return ctx.successResult(info)
	}

	PrintHuman(w, func(w io.Writer) {
		fmt.Fprintf(w, "Created workspace %s (%s) at %s\n", info.Name, info.ID, info.Root)
	})
	return ExitOK
}

func workspaceCreateSaveFailedHumanMessage(saveErr, cleanupErr error) string {
	if cleanupErr != nil {
		return fmt.Sprintf("%v (cleanup failed: %v)", saveErr, cleanupErr)
	}
	return fmt.Sprintf("failed to save workspace metadata: %v", saveErr)
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
	assistantExplicit bool,
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
	} else if assistantExplicit && !strings.EqualFold(stored.Assistant, assistantName) {
		return nil, false, fmt.Errorf(
			"existing workspace %q uses assistant %q, but %q was requested; "+
				"use a different workspace name or omit --assistant",
			name, stored.Assistant, assistantName,
		)
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

func gitLocalBranchExists(repoPath, branchName string) bool {
	branchName = strings.TrimSpace(branchName)
	if branchName == "" {
		return false
	}
	_, err := git.RunGitCtx(context.Background(), repoPath, "rev-parse", "--verify", "refs/heads/"+branchName)
	return err == nil
}
