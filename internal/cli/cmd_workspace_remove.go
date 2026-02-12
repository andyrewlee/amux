package cli

import (
	"fmt"
	"io"
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
			return returnJSONErrorMaybeIdempotent(
				w, wErr, gf, version, "workspace.remove", *idempotencyKey,
				ExitUnsafeBlocked, "confirmation_required", "pass --yes to confirm removal", nil,
			)
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
	if handled, code := maybeReplayIdempotentResponse(
		w, wErr, gf, version, "workspace.remove", *idempotencyKey,
	); handled {
		return code
	}

	svc, err := NewServices(version)
	if err != nil {
		if gf.JSON {
			return returnJSONErrorMaybeIdempotent(
				w, wErr, gf, version, "workspace.remove", *idempotencyKey,
				ExitInternalError, "init_failed", err.Error(), nil,
			)
		}
		Errorf(wErr, "failed to initialize: %v", err)
		return ExitInternalError
	}

	ws, err := svc.Store.Load(wsID)
	if err != nil {
		if os.IsNotExist(err) {
			if gf.JSON {
				return returnJSONErrorMaybeIdempotent(
					w, wErr, gf, version, "workspace.remove", *idempotencyKey,
					ExitNotFound, "not_found", fmt.Sprintf("workspace %s not found", wsID), nil,
				)
			}
			Errorf(wErr, "workspace %s not found", wsID)
			return ExitNotFound
		}
		if gf.JSON {
			return returnJSONErrorMaybeIdempotent(
				w, wErr, gf, version, "workspace.remove", *idempotencyKey,
				ExitInternalError, "metadata_load_failed", err.Error(), map[string]any{"workspace_id": string(wsID)},
			)
		}
		Errorf(wErr, "failed to load workspace metadata %s: %v", wsID, err)
		return ExitInternalError
	}

	if ws.IsPrimaryCheckout() {
		if gf.JSON {
			return returnJSONErrorMaybeIdempotent(
				w, wErr, gf, version, "workspace.remove", *idempotencyKey,
				ExitUnsafeBlocked, "primary_checkout", "cannot remove primary checkout", nil,
			)
		}
		Errorf(wErr, "cannot remove primary checkout")
		return ExitUnsafeBlocked
	}

	// Kill tmux sessions for this workspace
	_ = tmux.KillWorkspaceSessions(string(wsID), svc.TmuxOpts)

	// Remove worktree
	if err := git.RemoveWorkspace(ws.Repo, ws.Root); err != nil {
		if gf.JSON {
			return returnJSONErrorMaybeIdempotent(
				w, wErr, gf, version, "workspace.remove", *idempotencyKey,
				ExitInternalError, "remove_failed", err.Error(), nil,
			)
		}
		Errorf(wErr, "failed to remove worktree: %v", err)
		return ExitInternalError
	}

	// Delete branch (best-effort)
	_ = git.DeleteBranch(ws.Repo, ws.Branch)

	// Delete metadata
	if err := svc.Store.Delete(wsID); err != nil {
		if gf.JSON {
			return returnJSONErrorMaybeIdempotent(
				w, wErr, gf, version, "workspace.remove", *idempotencyKey,
				ExitInternalError, "metadata_delete_failed", err.Error(), nil,
			)
		}
		Errorf(wErr, "failed to delete metadata: %v", err)
		return ExitInternalError
	}

	info := workspaceToInfo(ws)

	if gf.JSON {
		return returnJSONSuccessWithIdempotency(
			w,
			wErr,
			gf,
			version,
			"workspace.remove",
			*idempotencyKey,
			map[string]any{"removed": info},
		)
	}

	PrintHuman(w, func(w io.Writer) {
		fmt.Fprintf(w, "Removed workspace %s (%s)\n", info.Name, info.ID)
	})
	return ExitOK
}
