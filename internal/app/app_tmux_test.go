package app

import (
	"errors"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"

	"github.com/andyrewlee/amux/internal/messages"
	"github.com/andyrewlee/amux/internal/tmux"
)

// cleanupRecordingTmuxOps records the kill calls cleanupAllTmuxSessions makes and
// lets each test inject the (bool, error) / error results that drive the toast
// branches. It embeds stubTmuxOps so only the two kill methods under test need
// overriding.
type cleanupRecordingTmuxOps struct {
	stubTmuxOps

	tagCleaned  bool
	tagErr      error
	prefixErr   error
	tagCalls    int
	prefixCalls int
	lastTags    map[string]string
	lastPrefix  string
	lastTagOpts tmux.Options
}

func (c *cleanupRecordingTmuxOps) KillSessionsMatchingTags(tags map[string]string, opts tmux.Options) (bool, error) {
	c.tagCalls++
	c.lastTags = tags
	c.lastTagOpts = opts
	return c.tagCleaned, c.tagErr
}

func (c *cleanupRecordingTmuxOps) KillSessionsWithPrefix(prefix string, _ tmux.Options) error {
	c.prefixCalls++
	c.lastPrefix = prefix
	return c.prefixErr
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
		ops := &cleanupRecordingTmuxOps{}
		app.tmuxService = ops

		toast := runCleanupCmd(t, cmd)
		if toast.Level != messages.ToastWarning {
			t.Fatalf("expected warning toast, got %q", toast.Level)
		}
		if ops.tagCalls != 0 || ops.prefixCalls != 0 {
			t.Fatalf("service attached after build must not be called; tags=%d prefix=%d", ops.tagCalls, ops.prefixCalls)
		}
	})

	t.Run("only prefix sessions cleaned reports prefix-only success", func(t *testing.T) {
		ops := &cleanupRecordingTmuxOps{tagCleaned: false}
		app := &App{tmuxService: ops, tmuxOptions: tmux.Options{ServerName: "srv"}}

		toast := runCleanupCmd(t, app.cleanupAllTmuxSessions())

		if toast.Level != messages.ToastSuccess {
			t.Fatalf("expected success toast, got %q", toast.Level)
		}
		want := "Cleaned up " + prefix + "* tmux sessions"
		if toast.Message != want {
			t.Fatalf("expected prefix-only message %q, got %q", want, toast.Message)
		}
		if ops.tagCalls != 1 || ops.prefixCalls != 1 {
			t.Fatalf("expected one tag and one prefix kill, got tags=%d prefix=%d", ops.tagCalls, ops.prefixCalls)
		}
		if ops.lastTags["@amux"] != "1" || len(ops.lastTags) != 1 {
			t.Fatalf("expected only the @amux=1 tag match, got %v", ops.lastTags)
		}
		if ops.lastPrefix != prefix {
			t.Fatalf("expected prefix %q, got %q", prefix, ops.lastPrefix)
		}
		if ops.lastTagOpts.ServerName != "srv" {
			t.Fatalf("expected captured tmuxOptions to flow through, got %+v", ops.lastTagOpts)
		}
	})

	t.Run("tagged and prefix cleaned reports combined success", func(t *testing.T) {
		ops := &cleanupRecordingTmuxOps{tagCleaned: true}
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
		ops := &cleanupRecordingTmuxOps{tagCleaned: false, tagErr: errors.New("boom")}
		app := &App{tmuxService: ops}

		toast := runCleanupCmd(t, app.cleanupAllTmuxSessions())

		if toast.Level != messages.ToastSuccess {
			t.Fatalf("expected success toast despite tag error, got %q level %q", toast.Message, toast.Level)
		}
		if ops.prefixCalls != 1 {
			t.Fatalf("expected prefix kill to still run after tag error, got %d calls", ops.prefixCalls)
		}
		if strings.Contains(toast.Message, "@amux and") {
			t.Fatalf("a tag error must not claim @amux sessions were cleaned: %q", toast.Message)
		}
	})

	t.Run("prefix kill error returns warning toast", func(t *testing.T) {
		ops := &cleanupRecordingTmuxOps{tagCleaned: true, prefixErr: errors.New("prefix exploded")}
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
		ops := &cleanupRecordingTmuxOps{tagCleaned: true, prefixErr: errors.New("x")}
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
		ops := &cleanupRecordingTmuxOps{}
		app := &App{tmuxService: ops, instanceID: "inst-A"}

		app.CleanupTmuxOnExit()

		if ops.tagCalls != 0 || ops.prefixCalls != 0 {
			t.Fatalf("CleanupTmuxOnExit must be a no-op; tags=%d prefix=%d", ops.tagCalls, ops.prefixCalls)
		}
	})
}
