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
	const usage = "Usage: amux workspace create <name> --project <path> [--assistant <name>] [--base <branch>] [--from-workspace <id>] [--stack] [--idempotency-key <key>] [--json]"
	fs := newFlagSet("workspace create")
	project := fs.String("project", "", "project repo path (required unless --from-workspace is used)")
	assistant := fs.String("assistant", "", "assistant name (defaults to configured default assistant)")
	base := fs.String("base", "", "base branch (auto-detected if omitted)")
	fromWorkspace := fs.String("from-workspace", "", "parent workspace ID for stack child creation")
	stack := fs.Bool("stack", false, "create as a child workspace of --from-workspace")
	idempotencyKey := fs.String("idempotency-key", "", "idempotency key for safe retries")
	name, err := parseSinglePositionalWithFlags(fs, args)
	if err != nil {
		return returnUsageError(w, wErr, gf, usage, version, err)
	}
	assistantName := strings.ToLower(strings.TrimSpace(*assistant))
	parentWorkspaceRaw := strings.TrimSpace(*fromWorkspace)
	if name == "" || (strings.TrimSpace(*project) == "" && parentWorkspaceRaw == "") {
		return returnUsageError(w, wErr, gf, usage, version, nil)
	}
	if *stack && parentWorkspaceRaw == "" {
		return returnUsageError(
			w,
			wErr,
			gf,
			usage,
			version,
			errors.New("--stack requires --from-workspace"),
		)
	}
	if parentWorkspaceRaw != "" && strings.TrimSpace(*base) != "" {
		return returnUsageError(
			w,
			wErr,
			gf,
			usage,
			version,
			errors.New("--base cannot be used with --from-workspace"),
		)
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

	svc, err := NewServices(version)
	if err != nil {
		return ctx.errResult(ExitInternalError, "init_failed", err.Error(), nil, fmt.Sprintf("failed to initialize: %v", err))
	}

	var parentWS *data.Workspace
	if parentWorkspaceRaw != "" {
		parentID, err := parseWorkspaceIDFlag(parentWorkspaceRaw)
		if err != nil {
			return returnUsageError(w, wErr, gf, usage, version, err)
		}
		parentWS, err = svc.Store.Load(parentID)
		if err != nil {
			if os.IsNotExist(err) {
				return ctx.errResult(ExitNotFound, "not_found", fmt.Sprintf("parent workspace %s not found", parentWorkspaceRaw), map[string]any{"workspace_id": parentWorkspaceRaw})
			}
			return ctx.errResult(ExitInternalError, "load_failed", err.Error(), map[string]any{"workspace_id": parentWorkspaceRaw}, fmt.Sprintf("failed to load parent workspace %s: %v", parentWorkspaceRaw, err))
		}
		if parentWS.Archived {
			return ctx.errResult(ExitUsage, "archived_parent", fmt.Sprintf("parent workspace %s is archived", parentWorkspaceRaw), map[string]any{"workspace_id": parentWorkspaceRaw})
		}
	}

	projectPath := strings.TrimSpace(*project)
	if projectPath != "" {
		projectPath, err = canonicalizeProjectPath(projectPath)
		if err != nil {
			return ctx.errResult(ExitUsage, "invalid_project_path", err.Error(), map[string]any{"project": *project}, fmt.Sprintf("invalid --project path: %v", err))
		}
	}
	if projectPath == "" && parentWS != nil {
		projectPath = parentWS.Repo
		if canonical, cErr := canonicalizeProjectPath(projectPath); cErr == nil {
			projectPath = canonical
		}
	}
	if parentWS != nil && data.NormalizePath(projectPath) != data.NormalizePath(parentWS.Repo) {
		return returnUsageError(
			w,
			wErr,
			gf,
			usage,
			version,
			fmt.Errorf("parent workspace %s belongs to %s, not %s", parentWorkspaceRaw, parentWS.Repo, projectPath),
		)
	}
	if !git.IsGitRepository(projectPath) {
		return ctx.errResult(ExitUsage, "not_git_repo", projectPath+" is not a git repository", nil)
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

	requestedName := strings.TrimSpace(name)
	finalName := requestedName
	if parentWS != nil {
		finalName = data.ComposeChildWorkspaceName(parentWS.Name, requestedName)
	}
	if err := validation.ValidateWorkspaceName(finalName); err != nil {
		return returnUsageError(
			w,
			wErr,
			gf,
			usage,
			version,
			fmt.Errorf("invalid workspace name: %w", err),
		)
	}

	// Determine base branch
	baseBranch := *base
	baseCommitPath := projectPath
	if parentWS != nil {
		baseBranch, err = git.ResolveCurrentBranchOrFallback(parentWS.Root, parentWS.Branch)
		if err != nil {
			return ctx.errResult(
				ExitInternalError,
				"parent_branch_failed",
				err.Error(),
				map[string]any{"workspace_id": string(parentWS.ID()), "workspace_root": parentWS.Root},
				fmt.Sprintf("failed to resolve parent branch for %s: %v", parentWS.Name, err),
			)
		}
		baseCommitPath = parentWS.Root
	} else if baseBranch == "" {
		baseBranch, err = git.GetBaseBranch(projectPath)
		baseBranch = resolveWorkspaceBaseFallback(projectPath, baseBranch, err)
	}
	baseCommit, err := git.ResolveRefCommit(baseCommitPath, baseBranch)
	if err != nil {
		return ctx.errResult(
			ExitInternalError,
			"base_commit_failed",
			err.Error(),
			map[string]any{"base": baseBranch, "repo": baseCommitPath},
			fmt.Sprintf("failed to resolve base commit for %s: %v", baseBranch, err),
		)
	}

	// Compute workspace path
	projectName := filepath.Base(projectPath)
	wsPath := filepath.Join(svc.Config.Paths.WorkspacesRoot, projectName, finalName)
	branchExistedBefore := gitLocalBranchExists(projectPath, finalName)

	// Idempotent path: if the target worktree already exists for this repo, reuse it.
	existingWS, found, err := loadExistingWorkspaceAtPath(
		svc,
		projectPath,
		wsPath,
		finalName,
		baseBranch,
		baseCommit,
		parentWS,
		assistantName,
		assistantExplicit,
	)
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

	createRepoPath := projectPath
	if parentWS != nil {
		createRepoPath = parentWS.Root
	}

	// Create the worktree
	if err := git.CreateWorkspace(createRepoPath, wsPath, finalName, baseBranch); err != nil {
		return ctx.errResult(ExitInternalError, "create_failed", err.Error(), nil, fmt.Sprintf("failed to create workspace: %v", err))
	}

	// Wait for .git file to appear (same pattern as workspace_service.go)
	gitFile := filepath.Join(wsPath, ".git")
	if err := waitForPath(gitFile, workspaceCreateReadyAttempts, workspaceCreateReadyDelay); err != nil {
		cleanupErr := rollbackWorkspaceCreate(projectPath, wsPath, finalName, !branchExistedBefore)
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
	ws := data.NewWorkspace(finalName, finalName, baseBranch, projectPath, wsPath)
	ws.BaseCommit = baseCommit
	ws.Assistant = assistantName
	if parentWS != nil {
		data.ApplyStackParent(ws, parentWS, baseBranch)
	}
	if err := svc.Store.Save(ws); err != nil {
		cleanupErr := rollbackWorkspaceCreate(projectPath, wsPath, finalName, !branchExistedBefore)
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
	projectPath, wsPath, name, baseBranch, baseCommit string,
	parentWS *data.Workspace,
	assistantName string,
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

	branch, err := git.ResolveCurrentBranchOrFallback(wsPath, baseBranch)
	if err != nil {
		return nil, false, err
	}

	id := data.Workspace{Repo: projectPath, Root: wsPath}.ID()
	stored, err := svc.Store.Load(id)
	if err != nil && !os.IsNotExist(err) {
		return nil, false, err
	}

	if os.IsNotExist(err) {
		ws := data.NewWorkspace(name, branch, baseBranch, projectPath, wsPath)
		if parentWS != nil {
			data.ApplyStackParent(ws, parentWS, baseBranch)
		}
		if derivedBaseCommit := deriveExistingWorkspaceBaseCommit(wsPath, ws.Base); derivedBaseCommit != "" {
			ws.BaseCommit = derivedBaseCommit
		} else if strings.TrimSpace(baseCommit) != "" && !ws.HasStackParent() {
			ws.BaseCommit = strings.TrimSpace(baseCommit)
		}
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
	if parentWS == nil {
		if stored.HasStackParent() {
			return nil, false, fmt.Errorf(
				"existing workspace %q is stacked under %q and cannot be reused as a standalone workspace; use `amux workspace reparent %s --root` first",
				name, stored.ParentWorkspaceID, stored.ID(),
			)
		}
	} else {
		if stored.HasStackParent() && stored.ParentWorkspaceID != parentWS.ID() {
			return nil, false, fmt.Errorf(
				"existing workspace %q is stacked under %q, but %q was requested",
				name, stored.ParentWorkspaceID, parentWS.ID(),
			)
		}
		if !stored.HasStackParent() {
			return nil, false, fmt.Errorf(
				"existing workspace %q is standalone and cannot be reused as a stack child; use `amux workspace reparent %s --parent %s` instead",
				name, stored.ID(), parentWS.ID(),
			)
		}
		if strings.TrimSpace(stored.ParentBranch) == "" ||
			strings.TrimSpace(string(stored.StackRootWorkspaceID)) == "" ||
			stored.StackDepth < 1 {
			data.ApplyStackParent(stored, parentWS, baseBranch)
		}
	}
	if strings.TrimSpace(stored.BaseCommit) == "" {
		stored.BaseCommit = deriveExistingWorkspaceBaseCommit(wsPath, stored.Base)
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

func deriveExistingWorkspaceBaseCommit(workspacePath, baseRef string) string {
	baseRef = strings.TrimSpace(baseRef)
	if baseRef == "" {
		return ""
	}
	baseCommit, err := git.MergeBase(workspacePath, "HEAD", baseRef)
	if err != nil {
		return ""
	}
	return strings.TrimSpace(baseCommit)
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
