package app

import (
	"fmt"
	"runtime/debug"

	tea "charm.land/bubbletea/v2"

	"github.com/andyrewlee/amux/internal/data"
	"github.com/andyrewlee/amux/internal/logging"
	"github.com/andyrewlee/amux/internal/messages"
)

// lifecycleOpKind names the workspace lifecycle op a wrapped Cmd performs —
// it selects which typed failure a panic produces.
type lifecycleOpKind string

const (
	lifecycleOpCreate  lifecycleOpKind = "create"
	lifecycleOpDelete  lifecycleOpKind = "delete"
	lifecycleOpShelve  lifecycleOpKind = "shelve"
	lifecycleOpRestore lifecycleOpKind = "restore"
)

// wrapLifecycleCmd converts a panicked workspace-lifecycle op Cmd into the
// op's typed failure message. The generic SafeCmd recovery produces
// messages.Error, which routes to a toast only — the workspace's mutating
// mark would never release (every later lifecycle op on it silently rejected
// and the row filtered out of projects loads) and a bulk drain's head would
// never advance. Emitting the op's own failure type lets the normal
// completion handlers release both.
func wrapLifecycleCmd(cmd tea.Cmd, kind lifecycleOpKind, project *data.Project, ws *data.Workspace) tea.Cmd {
	if cmd == nil {
		return nil
	}
	return func() (msg tea.Msg) {
		defer func() {
			if r := recover(); r != nil {
				wsName := ""
				if ws != nil {
					wsName = ws.Name
				}
				logging.Error("panic in workspace %s op for %s: %v\n%s", kind, wsName, r, debug.Stack())
				err := fmt.Errorf("workspace %s panic: %v", kind, r)
				if ws == nil {
					// No lifecycle mark was stamped for a nil-workspace op —
					// nothing to release; report through the generic path.
					msg = messages.Error{Err: err, Context: "command", Logged: true}
					return
				}
				ids := data.WorkspaceIdentityStrings(ws)
				switch kind {
				case lifecycleOpCreate:
					msg = messages.WorkspaceCreateFailed{Workspace: ws, Err: err}
				case lifecycleOpRestore:
					msg = messages.WorkspaceRestoreFailed{Project: project, Workspace: ws, Err: err, WorkspaceIDs: ids}
				case lifecycleOpShelve:
					msg = messages.WorkspaceShelveFailed{Project: project, Workspace: ws, Err: err, WorkspaceIDs: ids}
				default:
					msg = messages.WorkspaceDeleteFailed{Project: project, Workspace: ws, Err: err, WorkspaceIDs: ids}
				}
			}
		}()
		return cmd()
	}
}
