package app

import (
	"errors"
	"testing"

	"github.com/andyrewlee/amux/internal/app/workspacesvc"
	"github.com/andyrewlee/amux/internal/data"
	"github.com/andyrewlee/amux/internal/messages"
	"github.com/andyrewlee/amux/internal/testutil"
	"github.com/andyrewlee/amux/internal/ui/dashboard"
)

// TestHandleWorkspaceDeleteFailed_KeepsTombstoneOnUnknownStat proves a failed
// delete whose worktree root cannot be stat-classified (embedded NUL forces a
// non-NotExist error) does NOT clear the delete tombstone — it stays so startup
// recovery cannot destroy a worktree that may exist.
func TestHandleWorkspaceDeleteFailed_KeepsTombstoneOnUnknownStat(t *testing.T) {
	var cleared []data.WorkspaceID
	store := &testutil.FakeWorkspaceStore{
		ClearDeletingFunc: func(id data.WorkspaceID) error {
			cleared = append(cleared, id)
			return nil
		},
	}
	ws := &data.Workspace{Repo: "/repo", Root: "\x00unstatable"}
	app := &App{
		dashboard:        dashboard.New(),
		workspaceService: workspacesvc.New(nil, store, nil, ""),
	}

	app.handleWorkspaceDeleteFailed(messages.WorkspaceDeleteFailed{
		Workspace: ws,
		Err:       errors.New("delete failed"),
	})
	if len(cleared) != 0 {
		t.Fatalf("tombstone cleared on unproven absence: %v", cleared)
	}
}
