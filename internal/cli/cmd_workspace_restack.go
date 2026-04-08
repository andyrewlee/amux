package cli

import (
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/andyrewlee/amux/internal/data"
)

func cmdWorkspaceRestack(w, wErr io.Writer, gf GlobalFlags, args []string, version string) int {
	const usage = "Usage: amux workspace restack <id> [--recursive] [--idempotency-key <key>] [--json]"
	fs := newFlagSet("workspace restack")
	recursive := fs.Bool("recursive", false, "restack descendant workspaces after restacking the target")
	idempotencyKey := fs.String("idempotency-key", "", "idempotency key for safe retries")
	wsIDArg, err := parseSinglePositionalWithFlags(fs, args)
	if err != nil {
		return returnUsageError(w, wErr, gf, usage, version, err)
	}
	if strings.TrimSpace(wsIDArg) == "" {
		return returnUsageError(w, wErr, gf, usage, version, nil)
	}
	wsID, err := parseWorkspaceIDFlag(wsIDArg)
	if err != nil {
		return returnUsageError(w, wErr, gf, usage, version, err)
	}

	ctx := &cmdCtx{w: w, wErr: wErr, gf: gf, version: version, cmd: "workspace.restack", idemKey: *idempotencyKey}
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
		return ctx.errResult(ExitUnsafeBlocked, "primary_checkout", "cannot restack the primary checkout", map[string]any{"workspace_id": string(wsID)})
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
	if err := ensureWorkspacesClean(sequence); err != nil {
		return ctx.errResult(ExitUnsafeBlocked, "dirty_workspace", err.Error(), nil)
	}

	snapshots, err := snapshotWorkspaceStates(workspaceSnapshotScope(repoWorkspaces, sequence))
	if err != nil {
		return ctx.errResult(ExitInternalError, "snapshot_failed", err.Error(), nil)
	}

	specs := make([]workspaceMutationSpec, 0, len(sequence))
	for _, ws := range sequence {
		spec := workspaceMutationSpec{Workspace: ws}
		if ws.HasStackParent() {
			parent := index[ws.ParentWorkspaceID]
			if parent == nil {
				return ctx.errResult(
					ExitInternalError,
					"missing_parent",
					fmt.Sprintf("workspace %s is missing parent metadata for %s", ws.ID(), ws.ParentWorkspaceID),
					map[string]any{"workspace_id": string(ws.ID()), "parent_workspace_id": string(ws.ParentWorkspaceID)},
					fmt.Sprintf("workspace %s is missing its parent; use reparent to repair the stack", ws.Name),
				)
			}
			spec.Parent = parent
		} else {
			spec.RootBaseRef = resolveWorkspaceRootBaseRef(ws.Repo, ws.Base)
		}
		specs = append(specs, spec)
	}

	updated, err := executeWorkspaceMutationPlan(svc, specs, snapshots)
	if err != nil {
		return ctx.errResult(ExitInternalError, "restack_failed", err.Error(), map[string]any{"workspace_id": string(wsID)})
	}

	infos := make([]WorkspaceInfo, 0, len(updated))
	for _, ws := range updated {
		infos = append(infos, workspaceToInfo(ws))
	}
	result := map[string]any{
		"workspace": workspaceToInfo(target),
		"restacked": infos,
		"recursive": *recursive,
	}

	if gf.JSON {
		return ctx.successResult(result)
	}

	PrintHuman(w, func(w io.Writer) {
		fmt.Fprintf(w, "Restacked workspace %s (%s)\n", target.Name, target.ID())
		if *recursive && len(updated) > 1 {
			fmt.Fprintf(w, "Updated %d workspaces in stack order\n", len(updated))
		}
	})
	return ExitOK
}
