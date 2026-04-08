package cli

import (
	"errors"
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/andyrewlee/amux/internal/data"
)

func cmdWorkspaceReparent(w, wErr io.Writer, gf GlobalFlags, args []string, version string) int {
	const usage = "Usage: amux workspace reparent <id> (--parent <id> | --root) [--idempotency-key <key>] [--json]"
	fs := newFlagSet("workspace reparent")
	parentArg := fs.String("parent", "", "new parent workspace ID")
	root := fs.Bool("root", false, "detach the workspace to the repository base branch")
	idempotencyKey := fs.String("idempotency-key", "", "idempotency key for safe retries")
	wsIDArg, err := parseSinglePositionalWithFlags(fs, args)
	if err != nil {
		return returnUsageError(w, wErr, gf, usage, version, err)
	}
	if strings.TrimSpace(wsIDArg) == "" {
		return returnUsageError(w, wErr, gf, usage, version, nil)
	}
	if (*root && strings.TrimSpace(*parentArg) != "") || (!*root && strings.TrimSpace(*parentArg) == "") {
		return returnUsageError(w, wErr, gf, usage, version, errors.New("use exactly one of --parent or --root"))
	}
	wsID, err := parseWorkspaceIDFlag(wsIDArg)
	if err != nil {
		return returnUsageError(w, wErr, gf, usage, version, err)
	}

	ctx := &cmdCtx{w: w, wErr: wErr, gf: gf, version: version, cmd: "workspace.reparent", idemKey: *idempotencyKey}
	if handled, code := ctx.maybeReplay(); handled {
		return code
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
		return ctx.errResult(ExitUnsafeBlocked, "primary_checkout", "cannot reparent the primary checkout", map[string]any{"workspace_id": string(wsID)})
	}

	repoWorkspaces, err := activeRepoWorkspaces(svc.Store, target.Repo)
	if err != nil {
		return ctx.errResult(ExitInternalError, "list_failed", err.Error(), map[string]any{"repo": target.Repo}, fmt.Sprintf("failed to load repo workspaces: %v", err))
	}
	repoWorkspaces = ensureWorkspaceInList(repoWorkspaces, target)
	index := workspaceIndex(repoWorkspaces)

	var newParent *data.Workspace
	if !*root {
		parentID, err := parseWorkspaceIDFlag(strings.TrimSpace(*parentArg))
		if err != nil {
			return returnUsageError(w, wErr, gf, usage, version, err)
		}
		newParent, err = svc.Store.Load(parentID)
		if err != nil {
			if os.IsNotExist(err) {
				return ctx.errResult(ExitNotFound, "not_found", fmt.Sprintf("parent workspace %s not found", parentID), nil)
			}
			return ctx.errResult(ExitInternalError, "load_failed", err.Error(), map[string]any{"workspace_id": string(parentID)}, fmt.Sprintf("failed to load workspace %s: %v", parentID, err))
		}
		if newParent.Archived {
			return ctx.errResult(ExitUsage, "archived_parent", fmt.Sprintf("parent workspace %s is archived", parentID), map[string]any{"workspace_id": string(parentID)})
		}
		if data.NormalizePath(newParent.Repo) != data.NormalizePath(target.Repo) {
			return returnUsageError(w, wErr, gf, usage, version, fmt.Errorf("workspace %s belongs to a different repository", parentID))
		}
		if newParent.ID() == target.ID() {
			return returnUsageError(w, wErr, gf, usage, version, errors.New("workspace cannot be its own parent"))
		}
		if descendantSet(repoWorkspaces, target.ID())[newParent.ID()] {
			return returnUsageError(w, wErr, gf, usage, version, fmt.Errorf("cannot reparent %s under its own descendant %s", target.Name, newParent.Name))
		}
		repoWorkspaces = ensureWorkspaceInList(repoWorkspaces, newParent)
		index = workspaceIndex(repoWorkspaces)
	}

	sequence := workspaceSubtree(repoWorkspaces, target.ID())
	if len(sequence) == 0 {
		return ctx.errResult(ExitNotFound, "not_found", fmt.Sprintf("workspace %s not found in repo workspaces", wsID), nil)
	}
	if err := ensureWorkspacesClean(sequence); err != nil {
		return ctx.errResult(ExitUnsafeBlocked, "dirty_workspace", err.Error(), nil)
	}
	snapshots, err := snapshotWorkspaceStates(workspaceSnapshotScope(repoWorkspaces, sequence, newParent))
	if err != nil {
		return ctx.errResult(ExitInternalError, "snapshot_failed", err.Error(), nil)
	}

	specs := make([]workspaceMutationSpec, 0, len(sequence))
	repoBaseRef := resolveRepoBaseRef(target.Repo)
	for _, ws := range sequence {
		spec := workspaceMutationSpec{Workspace: ws}
		if ws.ID() == target.ID() {
			if newParent != nil {
				spec.Parent = newParent
			} else {
				spec.RootBaseRef = repoBaseRef
			}
			specs = append(specs, spec)
			continue
		}
		parent := index[ws.ParentWorkspaceID]
		if parent == nil {
			return ctx.errResult(
				ExitInternalError,
				"missing_parent",
				fmt.Sprintf("workspace %s is missing parent metadata for %s", ws.ID(), ws.ParentWorkspaceID),
				map[string]any{"workspace_id": string(ws.ID()), "parent_workspace_id": string(ws.ParentWorkspaceID)},
				fmt.Sprintf("workspace %s is missing its parent; repair it with a direct reparent first", ws.Name),
			)
		}
		spec.Parent = parent
		specs = append(specs, spec)
	}

	updated, err := executeWorkspaceMutationPlan(svc, specs, snapshots)
	if err != nil {
		return ctx.errResult(ExitInternalError, "reparent_failed", err.Error(), map[string]any{"workspace_id": string(wsID)})
	}

	infos := make([]WorkspaceInfo, 0, len(updated))
	for _, ws := range updated {
		infos = append(infos, workspaceToInfo(ws))
	}
	result := map[string]any{
		"workspace":  workspaceToInfo(target),
		"updated":    infos,
		"to_root":    *root,
		"new_parent": "",
	}
	if newParent != nil {
		result["new_parent"] = string(newParent.ID())
	}

	if gf.JSON {
		return ctx.successResult(result)
	}

	PrintHuman(w, func(w io.Writer) {
		if newParent != nil {
			fmt.Fprintf(w, "Reparented workspace %s (%s) under %s (%s)\n", target.Name, target.ID(), newParent.Name, newParent.ID())
		} else {
			fmt.Fprintf(w, "Reparented workspace %s (%s) to the repository base\n", target.Name, target.ID())
		}
		if len(updated) > 1 {
			fmt.Fprintf(w, "Updated %d workspaces in stack order\n", len(updated))
		}
	})
	return ExitOK
}
