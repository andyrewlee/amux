package app

import (
	"errors"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"

	"github.com/andyrewlee/amux/internal/messages"
	"github.com/andyrewlee/amux/internal/testutil/tmuxops"
	"github.com/andyrewlee/amux/internal/tmux"
)

// newCleanupOps builds a FakeTmuxOps whose two kill methods under test return
// the injected results; the fake records every call.
func newCleanupOps(tagCleaned bool, tagErr, prefixErr error) *tmuxops.FakeTmuxOps {
	return &tmuxops.FakeTmuxOps{
		KillSessionsMatchingTagsFunc: func(map[string]string, tmux.Options) (bool, error) {
			return tagCleaned, tagErr
		},
		KillSessionsWithPrefixFunc: func(string, tmux.Options) error {
			return prefixErr
		},
	}
}

// runCleanupCmd executes the command returned by cleanupAllTmuxSessions and
// asserts it produced a messages.Toast (the only message type this command
// emits). It fails the test if the command or its result is the wrong shape.
func runCleanupCmd(t *testing.T, cmd tea.Cmd) messages.Toast {
	t.Helper()
	if cmd == nil {
		t.Fatal("cleanupAllTmuxSessions returned a nil cmd")
	}
	msg := cmd()
	toast, ok := msg.(messages.Toast)
	if !ok {
		t.Fatalf("expected messages.Toast, got %T (%v)", msg, msg)
	}
	return toast
}

func TestCleanupAllTmuxSessions(t *testing.T) {
	prefix := tmux.SessionName("amux") + "-"

	t.Run("nil service warns and skips kills", func(t *testing.T) {
		app := &App{tmuxService: nil}
		toast := runCleanupCmd(t, app.cleanupAllTmuxSessions())

		if toast.Level != messages.ToastWarning {
			t.Fatalf("expected warning toast when tmux unavailable, got %q", toast.Level)
		}
		if toast.Message != "tmux cleanup unavailable" {
			t.Fatalf("unexpected message: %q", toast.Message)
		}
	})

	t.Run("nil service is captured at cmd build time", func(t *testing.T) {
		// The closure snapshots svc when cleanupAllTmuxSessions is called, so a
		// service attached after building the cmd must not be used.
		app := &App{tmuxService: nil}
		cmd := app.cleanupAllTmuxSessions()
		ops := newCleanupOps(false, nil, nil)
		app.tmuxService = ops

		toast := runCleanupCmd(t, cmd)
		if toast.Level != messages.ToastWarning {
			t.Fatalf("expected warning toast, got %q", toast.Level)
		}
		if len(ops.KillTagMatches()) != 0 || len(ops.KilledPrefixes()) != 0 {
			t.Fatalf("service attached after build must not be called; tags=%d prefix=%d", len(ops.KillTagMatches()), len(ops.KilledPrefixes()))
		}
	})

	t.Run("only prefix sessions cleaned reports prefix-only success", func(t *testing.T) {
		ops := newCleanupOps(false, nil, nil)
		app := &App{tmuxService: ops, tmuxOptions: tmux.Options{ServerName: "srv"}}

		toast := runCleanupCmd(t, app.cleanupAllTmuxSessions())

		if toast.Level != messages.ToastSuccess {
			t.Fatalf("expected success toast, got %q", toast.Level)
		}
		want := "Cleaned up " + prefix + "* tmux sessions"
		if toast.Message != want {
			t.Fatalf("expected prefix-only message %q, got %q", want, toast.Message)
		}
		if len(ops.KillTagMatches()) != 1 || len(ops.KilledPrefixes()) != 1 {
			t.Fatalf("expected one tag and one prefix kill, got tags=%d prefix=%d", len(ops.KillTagMatches()), len(ops.KilledPrefixes()))
		}
		if ops.LastKillTagMatch()["@amux"] != "1" || len(ops.LastKillTagMatch()) != 1 {
			t.Fatalf("expected only the @amux=1 tag match, got %v", ops.LastKillTagMatch())
		}
		if ops.KilledPrefixes()[0] != prefix {
			t.Fatalf("expected prefix %q, got %q", prefix, ops.KilledPrefixes()[0])
		}
		if ops.LastKillTagOpts().ServerName != "srv" {
			t.Fatalf("expected captured tmuxOptions to flow through, got %+v", ops.LastKillTagOpts())
		}
	})

	t.Run("tagged and prefix cleaned reports combined success", func(t *testing.T) {
		ops := newCleanupOps(true, nil, nil)
		app := &App{tmuxService: ops}

		toast := runCleanupCmd(t, app.cleanupAllTmuxSessions())

		if toast.Level != messages.ToastSuccess {
			t.Fatalf("expected success toast, got %q", toast.Level)
		}
		want := "Cleaned up @amux and " + prefix + "* tmux sessions"
		if toast.Message != want {
			t.Fatalf("expected combined message %q, got %q", want, toast.Message)
		}
	})

	t.Run("tag kill error is non-fatal and prefix success still reported", func(t *testing.T) {
		// A tag-match failure is only logged; the prefix sweep still runs and, on
		// success, drives the prefix-only success toast (cleanedTagged stays false).
		ops := newCleanupOps(false, errors.New("boom"), nil)
		app := &App{tmuxService: ops}

		toast := runCleanupCmd(t, app.cleanupAllTmuxSessions())

		if toast.Level != messages.ToastSuccess {
			t.Fatalf("expected success toast despite tag error, got %q level %q", toast.Message, toast.Level)
		}
		if len(ops.KilledPrefixes()) != 1 {
			t.Fatalf("expected prefix kill to still run after tag error, got %d calls", len(ops.KilledPrefixes()))
		}
		if strings.Contains(toast.Message, "@amux and") {
			t.Fatalf("a tag error must not claim @amux sessions were cleaned: %q", toast.Message)
		}
	})

	t.Run("prefix kill error returns warning toast", func(t *testing.T) {
		ops := newCleanupOps(true, nil, errors.New("prefix exploded"))
		app := &App{tmuxService: ops}

		toast := runCleanupCmd(t, app.cleanupAllTmuxSessions())

		if toast.Level != messages.ToastWarning {
			t.Fatalf("expected warning toast on prefix failure, got %q", toast.Level)
		}
		if !strings.Contains(toast.Message, "tmux cleanup failed") {
			t.Fatalf("expected failure message, got %q", toast.Message)
		}
		if !strings.Contains(toast.Message, "prefix exploded") {
			t.Fatalf("expected underlying error in message, got %q", toast.Message)
		}
	})

	t.Run("prefix error wins even when tagged sessions were cleaned", func(t *testing.T) {
		// cleanedTagged=true would otherwise produce a success toast; the prefix
		// error path returns first, so the warning must take precedence.
		ops := newCleanupOps(true, nil, errors.New("x"))
		app := &App{tmuxService: ops}

		toast := runCleanupCmd(t, app.cleanupAllTmuxSessions())
		if toast.Level == messages.ToastSuccess {
			t.Fatal("prefix error must not be reported as success even when tags were cleaned")
		}
	})
}

// TestCleanupTmuxOnExit pins the documented no-op contract: sessions are
// persisted across restarts, so exit cleanup must touch nothing and must not
// panic even with a nil service / zero-value App.
func TestCleanupTmuxOnExit(t *testing.T) {
	t.Run("nil service does not panic", func(t *testing.T) {
		app := &App{}
		app.CleanupTmuxOnExit()
	})

	t.Run("never invokes any tmux kill", func(t *testing.T) {
		ops := newCleanupOps(false, nil, nil)
		app := &App{tmuxService: ops, instanceID: "inst-A"}

		app.CleanupTmuxOnExit()

		if len(ops.KillTagMatches()) != 0 || len(ops.KilledPrefixes()) != 0 {
			t.Fatalf("CleanupTmuxOnExit must be a no-op; tags=%d prefix=%d", len(ops.KillTagMatches()), len(ops.KilledPrefixes()))
		}
	})
}
