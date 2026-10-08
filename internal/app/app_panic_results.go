package app

import (
	"fmt"
	"runtime/debug"

	tea "charm.land/bubbletea/v2"

	"github.com/andyrewlee/amux/internal/logging"
)

// panicAsMsg wraps a producer cmd so a panic inside it emits fallback(err) —
// the producer's own typed failure result — instead of propagating to
// SafeCmd's generic messages.Error. The typed result still reaches the
// result handler that clears the producer's in-flight guard; a bare SafeCmd
// conversion routes to an error banner and leaves the guard set for the
// rest of the session, silently disabling the feature.
//
// Used by every set-guard → dispatch-cmd → clear-in-typed-handler
// single-flight producer (git status, run-script status, upgrade, tmux
// activity scan). wrapLifecycleCmd applies the same pattern to workspace
// lifecycle ops.
func panicAsMsg(cmd tea.Cmd, fallback func(err error) tea.Msg) tea.Cmd {
	if cmd == nil {
		return nil
	}
	return func() (msg tea.Msg) {
		defer func() {
			if r := recover(); r != nil {
				logging.Error("panic in producer cmd: %v\n%s", r, debug.Stack())
				msg = fallback(fmt.Errorf("command panic: %v", r))
			}
		}()
		return cmd()
	}
}
