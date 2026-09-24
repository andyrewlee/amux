package messages

import "github.com/andyrewlee/amux/internal/data"

// RunScriptStatusResult carries an asynchronous run-script status check back
// to the Update loop. The check itself shells tmux, so it runs off the Update
// goroutine; WorkspaceIDs is stamped at request time so a stale result (the
// user switched or shelved the workspace mid-flight) is rejected by identity,
// not by pointer.
type RunScriptStatusResult struct {
	Workspace    *data.Workspace
	WorkspaceIDs []string
	Alive        bool
	LastExit     int
}
