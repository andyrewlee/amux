package cli

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"strings"

	"github.com/andyrewlee/amux/internal/data"
	"github.com/andyrewlee/amux/internal/git"
)

var (
	cliExecCommandContext        = exec.CommandContext
	cliLookPath                  = exec.LookPath
	errWorkspacePRNoCommitsAhead = errors.New("workspace has no commits ahead of its PR base")
)

type ghPRView struct {
	Number              int                 `json:"number"`
	URL                 string              `json:"url"`
	Title               string              `json:"title"`
	Body                string              `json:"body"`
	HeadRefName         string              `json:"headRefName"`
	HeadRepositoryOwner ghPRRepositoryOwner `json:"headRepositoryOwner"`
	BaseRefName         string              `json:"baseRefName"`
	IsDraft             bool                `json:"isDraft"`
	State               string              `json:"state"`
}

type ghPRRepositoryOwner struct {
	Login string `json:"login"`
}

type workspacePRInfo struct {
	WorkspaceID   string `json:"workspace_id"`
	WorkspaceName string `json:"workspace_name"`
	Number        int    `json:"number"`
	URL           string `json:"url"`
	Title         string `json:"title"`
	Head          string `json:"head"`
	Base          string `json:"base"`
	Draft         bool   `json:"draft"`
	State         string `json:"state"`
	Action        string `json:"action"`
}

func cmdWorkspacePR(w, wErr io.Writer, gf GlobalFlags, args []string, version string) int {
	const usage = "Usage: amux workspace pr <id> [--recursive] [--draft] [--remote <name>] [--title <title>] [--body <body>] [--idempotency-key <key>] [--json]"
	fs := newFlagSet("workspace pr")
	recursive := fs.Bool("recursive", false, "create or retarget pull requests for descendant workspaces too")
	draft := fs.Bool("draft", false, "create new pull requests as drafts")
	remote := fs.String("remote", "origin", "remote to push branches and resolve pull requests against")
	title := fs.String("title", "", "pull request title (single-workspace mode only)")
	body := fs.String("body", "", "pull request body (single-workspace mode only)")
	idempotencyKey := fs.String("idempotency-key", "", "idempotency key for safe retries")
	wsIDArg, err := parseSinglePositionalWithFlags(fs, args)
	if err != nil {
		return returnUsageError(w, wErr, gf, usage, version, err)
	}
	if strings.TrimSpace(wsIDArg) == "" {
		return returnUsageError(w, wErr, gf, usage, version, nil)
	}
	if *recursive && (strings.TrimSpace(*title) != "" || strings.TrimSpace(*body) != "") {
		return returnUsageError(w, wErr, gf, usage, version, errors.New("--title and --body are only supported without --recursive"))
	}
	wsID, err := parseWorkspaceIDFlag(wsIDArg)
	if err != nil {
		return returnUsageError(w, wErr, gf, usage, version, err)
	}

	ctx := &cmdCtx{w: w, wErr: wErr, gf: gf, version: version, cmd: "workspace.pr", idemKey: *idempotencyKey}
	if handled, code := ctx.maybeReplay(); handled {
		return code
	}

	if _, err := cliLookPath("gh"); err != nil {
		return ctx.errResult(ExitDependency, "missing_dependency", "GitHub CLI (gh) is required for workspace PR automation", nil)
	}

	svc, err := NewServices(version)
	if err != nil {
		return ctx.errResult(ExitInternalError, "init_failed", err.Error(), nil, fmt.Sprintf("failed to initialize: %v", err))
	}

	target, err := svc.Store.Load(wsID)
	if err != nil {
		if os.IsNotExist(err) {
			return ctx.errResult(ExitNotFound, "not_found", fmt.Sprintf("workspace %s not found", wsID), nil)
		}
		return ctx.errResult(ExitInternalError, "load_failed", err.Error(), map[string]any{"workspace_id": string(wsID)}, fmt.Sprintf("failed to load workspace %s: %v", wsID, err))
	}
	if target.Archived {
		return ctx.errResult(ExitUsage, "archived_workspace", fmt.Sprintf("workspace %s is archived", wsID), map[string]any{"workspace_id": string(wsID)})
	}
	if target.IsPrimaryCheckout() {
		return ctx.errResult(ExitUnsafeBlocked, "primary_checkout", "cannot create pull requests for the primary checkout", map[string]any{"workspace_id": string(wsID)})
	}
	remoteName := strings.TrimSpace(*remote)
	prRemote, remoteURL, pushURL, err := resolveWorkspacePRRemoteConfig(target.Repo, remoteName)
	if err != nil {
		if strings.TrimSpace(remoteURL) == "" {
			return ctx.errResult(ExitUsage, "missing_remote", err.Error(), map[string]any{"remote": strings.TrimSpace(*remote)})
		}
		return ctx.errResult(
			ExitUsage,
			"unsupported_remote",
			err.Error(),
			map[string]any{"remote": remoteName, "remote_url": remoteURL, "push_url": pushURL},
		)
	}

	repoWorkspaces, err := activeRepoWorkspaces(svc.Store, target.Repo)
	if err != nil {
		return ctx.errResult(ExitInternalError, "list_failed", err.Error(), map[string]any{"repo": target.Repo}, fmt.Sprintf("failed to load repo workspaces: %v", err))
	}
	repoWorkspaces = ensureWorkspaceInList(repoWorkspaces, target)
	index := workspaceIndex(repoWorkspaces)
	sequence := []*data.Workspace{target}
	if *recursive {
		sequence = workspaceSubtree(repoWorkspaces, target.ID())
	}
	if len(sequence) == 0 {
		return ctx.errResult(ExitNotFound, "not_found", fmt.Sprintf("workspace %s not found in repo workspaces", wsID), nil)
	}
	snapshots, err := snapshotWorkspaceStates(workspaceSnapshotScope(repoWorkspaces, sequence))
	if err != nil {
		return ctx.errResult(ExitInternalError, "snapshot_failed", err.Error(), nil)
	}

	affected := make(map[data.WorkspaceID]bool, len(sequence))
	requiredBasePush := make(map[data.WorkspaceID]bool, len(sequence))
	for _, ws := range sequence {
		affected[ws.ID()] = true
	}
	for _, ws := range sequence {
		if ws == nil || !ws.HasStackParent() {
			continue
		}
		parent := workspaceStackParent(ws, index)
		if parent == nil || parent.IsPrimaryCheckout() {
			continue
		}
		requiredBasePush[parent.ID()] = true
	}
	pushedBranches := make(map[string]bool)
	prs := make([]workspacePRInfo, 0, len(sequence))
	skipped := make([]workspacePRInfo, 0)
	for _, ws := range sequence {
		snapshot := snapshots[ws.ID()]
		headBranch := snapshot.Branch
		baseRef, baseErr := resolveWorkspacePRBaseRef(ws, index, snapshots)
		if baseErr != nil {
			return ctx.errResult(
				ExitUnsafeBlocked,
				"broken_stack",
				baseErr.Error(),
				map[string]any{"workspace_id": string(ws.ID()), "parent_workspace_id": string(ws.ParentWorkspaceID)},
			)
		}
		baseRef = strings.TrimSpace(baseRef)
		if baseRef == "" {
			return ctx.errResult(ExitInternalError, "base_branch_failed", "failed to resolve PR base for "+ws.Name, map[string]any{"workspace_id": string(ws.ID())})
		}
		prRepoSelector := resolveWorkspacePRRepoSelector(ws, index, prRemote)
		existingPR, hasExistingPR, err := findWorkspacePRByHead(
			ws.Root,
			prRepoSelector,
			headBranch,
			prRemote.HeadOwner,
		)
		if err != nil {
			return ctx.errResult(ExitInternalError, "pr_failed", err.Error(), map[string]any{"workspace_id": string(ws.ID()), "branch": headBranch})
		}
		if !hasExistingPR {
			ahead, aheadErr := git.CountCommitsAhead(ws.Root, baseRef, headBranch)
			if aheadErr != nil {
				return ctx.errResult(ExitInternalError, "pr_failed", aheadErr.Error(), map[string]any{"workspace_id": string(ws.ID()), "branch": headBranch})
			}
			if ahead <= 0 {
				if *recursive {
					if requiredBasePush[ws.ID()] {
						if err := pushWorkspaceBranch(svc, ws, remoteName, headBranch, workspacePRAllowsForcePush(ws), pushedBranches); err != nil {
							return ctx.errResult(ExitInternalError, "push_failed", err.Error(), map[string]any{"workspace_id": string(ws.ID()), "branch": headBranch})
						}
					}
					skipped = append(skipped, workspacePRInfo{
						WorkspaceID:   string(ws.ID()),
						WorkspaceName: ws.Name,
						Head:          headBranch,
						Base:          normalizeRemoteRefToBranch(ws.Repo, baseRef),
						Action:        "skipped",
					})
					continue
				}
				return ctx.errResult(
					ExitInternalError,
					"pr_failed",
					fmt.Errorf("%w: workspace %s has no commits ahead of %s", errWorkspacePRNoCommitsAhead, ws.Name, normalizeRemoteRefToBranch(ws.Repo, baseRef)).Error(),
					map[string]any{"workspace_id": string(ws.ID()), "branch": headBranch},
				)
			}
		}
		if ws.HasStackParent() {
			parent := workspaceStackParent(ws, index)
			if parent != nil && !parent.IsPrimaryCheckout() && !affected[parent.ID()] {
				parentBranch := snapshots[parent.ID()].Branch
				if err := pushWorkspaceBranch(svc, parent, remoteName, parentBranch, workspacePRAllowsForcePush(parent), pushedBranches); err != nil {
					return ctx.errResult(ExitInternalError, "push_failed", err.Error(), map[string]any{"workspace_id": string(parent.ID()), "branch": parentBranch})
				}
			}
		}
		if err := pushWorkspaceBranch(svc, ws, remoteName, headBranch, workspacePRAllowsForcePush(ws), pushedBranches); err != nil {
			return ctx.errResult(ExitInternalError, "push_failed", err.Error(), map[string]any{"workspace_id": string(ws.ID()), "branch": headBranch})
		}

		prTitle := strings.TrimSpace(*title)
		prBody := strings.TrimSpace(*body)
		if prTitle == "" && prBody != "" {
			prTitle, err = git.GetLastCommitSubject(ws.Root)
			if err != nil || strings.TrimSpace(prTitle) == "" {
				prTitle = ws.Name
			}
		}

		pr, err := ensureWorkspacePR(
			ws,
			prRemote,
			prRepoSelector,
			headBranch,
			baseRef,
			normalizeRemoteRefToBranch(ws.Repo, baseRef),
			prTitle,
			prBody,
			*draft,
			existingPR,
			hasExistingPR,
		)
		if err != nil {
			if *recursive && errors.Is(err, errWorkspacePRNoCommitsAhead) {
				skipped = append(skipped, workspacePRInfo{
					WorkspaceID:   string(ws.ID()),
					WorkspaceName: ws.Name,
					Head:          headBranch,
					Base:          normalizeRemoteRefToBranch(ws.Repo, baseRef),
					Action:        "skipped",
				})
				continue
			}
			return ctx.errResult(ExitInternalError, "pr_failed", err.Error(), map[string]any{"workspace_id": string(ws.ID()), "branch": headBranch})
		}
		prs = append(prs, pr)
	}

	result := map[string]any{
		"workspace":     workspaceToInfo(target),
		"pull_requests": prs,
		"skipped":       skipped,
		"recursive":     *recursive,
		"remote":        remoteName,
	}
	if gf.JSON {
		return ctx.successResult(result)
	}

	PrintHuman(w, func(w io.Writer) {
		for _, pr := range prs {
			fmt.Fprintf(w, "%s: %s (base %s, #%d)\n", pr.WorkspaceName, pr.URL, pr.Base, pr.Number)
		}
		for _, info := range skipped {
			fmt.Fprintf(w, "%s: skipped (no commits ahead of %s)\n", info.WorkspaceName, info.Base)
		}
	})
	return ExitOK
}

func resolveWorkspacePRRemoteConfig(repoPath, remoteName string) (ghPRRemoteConfig, string, string, error) {
	remoteURL, err := git.GetRemoteURL(repoPath, remoteName)
	if err != nil {
		return ghPRRemoteConfig{}, "", "", err
	}
	pushURL, err := git.GetRemotePushURL(repoPath, remoteName)
	if err != nil {
		return ghPRRemoteConfig{}, remoteURL, "", err
	}
	if cfg, cfgErr := ghPRRemoteConfigFromRemoteURLs(remoteURL, pushURL); cfgErr == nil {
		return cfg, remoteURL, pushURL, nil
	}

	rawRemoteURL, rawErr := git.RunGitCtx(context.Background(), repoPath, "config", "--get", "remote."+strings.TrimSpace(remoteName)+".url")
	if rawErr != nil {
		return ghPRRemoteConfig{}, remoteURL, pushURL, ghPRRemoteConfigError(remoteURL, pushURL)
	}
	rawPushURL, rawPushErr := git.RunGitCtx(context.Background(), repoPath, "config", "--get", "remote."+strings.TrimSpace(remoteName)+".pushurl")
	if rawPushErr != nil || strings.TrimSpace(rawPushURL) == "" {
		rawPushURL = rawRemoteURL
	}
	cfg, cfgErr := ghPRRemoteConfigFromRemoteURLs(rawRemoteURL, rawPushURL)
	if cfgErr != nil {
		return ghPRRemoteConfig{}, remoteURL, pushURL, ghPRRemoteConfigError(remoteURL, pushURL)
	}
	return cfg, rawRemoteURL, rawPushURL, nil
}

func ghPRRemoteConfigError(fetchURL, pushURL string) error {
	if _, err := ghPRRemoteConfigFromRemoteURLs(fetchURL, pushURL); err != nil {
		return err
	}
	return errors.New("unsupported remote configuration")
}

func resolveWorkspacePRBaseRef(
	ws *data.Workspace,
	index map[data.WorkspaceID]*data.Workspace,
	snapshots map[data.WorkspaceID]workspaceSnapshot,
) (string, error) {
	if ws == nil {
		return "", nil
	}
	if ws.HasStackParent() {
		parent := index[ws.ParentWorkspaceID]
		if parent == nil {
			return "", fmt.Errorf("workspace %s stack parent %s is missing or archived", ws.Name, ws.ParentWorkspaceID)
		}
		parentSnapshot, ok := snapshots[parent.ID()]
		if !ok || strings.TrimSpace(parentSnapshot.Branch) == "" {
			return "", fmt.Errorf("workspace %s stack parent %s has no usable branch", ws.Name, parent.ID())
		}
		return parentSnapshot.Branch, nil
	}
	return resolveWorkspaceRootBaseRef(ws.Repo, ws.Base), nil
}

func resolveWorkspacePRRepoSelector(
	ws *data.Workspace,
	index map[data.WorkspaceID]*data.Workspace,
	remoteConfig ghPRRemoteConfig,
) string {
	if workspaceUsesStackedPRRepo(ws, index) && strings.TrimSpace(remoteConfig.PushRepoSelector) != "" {
		return strings.TrimSpace(remoteConfig.PushRepoSelector)
	}
	return strings.TrimSpace(remoteConfig.BaseRepoSelector)
}

func pushWorkspaceBranch(
	svc *Services,
	ws *data.Workspace,
	remote, branch string,
	force bool,
	pushed map[string]bool,
) error {
	branch = strings.TrimSpace(branch)
	if branch == "" || pushed[branch] {
		return nil
	}
	workspaceRoot := ""
	if ws != nil {
		workspaceRoot = ws.Root
	}
	if err := git.PushBranch(workspaceRoot, remote, branch); err != nil {
		if !force {
			return err
		}
		if err := git.PushBranchForceWithLease(workspaceRoot, remote, branch); err != nil {
			return err
		}
	}
	if ws != nil && ws.PendingForcePush {
		ws.PendingForcePush = false
		if svc != nil {
			if err := svc.Store.Save(ws); err != nil {
				ws.PendingForcePush = true
				return fmt.Errorf("save pending force-push state for %s: %w", ws.Name, err)
			}
		}
	}
	pushed[branch] = true
	return nil
}

func workspaceStackParent(ws *data.Workspace, index map[data.WorkspaceID]*data.Workspace) *data.Workspace {
	if ws == nil || !ws.HasStackParent() {
		return nil
	}
	return index[ws.ParentWorkspaceID]
}

func workspaceUsesStackedPRRepo(ws *data.Workspace, index map[data.WorkspaceID]*data.Workspace) bool {
	parent := workspaceStackParent(ws, index)
	return parent != nil && !parent.IsPrimaryCheckout()
}

func workspacePRAllowsForcePush(ws *data.Workspace) bool {
	return ws != nil && ws.PendingForcePush
}

func ensureWorkspacePR(
	ws *data.Workspace,
	remoteConfig ghPRRemoteConfig,
	repoSelector string,
	headBranch, compareBaseRef, prBaseBranch, title, body string,
	draft bool,
	existing ghPRView,
	hasExisting bool,
) (workspacePRInfo, error) {
	headSelector := remoteConfig.HeadSelector(headBranch)
	headLookup := strings.TrimSpace(headBranch)
	if hasExisting {
		action := "existing"
		needsEdit := existing.BaseRefName != prBaseBranch
		if strings.TrimSpace(title) != "" && existing.Title != title {
			needsEdit = true
		}
		if strings.TrimSpace(body) != "" && existing.Body != body {
			needsEdit = true
		}
		if needsEdit {
			if err := ghEditPR(ws.Root, repoSelector, existing.Number, prBaseBranch, title, body); err != nil {
				return workspacePRInfo{}, err
			}
			var err error
			existing, err = ghViewPRByNumber(ws.Root, repoSelector, existing.Number)
			if err != nil {
				return workspacePRInfo{}, err
			}
			action = "updated"
		}
		return workspacePRInfo{
			WorkspaceID:   string(ws.ID()),
			WorkspaceName: ws.Name,
			Number:        existing.Number,
			URL:           existing.URL,
			Title:         existing.Title,
			Head:          existing.HeadRefName,
			Base:          existing.BaseRefName,
			Draft:         existing.IsDraft,
			State:         existing.State,
			Action:        action,
		}, nil
	}

	ahead, aheadErr := git.CountCommitsAhead(ws.Root, compareBaseRef, headBranch)
	if aheadErr != nil {
		return workspacePRInfo{}, aheadErr
	}
	if ahead <= 0 {
		return workspacePRInfo{}, fmt.Errorf("%w: workspace %s has no commits ahead of %s", errWorkspacePRNoCommitsAhead, ws.Name, prBaseBranch)
	}
	if err := ghCreatePR(ws.Root, repoSelector, headSelector, prBaseBranch, title, body, draft, ws.Name); err != nil {
		if created, viewErr := ghFindPRByHead(ws.Root, repoSelector, headLookup, remoteConfig.HeadOwner); viewErr == nil {
			return workspacePRInfo{
				WorkspaceID:   string(ws.ID()),
				WorkspaceName: ws.Name,
				Number:        created.Number,
				URL:           created.URL,
				Title:         created.Title,
				Head:          created.HeadRefName,
				Base:          created.BaseRefName,
				Draft:         created.IsDraft,
				State:         created.State,
				Action:        "existing",
			}, nil
		}
		return workspacePRInfo{}, err
	}
	created, err := ghFindPRByHead(ws.Root, repoSelector, headLookup, remoteConfig.HeadOwner)
	if err != nil {
		return workspacePRInfo{}, err
	}
	return workspacePRInfo{
		WorkspaceID:   string(ws.ID()),
		WorkspaceName: ws.Name,
		Number:        created.Number,
		URL:           created.URL,
		Title:         created.Title,
		Head:          created.HeadRefName,
		Base:          created.BaseRefName,
		Draft:         created.IsDraft,
		State:         created.State,
		Action:        "created",
	}, nil
}
