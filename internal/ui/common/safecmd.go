package common

import (
	"fmt"
	"runtime/debug"
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/andyrewlee/amux/internal/logging"
	"github.com/andyrewlee/amux/internal/messages"
)

// SafeCmd wraps a command with panic recovery.
func SafeCmd(cmd tea.Cmd) tea.Cmd {
	if cmd == nil {
		return nil
	}
	return func() (msg tea.Msg) {
		defer func() {
			if r := recover(); r != nil {
				logging.Error("panic in command: %v\n%s", r, debug.Stack())
				msg = messages.Error{Err: fmt.Errorf("command panic: %v", r), Context: "command", Logged: true}
			}
		}()
		return cmd()
	}
}

// SafeBatch wraps commands in panic recovery before batching.
func SafeBatch(cmds ...tea.Cmd) tea.Cmd {
	if len(cmds) == 0 {
		return nil
	}
	safe := make([]tea.Cmd, 0, len(cmds))
	for _, cmd := range cmds {
		if cmd == nil {
			continue
		}
		safe = append(safe, SafeCmd(cmd))
	}
	if len(safe) == 0 {
		return nil
	}
	return tea.Batch(safe...)
}

// SafeTick wraps tea.Tick with panic recovery in the callback.
func SafeTick(d time.Duration, fn func(time.Time) tea.Msg) tea.Cmd {
	if fn == nil {
		return nil
	}
	return tea.Tick(d, func(t time.Time) (msg tea.Msg) {
		defer func() {
			if r := recover(); r != nil {
				logging.Error("panic in tick: %v\n%s", r, debug.Stack())
				msg = messages.Error{Err: fmt.Errorf("tick panic: %v", r), Context: "tick", Logged: true}
			}
		}()
		return fn(t)
	})
}
