package activity

import (
	"github.com/andyrewlee/amux/internal/logging"
	"github.com/andyrewlee/amux/internal/tmux"
)

// capturePaneSafe runs one capture under a recover so a panicking callback
// (the tmux subprocess paths it wraps have panicked before) degrades to
// ok=false for that session — the serial era's panicAsMsg equivalent per
// job — instead of crashing the process from an unprotected worker
// goroutine.
func capturePaneSafe(captureFn CaptureFn, name string, lines int, opts tmux.Options) (content string, ok bool) {
	defer func() {
		if r := recover(); r != nil {
			logging.Error("activity capture-pane panicked for %s: %v", name, r)
			content, ok = "", false
		}
	}()
	return captureFn(name, lines, opts)
}
