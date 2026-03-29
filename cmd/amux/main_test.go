//go:build !windows

package main

import (
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
)

func resetMouseFilterState() {
	lastMouseMotionEvent = time.Time{}
	lastMouseWheelEvent = time.Time{}
	lastMouseX = 0
	lastMouseY = 0
}

func TestMouseWheelNotThrottledByMotion(t *testing.T) {
	resetMouseFilterState()

	motion := tea.MouseMotionMsg{X: 10, Y: 10, Button: tea.MouseLeft}
	if mouseEventFilter(nil, motion) == nil {
		t.Fatalf("expected motion event to pass through")
	}

	wheel := tea.MouseWheelMsg{X: 10, Y: 10, Button: tea.MouseWheelDown}
	if mouseEventFilter(nil, wheel) == nil {
		t.Fatalf("expected wheel event to pass through after motion")
	}
}

func TestMouseWheelThrottleIndependent(t *testing.T) {
	resetMouseFilterState()

	wheel := tea.MouseWheelMsg{X: 10, Y: 10, Button: tea.MouseWheelDown}
	if mouseEventFilter(nil, wheel) == nil {
		t.Fatalf("expected first wheel event to pass through")
	}
	if mouseEventFilter(nil, wheel) != nil {
		t.Fatalf("expected second wheel event to be throttled")
	}
}

func TestIsVersionInvocation(t *testing.T) {
	tests := []struct {
		name string
		args []string
		want bool
	}{
		{name: "long flag", args: []string{"--version"}, want: true},
		{name: "short flag", args: []string{"-v"}, want: true},
		{name: "no args", args: nil, want: false},
		{name: "unexpected command", args: []string{"status"}, want: false},
		{name: "extra args after version", args: []string{"--version", "status"}, want: false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := isVersionInvocation(tt.args); got != tt.want {
				t.Fatalf("isVersionInvocation() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestUnsupportedInvocationMessage(t *testing.T) {
	tests := []struct {
		name string
		arg  string
		want string
	}{
		{name: "unexpected command", arg: "status", want: `unexpected argument "status"`},
		{name: "tui subcommand hint", arg: "tui", want: "run `amux` directly to start the terminal UI"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := unsupportedInvocationMessage(tt.arg); !strings.Contains(got, tt.want) {
				t.Fatalf("unsupportedInvocationMessage() = %q, want substring %q", got, tt.want)
			}
		})
	}
}

func TestNonInteractiveMessage(t *testing.T) {
	if got := nonInteractiveMessage(); !strings.Contains(got, "interactive terminal") {
		t.Fatalf("nonInteractiveMessage() = %q, want interactive-terminal guidance", got)
	}
}

func TestShouldLaunchTUIRequiresAllTTYStreams(t *testing.T) {
	tests := []struct {
		name      string
		stdinTTY  bool
		stdoutTTY bool
		stderrTTY bool
		want      bool
	}{
		{name: "all tty", stdinTTY: true, stdoutTTY: true, stderrTTY: true, want: true},
		{name: "stdout redirected", stdinTTY: true, stdoutTTY: false, stderrTTY: true, want: false},
		{name: "stdin non tty", stdinTTY: false, stdoutTTY: true, stderrTTY: true, want: false},
		{name: "stderr non tty", stdinTTY: true, stdoutTTY: true, stderrTTY: false, want: false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := shouldLaunchTUI(tt.stdinTTY, tt.stdoutTTY, tt.stderrTTY); got != tt.want {
				t.Fatalf("shouldLaunchTUI() = %v, want %v", got, tt.want)
			}
		})
	}
}
