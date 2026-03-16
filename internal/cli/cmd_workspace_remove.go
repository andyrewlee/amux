package cli

import (
	"fmt"
	"io"
	"log/slog"
	"os"
	"strings"

	"github.com/andyrewlee/amux/internal/data"
	"github.com/andyrewlee/amux/internal/git"
	"github.com/andyrewlee/amux/internal/tmux"
)

func cmdWorkspaceRemove(w, wErr io.Writer, gf GlobalFlags, args []string, version string) int {
	const usage = "Usage: amux workspace remove <id> --yes [--idempotency-key <key>] [--json]"
	fs := newFlagSet("workspace remove")
	yes := fs.Bool("yes", false, "confirm removal (required)")
	idempotencyKey := fs.String("idempotency-key", "", "idempotency key for safe retries")
	wsIDArg, err := parseSinglePositionalWithFlags(fs, args)
	if err != nil {
		return returnUsageError(w, wErr, gf, usage, version, err)
	}
	if wsIDArg == "" {
		return returnUsageError(w, wErr, gf, usage, version, nil)
	}
	if !*yes {
		if gf.JSON {
			ReturnError(w, "confirmation_required", "pass --yes to confirm removal", nil, version)
			return ExitUnsafeBlocked
		}
		Errorf(wErr, "pass --yes to confirm removal")
		return ExitUnsafeBlocked
	}
	wsID := data.WorkspaceID(strings.TrimSpace(wsIDArg))
	if !data.IsValidWorkspaceID(wsID) {
		return returnUsageError(
			w,
			wErr,
			gf,
			usage,
			version,
			fmt.Errorf("invalid workspace id: %s", wsIDArg),
		)
	}
	ctx := &cmdCtx{w: w, wErr: wErr, gf: gf, version: version, cmd: "workspace.remove", idemKey: *idempotencyKey}

	if handled, code := ctx.maybeReplay(); handled {
		return code
	}

	svc, err := NewServices(version)
	if err != nil {
		return ctx.errResult(ExitInternalError, "init_failed", err.Error(), nil, fmt.Sprintf("failed to initialize: %v", err))
	}

	ws, err := svc.Store.Load(wsID)
	if err != nil {
		if os.IsNotExist(err) {
			return ctx.errResult(ExitNotFound, "not_found", fmt.Sprintf("workspace %s not found", wsID), nil)
		}
		return ctx.errResult(ExitInternalError, "metadata_load_failed", err.Error(), map[string]any{"workspace_id": string(wsID)}, fmt.Sprintf("failed to load workspace metadata %s: %v", wsID, err))
	}

	if ws.IsPrimaryCheckout() {
		return ctx.errResult(ExitUnsafeBlocked, "primary_checkout", "cannot remove primary checkout", nil)
	}

	// Kill tmux sessions for this workspace
	if err := tmux.KillWorkspaceSessions(string(wsID), svc.TmuxOpts); err != nil {
		slog.Debug("best-effort workspace session kill failed", "workspace", string(wsID), "error", err)
	}

	// Remove worktree
	if err := git.RemoveWorkspace(ws.Repo, ws.Root); err != nil {
		return ctx.errResult(ExitInternalError, "remove_failed", err.Error(), nil, fmt.Sprintf("failed to remove worktree: %v", err))
	}

	// Delete branch (best-effort)
	if err := git.DeleteBranch(ws.Repo, ws.Branch); err != nil {
		slog.Debug("best-effort branch delete failed", "repo", ws.Repo, "branch", ws.Branch, "error", err)
	}

	// Delete metadata
	if err := svc.Store.Delete(wsID); err != nil {
		return ctx.errResult(ExitInternalError, "metadata_delete_failed", err.Error(), nil, fmt.Sprintf("failed to delete metadata: %v", err))
	}

	info := workspaceToInfo(ws)

	if gf.JSON {
		return ctx.successResult(map[string]any{"removed": info})
	}

	PrintHuman(w, func(w io.Writer) {
		fmt.Fprintf(w, "Removed workspace %s (%s)\n", info.Name, info.ID)
	})
	return ExitOK
}
